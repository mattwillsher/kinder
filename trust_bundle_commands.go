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
	// Trust bundle flags
	trustBundleIncludeMozilla bool
	trustBundleTargetNS       string
	trustBundleImageName      string
	trustBundleImageTag       string
	trustBundleSaveLocal      bool
)

var trustBundleCmd = &cobra.Command{
	Use:   "trust-bundle",
	Short: "Manage trust-manager OCI bundles",
	Long: `Commands for managing trust-manager OCI bundles.

These bundles contain Kubernetes manifests (Kustomization, ConfigMap, Bundle)
that can be deployed via ArgoCD OCI support to distribute the kinder CA
certificate across your cluster using trust-manager.`,
}

var trustBundlePushCmd = &cobra.Command{
	Use:   "push",
	Short: "Build and push trust-manager manifests to the registry",
	Long: `Build and push an OCI artifact containing trust-manager Kubernetes manifests.

The artifact contains:
  - kustomization.yaml: Kustomize bundle referencing the manifests
  - configmap.yaml: ConfigMap with the CA certificate (source)
  - bundle.yaml: trust-manager Bundle CRD for distribution

ArgoCD can reference this artifact directly:
  apiVersion: argoproj.io/v1alpha1
  kind: Application
  spec:
    sources:
      - repoURL: zot:5000/trust-manager-bundle
        targetRevision: latest
        ref: trustBundle
    source:
      repoURL: $trustBundle
      path: .`,
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

		cfg := kubernetes.TrustManagerBundleConfig{
			RootCACertPath:    caCertPath,
			RegistryURL:       "localhost:5000",
			ImageName:         trustBundleImageName,
			ImageTag:          trustBundleImageTag,
			IncludeMozillaCAs: trustBundleIncludeMozilla,
			TargetNamespace:   trustBundleTargetNS,
		}

		Header("Building trust-manager bundle...")
		BlankLine()

		// Build and push the bundle
		ProgressStart("📦", "Building OCI artifact")
		if err := kubernetes.BuildAndPushTrustManagerBundle(ctx, cfg); err != nil {
			ProgressDone(false, err.Error())
			return fmt.Errorf("failed to push trust-manager bundle: %w", err)
		}
		ProgressDone(true, fmt.Sprintf("Pushed to localhost:5000/%s:%s", cfg.ImageName, cfg.ImageTag))

		// Optionally save manifests locally for inspection
		if trustBundleSaveLocal {
			ProgressStart("💾", "Saving manifests locally")

			// Read the CA certificate
			kinderCA, err := os.ReadFile(caCertPath)
			if err != nil {
				ProgressDone(false, err.Error())
				return fmt.Errorf("failed to read CA certificate: %w", err)
			}

			// Download Mozilla CAs if requested
			var mozillaCA []byte
			if trustBundleIncludeMozilla {
				mozillaCA, err = downloadMozillaCACerts(ctx)
				if err != nil {
					ProgressDone(false, "Mozilla CA download failed")
					Verbose("Warning: %v\n", err)
				}
			}

			// Generate and save manifests
			manifests, err := generateTrustManagerManifests(cfg, kinderCA, mozillaCA)
			if err != nil {
				ProgressDone(false, err.Error())
				return fmt.Errorf("failed to generate manifests: %w", err)
			}

			if err := kubernetes.SaveTrustManagerManifests(dataDir, manifests); err != nil {
				ProgressDone(false, err.Error())
				return fmt.Errorf("failed to save manifests: %w", err)
			}
			manifestsDir := filepath.Join(dataDir, "trust-manager-manifests")
			ProgressDone(true, fmt.Sprintf("Saved to %s", manifestsDir))
		}

		BlankLine()
		Success("Trust bundle pushed successfully!")
		BlankLine()

		Header("Usage with ArgoCD:")
		Output("  Image: localhost:5000/%s:%s\n", cfg.ImageName, cfg.ImageTag)
		Output("  From Kind: zot:5000/%s:%s\n", cfg.ImageName, cfg.ImageTag)

		return nil
	},
}

var trustBundleShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show the generated trust-manager manifests",
	Long:  `Display the Kubernetes manifests that would be included in the trust-manager bundle.`,
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

		// Read the CA certificate
		kinderCA, err := os.ReadFile(caCertPath)
		if err != nil {
			return fmt.Errorf("failed to read CA certificate: %w", err)
		}

		// Download Mozilla CAs if requested
		var mozillaCA []byte
		if trustBundleIncludeMozilla {
			mozillaCA, err = downloadMozillaCACerts(ctx)
			if err != nil {
				Verbose("Warning: Failed to download Mozilla CAs: %v\n", err)
			}
		}

		cfg := kubernetes.TrustManagerBundleConfig{
			RootCACertPath:    caCertPath,
			IncludeMozillaCAs: trustBundleIncludeMozilla,
			TargetNamespace:   trustBundleTargetNS,
		}

		manifests, err := generateTrustManagerManifests(cfg, kinderCA, mozillaCA)
		if err != nil {
			return fmt.Errorf("failed to generate manifests: %w", err)
		}

		// Print manifests
		Output("# kustomization.yaml\n")
		Output("---\n%s\n", string(manifests.Kustomization))
		Output("# bundle.yaml\n")
		Output("---\n%s\n", string(manifests.Bundle))
		Output("# configmap.yaml (certificate content truncated)\n")
		Output("---\n")
		// Show first 20 lines of configmap to avoid flooding terminal
		lines := splitLines(string(manifests.ConfigMap))
		for i, line := range lines {
			if i >= 20 {
				Output("... (%d more lines)\n", len(lines)-20)
				break
			}
			Output("%s\n", line)
		}

		return nil
	},
}

// Helper functions exposed for the commands

func downloadMozillaCACerts(ctx context.Context) ([]byte, error) {
	// This delegates to the unexported function in the kubernetes package
	// We need to make a simple HTTP call here
	return kubernetes.DownloadMozillaCACerts(ctx)
}

func generateTrustManagerManifests(cfg kubernetes.TrustManagerBundleConfig, kinderCA, mozillaCA []byte) (*kubernetes.TrustManagerManifests, error) {
	return kubernetes.GenerateTrustManagerManifests(cfg, kinderCA, mozillaCA)
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func init() {
	// Setup flags for trust-bundle push command
	trustBundlePushCmd.Flags().BoolVar(&trustBundleIncludeMozilla, "include-mozilla", true, "Include Mozilla CA certificates in the bundle")
	trustBundlePushCmd.Flags().StringVar(&trustBundleTargetNS, "target-namespace", "", "Restrict bundle to a specific namespace (empty = all namespaces)")
	trustBundlePushCmd.Flags().StringVar(&trustBundleImageName, "image-name", kubernetes.TrustManagerBundleImageName, "Image name for the bundle")
	trustBundlePushCmd.Flags().StringVar(&trustBundleImageTag, "image-tag", kubernetes.TrustManagerBundleImageTag, "Image tag for the bundle")
	trustBundlePushCmd.Flags().BoolVar(&trustBundleSaveLocal, "save-local", false, "Also save manifests to local data directory")

	// Setup flags for trust-bundle show command
	trustBundleShowCmd.Flags().BoolVar(&trustBundleIncludeMozilla, "include-mozilla", true, "Include Mozilla CA certificates in the bundle")
	trustBundleShowCmd.Flags().StringVar(&trustBundleTargetNS, "target-namespace", "", "Restrict bundle to a specific namespace")

	// Add subcommands
	trustBundleCmd.AddCommand(trustBundlePushCmd)
	trustBundleCmd.AddCommand(trustBundleShowCmd)
}
