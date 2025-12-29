package kubernetes

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"time"

	"codeberg.org/hipkoi/kinder/config"
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
	// CertManagerIssuerImageName is the default name for the cert-manager issuer image
	CertManagerIssuerImageName = "cert-manager-issuer"
	// CertManagerIssuerImageTag is the default tag
	CertManagerIssuerImageTag = "latest"
	// CertManagerIssuerName is the default ClusterIssuer name
	CertManagerIssuerName = "kinder-ca"
	// CertManagerIssuerEmail is the default ACME account email
	CertManagerIssuerEmail = "admin@localhost"
	// CertManagerIssuerIngressClass is the default ingress class for HTTP-01 solver
	CertManagerIssuerIngressClass = "traefik"
)

// CertManagerIssuerConfig holds configuration for building cert-manager issuer manifests
type CertManagerIssuerConfig struct {
	// RootCACertPath is the path to the kinder root CA certificate
	RootCACertPath string
	// RegistryURL is the registry URL to push to (default: localhost:5000)
	RegistryURL string
	// ImageName is the image name (default: cert-manager-issuer)
	ImageName string
	// ImageTag is the image tag (default: latest)
	ImageTag string
	// IssuerName is the name of the ClusterIssuer (default: kinder-ca)
	IssuerName string
	// Email is the ACME account email (default: admin@localhost)
	Email string
	// ACMEServerURL is the Step CA ACME server URL
	// If empty, derived from Domain and Port
	ACMEServerURL string
	// Domain is the base domain for services (default: c0000201.sslip.io)
	Domain string
	// Port is the HTTPS port (default: 8443)
	Port string
	// IngressClass is the ingress class for HTTP-01 solver (default: traefik)
	IngressClass string
	// UseDNS01 uses DNS-01 solver instead of HTTP-01
	UseDNS01 bool
	// DNS01Provider is the DNS provider for DNS-01 (e.g., cloudflare, route53)
	DNS01Provider string
	// IncludeExampleCert includes an example Certificate resource
	IncludeExampleCert bool
	// ExampleCertDomain is the domain for the example certificate
	ExampleCertDomain string
}

// CertManagerIssuerManifests holds the generated Kubernetes manifests
type CertManagerIssuerManifests struct {
	// Kustomization is the kustomization.yaml content
	Kustomization []byte
	// ClusterIssuer is the clusterissuer.yaml
	ClusterIssuer []byte
	// ExampleCert is the optional example-certificate.yaml
	ExampleCert []byte
}

// BuildAndPushCertManagerIssuer creates an OCI image containing cert-manager
// issuer manifests and pushes it to the registry
func BuildAndPushCertManagerIssuer(ctx context.Context, cfg CertManagerIssuerConfig) error {
	// Apply defaults
	applyIssuerDefaults(&cfg)

	// Read the kinder root CA
	kinderCA, err := os.ReadFile(cfg.RootCACertPath)
	if err != nil {
		return fmt.Errorf("failed to read kinder CA certificate: %w", err)
	}

	// Generate manifests
	manifests, err := GenerateCertManagerIssuerManifests(cfg, kinderCA)
	if err != nil {
		return fmt.Errorf("failed to generate cert-manager issuer manifests: %w", err)
	}

	// Create OCI image with the manifests
	img, err := createCertManagerIssuerImage(manifests)
	if err != nil {
		return fmt.Errorf("failed to create cert-manager issuer image: %w", err)
	}

	// Push to registry
	imageRef := fmt.Sprintf("%s/%s:%s", cfg.RegistryURL, cfg.ImageName, cfg.ImageTag)
	if err := pushImage(ctx, img, imageRef); err != nil {
		return fmt.Errorf("failed to push cert-manager issuer image: %w", err)
	}

	return nil
}

