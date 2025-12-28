package main

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"codeberg.org/hipkoi/kinder/cacert"
	"codeberg.org/hipkoi/kinder/docker"
	"github.com/spf13/cobra"
)

var caCmd = &cobra.Command{
	Use:   "ca",
	Short: "Manage CA certificates",
	Long:  `Commands for generating and inspecting CA certificates.`,
}

var generateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate CA certificate and private key",
	Long: `Generate a new CA certificate and private key using ECDSA.
The certificate uses the machine's hostname as the CN and "kinder" as the Organization.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Set default domain if not provided
		if traefikDomain == "" {
			traefikDomain = docker.DefaultTraefikDomain
		}

		// Get default paths if not provided
		if certPath == "" {
			dataDir, err := getDataDir()
			if err != nil {
				return fmt.Errorf("failed to get data directory: %w", err)
			}
			certPath = filepath.Join(dataDir, CACertFilename)
		}

		if keyPath == "" {
			dataDir, err := getDataDir()
			if err != nil {
				return fmt.Errorf("failed to get data directory: %w", err)
			}
			keyPath = filepath.Join(dataDir, CAKeyFilename)
		}

		// Ensure the directory exists
		certDir := filepath.Dir(certPath)
		if err := os.MkdirAll(certDir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", certDir, err)
		}

		keyDir := filepath.Dir(keyPath)
		if err := os.MkdirAll(keyDir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", keyDir, err)
		}

		// Generate the CA certificate with domain constraints
		if err := cacert.GenerateCAWithDomain(certPath, keyPath, traefikDomain); err != nil {
			return fmt.Errorf("failed to generate CA certificate: %w", err)
		}

		fmt.Printf("CA certificate generated successfully:\n")
		fmt.Printf("  Certificate: %s\n", certPath)
		fmt.Printf("  Private Key: %s\n", keyPath)

		return nil
	},
}

var printCmd = &cobra.Command{
	Use:   "print",
	Short: "Print CA certificate information",
	Long:  `Display detailed information about the CA certificate including subject, validity, key type, and name constraints.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Get default paths if not provided
		if certPath == "" {
			dataDir, err := getDataDir()
			if err != nil {
				return fmt.Errorf("failed to get data directory: %w", err)
			}
			certPath = filepath.Join(dataDir, CACertFilename)
		}

		if keyPath == "" {
			dataDir, err := getDataDir()
			if err != nil {
				return fmt.Errorf("failed to get data directory: %w", err)
			}
			keyPath = filepath.Join(dataDir, CAKeyFilename)
		}

		// Read and parse certificate
		certPEM, err := os.ReadFile(certPath)
		if err != nil {
			return fmt.Errorf("failed to read certificate: %w", err)
		}

		block, _ := pem.Decode(certPEM)
		if block == nil {
			return fmt.Errorf("failed to decode PEM block from certificate")
		}

		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return fmt.Errorf("failed to parse certificate: %w", err)
		}

		// Read and parse private key
		keyPEM, err := os.ReadFile(keyPath)
		if err != nil {
			return fmt.Errorf("failed to read private key: %w", err)
		}

		keyBlock, _ := pem.Decode(keyPEM)
		if keyBlock == nil {
			return fmt.Errorf("failed to decode PEM block from private key")
		}

		privateKey, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
		if err != nil {
			return fmt.Errorf("failed to parse private key: %w", err)
		}

		// Determine key type and details
		var keyType string
		var keyDetails string
		switch key := privateKey.(type) {
		case *ecdsa.PrivateKey:
			keyType = "ECDSA"
			keyDetails = fmt.Sprintf("Curve: %s, Key Size: %d bits", key.Curve.Params().Name, key.Curve.Params().BitSize)
		default:
			keyType = fmt.Sprintf("%T", privateKey)
			keyDetails = "Unknown key details"
		}

		// Print certificate information
		fmt.Printf("CA Certificate Information\n")
		fmt.Printf("==========================\n\n")
		fmt.Printf("Certificate Path: %s\n", certPath)
		fmt.Printf("Private Key Path: %s\n\n", keyPath)

		fmt.Printf("Subject:\n")
		fmt.Printf("  Common Name (CN): %s\n", cert.Subject.CommonName)
		if len(cert.Subject.Organization) > 0 {
			fmt.Printf("  Organization (O): %s\n", strings.Join(cert.Subject.Organization, ", "))
		}
		fmt.Printf("\n")

		fmt.Printf("Validity:\n")
		fmt.Printf("  Not Before: %s\n", cert.NotBefore.Format("2006-01-02 15:04:05 MST"))
		fmt.Printf("  Not After:  %s\n", cert.NotAfter.Format("2006-01-02 15:04:05 MST"))
		fmt.Printf("\n")

		fmt.Printf("Serial Number: %s\n\n", cert.SerialNumber.Text(16))

		fmt.Printf("Key Information:\n")
		fmt.Printf("  Type: %s\n", keyType)
		fmt.Printf("  Format: PKCS#8\n")
		fmt.Printf("  %s\n\n", keyDetails)

		fmt.Printf("Certificate Properties:\n")
		fmt.Printf("  Is CA: %t\n", cert.IsCA)
		fmt.Printf("  Basic Constraints Valid: %t\n\n", cert.BasicConstraintsValid)

		fmt.Printf("Key Usage:\n")
		if cert.KeyUsage&x509.KeyUsageDigitalSignature != 0 {
			fmt.Printf("  - Digital Signature\n")
		}
		if cert.KeyUsage&x509.KeyUsageKeyEncipherment != 0 {
			fmt.Printf("  - Key Encipherment\n")
		}
		if cert.KeyUsage&x509.KeyUsageCertSign != 0 {
			fmt.Printf("  - Certificate Sign\n")
		}
		fmt.Printf("\n")

		if len(cert.ExtKeyUsage) > 0 {
			fmt.Printf("Extended Key Usage:\n")
			for _, eku := range cert.ExtKeyUsage {
				switch eku {
				case x509.ExtKeyUsageServerAuth:
					fmt.Printf("  - TLS Web Server Authentication\n")
				case x509.ExtKeyUsageClientAuth:
					fmt.Printf("  - TLS Web Client Authentication\n")
				default:
					fmt.Printf("  - %v\n", eku)
				}
			}
			fmt.Printf("\n")
		}

		// Print name constraints
		if len(cert.PermittedDNSDomains) > 0 {
			fmt.Printf("Name Constraints (Critical: %t):\n", cert.PermittedDNSDomainsCritical)
			fmt.Printf("  Permitted DNS Domains:\n")
			for _, domain := range cert.PermittedDNSDomains {
				fmt.Printf("    - %s\n", domain)
			}
			fmt.Printf("\n")
		}

		if len(cert.PermittedIPRanges) > 0 {
			fmt.Printf("  Permitted IP Ranges:\n")
			for _, ipRange := range cert.PermittedIPRanges {
				ones, bits := ipRange.Mask.Size()
				if bits == 32 {
					fmt.Printf("    - %s/%d (IPv4)\n", ipRange.IP, ones)
				} else {
					fmt.Printf("    - %s/%d (IPv6)\n", ipRange.IP, ones)
				}
			}
			fmt.Printf("\n")
		}

		return nil
	},
}
