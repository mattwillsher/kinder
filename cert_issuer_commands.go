package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"codeberg.org/hipkoi/kinder/kubernetes"
	"github.com/spf13/cobra"
)

var (
	// Cert issuer flags
	certIssuerName           string
	certIssuerEmail          string
	certIssuerACMEServer     string
	certIssuerIngressClass   string
	certIssuerUseDNS01       bool
	certIssuerDNS01Provider  string
	certIssuerImageName      string
	certIssuerImageTag       string
	certIssuerSaveLocal      bool
	certIssuerIncludeExample bool
	certIssuerExampleDomain  string
)

var certIssuerCmd = &cobra.Command{
	Use:   "cert-issuer",
	Short: "Manage cert-manager ClusterIssuer OCI bundles",
	Long: `Commands for managing cert-manager ClusterIssuer OCI bundles.

These bundles contain a ClusterIssuer configured to use the kinder Step CA
as an ACME server. The CA certificate is embedded in the caBundle field
so cert-manager trusts the Step CA without additional configuration.`,
}

var certIssuerPushCmd = &cobra.Command{
	Use:   "push",
	Short: "Build and push cert-manager issuer manifests to the registry",
	Long: `Build and push an OCI artifact containing cert-manager ClusterIssuer manifests.

The artifact contains:
  - kustomization.yaml: Kustomize bundle referencing the manifests
  - clusterissuer.yaml: ClusterIssuer with embedded CA bundle
  - example-certificate.yaml: (optional) Example Certificate resource

The ClusterIssuer is configured to:
  - Use Step CA's ACME endpoint for certificate issuance
  - Trust the kinder root CA via embedded caBundle
  - Use HTTP-01 solver with Traefik by default

ArgoCD can reference this artifact directly:
  apiVersion: argoproj.io/v1alpha1
  kind: Application
  spec:
    sources:
      - repoURL: zot:5000/cert-manager-issuer
        targetRevision: latest`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()

		// Get data directory and cert path
		dataDir, err := getDataDir()
		if err != nil {
			return fmt.Errorf("failed to get data directory: %w", err)
		}

		caCertPath := certPath
		if caCertPath == "" {
			caCertPath = filepath.Join(dataDir, CACertFilename)
		}

		// Check if CA certificate exists
		if _, err := os.Stat(caCertPath); os.IsNotExist(err) {
			return fmt.Errorf("CA certificate not found at %s. Run 'kinder start' or 'kinder ca generate' first", caCertPath)
		}

		cfg := kubernetes.CertManagerIssuerConfig{
			RootCACertPath:     caCertPath,
			RegistryURL:        "localhost:5000",
			ImageName:          certIssuerImageName,
			ImageTag:           certIssuerImageTag,
			IssuerName:         certIssuerName,
			Email:              certIssuerEmail,
			ACMEServerURL:      certIssuerACMEServer,
			Domain:             traefikDomain,
			Port:               traefikPort,
			IngressClass:       certIssuerIngressClass,
			UseDNS01:           certIssuerUseDNS01,
			DNS01Provider:      certIssuerDNS01Provider,
			IncludeExampleCert: certIssuerIncludeExample,
			ExampleCertDomain:  certIssuerExampleDomain,
		}

		Header("Building cert-manager issuer bundle...")
		BlankLine()

		// Build and push the bundle
		ProgressStart("📦", "Building OCI artifact")
		if err := kubernetes.BuildAndPushCertManagerIssuer(ctx, cfg); err != nil {
			ProgressDone(false, err.Error())
			return fmt.Errorf("failed to push cert-manager issuer bundle: %w", err)
		}
		ProgressDone(true, fmt.Sprintf("Pushed to localhost:5000/%s:%s", cfg.ImageName, cfg.ImageTag))

		// Optionally save manifests locally for inspection
		if certIssuerSaveLocal {
			ProgressStart("💾", "Saving manifests locally")

			// Read the CA certificate
			kinderCA, err := os.ReadFile(caCertPath)
			if err != nil {
				ProgressDone(false, err.Error())
				return fmt.Errorf("failed to read CA certificate: %w", err)
			}

			// Generate and save manifests
			manifests, err := kubernetes.GenerateCertManagerIssuerManifests(cfg, kinderCA)
			if err != nil {
				ProgressDone(false, err.Error())
				return fmt.Errorf("failed to generate manifests: %w", err)
			}

			if err := kubernetes.SaveCertManagerIssuerManifests(dataDir, manifests); err != nil {
				ProgressDone(false, err.Error())
				return fmt.Errorf("failed to save manifests: %w", err)
			}
			manifestsDir := filepath.Join(dataDir, "cert-manager-issuer")
			ProgressDone(true, fmt.Sprintf("Saved to %s", manifestsDir))
		}

		BlankLine()
		Success("Cert-manager issuer pushed successfully!")
		BlankLine()

		Header("Usage with ArgoCD:")
		Output("  Image: localhost:5000/%s:%s\n", cfg.ImageName, cfg.ImageTag)
		Output("  From Kind: zot:5000/%s:%s\n", cfg.ImageName, cfg.ImageTag)
		BlankLine()

		Header("Usage in cluster:")
		Output("  # Request a certificate\n")
		Output("  apiVersion: cert-manager.io/v1\n")
		Output("  kind: Certificate\n")
		Output("  spec:\n")
		Output("    issuerRef:\n")
		Output("      name: %s\n", certIssuerName)
		Output("      kind: ClusterIssuer\n")

		return nil
	},
}

var certIssuerShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show the generated cert-manager issuer manifests",
	Long:  `Display the Kubernetes manifests that would be included in the cert-manager issuer bundle.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Get data directory and cert path
		dataDir, err := getDataDir()
		if err != nil {
			return fmt.Errorf("failed to get data directory: %w", err)
		}

		caCertPath := certPath
		if caCertPath == "" {
			caCertPath = filepath.Join(dataDir, CACertFilename)
		}

		// Check if CA certificate exists
		if _, err := os.Stat(caCertPath); os.IsNotExist(err) {
			return fmt.Errorf("CA certificate not found at %s. Run 'kinder start' or 'kinder ca generate' first", caCertPath)
		}

		// Read the CA certificate
		kinderCA, err := os.ReadFile(caCertPath)
		if err != nil {
			return fmt.Errorf("failed to read CA certificate: %w", err)
		}

		cfg := kubernetes.CertManagerIssuerConfig{
			RootCACertPath:     caCertPath,
			IssuerName:         certIssuerName,
			Email:              certIssuerEmail,
			ACMEServerURL:      certIssuerACMEServer,
			Domain:             traefikDomain,
			Port:               traefikPort,
			IngressClass:       certIssuerIngressClass,
			UseDNS01:           certIssuerUseDNS01,
			DNS01Provider:      certIssuerDNS01Provider,
			IncludeExampleCert: certIssuerIncludeExample,
			ExampleCertDomain:  certIssuerExampleDomain,
		}

		manifests, err := kubernetes.GenerateCertManagerIssuerManifests(cfg, kinderCA)
		if err != nil {
			return fmt.Errorf("failed to generate manifests: %w", err)
		}

		// Print manifests
		Output("# kustomization.yaml\n")
		Output("---\n%s\n", string(manifests.Kustomization))
		Output("# clusterissuer.yaml\n")
		Output("---\n%s\n", string(manifests.ClusterIssuer))

		if len(manifests.ExampleCert) > 0 {
			Output("# example-certificate.yaml\n")
			Output("---\n%s", string(manifests.ExampleCert))
		}

		return nil
	},
}

func init() {
	// Setup flags for cert-issuer push command
	certIssuerPushCmd.Flags().StringVar(&certIssuerName, "issuer-name", kubernetes.CertManagerIssuerName, "Name of the ClusterIssuer")
	certIssuerPushCmd.Flags().StringVar(&certIssuerEmail, "email", kubernetes.CertManagerIssuerEmail, "ACME account email address")
	certIssuerPushCmd.Flags().StringVar(&certIssuerACMEServer, "acme-server", "", "ACME server URL (default: derived from domain/port)")
	certIssuerPushCmd.Flags().StringVar(&certIssuerIngressClass, "ingress-class", kubernetes.CertManagerIssuerIngressClass, "Ingress class for HTTP-01 solver")
	certIssuerPushCmd.Flags().BoolVar(&certIssuerUseDNS01, "dns01", false, "Use DNS-01 solver instead of HTTP-01")
	certIssuerPushCmd.Flags().StringVar(&certIssuerDNS01Provider, "dns01-provider", "", "DNS provider for DNS-01 (e.g., cloudflare, route53)")
	certIssuerPushCmd.Flags().StringVar(&certIssuerImageName, "image-name", kubernetes.CertManagerIssuerImageName, "Image name for the bundle")
	certIssuerPushCmd.Flags().StringVar(&certIssuerImageTag, "image-tag", kubernetes.CertManagerIssuerImageTag, "Image tag for the bundle")
	certIssuerPushCmd.Flags().BoolVar(&certIssuerSaveLocal, "save-local", false, "Also save manifests to local data directory")
	certIssuerPushCmd.Flags().BoolVar(&certIssuerIncludeExample, "include-example", false, "Include an example Certificate resource")
	certIssuerPushCmd.Flags().StringVar(&certIssuerExampleDomain, "example-domain", "", "Domain for the example certificate (default: example.<domain>)")

	// Setup flags for cert-issuer show command
	certIssuerShowCmd.Flags().StringVar(&certIssuerName, "issuer-name", kubernetes.CertManagerIssuerName, "Name of the ClusterIssuer")
	certIssuerShowCmd.Flags().StringVar(&certIssuerEmail, "email", kubernetes.CertManagerIssuerEmail, "ACME account email address")
	certIssuerShowCmd.Flags().StringVar(&certIssuerACMEServer, "acme-server", "", "ACME server URL (default: derived from domain/port)")
	certIssuerShowCmd.Flags().StringVar(&certIssuerIngressClass, "ingress-class", kubernetes.CertManagerIssuerIngressClass, "Ingress class for HTTP-01 solver")
	certIssuerShowCmd.Flags().BoolVar(&certIssuerUseDNS01, "dns01", false, "Use DNS-01 solver instead of HTTP-01")
	certIssuerShowCmd.Flags().StringVar(&certIssuerDNS01Provider, "dns01-provider", "", "DNS provider for DNS-01")
	certIssuerShowCmd.Flags().BoolVar(&certIssuerIncludeExample, "include-example", false, "Include an example Certificate resource")
	certIssuerShowCmd.Flags().StringVar(&certIssuerExampleDomain, "example-domain", "", "Domain for the example certificate")

	// Add subcommands
	certIssuerCmd.AddCommand(certIssuerPushCmd)
	certIssuerCmd.AddCommand(certIssuerShowCmd)
}
