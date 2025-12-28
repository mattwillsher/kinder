package docker

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"codeberg.org/hipkoi/kinder/config"
)

// yamlSafeNameRegex matches valid Kubernetes resource names
var yamlSafeNameRegex = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

const (
	// ArgoCDNamespace is the namespace where ArgoCD is installed
	ArgoCDNamespace = "argocd"
	// ArgoCDInstallURL is the base URL for ArgoCD installation manifests
	ArgoCDInstallURL = "https://raw.githubusercontent.com/argoproj/argo-cd"
)

// GitCredentialType represents the type of Git credentials
type GitCredentialType string

const (
	// GitCredentialNone indicates no credentials (public repo)
	GitCredentialNone GitCredentialType = "none"
	// GitCredentialHTTP indicates HTTP/HTTPS with username/password or token
	GitCredentialHTTP GitCredentialType = "http"
	// GitCredentialSSH indicates SSH with private key
	GitCredentialSSH GitCredentialType = "ssh"
)

// sanitizeYAMLValue escapes or rejects values that could cause YAML injection.
// Returns an error if the value contains dangerous content.
func sanitizeYAMLValue(value string) (string, error) {
	// Reject newlines which could inject YAML structure
	if strings.ContainsAny(value, "\n\r") {
		return "", fmt.Errorf("value contains newlines which are not allowed")
	}
	// Reject null bytes
	if strings.Contains(value, "\x00") {
		return "", fmt.Errorf("value contains null bytes which are not allowed")
	}
	return value, nil
}

// sanitizeK8sName validates and returns a Kubernetes-safe resource name.
// Names must be lowercase alphanumeric with hyphens, max 63 chars.
func sanitizeK8sName(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("name cannot be empty")
	}
	// Convert to lowercase and limit length
	name = strings.ToLower(name)
	if len(name) > 63 {
		name = name[:63]
	}
	// Trim trailing hyphens after truncation
	name = strings.TrimRight(name, "-")

	if !yamlSafeNameRegex.MatchString(name) {
		return "", fmt.Errorf("invalid Kubernetes name: %q (must be lowercase alphanumeric with hyphens)", name)
	}
	return name, nil
}

// validateGitURL validates that a string is a valid Git repository URL.
func validateGitURL(repoURL string) error {
	if repoURL == "" {
		return fmt.Errorf("repository URL cannot be empty")
	}
	// Check for SSH URLs (git@host:path)
	if strings.HasPrefix(repoURL, "git@") {
		if !strings.Contains(repoURL, ":") {
			return fmt.Errorf("invalid SSH URL format: missing colon separator")
		}
		return nil
	}
	// Check for HTTPS URLs
	parsed, err := url.Parse(repoURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return fmt.Errorf("URL scheme must be http, https, or git@: got %q", parsed.Scheme)
	}
	if parsed.Host == "" {
		return fmt.Errorf("URL must have a host")
	}
	return nil
}

// ArgoCDConfig holds configuration for ArgoCD bootstrap
type ArgoCDConfig struct {
	// Version is the ArgoCD version to install (default: v2.13.3)
	Version string
	// Namespace is the ArgoCD namespace (default: argocd)
	Namespace string

	// Git repository configuration for initial app
	// RepoURL is the Git repository URL (required for initial app)
	RepoURL string
	// RepoPath is the path within the repo (default: .)
	RepoPath string
	// RepoBranch is the branch to track (default: main)
	RepoBranch string
	// AppName is the name of the initial Application (default: root)
	AppName string
	// TargetNamespace is the namespace for deployed resources (default: default)
	TargetNamespace string

	// Git credentials
	// CredentialType is the type of credentials (none, http, ssh)
	CredentialType GitCredentialType
	// HTTPUsername is the username for HTTP auth
	HTTPUsername string
	// HTTPPassword is the password or token for HTTP auth
	HTTPPassword string
	// SSHPrivateKey is the SSH private key content
	SSHPrivateKey string
	// SSHPrivateKeyPath is the path to SSH private key file
	SSHPrivateKeyPath string

	// Additional options
	// IncludeKinderApps includes Applications for trust-bundle and cert-issuer
	IncludeKinderApps bool
	// ZotRegistryURL is the Zot registry URL from within the cluster (default: zot:5000)
	ZotRegistryURL string
	// SkipInitialApp skips creating the initial application (install ArgoCD only)
	SkipInitialApp bool
	// WaitReady waits for ArgoCD to be ready before returning
	WaitReady bool
	// WaitTimeout is the timeout for waiting (default: 5m)
	WaitTimeout time.Duration
	// KubeconfigPath is the path to kubeconfig (default: uses current context)
	KubeconfigPath string
	// KubeContext is the kubectl context to use
	KubeContext string
}

