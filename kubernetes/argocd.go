package kubernetes

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

var k8sNameRegex = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

const (
	ArgoCDNamespace  = "argocd"
	ArgoCDInstallURL = "https://raw.githubusercontent.com/argoproj/argo-cd"
)

type GitCredentialType string

const (
	GitCredentialNone GitCredentialType = "none"
	GitCredentialHTTP GitCredentialType = "http"
	GitCredentialSSH  GitCredentialType = "ssh"
)

// ArgoCDConfig holds configuration for ArgoCD installation.
type ArgoCDConfig struct {
	Version           string
	Namespace         string
	KubeconfigPath    string
	KubeContext       string
	WaitTimeout       time.Duration
	CACertPEM         string
	ManifestURL       string
	IncludeKinderApps bool
	ZotRegistryURL    string
	Domain            string
	Port              string

	// Git repository for initial app
	RepoURL         string
	RepoPath        string
	RepoBranch      string
	AppName         string
	TargetNamespace string
	SkipInitialApp  bool

	// Credentials
	CredentialType    GitCredentialType
	HTTPUsername      string
	HTTPPassword      string
	SSHPrivateKey     string
	SSHPrivateKeyPath string
}

// Install installs ArgoCD with anonymous access enabled (no authentication).
func Install(ctx context.Context, cfg ArgoCDConfig, progress func(string)) error {
	setDefaults(&cfg)
	if err := loadSSHKey(&cfg); err != nil {
		return err
	}

	steps := []struct {
		msg string
		fn  func() error
	}{
		{"Creating namespace", func() error { return kubectl(ctx, cfg, namespaceYAML(cfg.Namespace)) }},
		{"Installing ArgoCD " + cfg.Version, func() error { return kubectlURL(ctx, cfg, installURL(cfg.Version)) }},
		{"Disabling authentication", func() error { return disableAuth(ctx, cfg) }},
		{"Waiting for rollout", func() error { return waitReady(ctx, cfg) }},
	}

	for _, s := range steps {
		if progress != nil {
			progress(s.msg)
		}
		if err := s.fn(); err != nil {
			return fmt.Errorf("%s: %w", strings.ToLower(s.msg), err)
		}
	}

	// Optional: mount CA cert for TLS to private registries
	if cfg.CACertPEM != "" {
		if err := kubectl(ctx, cfg, caSecretYAML(cfg)); err != nil {
			return fmt.Errorf("create CA secret: %w", err)
		}
		if err := patchRepoServer(ctx, cfg); err != nil {
			return fmt.Errorf("patch repo-server: %w", err)
		}
	}

	// Optional: create repository credentials
	if cfg.CredentialType != GitCredentialNone && cfg.RepoURL != "" {
		if progress != nil {
			progress("Creating repository credentials")
		}
		secret, err := repoSecretYAML(cfg)
		if err != nil {
			return err
		}
		if err := kubectl(ctx, cfg, secret); err != nil {
			return fmt.Errorf("create repo secret: %w", err)
		}
	}

	// Optional: create initial Application
	if !cfg.SkipInitialApp && cfg.RepoURL != "" {
		if progress != nil {
			progress("Creating application " + cfg.AppName)
		}
		app, err := applicationYAML(cfg)
		if err != nil {
			return err
		}
		if err := kubectl(ctx, cfg, app); err != nil {
			return fmt.Errorf("create application: %w", err)
		}
	}

	// Optional: create kinder OCI apps
	if cfg.IncludeKinderApps {
		if progress != nil {
			progress("Creating kinder applications")
		}
		if err := kubectl(ctx, cfg, kinderAppsYAML(cfg)); err != nil {
			return fmt.Errorf("create kinder apps: %w", err)
		}
	}

	// Optional: apply app-of-apps manifest from URL
	if cfg.ManifestURL != "" {
		if err := validateURL(cfg.ManifestURL); err != nil {
			return fmt.Errorf("invalid manifest URL: %w", err)
		}
		if progress != nil {
			progress("Applying " + cfg.ManifestURL)
		}
		if err := kubectlURL(ctx, cfg, cfg.ManifestURL); err != nil {
			return fmt.Errorf("apply manifest URL: %w", err)
		}
	}

	return nil
}

