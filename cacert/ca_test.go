package cacert

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateCA(t *testing.T) {
	// Create temporary directory for test files
	tmpDir, err := os.MkdirTemp("", "ca-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	certPath := filepath.Join(tmpDir, "ca.crt")
	keyPath := filepath.Join(tmpDir, "ca.key")

	// Test certificate generation
	err = GenerateCA(certPath, keyPath)
	if err != nil {
		t.Fatalf("GenerateCA failed: %v", err)
	}

	// Verify certificate file exists
	if _, err := os.Stat(certPath); os.IsNotExist(err) {
		t.Errorf("certificate file was not created")
	}

	// Verify key file exists
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		t.Errorf("private key file was not created")
	}
}

func TestGeneratedCertificateProperties(t *testing.T) {
	// Create temporary directory for test files
	tmpDir, err := os.MkdirTemp("", "ca-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	certPath := filepath.Join(tmpDir, "ca.crt")
	keyPath := filepath.Join(tmpDir, "ca.key")

	// Generate certificate
	err = GenerateCA(certPath, keyPath)
	if err != nil {
		t.Fatalf("GenerateCA failed: %v", err)
	}

	// Read and parse certificate
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("failed to read certificate: %v", err)
	}

	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("failed to decode PEM block")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("failed to parse certificate: %v", err)
	}

	// Test: Certificate is a CA
	if !cert.IsCA {
		t.Error("certificate should be marked as CA")
	}

	// Test: Basic constraints are valid
	if !cert.BasicConstraintsValid {
		t.Error("basic constraints should be valid")
	}

	// Test: Organization is "kinder"
	if len(cert.Subject.Organization) == 0 || cert.Subject.Organization[0] != "kinder" {
		t.Errorf("expected Organization 'kinder', got %v", cert.Subject.Organization)
	}

	// Test: CN contains hostname and "kinder Root CA"
	hostname, _ := os.Hostname()
	expectedCN := fmt.Sprintf("kinder Root CA (%s)", hostname)
	if cert.Subject.CommonName != expectedCN {
		t.Errorf("expected CN '%s', got '%s'", expectedCN, cert.Subject.CommonName)
	}

	// Test: Has correct key usage (root CA only needs CertSign and CRLSign)
	expectedKeyUsage := x509.KeyUsageCertSign | x509.KeyUsageCRLSign
	if cert.KeyUsage != expectedKeyUsage {
		t.Errorf("expected key usage %d, got %d", expectedKeyUsage, cert.KeyUsage)
	}

	// Test: Root CA should NOT have extended key usage
	if len(cert.ExtKeyUsage) > 0 {
		t.Errorf("root CA should not have extended key usage, got %v", cert.ExtKeyUsage)
	}
}

func TestCertificateNameConstraints(t *testing.T) {
	// Create temporary directory for test files
	tmpDir, err := os.MkdirTemp("", "ca-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	certPath := filepath.Join(tmpDir, "ca.crt")
	keyPath := filepath.Join(tmpDir, "ca.key")

	// Generate certificate
	err = GenerateCA(certPath, keyPath)
	if err != nil {
		t.Fatalf("GenerateCA failed: %v", err)
	}

	// Read and parse certificate
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("failed to read certificate: %v", err)
	}

	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("failed to decode PEM block")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("failed to parse certificate: %v", err)
	}

	// Test: Name constraints are critical
	if !cert.PermittedDNSDomainsCritical {
		t.Error("permitted DNS domains should be marked as critical")
	}

	// Test: Permitted DNS domains
	expectedDNSDomains := []string{"c0000201.sslip.io", ".c0000201.sslip.io", "localhost", ".localhost", "stepca"}
	if len(cert.PermittedDNSDomains) != len(expectedDNSDomains) {
		t.Errorf("expected %d permitted DNS domains, got %d", len(expectedDNSDomains), len(cert.PermittedDNSDomains))
	}

	// Check that all expected domains are present (order may vary)
	for _, expected := range expectedDNSDomains {
		found := false
		for _, actual := range cert.PermittedDNSDomains {
			if actual == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected permitted DNS domain '%s' not found", expected)
		}
	}

	// Test: Permitted IP ranges
	expectedIPs := []struct {
		ip   string
		cidr int
	}{
		{"127.0.0.1", 32},
		{"::1", 128},
		{"192.0.2.0", 24},
	}

	if len(cert.PermittedIPRanges) != len(expectedIPs) {
		t.Errorf("expected %d permitted IP ranges, got %d", len(expectedIPs), len(cert.PermittedIPRanges))
	}

	for _, expected := range expectedIPs {
		expectedIP := net.ParseIP(expected.ip)
		found := false
		for _, ipRange := range cert.PermittedIPRanges {
			if ipRange.IP.Equal(expectedIP) {
				found = true
				// Verify the CIDR mask
				ones, _ := ipRange.Mask.Size()
				if ones != expected.cidr {
					t.Errorf("expected CIDR /%d for %s, got /%d", expected.cidr, expected.ip, ones)
				}
				break
			}
		}
		if !found {
			t.Errorf("expected permitted IP range %s/%d not found", expected.ip, expected.cidr)
		}
	}
}

func TestGenerateCAInvalidPaths(t *testing.T) {
	// Test with invalid cert path (directory doesn't exist and can't be created)
	err := GenerateCA("/nonexistent/readonly/ca.crt", "/tmp/ca.key")
	if err == nil {
		t.Error("expected error when certificate path is invalid")
	}

	// Test with invalid key path
	tmpDir, err := os.MkdirTemp("", "ca-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	certPath := filepath.Join(tmpDir, "ca.crt")
	err = GenerateCA(certPath, "/nonexistent/readonly/ca.key")
	if err == nil {
		t.Error("expected error when key path is invalid")
	}
}

func TestCertificateValidity(t *testing.T) {
	// Create temporary directory for test files
	tmpDir, err := os.MkdirTemp("", "ca-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	certPath := filepath.Join(tmpDir, "ca.crt")
	keyPath := filepath.Join(tmpDir, "ca.key")

	// Generate certificate
	err = GenerateCA(certPath, keyPath)
	if err != nil {
		t.Fatalf("GenerateCA failed: %v", err)
	}

	// Read and parse certificate
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("failed to read certificate: %v", err)
	}

	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("failed to decode PEM block")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("failed to parse certificate: %v", err)
	}

	// Read and parse private key
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("failed to read private key: %v", err)
	}

	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		t.Fatal("failed to decode PEM block for key")
	}

	if keyBlock.Type != "PRIVATE KEY" {
		t.Errorf("expected PEM type 'PRIVATE KEY', got '%s'", keyBlock.Type)
	}

	// Verify the certificate is valid (self-signed)
	roots := x509.NewCertPool()
	roots.AddCert(cert)

	opts := x509.VerifyOptions{
		Roots: roots,
	}

	if _, err := cert.Verify(opts); err != nil {
		t.Errorf("certificate verification failed: %v", err)
	}
}
