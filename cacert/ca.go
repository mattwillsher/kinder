package cacert

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"time"
)

// GenerateCA generates a new CA certificate and private key using ECDSA.
// The certificate uses the machine's hostname as the CN and "kinder" as the Organization.
func GenerateCA(certPath, keyPath string) error {
	return GenerateCAWithDomain(certPath, keyPath, "c0000201.sslip.io")
}

// GenerateCAWithDomain generates a CA certificate with name constraints for the specified domain
func GenerateCAWithDomain(certPath, keyPath, domain string) error {
	// Get the hostname for the CN
	hostname, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("failed to get hostname: %w", err)
	}

	// Generate ECDSA private key using P-256 curve
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("failed to generate ECDSA private key: %w", err)
	}

	// Create certificate template
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return fmt.Errorf("failed to generate serial number: %w", err)
	}

	notBefore := time.Now()
	notAfter := notBefore.Add(365 * 24 * time.Hour) // 1 year

	// Build permitted DNS domains: domain, .domain, localhost, .localhost, stepca
	permittedDomains := []string{
		domain,
		"." + domain,
		"localhost",
		".localhost",
		"stepca",
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   fmt.Sprintf("kinder Root CA (%s)", hostname),
			Organization: []string{"kinder"},
		},
		NotBefore: notBefore,
		NotAfter:  notAfter,
		// Root CA only needs CertSign and CRLSign - no key encipherment or digital signature
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		// Name constraints to limit the CA to specific domains
		PermittedDNSDomainsCritical: true,
		PermittedDNSDomains:         permittedDomains,
		PermittedIPRanges: []*net.IPNet{
			{IP: net.ParseIP("127.0.0.1"), Mask: net.CIDRMask(32, 32)}, // 127.0.0.1/32
			{IP: net.ParseIP("::1"), Mask: net.CIDRMask(128, 128)},     // ::1/128
			{IP: net.ParseIP("192.0.2.0"), Mask: net.CIDRMask(24, 32)}, // 192.0.2.0/24 (TEST-NET-1)
		},
	}

	// Create self-signed certificate
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return fmt.Errorf("failed to create certificate: %w", err)
	}

	// Write certificate to file
	certOut, err := os.Create(certPath)
	if err != nil {
		return fmt.Errorf("failed to create certificate file: %w", err)
	}
	defer certOut.Close()

	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		return fmt.Errorf("failed to write certificate to file: %w", err)
	}

	// Marshal ECDSA private key to PKCS8 format
	privKeyBytes, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return fmt.Errorf("failed to marshal private key: %w", err)
	}

	// Write private key to file with restrictive permissions (0600)
	// Using WriteFile with explicit permissions to avoid race condition
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privKeyBytes})
	if err := os.WriteFile(keyPath, keyPEM, 0600); err != nil {
		return fmt.Errorf("failed to write private key to file: %w", err)
	}

	return nil
}

// GenerateIntermediate generates an intermediate CA certificate signed by the root CA
func GenerateIntermediate(rootCertPath, rootKeyPath, intermediateCertPath, intermediateKeyPath string) error {
	// Read root CA certificate
	rootCertPEM, err := os.ReadFile(rootCertPath)
	if err != nil {
		return fmt.Errorf("failed to read root certificate: %w", err)
	}

	rootCertBlock, _ := pem.Decode(rootCertPEM)
	if rootCertBlock == nil {
		return fmt.Errorf("failed to decode root certificate PEM")
	}

	rootCert, err := x509.ParseCertificate(rootCertBlock.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse root certificate: %w", err)
	}

	// Read root CA private key
	rootKeyPEM, err := os.ReadFile(rootKeyPath)
	if err != nil {
		return fmt.Errorf("failed to read root private key: %w", err)
	}

	rootKeyBlock, _ := pem.Decode(rootKeyPEM)
	if rootKeyBlock == nil {
		return fmt.Errorf("failed to decode root private key PEM")
	}

	rootKey, err := x509.ParsePKCS8PrivateKey(rootKeyBlock.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse root private key: %w", err)
	}

	rootPrivateKey, ok := rootKey.(*ecdsa.PrivateKey)
	if !ok {
		return fmt.Errorf("root private key is not ECDSA")
	}

	// Generate new private key for intermediate
	intermediateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("failed to generate intermediate private key: %w", err)
	}

	// Create intermediate certificate template
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return fmt.Errorf("failed to generate serial number: %w", err)
	}

	notBefore := time.Now()
	notAfter := notBefore.Add(365 * 24 * time.Hour) // 1 year

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   "kinder Intermediate CA",
			Organization: []string{"kinder"},
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            0,
		MaxPathLenZero:        true,
	}

	// Sign intermediate certificate with root CA
	certDER, err := x509.CreateCertificate(rand.Reader, &template, rootCert, &intermediateKey.PublicKey, rootPrivateKey)
	if err != nil {
		return fmt.Errorf("failed to create intermediate certificate: %w", err)
	}

	// Write intermediate certificate to file
	certOut, err := os.Create(intermediateCertPath)
	if err != nil {
		return fmt.Errorf("failed to create intermediate certificate file: %w", err)
	}
	defer certOut.Close()

	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		return fmt.Errorf("failed to write intermediate certificate to file: %w", err)
	}

	// Marshal ECDSA private key to PKCS8 format
	privKeyBytes, err := x509.MarshalPKCS8PrivateKey(intermediateKey)
	if err != nil {
		return fmt.Errorf("failed to marshal intermediate private key: %w", err)
	}

	// Write intermediate private key to file with restrictive permissions (0600)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privKeyBytes})
	if err := os.WriteFile(intermediateKeyPath, keyPEM, 0600); err != nil {
		return fmt.Errorf("failed to write intermediate private key to file: %w", err)
	}

	return nil
}