// disableAuth configures ArgoCD for anonymous admin access.
func disableAuth(ctx context.Context, cfg ArgoCDConfig) error {
	patches := map[string]string{
		"argocd-cmd-params-cm": `{"data":{"server.insecure":"true"}}`,
		"argocd-cm":            `{"data":{"users.anonymous.enabled":"true"}}`,
		"argocd-rbac-cm":       `{"data":{"policy.default":"role:admin"}}`,
	}
	for cm, patch := range patches {
		if err := kubectlPatch(ctx, cfg, "configmap", cm, patch); err != nil {
			return fmt.Errorf("patch %s: %w", cm, err)
		}
	}
	return nil
}

// waitReady waits for core ArgoCD deployments.
func waitReady(ctx context.Context, cfg ArgoCDConfig) error {
	deploys := []string{"argocd-server", "argocd-repo-server", "argocd-redis"}
	timeout := cfg.WaitTimeout.String()

	for _, d := range deploys {
		args := kubectlArgs(cfg, "rollout", "status", "deployment/"+d, "-n", cfg.Namespace, "--timeout", timeout)
		if err := exec.CommandContext(ctx, "kubectl", args...).Run(); err != nil {
			continue // optional deployments may not exist
		}
	}
	return nil
}

// patchRepoServer mounts the CA certificate into argocd-repo-server.
func patchRepoServer(ctx context.Context, cfg ArgoCDConfig) error {
	patch := `{"spec":{"template":{"spec":{` +
		`"volumes":[{"name":"kinder-ca","secret":{"secretName":"kinder-ca-cert"}}],` +
		`"containers":[{"name":"argocd-repo-server",` +
		`"env":[{"name":"SSL_CERT_FILE","value":"/etc/ssl/certs/kinder-ca.crt"}],` +
		`"volumeMounts":[{"name":"kinder-ca","mountPath":"/etc/ssl/certs/kinder-ca.crt","subPath":"ca.crt","readOnly":true}]}]}}}}`
	return kubectlPatch(ctx, cfg, "deployment", "argocd-repo-server", patch)
}

// --- kubectl helpers ---

func kubectlArgs(cfg ArgoCDConfig, args ...string) []string {
	if cfg.KubeContext != "" {
		args = append([]string{"--context", cfg.KubeContext}, args...)
	}
	if cfg.KubeconfigPath != "" {
		args = append([]string{"--kubeconfig", cfg.KubeconfigPath}, args...)
	}
	return args
}

func kubectl(ctx context.Context, cfg ArgoCDConfig, manifest string) error {
	args := kubectlArgs(cfg, "apply", "-f", "-")
	cmd := exec.CommandContext(ctx, "kubectl", args...)
	cmd.Stdin = strings.NewReader(manifest)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, stderr.String())
	}
	return nil
}

func kubectlURL(ctx context.Context, cfg ArgoCDConfig, url string) error {
	args := kubectlArgs(cfg, "apply", "-f", url, "-n", cfg.Namespace)
	cmd := exec.CommandContext(ctx, "kubectl", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, stderr.String())
	}
	return nil
}

func kubectlPatch(ctx context.Context, cfg ArgoCDConfig, kind, name, patch string) error {
	args := kubectlArgs(cfg, "patch", kind, name, "-n", cfg.Namespace, "--type=strategic", "-p", patch)
	cmd := exec.CommandContext(ctx, "kubectl", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, stderr.String())
	}
	return nil
}

// --- YAML generators ---

func namespaceYAML(ns string) string {
	return fmt.Sprintf(`apiVersion: v1
kind: Namespace
metadata:
  name: %s
  labels:
    app.kubernetes.io/name: argocd
`, ns)
}

func caSecretYAML(cfg ArgoCDConfig) string {
	return fmt.Sprintf(`apiVersion: v1
kind: Secret
metadata:
  name: kinder-ca-cert
  namespace: %s
type: Opaque
stringData:
  ca.crt: |
%s`, cfg.Namespace, indent(cfg.CACertPEM, 4))
}

