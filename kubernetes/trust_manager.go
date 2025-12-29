package kubernetes

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
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
	// TrustManagerBundleImageName is the default name for the trust-manager manifests image
	TrustManagerBundleImageName = "trust-manager-bundle"
	// TrustManagerBundleImageTag is the default tag
	TrustManagerBundleImageTag = "latest"
	// TrustManagerNamespace is the namespace where trust-manager resources are deployed
	TrustManagerNamespace = "cert-manager"
	// TrustManagerBundleName is the name of the Bundle resource
	TrustManagerBundleName = "kinder-ca-bundle"
	// TrustManagerConfigMapName is the name of the source ConfigMap
	TrustManagerConfigMapName = "kinder-ca-source"
	// TrustManagerTargetConfigMapKey is the key in the target ConfigMap
	TrustManagerTargetConfigMapKey = "ca-certificates.crt"
)

// TrustManagerBundleConfig holds configuration for building trust-manager manifests
type TrustManagerBundleConfig struct {
	// RootCACertPath is the path to the kinder root CA certificate
	RootCACertPath string
	// RegistryURL is the registry URL to push to (default: localhost:5000)
	RegistryURL string
	// ImageName is the image name (default: trust-manager-bundle)
	ImageName string
	// ImageTag is the image tag (default: latest)
	ImageTag string
	// Namespace is the namespace for the Bundle resource (default: cert-manager)
	Namespace string
	// BundleName is the name of the Bundle resource (default: kinder-ca-bundle)
	BundleName string
	// TargetNamespace restricts the bundle to a specific namespace (empty = all namespaces)
	TargetNamespace string
	// IncludeMozillaCAs includes Mozilla CA bundle alongside kinder CA
	IncludeMozillaCAs bool
}

// BuildAndPushTrustManagerBundle creates an OCI image containing trust-manager
// Kubernetes manifests and pushes it to the registry
func BuildAndPushTrustManagerBundle(ctx context.Context, cfg TrustManagerBundleConfig) error {
	// Set defaults
	if cfg.RegistryURL == "" {
		cfg.RegistryURL = "localhost:5000"
	}
	if cfg.ImageName == "" {
		cfg.ImageName = TrustManagerBundleImageName
	}
	if cfg.ImageTag == "" {
		cfg.ImageTag = TrustManagerBundleImageTag
	}
	if cfg.Namespace == "" {
		cfg.Namespace = TrustManagerNamespace
	}
	if cfg.BundleName == "" {
		cfg.BundleName = TrustManagerBundleName
	}

	// Read the kinder root CA
	kinderCA, err := os.ReadFile(cfg.RootCACertPath)
	if err != nil {
		return fmt.Errorf("failed to read kinder CA certificate: %w", err)
	}

	// Optionally download and include Mozilla CAs
	var mozillaCA []byte
	if cfg.IncludeMozillaCAs {
		mozillaCA, err = DownloadMozillaCACerts(ctx)
		if err != nil {
			return fmt.Errorf("failed to download Mozilla CA certificates: %w", err)
		}
	}

	// Generate manifests
	manifests, err := GenerateTrustManagerManifests(cfg, kinderCA, mozillaCA)
	if err != nil {
		return fmt.Errorf("failed to generate trust-manager manifests: %w", err)
	}

	// Create OCI image with the manifests
	img, err := createTrustManagerImage(manifests)
	if err != nil {
		return fmt.Errorf("failed to create trust-manager bundle image: %w", err)
	}

	// Push to registry
	imageRef := fmt.Sprintf("%s/%s:%s", cfg.RegistryURL, cfg.ImageName, cfg.ImageTag)
	if err := pushImage(ctx, img, imageRef); err != nil {
		return fmt.Errorf("failed to push trust-manager bundle image: %w", err)
	}

	return nil
}

// TrustManagerManifests holds the generated Kubernetes manifests
type TrustManagerManifests struct {
	// Kustomization is the kustomization.yaml content
	Kustomization []byte
	// ConfigMap is the configmap.yaml containing the CA cert
	ConfigMap []byte
	// Bundle is the bundle.yaml for trust-manager
	Bundle []byte
}