// ArgoCDManifests holds the generated Kubernetes manifests
type ArgoCDManifests struct {
	// Namespace is the namespace.yaml
	Namespace []byte
	// RepoSecret is the repository-secret.yaml (optional, for private repos)
	RepoSecret []byte
	// InitialApp is the initial-app.yaml
	InitialApp []byte
	// KinderApps is the kinder-apps.yaml (optional)
	KinderApps []byte
}

// BootstrapArgoCD installs ArgoCD and configures the initial application
func BootstrapArgoCD(ctx context.Context, cfg ArgoCDConfig, progressFn func(step, message string)) error {
	// Apply defaults
	applyArgoCDDefaults(&cfg)

	// Load SSH key from file if path provided
	if cfg.SSHPrivateKeyPath != "" && cfg.SSHPrivateKey == "" {
		keyData, err := os.ReadFile(cfg.SSHPrivateKeyPath)
		if err != nil {
			return fmt.Errorf("failed to read SSH private key: %w", err)
		}
		cfg.SSHPrivateKey = string(keyData)
	}

	// Step 1: Create namespace
	if progressFn != nil {
		progressFn("namespace", "Creating ArgoCD namespace")
	}
	if err := kubectlApply(ctx, cfg, generateArgoCDNamespaceYAML(cfg.Namespace)); err != nil {
		return fmt.Errorf("failed to create namespace: %w", err)
	}

	// Step 2: Install ArgoCD
	if progressFn != nil {
		progressFn("install", fmt.Sprintf("Installing ArgoCD %s", cfg.Version))
	}
	installURL := fmt.Sprintf("%s/%s/manifests/install.yaml", ArgoCDInstallURL, cfg.Version)
	if err := kubectlApplyURL(ctx, cfg, installURL); err != nil {
		return fmt.Errorf("failed to install ArgoCD: %w", err)
	}

	// Step 3: Wait for ArgoCD to be ready if requested
	if cfg.WaitReady {
		if progressFn != nil {
			progressFn("wait", "Waiting for ArgoCD to be ready")
		}
		if err := waitForArgoCD(ctx, cfg); err != nil {
			return fmt.Errorf("ArgoCD not ready: %w", err)
		}
	}

	// Step 4: Create repository secret if credentials provided
	if cfg.CredentialType != GitCredentialNone && cfg.RepoURL != "" {
		if progressFn != nil {
			progressFn("secret", "Creating repository credentials")
		}
		secret, err := generateRepoSecretYAML(cfg)
		if err != nil {
			return fmt.Errorf("failed to generate repository secret: %w", err)
		}
		if err := kubectlApply(ctx, cfg, secret); err != nil {
			return fmt.Errorf("failed to create repository secret: %w", err)
		}
	}

	// Step 5: Create initial application
	if !cfg.SkipInitialApp && cfg.RepoURL != "" {
		if progressFn != nil {
			progressFn("app", fmt.Sprintf("Creating initial application '%s'", cfg.AppName))
		}
		appYAML, err := generateInitialAppYAML(cfg)
		if err != nil {
			return fmt.Errorf("failed to generate initial application manifest: %w", err)
		}
		if err := kubectlApply(ctx, cfg, appYAML); err != nil {
			return fmt.Errorf("failed to create initial application: %w", err)
		}
	}

	// Step 6: Create kinder apps if requested
	if cfg.IncludeKinderApps {
		if progressFn != nil {
			progressFn("kinder", "Creating kinder OCI applications")
		}
		if err := kubectlApply(ctx, cfg, generateKinderAppsYAML(cfg)); err != nil {
			return fmt.Errorf("failed to create kinder applications: %w", err)
		}
	}

	return nil
}

