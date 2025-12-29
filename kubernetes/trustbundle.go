package kubernetes

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

const (
	// TrustBundleImageName is the name for the trust bundle image in the local registry
	TrustBundleImageName = "localhost:5000/trust-bundle"
	// TrustBundleImageTag is the tag for the trust bundle image
	TrustBundleImageTag = "latest"
	// MozillaCACertURL is the URL to download Mozilla's CA certificate bundle
	MozillaCACertURL = "https://curl.se/ca/cacert.pem"
	// BundleFilePath is the path inside the OCI image where the bundle is stored
	BundleFilePath = "trust-bundle.pem"
)

// TrustBundleConfig holds configuration for building the trust bundle
type TrustBundleConfig struct {
	// RootCACertPath is the path to the kinder root CA certificate
	RootCACertPath string
	// RegistryURL is the registry URL to push to (default: localhost:5000)
	RegistryURL string
	// ImageName is the image name (default: trust-bundle)
	ImageName string
	// ImageTag is the image tag (default: latest)
	ImageTag string
}

// BuildAndPushTrustBundle creates an OCI image containing the combined trust bundle
// and pushes it to the local registry
func BuildAndPushTrustBundle(ctx context.Context, cfg TrustBundleConfig) error {
	// Set defaults
	if cfg.RegistryURL == "" {
		cfg.RegistryURL = "localhost:5000"
	}
	if cfg.ImageName == "" {
		cfg.ImageName = "trust-bundle"
	}
	if cfg.ImageTag == "" {
		cfg.ImageTag = "latest"
	}

	// Read the kinder root CA
	kinderCA, err := os.ReadFile(cfg.RootCACertPath)
	if err != nil {
		return fmt.Errorf("failed to read kinder CA certificate: %w", err)
	}

	// Download Mozilla CA bundle
	mozillaCA, err := DownloadMozillaCACerts(ctx)
	if err != nil {
		return fmt.Errorf("failed to download Mozilla CA certificates: %w", err)
	}

	// Combine the certificates: kinder CA first, then Mozilla CAs
	combinedBundle := combineCABundles(kinderCA, mozillaCA)

	// Create OCI image with the bundle
	img, err := createTrustBundleImage(combinedBundle)
	if err != nil {
		return fmt.Errorf("failed to create trust bundle image: %w", err)
	}

	// Push to registry
	imageRef := fmt.Sprintf("%s/%s:%s", cfg.RegistryURL, cfg.ImageName, cfg.ImageTag)
	if err := pushImage(ctx, img, imageRef); err != nil {
		return fmt.Errorf("failed to push trust bundle image: %w", err)
	}

	return nil
}

// DownloadMozillaCACerts downloads the Mozilla CA certificate bundle from curl.se
func DownloadMozillaCACerts(ctx context.Context) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", MozillaCACertURL, nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to download CA bundle: HTTP %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

// combineCABundles combines multiple PEM certificate bundles into one
func combineCABundles(bundles ...[]byte) []byte {
	var combined bytes.Buffer

	for i, bundle := range bundles {
		if len(bundle) == 0 {
			continue
		}
		// Add a header comment for the first bundle (kinder CA)
		if i == 0 {
			combined.WriteString("# Kinder Root CA Certificate\n")
		} else {
			combined.WriteString("\n# Mozilla CA Certificate Bundle\n")
		}
		combined.Write(bundle)
		// Ensure there's a newline at the end
		if bundle[len(bundle)-1] != '\n' {
			combined.WriteByte('\n')
		}
	}

	return combined.Bytes()
}

// createTrustBundleImage creates an OCI image containing the trust bundle
func createTrustBundleImage(bundle []byte) (v1.Image, error) {
	// Create a tar archive with the bundle file
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	// Add the bundle file to the tar archive
	header := &tar.Header{
		Name:    BundleFilePath,
		Mode:    0644,
		Size:    int64(len(bundle)),
		ModTime: time.Now(),
	}

	if err := tw.WriteHeader(header); err != nil {
		return nil, fmt.Errorf("failed to write tar header: %w", err)
	}

	if _, err := tw.Write(bundle); err != nil {
		return nil, fmt.Errorf("failed to write bundle to tar: %w", err)
	}

	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("failed to close tar writer: %w", err)
	}

	// Create a layer from the tar archive
	layer, err := tarball.LayerFromReader(&buf)
	if err != nil {
		return nil, fmt.Errorf("failed to create layer: %w", err)
	}

	// Start with an empty image and add our layer
	img := empty.Image
	img, err = mutate.AppendLayers(img, layer)
	if err != nil {
		return nil, fmt.Errorf("failed to append layer: %w", err)
	}

	// Set image config with annotations
	cfg, err := img.ConfigFile()
	if err != nil {
		return nil, fmt.Errorf("failed to get config file: %w", err)
	}

	cfg.Author = "kinder"
	cfg.Created = v1.Time{Time: time.Now()}
	cfg.Config.Labels = map[string]string{
		"org.opencontainers.image.title":       "Trust Bundle",
		"org.opencontainers.image.description": "Combined CA certificate bundle with kinder root CA and Mozilla CAs",
		"org.opencontainers.image.source":      "https://codeberg.org/hipkoi/kinder",
		"trust-manager.io/bundle":              "true",
	}

	img, err = mutate.ConfigFile(img, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to set config: %w", err)
	}

	// Set the media type to OCI
	img = mutate.MediaType(img, types.OCIManifestSchema1)

	return img, nil
}

// pushImage pushes an image to a registry
func pushImage(ctx context.Context, img v1.Image, imageRef string) error {
	ref, err := name.ParseReference(imageRef, name.Insecure)
	if err != nil {
		return fmt.Errorf("failed to parse image reference: %w", err)
	}

	// Create a transport that uses HTTP for insecure registries
	tr, err := transport.NewWithContext(ctx, ref.Context().Registry, authn.Anonymous, http.DefaultTransport, []string{ref.Scope(transport.PushScope)})
	if err != nil {
		return fmt.Errorf("failed to create transport: %w", err)
	}

	// Push with the custom transport for HTTP registry
	if err := remote.Write(ref, img,
		remote.WithContext(ctx),
		remote.WithAuth(authn.Anonymous),
		remote.WithTransport(tr),
	); err != nil {
		return fmt.Errorf("failed to push image: %w", err)
	}

	return nil
}

// GetTrustBundleDigest returns the digest of the trust bundle in the registry
func GetTrustBundleDigest(ctx context.Context, registryURL string) (string, error) {
	if registryURL == "" {
		registryURL = "localhost:5000"
	}

	imageRef := fmt.Sprintf("%s/trust-bundle:latest", registryURL)
	ref, err := name.ParseReference(imageRef, name.Insecure)
	if err != nil {
		return "", fmt.Errorf("failed to parse image reference: %w", err)
	}

	desc, err := remote.Head(ref, remote.WithContext(ctx), remote.WithAuth(authn.Anonymous))
	if err != nil {
		return "", fmt.Errorf("failed to get image digest: %w", err)
	}

	return desc.Digest.String(), nil
}

// ComputeBundleHash computes a SHA256 hash of the combined bundle for change detection
func ComputeBundleHash(kinderCACert []byte, mozillaCA []byte) string {
	combined := combineCABundles(kinderCACert, mozillaCA)
	hash := sha256.Sum256(combined)
	return fmt.Sprintf("%x", hash[:8]) // First 8 bytes as hex
}
