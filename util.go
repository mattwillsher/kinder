package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"codeberg.org/hipkoi/kinder/config"
	"codeberg.org/hipkoi/kinder/kubernetes"
)

// joinErrors joins a slice of error strings with ", "
func joinErrors(errs []string) string {
	return strings.Join(errs, ", ")
}

// buildRegistryMirrorMap creates a registry mirror map from config.
// Each configured registry is mapped to the local Zot registry.
func buildRegistryMirrorMap() map[string]string {
	mirrors := config.GetStringSlice(config.KeyRegistryMirrors)
	if len(mirrors) == 0 {
		mirrors = config.DefaultRegistryMirrors
	}

	registryMirrors := make(map[string]string)
	for _, registry := range mirrors {
		registryMirrors[registry] = "http://zot:5000"
	}
	return registryMirrors
}

const (
	// CACertFilename is the filename for the CA certificate
	CACertFilename = "ca.crt"
	// CAKeyFilename is the filename for the CA private key
	CAKeyFilename = "ca.key"
)

// getDataDir returns the data directory, checking config first then XDG standard.
// This respects the dataDir config option and KINDER_DATADIR environment variable.
func getDataDir() (string, error) {
	return config.GetDataDir()
}

// getDataDirForApp returns the data directory for a specific app name.
// Follows XDG Base Directory specification. Does not check config.
// Used for deriving paths when app name differs from default.
func getDataDirForApp(appName string) (string, error) {
	var baseDir string

	// Check XDG_DATA_HOME first
	if xdgData := os.Getenv("XDG_DATA_HOME"); xdgData != "" {
		baseDir = xdgData
	} else {
		// Fallback to ~/.local/share on Unix-like systems
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get user home directory: %w", err)
		}
		baseDir = filepath.Join(homeDir, ".local", "share")
	}

	return filepath.Join(baseDir, appName), nil
}

// cleanContainerData removes all generated configuration files except CA cert and key
func cleanContainerData(dataDir string) error {
	// Check if directory exists
	if _, err := os.Stat(dataDir); os.IsNotExist(err) {
		return nil // Nothing to clean
	}

	// Read directory contents
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return fmt.Errorf("failed to read data directory: %w", err)
	}

	// Remove everything except CA certificate and key
	for _, entry := range entries {
		name := entry.Name()
		// Skip CA certificate and key
		if name == CACertFilename || name == CAKeyFilename {
			continue
		}

		path := filepath.Join(dataDir, name)
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("failed to remove %s: %w", path, err)
		}
	}

	return nil
}

// pushTrustBundle creates and pushes the trust bundle OCI image to the local registry
func pushTrustBundle(ctx context.Context, caCertPath string) error {
	cfg := kubernetes.TrustBundleConfig{
		RootCACertPath: caCertPath,
		RegistryURL:    "localhost:5000",
		ImageName:      "trust-bundle",
		ImageTag:       "latest",
	}

	if err := kubernetes.BuildAndPushTrustBundle(ctx, cfg); err != nil {
		return err
	}

	// Also push trust-manager manifests bundle
	tmCfg := kubernetes.TrustManagerBundleConfig{
		RootCACertPath:    caCertPath,
		RegistryURL:       "localhost:5000",
		ImageName:         kubernetes.TrustManagerBundleImageName,
		ImageTag:          kubernetes.TrustManagerBundleImageTag,
		IncludeMozillaCAs: true,
	}

	return kubernetes.BuildAndPushTrustManagerBundle(ctx, tmCfg)
}

// pushCertManagerIssuer creates and pushes the cert-manager issuer OCI image
func pushCertManagerIssuer(ctx context.Context, caCertPath string) error {
	domain := config.GetString(config.KeyDomain)
	port := config.GetString(config.KeyTraefikPort)

	cfg := kubernetes.CertManagerIssuerConfig{
		RootCACertPath: caCertPath,
		RegistryURL:    "localhost:5000",
		Domain:         domain,
		Port:           port,
	}

	return kubernetes.BuildAndPushCertManagerIssuer(ctx, cfg)
}