// applyIssuerDefaults sets default values for unset config fields
func applyIssuerDefaults(cfg *CertManagerIssuerConfig) {
	if cfg.RegistryURL == "" {
		cfg.RegistryURL = "localhost:5000"
	}
	if cfg.ImageName == "" {
		cfg.ImageName = CertManagerIssuerImageName
	}
	if cfg.ImageTag == "" {
		cfg.ImageTag = CertManagerIssuerImageTag
	}
	if cfg.IssuerName == "" {
		cfg.IssuerName = CertManagerIssuerName
	}
	if cfg.Email == "" {
		cfg.Email = CertManagerIssuerEmail
	}
	if cfg.Domain == "" {
		cfg.Domain = config.DefaultDomain
	}
	if cfg.Port == "" {
		cfg.Port = config.DefaultTraefikPort
	}
	if cfg.IngressClass == "" {
		cfg.IngressClass = CertManagerIssuerIngressClass
	}
	if cfg.ACMEServerURL == "" {
		cfg.ACMEServerURL = fmt.Sprintf("https://ca.%s:%s/acme/acme/directory", cfg.Domain, cfg.Port)
	}
	if cfg.ExampleCertDomain == "" && cfg.IncludeExampleCert {
		cfg.ExampleCertDomain = fmt.Sprintf("example.%s", cfg.Domain)
	}
}

// GenerateCertManagerIssuerManifests creates the Kubernetes manifests for cert-manager issuer
func GenerateCertManagerIssuerManifests(cfg CertManagerIssuerConfig, kinderCA []byte) (*CertManagerIssuerManifests, error) {
	// Apply defaults
	applyIssuerDefaults(&cfg)

	// Base64 encode the CA certificate for the caBundle field
	caBundle := base64.StdEncoding.EncodeToString(kinderCA)

	// Generate ClusterIssuer YAML
	clusterIssuer := generateClusterIssuerYAML(cfg, caBundle)

	// Generate Kustomization YAML
	resources := []string{"clusterissuer.yaml"}

	// Optionally generate example Certificate
	var exampleCert []byte
	if cfg.IncludeExampleCert {
		exampleCert = []byte(generateExampleCertificateYAML(cfg))
		resources = append(resources, "example-certificate.yaml")
	}

	kustomization := generateIssuerKustomizationYAML(resources)

	return &CertManagerIssuerManifests{
		Kustomization: []byte(kustomization),
		ClusterIssuer: []byte(clusterIssuer),
		ExampleCert:   exampleCert,
	}, nil
}

// generateClusterIssuerYAML creates the ClusterIssuer manifest
func generateClusterIssuerYAML(cfg CertManagerIssuerConfig, caBundle string) string {
	// Build solver configuration
	var solverYAML string
	if cfg.UseDNS01 && cfg.DNS01Provider != "" {
		solverYAML = generateDNS01SolverYAML(cfg.DNS01Provider)
	} else {
		solverYAML = generateHTTP01SolverYAML(cfg.IngressClass)
	}

	return fmt.Sprintf(`apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: %s
  labels:
    app.kubernetes.io/name: kinder-ca-issuer
    app.kubernetes.io/component: certificate-issuer
    app.kubernetes.io/managed-by: kinder
spec:
  acme:
    # Step CA ACME server endpoint
    server: %s
    # Email for ACME registration
    email: %s
    # Secret to store the ACME account private key
    privateKeySecretRef:
      name: %s-account-key
    # CA bundle to trust the Step CA certificate
    caBundle: %s
    # Solver configuration
    solvers:
%s`, cfg.IssuerName, cfg.ACMEServerURL, cfg.Email, cfg.IssuerName, caBundle, solverYAML)
}

// generateHTTP01SolverYAML creates HTTP-01 solver configuration
func generateHTTP01SolverYAML(ingressClass string) string {
	return fmt.Sprintf(`      - http01:
          ingress:
            ingressClassName: %s
`, ingressClass)
}

