package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"codeberg.org/hipkoi/kinder/config"
	"codeberg.org/hipkoi/kinder/kubernetes"
	"github.com/spf13/cobra"
)

var (
	// ArgoCD flags
	argocdVersion         string
	argocdNamespace       string
	argocdRepoURL         string
	argocdRepoPath        string
	argocdRepoBranch      string
	argocdAppName         string
	argocdTargetNamespace string
	argocdManifestURL     string
	argocdIncludeKinder   bool
	argocdWaitTimeout     time.Duration
	argocdSkipApp         bool

	// Git credential flags
	argocdGitUsername   string
	argocdGitPassword   string
	argocdGitSSHKeyPath string

	// Kubectl flags
	argocdKubeconfig string
	argocdContext    string
)

var argocdCmd = &cobra.Command{
	Use:   "argocd",
	Short: "Manage ArgoCD installation and configuration",
	Long: `Commands for installing and configuring ArgoCD in the cluster.

ArgoCD is installed directly to the cluster using kubectl. After installation,
you can configure it to pull from your GitOps repository and optionally
include kinder's OCI-based applications (trust-bundle, cert-issuer).`,
}

var argocdBootstrapCmd = &cobra.Command{
	Use:   "bootstrap",
	Short: "Install ArgoCD with anonymous access",
	Long: `Install ArgoCD to the cluster with anonymous admin access (no login required).

This command:
  1. Creates the argocd namespace
  2. Installs ArgoCD from upstream manifests
  3. Disables authentication (anonymous admin access)
  4. Waits for rollout to complete
  5. (Optional) Mounts CA certificate for private registry access
  6. (Optional) Creates repository credentials for private repos
  7. (Optional) Creates an initial Application pointing to your GitOps repo
  8. (Optional) Creates Applications for kinder OCI bundles
  9. (Optional) Applies additional manifests from a URL (app-of-apps pattern)

Examples:
  # Install ArgoCD only
  kinder argocd bootstrap

  # Install with public GitOps repo
  kinder argocd bootstrap --repo-url https://github.com/org/gitops

  # Install with private repo (HTTPS token)
  kinder argocd bootstrap \
    --repo-url https://github.com/org/private \
    --git-username myuser \
    --git-password ghp_xxxxxxxxxxxx

  # Install with private repo (SSH key)
  kinder argocd bootstrap \
    --repo-url git@github.com:org/private.git \
    --git-ssh-key ~/.ssh/id_ed25519

  # Include kinder OCI apps
  kinder argocd bootstrap --include-kinder-apps

  # Install app-of-apps from URL
  kinder argocd bootstrap \
    --manifest-url https://raw.githubusercontent.com/org/gitops/main/app-of-apps.yaml`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()

		// Resolve version: CLI flag > config file > default
		version := argocdVersion
		if version == "" {
			version = config.GetString(config.KeyArgocdVersion)
		}

		// Resolve manifest URL: CLI flag > config file
		manifestURL := argocdManifestURL
		if manifestURL == "" {
			manifestURL = config.GetString(config.KeyArgocdManifestURL)
		}

		// Determine credential type
		credType := kubernetes.GitCredentialNone
		if argocdGitUsername != "" && argocdGitPassword != "" {
			credType = kubernetes.GitCredentialHTTP
		} else if argocdGitSSHKeyPath != "" {
			credType = kubernetes.GitCredentialSSH
		}

		// Validate credential combinations
		if argocdGitUsername != "" && argocdGitPassword == "" {
			return fmt.Errorf("--git-password is required when --git-username is provided")
		}
		if argocdGitPassword != "" && argocdGitUsername == "" {
			return fmt.Errorf("--git-username is required when --git-password is provided")
		}

		// Load CA cert for registry TLS trust
		dataDir, err := config.GetDataDir()
		if err != nil {
			return fmt.Errorf("failed to get data dir: %w", err)
		}
		caCertPEM, _ := os.ReadFile(dataDir + "/ca.crt")

		cfg := kubernetes.ArgoCDConfig{
			Version:           version,
			Namespace:         argocdNamespace,
			RepoURL:           argocdRepoURL,
			RepoPath:          argocdRepoPath,
			RepoBranch:        argocdRepoBranch,
			AppName:           argocdAppName,
			TargetNamespace:   argocdTargetNamespace,
			ManifestURL:       manifestURL,
			CredentialType:    credType,
			HTTPUsername:      argocdGitUsername,
			HTTPPassword:      argocdGitPassword,
			SSHPrivateKeyPath: argocdGitSSHKeyPath,
			IncludeKinderApps: argocdIncludeKinder,
			SkipInitialApp:    argocdSkipApp || argocdRepoURL == "",
			WaitTimeout:       argocdWaitTimeout,
			KubeconfigPath:    argocdKubeconfig,
			KubeContext:       argocdContext,
			Domain:            config.GetString(config.KeyDomain),
			Port:              config.GetString(config.KeyTraefikPort),
			CACertPEM:         string(caCertPEM),
		}

		Header("Installing ArgoCD...")
		BlankLine()

		if err := kubernetes.Install(ctx, cfg, func(msg string) {
			ProgressStart("", msg)
			ProgressDone(true, "")
		}); err != nil {
			return fmt.Errorf("failed to install ArgoCD: %w", err)
		}

		BlankLine()
		Success("ArgoCD installed with anonymous admin access")
		BlankLine()

		// Show access instructions
		Header("Connect to ArgoCD UI:")
		Output("  kubectl port-forward svc/argocd-server -n %s 8080:80\n", cfg.Namespace)
		Output("  Open: http://localhost:8080\n")
		BlankLine()
		Output("  No login required (anonymous admin access enabled)\n")

		return nil
	},
}

var argocdShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show the ArgoCD installation configuration",
	Long:  `Display the ArgoCD installation URL and configuration that would be applied.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Resolve version: CLI flag > config file > default
		version := argocdVersion
		if version == "" {
			version = config.GetString(config.KeyArgocdVersion)
		}

		// Resolve manifest URL: CLI flag > config file
		manifestURL := argocdManifestURL
		if manifestURL == "" {
			manifestURL = config.GetString(config.KeyArgocdManifestURL)
		}

		Output("# ArgoCD Installation\n\n")
		Output("Version: %s\n", version)
		Output("Namespace: %s\n", argocdNamespace)
		Output("Install URL: %s/%s/manifests/install.yaml\n\n", kubernetes.ArgoCDInstallURL, version)

		Output("# Configuration\n\n")
		Output("Authentication: Anonymous admin access (disabled)\n")
		Output("Server mode: Insecure (no TLS)\n\n")

		if argocdRepoURL != "" {
			Output("# Initial Application\n\n")
			Output("Repository: %s\n", argocdRepoURL)
			Output("Path: %s\n", argocdRepoPath)
			Output("Branch: %s\n", argocdRepoBranch)
			Output("App Name: %s\n", argocdAppName)
			Output("Target Namespace: %s\n\n", argocdTargetNamespace)
		}

		if argocdIncludeKinder {
			Output("# Kinder Apps\n\n")
			Output("Includes: trust-bundle, cert-issuer (OCI from local registry)\n\n")
		}

		if manifestURL != "" {
			Output("# App-of-Apps\n\n")
			Output("Manifest URL: %s\n", manifestURL)
		}

		return nil
	},
}

func init() {
	// Common flags for bootstrap and show
	commonFlags := func(cmd *cobra.Command) {
		cmd.Flags().StringVar(&argocdVersion, "version", "", fmt.Sprintf("ArgoCD version to install (default %s)", config.DefaultArgocdVersion))
		cmd.Flags().StringVar(&argocdNamespace, "namespace", kubernetes.ArgoCDNamespace, "ArgoCD namespace")
		cmd.Flags().StringVar(&argocdRepoURL, "repo-url", "", "Git repository URL for initial app")
		cmd.Flags().StringVar(&argocdRepoPath, "repo-path", ".", "Path within the repository")
		cmd.Flags().StringVar(&argocdRepoBranch, "repo-branch", "main", "Branch to track")
		cmd.Flags().StringVar(&argocdAppName, "app-name", "root", "Name of the initial Application")
		cmd.Flags().StringVar(&argocdTargetNamespace, "target-namespace", "default", "Target namespace for deployed resources")
		cmd.Flags().StringVar(&argocdManifestURL, "manifest-url", "", "URL to fetch and apply additional manifests (app-of-apps pattern)")
		cmd.Flags().StringVar(&argocdGitUsername, "git-username", "", "Git username for HTTP auth")
		cmd.Flags().StringVar(&argocdGitPassword, "git-password", "", "Git password or token for HTTP auth")
		cmd.Flags().StringVar(&argocdGitSSHKeyPath, "git-ssh-key", "", "Path to SSH private key file")
		cmd.Flags().BoolVar(&argocdIncludeKinder, "include-kinder-apps", false, "Include Applications for trust-bundle and cert-issuer")
		cmd.Flags().BoolVar(&argocdSkipApp, "skip-app", false, "Skip creating the initial application")
	}

	// Setup flags for bootstrap command
	commonFlags(argocdBootstrapCmd)
	argocdBootstrapCmd.Flags().DurationVar(&argocdWaitTimeout, "wait-timeout", 5*time.Minute, "Timeout for rollout")
	argocdBootstrapCmd.Flags().StringVar(&argocdKubeconfig, "kubeconfig", "", "Path to kubeconfig file")
	argocdBootstrapCmd.Flags().StringVar(&argocdContext, "context", "", "Kubernetes context to use")

	// Setup flags for show command
	commonFlags(argocdShowCmd)

	// Add subcommands
	argocdCmd.AddCommand(argocdBootstrapCmd)
	argocdCmd.AddCommand(argocdShowCmd)
}