// GenerateTrustManagerManifests creates the Kubernetes manifests for trust-manager
func GenerateTrustManagerManifests(cfg TrustManagerBundleConfig, kinderCA, mozillaCA []byte) (*TrustManagerManifests, error) {
	// Apply defaults
	if cfg.Namespace == "" {
		cfg.Namespace = TrustManagerNamespace
	}
	if cfg.BundleName == "" {
		cfg.BundleName = TrustManagerBundleName
	}

	// Combine certificates if Mozilla CAs are included
	var combinedCert string
	if len(mozillaCA) > 0 {
		combinedCert = string(combineCABundles(kinderCA, mozillaCA))
	} else {
		combinedCert = string(kinderCA)
	}

	// Generate ConfigMap YAML
	configMap := generateConfigMapYAML(cfg.Namespace, combinedCert)

	// Generate Bundle YAML
	bundle := generateBundleYAML(cfg)

	// Generate Kustomization YAML
	kustomization := generateKustomizationYAML()

	return &TrustManagerManifests{
		Kustomization: []byte(kustomization),
		ConfigMap:     []byte(configMap),
		Bundle:        []byte(bundle),
	}, nil
}

// generateConfigMapYAML creates a ConfigMap containing the CA certificate
func generateConfigMapYAML(namespace, certPEM string) string {
	return fmt.Sprintf(`apiVersion: v1
kind: ConfigMap
metadata:
  name: %s
  namespace: %s
  labels:
    app.kubernetes.io/name: kinder-ca
    app.kubernetes.io/component: trust-bundle
    app.kubernetes.io/managed-by: kinder
data:
  ca.crt: |
%s`, TrustManagerConfigMapName, namespace, indentPEMForConfigMap(certPEM, 4))
}

// generateBundleYAML creates a trust-manager Bundle resource
func generateBundleYAML(cfg TrustManagerBundleConfig) string {
	// Build the target section
	targetSection := fmt.Sprintf(`  target:
    configMap:
      key: %s`, TrustManagerTargetConfigMapKey)

	// Add namespace selector if restricted to a specific namespace
	if cfg.TargetNamespace != "" {
		targetSection += fmt.Sprintf(`
    namespaceSelector:
      matchLabels:
        kubernetes.io/metadata.name: %s`, cfg.TargetNamespace)
	}

	return fmt.Sprintf(`apiVersion: trust.cert-manager.io/v1alpha1
kind: Bundle
metadata:
  name: %s
  labels:
    app.kubernetes.io/name: kinder-ca
    app.kubernetes.io/component: trust-bundle
    app.kubernetes.io/managed-by: kinder
spec:
  sources:
    - configMap:
        name: %s
        key: ca.crt
%s
`, cfg.BundleName, TrustManagerConfigMapName, targetSection)
}

// generateKustomizationYAML creates the kustomization.yaml
func generateKustomizationYAML() string {
	return `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - configmap.yaml
  - bundle.yaml
`
}

// indentPEMForConfigMap indents each line of the PEM content by the specified number of spaces
func indentPEMForConfigMap(pem string, spaces int) string {
	indent := ""
	for i := 0; i < spaces; i++ {
		indent += " "
	}

	var result bytes.Buffer
	lines := bytes.Split([]byte(pem), []byte("\n"))
	for i, line := range lines {
		if len(line) > 0 {
			result.WriteString(indent)
			result.Write(line)
		}
		if i < len(lines)-1 {
			result.WriteString("\n")
		}
	}
	return result.String()
}