// applyArgoCDDefaults sets default values for unset config fields
func applyArgoCDDefaults(cfg *ArgoCDConfig) {
	if cfg.Version == "" {
		cfg.Version = config.DefaultArgocdVersion
	}
	if cfg.Namespace == "" {
		cfg.Namespace = ArgoCDNamespace
	}
	if cfg.RepoPath == "" {
		cfg.RepoPath = "."
	}
	if cfg.RepoBranch == "" {
		cfg.RepoBranch = "main"
	}
	if cfg.AppName == "" {
		cfg.AppName = "root"
	}
	if cfg.TargetNamespace == "" {
		cfg.TargetNamespace = "default"
	}
	if cfg.CredentialType == "" {
		cfg.CredentialType = GitCredentialNone
	}
	if cfg.ZotRegistryURL == "" {
		cfg.ZotRegistryURL = "zot:5000"
	}
	if cfg.WaitTimeout == 0 {
		cfg.WaitTimeout = 5 * time.Minute
	}
}

// kubectlApply applies a manifest to the cluster
func kubectlApply(ctx context.Context, cfg ArgoCDConfig, manifest string) error {
	args := []string{"apply", "-f", "-"}
	if cfg.KubeconfigPath != "" {
		args = append([]string{"--kubeconfig", cfg.KubeconfigPath}, args...)
	}
	if cfg.KubeContext != "" {
		args = append([]string{"--context", cfg.KubeContext}, args...)
	}

	cmd := exec.CommandContext(ctx, "kubectl", args...)
	cmd.Stdin = strings.NewReader(manifest)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, stderr.String())
	}

	return nil
}

// kubectlApplyURL applies a manifest from a URL to the cluster
func kubectlApplyURL(ctx context.Context, cfg ArgoCDConfig, manifestURL string) error {
	args := []string{"apply", "-f", manifestURL}
	if cfg.KubeconfigPath != "" {
		args = append([]string{"--kubeconfig", cfg.KubeconfigPath}, args...)
	}
	if cfg.KubeContext != "" {
		args = append([]string{"--context", cfg.KubeContext}, args...)
	}
	if cfg.Namespace != "" {
		args = append(args, "-n", cfg.Namespace)
	}

	cmd := exec.CommandContext(ctx, "kubectl", args...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, stderr.String())
	}

	return nil
}

// waitForArgoCD waits for ArgoCD deployments to be ready
func waitForArgoCD(ctx context.Context, cfg ArgoCDConfig) error {
	deployments := []string{
		"argocd-server",
		"argocd-repo-server",
		"argocd-applicationset-controller",
		"argocd-redis",
		"argocd-notifications-controller",
		"argocd-dex-server",
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, cfg.WaitTimeout)
	defer cancel()

	for _, deploy := range deployments {
		args := []string{"rollout", "status", "deployment/" + deploy, "-n", cfg.Namespace, "--timeout", cfg.WaitTimeout.String()}
		if cfg.KubeconfigPath != "" {
			args = append([]string{"--kubeconfig", cfg.KubeconfigPath}, args...)
		}
		if cfg.KubeContext != "" {
			args = append([]string{"--context", cfg.KubeContext}, args...)
		}

		cmd := exec.CommandContext(timeoutCtx, "kubectl", args...)
		if err := cmd.Run(); err != nil {
			// Some deployments may not exist in all versions, continue
			continue
		}
	}

	return nil
}

// generateArgoCDNamespaceYAML creates the ArgoCD namespace
func generateArgoCDNamespaceYAML(namespace string) string {
	return fmt.Sprintf(`apiVersion: v1
kind: Namespace
metadata:
  name: %s
  labels:
    app.kubernetes.io/name: argocd
    app.kubernetes.io/managed-by: kinder
`, namespace)
}