// generateDNS01SolverYAML creates DNS-01 solver configuration placeholder
func generateDNS01SolverYAML(provider string) string {
	// This is a placeholder - actual DNS-01 config depends heavily on the provider
	// and requires additional secrets for credentials
	return fmt.Sprintf(`      - dns01:
          # Configure your DNS provider here
          # See: https://cert-manager.io/docs/configuration/acme/dns01/
          %s: {}
`, provider)
}

// generateExampleCertificateYAML creates an example Certificate resource
func generateExampleCertificateYAML(cfg CertManagerIssuerConfig) string {
	return fmt.Sprintf(`apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: example-cert
  namespace: default
  labels:
    app.kubernetes.io/name: example-certificate
    app.kubernetes.io/managed-by: kinder
spec:
  # Secret that will contain the TLS certificate
  secretName: example-cert-tls
  # Duration of the certificate (default: 90 days)
  duration: 2160h # 90 days
  # Renew 30 days before expiry
  renewBefore: 720h # 30 days
  # Common name (deprecated but still used by some applications)
  commonName: %s
  # DNS names for the certificate
  dnsNames:
    - %s
  # Reference to the ClusterIssuer
  issuerRef:
    name: %s
    kind: ClusterIssuer
    group: cert-manager.io
`, cfg.ExampleCertDomain, cfg.ExampleCertDomain, cfg.IssuerName)
}

// generateIssuerKustomizationYAML creates the kustomization.yaml
func generateIssuerKustomizationYAML(resources []string) string {
	var resourcesYAML string
	for _, r := range resources {
		resourcesYAML += fmt.Sprintf("  - %s\n", r)
	}

	return fmt.Sprintf(`apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
%s`, resourcesYAML)
}

// createCertManagerIssuerImage creates an OCI image containing the manifests
func createCertManagerIssuerImage(manifests *CertManagerIssuerManifests) (v1.Image, error) {
	// Create a tar archive with the manifest files
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	files := map[string][]byte{
		"kustomization.yaml": manifests.Kustomization,
		"clusterissuer.yaml": manifests.ClusterIssuer,
	}

	// Add example certificate if present
	if len(manifests.ExampleCert) > 0 {
		files["example-certificate.yaml"] = manifests.ExampleCert
	}

	for fileName, content := range files {
		header := &tar.Header{
			Name:    fileName,
			Mode:    0644,
			Size:    int64(len(content)),
			ModTime: time.Now(),
		}

		if err := tw.WriteHeader(header); err != nil {
			return nil, fmt.Errorf("failed to write tar header for %s: %w", fileName, err)
		}

		if _, err := tw.Write(content); err != nil {
			return nil, fmt.Errorf("failed to write %s to tar: %w", fileName, err)
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
		"org.opencontainers.image.title":       "Cert-Manager Issuer",
		"org.opencontainers.image.description": "ClusterIssuer configuration for Step CA ACME server",
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

// GetCertManagerIssuerDigest returns the digest of the cert-manager issuer in the registry
func GetCertManagerIssuerDigest(ctx context.Context, registryURL, imageName string) (string, error) {
	if registryURL == "" {
		registryURL = "localhost:5000"
	}
	if imageName == "" {
		imageName = CertManagerIssuerImageName
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

// SaveCertManagerIssuerManifests saves manifests to disk for local inspection
func SaveCertManagerIssuerManifests(dataDir string, manifests *CertManagerIssuerManifests) error {
	manifestsDir := dataDir + "/cert-manager-issuer"
	if err := os.MkdirAll(manifestsDir, 0755); err != nil {
		return fmt.Errorf("failed to create manifests directory: %w", err)
	}

	files := map[string][]byte{
		"kustomization.yaml": manifests.Kustomization,
		"clusterissuer.yaml": manifests.ClusterIssuer,
	}

	if len(manifests.ExampleCert) > 0 {
		files["example-certificate.yaml"] = manifests.ExampleCert
	}

	for fileName, content := range files {
		if err := os.WriteFile(manifestsDir+"/"+fileName, content, 0644); err != nil {
			return fmt.Errorf("failed to write %s: %w", fileName, err)
		}
	}

	return nil
}