// createTrustManagerImage creates an OCI image containing the manifests
func createTrustManagerImage(manifests *TrustManagerManifests) (v1.Image, error) {
	// Create a tar archive with the manifest files
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	files := map[string][]byte{
		"kustomization.yaml": manifests.Kustomization,
		"configmap.yaml":     manifests.ConfigMap,
		"bundle.yaml":        manifests.Bundle,
	}

	for name, content := range files {
		header := &tar.Header{
			Name:    name,
			Mode:    0644,
			Size:    int64(len(content)),
			ModTime: time.Now(),
		}

		if err := tw.WriteHeader(header); err != nil {
			return nil, fmt.Errorf("failed to write tar header for %s: %w", name, err)
		}

		if _, err := tw.Write(content); err != nil {
			return nil, fmt.Errorf("failed to write %s to tar: %w", name, err)
		}
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
	imgCfg, err := img.ConfigFile()
	if err != nil {
		return nil, fmt.Errorf("failed to get config file: %w", err)
	}

	imgCfg.Author = "kinder"
	imgCfg.Created = v1.Time{Time: time.Now()}
	imgCfg.Config.Labels = map[string]string{
		"org.opencontainers.image.title":       "Trust Manager Bundle",
		"org.opencontainers.image.description": "Kustomization bundle with trust-manager resources for kinder CA",
		"org.opencontainers.image.source":      "https://codeberg.org/hipkoi/kinder",
		"argocd.argoproj.io/manifest-type":     "kustomize",
	}

	img, err = mutate.ConfigFile(img, imgCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to set config: %w", err)
	}

	// Set the media type to OCI
	img = mutate.MediaType(img, types.OCIManifestSchema1)

	return img, nil
}

// GetTrustManagerBundleDigest returns the digest of the trust-manager bundle in the registry
func GetTrustManagerBundleDigest(ctx context.Context, registryURL, imageName string) (string, error) {
	if registryURL == "" {
		registryURL = "localhost:5000"
	}
	if imageName == "" {
		imageName = TrustManagerBundleImageName
	}

	imageRef := fmt.Sprintf("%s/%s:latest", registryURL, imageName)
	ref, err := name.ParseReference(imageRef, name.Insecure)
	if err != nil {
		return "", fmt.Errorf("failed to parse image reference: %w", err)
	}

	// Create transport for insecure registry
	tr, err := transport.NewWithContext(ctx, ref.Context().Registry, authn.Anonymous, http.DefaultTransport, []string{ref.Scope(transport.PullScope)})
	if err != nil {
		return "", fmt.Errorf("failed to create transport: %w", err)
	}

	desc, err := remote.Head(ref, remote.WithContext(ctx), remote.WithAuth(authn.Anonymous), remote.WithTransport(tr))
	if err != nil {
		return "", fmt.Errorf("failed to get image digest: %w", err)
	}

	return desc.Digest.String(), nil
}

// ExtractTrustManagerManifests extracts manifests from a local path for verification
func ExtractTrustManagerManifests(dataDir string) (*TrustManagerManifests, error) {
	manifestsDir := filepath.Join(dataDir, "trust-manager-manifests")

	kustomization, err := os.ReadFile(filepath.Join(manifestsDir, "kustomization.yaml"))
	if err != nil {
		return nil, fmt.Errorf("failed to read kustomization.yaml: %w", err)
	}

	configMap, err := os.ReadFile(filepath.Join(manifestsDir, "configmap.yaml"))
	if err != nil {
		return nil, fmt.Errorf("failed to read configmap.yaml: %w", err)
	}

	bundle, err := os.ReadFile(filepath.Join(manifestsDir, "bundle.yaml"))
	if err != nil {
		return nil, fmt.Errorf("failed to read bundle.yaml: %w", err)
	}

	return &TrustManagerManifests{
		Kustomization: kustomization,
		ConfigMap:     configMap,
		Bundle:        bundle,
	}, nil
}

// SaveTrustManagerManifests saves manifests to disk for local inspection
func SaveTrustManagerManifests(dataDir string, manifests *TrustManagerManifests) error {
	manifestsDir := filepath.Join(dataDir, "trust-manager-manifests")
	if err := os.MkdirAll(manifestsDir, 0755); err != nil {
		return fmt.Errorf("failed to create manifests directory: %w", err)
	}

	files := map[string][]byte{
		"kustomization.yaml": manifests.Kustomization,
		"configmap.yaml":     manifests.ConfigMap,
		"bundle.yaml":        manifests.Bundle,
	}

	for name, content := range files {
		if err := os.WriteFile(filepath.Join(manifestsDir, name), content, 0644); err != nil {
			return fmt.Errorf("failed to write %s: %w", name, err)
		}
	}

	return nil
}