// generateRepoSecretYAML creates the repository credentials secret
func generateRepoSecretYAML(cfg ArgoCDConfig) (string, error) {
	repoURL := cfg.RepoURL

	var dataSection string

	switch cfg.CredentialType {
	case GitCredentialHTTP:
		if cfg.HTTPUsername == "" || cfg.HTTPPassword == "" {
			return "", fmt.Errorf("HTTP credentials require both username and password/token")
		}
		username := base64.StdEncoding.EncodeToString([]byte(cfg.HTTPUsername))
		password := base64.StdEncoding.EncodeToString([]byte(cfg.HTTPPassword))
		url := base64.StdEncoding.EncodeToString([]byte(repoURL))
		repoType := base64.StdEncoding.EncodeToString([]byte("git"))

		dataSection = fmt.Sprintf(`data:
  type: %s
  url: %s
  username: %s
  password: %s`, repoType, url, username, password)

	case GitCredentialSSH:
		if cfg.SSHPrivateKey == "" {
			return "", fmt.Errorf("SSH credentials require a private key")
		}
		sshKey := base64.StdEncoding.EncodeToString([]byte(cfg.SSHPrivateKey))
		url := base64.StdEncoding.EncodeToString([]byte(repoURL))
		repoType := base64.StdEncoding.EncodeToString([]byte("git"))

		dataSection = fmt.Sprintf(`data:
  type: %s
  url: %s
  sshPrivateKey: %s`, repoType, url, sshKey)

	default:
		return "", fmt.Errorf("unsupported credential type: %s", cfg.CredentialType)
	}

	repoName := extractRepoName(repoURL)

	return fmt.Sprintf(`apiVersion: v1
kind: Secret
metadata:
  name: repo-%s
  namespace: %s
  labels:
    argocd.argoproj.io/secret-type: repository
    app.kubernetes.io/managed-by: kinder
%s
`, repoName, cfg.Namespace, dataSection), nil
}

// extractRepoName extracts a safe name from the repository URL
func extractRepoName(repoURL string) string {
	if strings.HasPrefix(repoURL, "git@") {
		parts := strings.Split(repoURL, ":")
		if len(parts) >= 2 {
			path := parts[1]
			path = strings.TrimSuffix(path, ".git")
			path = strings.ReplaceAll(path, "/", "-")
			return strings.ToLower(path)
		}
	}

	parsed, err := url.Parse(repoURL)
	if err != nil {
		return "private-repo"
	}

	path := strings.TrimPrefix(parsed.Path, "/")
	path = strings.TrimSuffix(path, ".git")
	path = strings.ReplaceAll(path, "/", "-")

	if path == "" {
		return "private-repo"
	}

	return strings.ToLower(path)
}

// generateInitialAppYAML creates the initial Application resource.
// Returns an error if any input values fail validation.
func generateInitialAppYAML(cfg ArgoCDConfig) (string, error) {
	// Validate app name
	appName, err := sanitizeK8sName(cfg.AppName)
	if err != nil {
		return "", fmt.Errorf("invalid app name: %w", err)
	}

	// Validate namespace
	namespace, err := sanitizeK8sName(cfg.Namespace)
	if err != nil {
		return "", fmt.Errorf("invalid namespace: %w", err)
	}

	// Validate target namespace
	targetNamespace, err := sanitizeK8sName(cfg.TargetNamespace)
	if err != nil {
		return "", fmt.Errorf("invalid target namespace: %w", err)
	}

	// Validate repo URL
	if err := validateGitURL(cfg.RepoURL); err != nil {
		return "", fmt.Errorf("invalid repository URL: %w", err)
	}
	repoURL, err := sanitizeYAMLValue(cfg.RepoURL)
	if err != nil {
		return "", fmt.Errorf("invalid repository URL: %w", err)
	}

	// Validate branch
	branch, err := sanitizeYAMLValue(cfg.RepoBranch)
	if err != nil {
		return "", fmt.Errorf("invalid branch: %w", err)
	}

	// Validate path
	path, err := sanitizeYAMLValue(cfg.RepoPath)
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}

	return fmt.Sprintf(`apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: %s
  namespace: %s
  labels:
    app.kubernetes.io/name: %s
    app.kubernetes.io/component: root-app
    app.kubernetes.io/managed-by: kinder
  finalizers:
    - resources-finalizer.argocd.argoproj.io
spec:
  project: default
  source:
    repoURL: %s
    targetRevision: %s
    path: %s
  destination:
    server: https://kubernetes.default.svc
    namespace: %s
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions:
      - CreateNamespace=true
`, appName, namespace, appName, repoURL, branch, path, targetNamespace), nil
}