func repoSecretYAML(cfg ArgoCDConfig) (string, error) {
	var data string
	switch cfg.CredentialType {
	case GitCredentialHTTP:
		if cfg.HTTPUsername == "" || cfg.HTTPPassword == "" {
			return "", fmt.Errorf("HTTP credentials require username and password")
		}
		data = fmt.Sprintf(`data:
  type: %s
  url: %s
  username: %s
  password: %s`,
			b64("git"), b64(cfg.RepoURL), b64(cfg.HTTPUsername), b64(cfg.HTTPPassword))
	case GitCredentialSSH:
		if cfg.SSHPrivateKey == "" {
			return "", fmt.Errorf("SSH credentials require private key")
		}
		data = fmt.Sprintf(`data:
  type: %s
  url: %s
  sshPrivateKey: %s`,
			b64("git"), b64(cfg.RepoURL), b64(cfg.SSHPrivateKey))
	default:
		return "", fmt.Errorf("unsupported credential type: %s", cfg.CredentialType)
	}

	return fmt.Sprintf(`apiVersion: v1
kind: Secret
metadata:
  name: repo-%s
  namespace: %s
  labels:
    argocd.argoproj.io/secret-type: repository
%s
`, repoName(cfg.RepoURL), cfg.Namespace, data), nil
}

func applicationYAML(cfg ArgoCDConfig) (string, error) {
	if err := validateGitURL(cfg.RepoURL); err != nil {
		return "", err
	}
	appName, err := sanitizeName(cfg.AppName)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(`apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: %s
  namespace: %s
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
`, appName, cfg.Namespace, cfg.RepoURL, cfg.RepoBranch, cfg.RepoPath, cfg.TargetNamespace), nil
}

func kinderAppsYAML(cfg ArgoCDConfig) string {
	app := func(name, image, tag string) string {
		return fmt.Sprintf(`apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: %s
  namespace: %s
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
`, name, cfg.Namespace, cfg.ZotRegistryURL, image, tag)
	}
	return app("kinder-trust-bundle", TrustManagerBundleImageName, TrustManagerBundleImageTag) +
		"---\n" +
		app("kinder-cert-issuer", CertManagerIssuerImageName, CertManagerIssuerImageTag)
}

// --- Helpers ---

func setDefaults(cfg *ArgoCDConfig) {
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
	if cfg.Domain == "" {
		cfg.Domain = config.DefaultDomain
	}
	if cfg.Port == "" {
		cfg.Port = config.DefaultTraefikPort
	}
	if cfg.ZotRegistryURL == "" {
		cfg.ZotRegistryURL = fmt.Sprintf("registry.%s:%s", cfg.Domain, cfg.Port)
	}
	if cfg.WaitTimeout == 0 {
		cfg.WaitTimeout = 5 * time.Minute
	}
}

func loadSSHKey(cfg *ArgoCDConfig) error {
	if cfg.SSHPrivateKeyPath != "" && cfg.SSHPrivateKey == "" {
		data, err := os.ReadFile(cfg.SSHPrivateKeyPath)
		if err != nil {
			return fmt.Errorf("read SSH key: %w", err)
		}
		cfg.SSHPrivateKey = string(data)
	}
	return nil
}

func installURL(version string) string {
	return fmt.Sprintf("%s/%s/manifests/install.yaml", ArgoCDInstallURL, version)
}

func sanitizeName(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("name cannot be empty")
	}
	name = strings.ToLower(name)
	if len(name) > 63 {
		name = name[:63]
	}
	name = strings.TrimRight(name, "-")
	if !k8sNameRegex.MatchString(name) {
		return "", fmt.Errorf("invalid name: %q", name)
	}
	return name, nil
}

func validateURL(u string) error {
	parsed, err := url.Parse(u)
	if err != nil {
		return err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("scheme must be http or https")
	}
	if parsed.Host == "" {
		return fmt.Errorf("missing host")
	}
	return nil
}

func validateGitURL(u string) error {
	if u == "" {
		return fmt.Errorf("empty URL")
	}
	if strings.HasPrefix(u, "git@") {
		return nil
	}
	return validateURL(u)
}

func repoName(u string) string {
	if strings.HasPrefix(u, "git@") {
		if parts := strings.Split(u, ":"); len(parts) >= 2 {
			return strings.ToLower(strings.ReplaceAll(strings.TrimSuffix(parts[1], ".git"), "/", "-"))
		}
	}
	if parsed, err := url.Parse(u); err == nil {
		path := strings.TrimPrefix(parsed.Path, "/")
		path = strings.TrimSuffix(path, ".git")
		if path != "" {
			return strings.ToLower(strings.ReplaceAll(path, "/", "-"))
		}
	}
	return "repo"
}

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func indent(s string, n int) string {
	prefix := strings.Repeat(" ", n)
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		if line != "" {
			lines = append(lines, prefix+line)
		}
	}
	return strings.Join(lines, "\n")
}