// generateKinderAppsYAML creates Applications for kinder OCI bundles
func generateKinderAppsYAML(cfg ArgoCDConfig) string {
	return fmt.Sprintf(`apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: kinder-trust-bundle
  namespace: %s
  labels:
    app.kubernetes.io/name: kinder-trust-bundle
    app.kubernetes.io/component: trust-manager
    app.kubernetes.io/managed-by: kinder
spec:
  project: default
  source:
    repoURL: %s/%s
    targetRevision: %s
  destination:
    server: https://kubernetes.default.svc
    namespace: cert-manager
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
---
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: kinder-cert-issuer
  namespace: %s
  labels:
    app.kubernetes.io/name: kinder-cert-issuer
    app.kubernetes.io/component: cert-manager
    app.kubernetes.io/managed-by: kinder
spec:
  project: default
  source:
    repoURL: %s/%s
    targetRevision: %s
  destination:
    server: https://kubernetes.default.svc
    namespace: cert-manager
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
`, cfg.Namespace, cfg.ZotRegistryURL, TrustManagerBundleImageName, TrustManagerBundleImageTag,
		cfg.Namespace, cfg.ZotRegistryURL, CertManagerIssuerImageName, CertManagerIssuerImageTag)
}

// GenerateArgoCDManifests creates the Kubernetes manifests for inspection
func GenerateArgoCDManifests(cfg ArgoCDConfig) (*ArgoCDManifests, error) {
	applyArgoCDDefaults(&cfg)

	namespace := generateArgoCDNamespaceYAML(cfg.Namespace)

	var repoSecret []byte
	if cfg.CredentialType != GitCredentialNone && cfg.RepoURL != "" {
		secret, err := generateRepoSecretYAML(cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to generate repository secret: %w", err)
		}
		repoSecret = []byte(secret)
	}

	var initialApp []byte
	if !cfg.SkipInitialApp && cfg.RepoURL != "" {
		appYAML, err := generateInitialAppYAML(cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to generate initial application manifest: %w", err)
		}
		initialApp = []byte(appYAML)
	}

	var kinderApps []byte
	if cfg.IncludeKinderApps {
		kinderApps = []byte(generateKinderAppsYAML(cfg))
	}

	return &ArgoCDManifests{
		Namespace:  []byte(namespace),
		RepoSecret: repoSecret,
		InitialApp: initialApp,
		KinderApps: kinderApps,
	}, nil
}

// GetArgoCDPassword retrieves the initial admin password from the cluster
func GetArgoCDPassword(ctx context.Context, cfg ArgoCDConfig) (string, error) {
	applyArgoCDDefaults(&cfg)

	args := []string{"get", "secret", "argocd-initial-admin-secret", "-n", cfg.Namespace, "-o", "jsonpath={.data.password}"}
	if cfg.KubeconfigPath != "" {
		args = append([]string{"--kubeconfig", cfg.KubeconfigPath}, args...)
	}
	if cfg.KubeContext != "" {
		args = append([]string{"--context", cfg.KubeContext}, args...)
	}

	cmd := exec.CommandContext(ctx, "kubectl", args...)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get admin password: %w", err)
	}

	decoded, err := base64.StdEncoding.DecodeString(string(output))
	if err != nil {
		return "", fmt.Errorf("failed to decode password: %w", err)
	}

	return string(decoded), nil
}
