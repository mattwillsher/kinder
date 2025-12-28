package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"codeberg.org/hipkoi/kinder/config"
	"codeberg.org/hipkoi/kinder/docker"
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
	argocdIncludeKinder   bool
	argocdWait            bool
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
	Short: "Install ArgoCD and configure initial application",
	Long: `Install ArgoCD to the cluster and optionally configure an initial Application.

This command:
  1. Creates the argocd namespace
  2. Installs ArgoCD from upstream manifests
  3. (Optional) Waits for ArgoCD to be ready
  4. (Optional) Creates repository credentials for private repos
  5. (Optional) Creates an initial Application pointing to your GitOps repo
  6. (Optional) Creates Applications for kinder OCI bundles

Examples:
  # Install ArgoCD only (no initial app)
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

  # Include kinder OCI apps and wait for ready
  kinder argocd bootstrap \
    --repo-url https://github.com/org/gitops \
    --include-kinder-apps \
    --wait`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()

		// Resolve version: CLI flag > config file > default
		version := argocdVersion
		if version == "" {
			version = config.GetString(config.KeyArgocdVersion)
		}

		// Determine credential type
		credType := docker.GitCredentialNone
		if argocdGitUsername != "" && argocdGitPassword != "" {
			credType = docker.GitCredentialHTTP
		} else if argocdGitSSHKeyPath != "" {
			credType = docker.GitCredentialSSH
		}

		// Validate credential combinations
		if argocdGitUsername != "" && argocdGitPassword == "" {
			return fmt.Errorf("--git-password is required when --git-username is provided")
		}
		if argocdGitPassword != "" && argocdGitUsername == "" {
			return fmt.Errorf("--git-username is required when --git-password is provided")
		}

		cfg := docker.ArgoCDConfig{
			Version:           version,
			Namespace:         argocdNamespace,
			RepoURL:           argocdRepoURL,
			RepoPath:          argocdRepoPath,
			RepoBranch:        argocdRepoBranch,
			AppName:           argocdAppName,
			TargetNamespace:   argocdTargetNamespace,
			CredentialType:    credType,
			HTTPUsername:      argocdGitUsername,
			HTTPPassword:      argocdGitPassword,
			SSHPrivateKeyPath: argocdGitSSHKeyPath,
			IncludeKinderApps: argocdIncludeKinder,
			SkipInitialApp:    argocdSkipApp || argocdRepoURL == "",
			WaitReady:         argocdWait,
			WaitTimeout:       argocdWaitTimeout,
			KubeconfigPath:    argocdKubeconfig,
			KubeContext:       argocdContext,
		}

		Header("Bootstrapping ArgoCD...")
		BlankLine()

		// Progress callback
		progressFn := func(step, message string) {
			switch step {
			case "namespace":
				ProgressStart("📁", message)
				ProgressDone(true, "")
			case "install":
				ProgressStart("📦", message)
			case "wait":
				ProgressDone(true, "Installed")
				ProgressStart("⏳", message)
			case "secret":
				if !argocdWait {
					ProgressDone(true, "Installed")
				} else {
					ProgressDone(true, "Ready")
				}
				ProgressStart("🔑", message)
				ProgressDone(true, "")
			case "app":
				if cfg.CredentialType == docker.GitCredentialNone {
					if !argocdWait {
						ProgressDone(true, "Installed")
					} else {
						ProgressDone(true, "Ready")
					}
				}
				ProgressStart("📱", message)
				ProgressDone(true, "")
			case "kinder":
				ProgressStart("🔧", message)
				ProgressDone(true, "")
			}
		}

		if err := docker.BootstrapArgoCD(ctx, cfg, progressFn); err != nil {
			return fmt.Errorf("failed to bootstrap ArgoCD: %w", err)
		}

		// Final progress done if we haven't printed it yet
		if !argocdWait && argocdRepoURL == "" && !argocdIncludeKinder {
			ProgressDone(true, "Installed")
		}

		BlankLine()
		Success("ArgoCD bootstrapped successfully!")
		BlankLine()

		// Get and display admin password
		password, err := docker.GetArgoCDPassword(ctx, cfg)
		if err == nil && password != "" {
			Header("Access:")
			Output("  Username: admin\n")
			Output("  Password: %s\n", password)
			BlankLine()
		}

		// Show port-forward instructions
		Header("Connect to ArgoCD UI:")
		appName := config.GetString(config.KeyAppName)
		if appName == "" {
			appName = config.DefaultAppName
		}
		Output("  kubectl port-forward svc/argocd-server -n %s 8080:443\n", cfg.Namespace)
		Output("  Open: https://localhost:8080\n")

		return nil
	},
}

var argocdShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show the ArgoCD manifests that would be applied",
	Long:  `Display the Kubernetes manifests that would be applied during bootstrap.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Resolve version: CLI flag > config file > default
		version := argocdVersion
		if version == "" {
			version = config.GetString(config.KeyArgocdVersion)
		}

		// Determine credential type
		credType := docker.GitCredentialNone
		if argocdGitUsername != "" && argocdGitPassword != "" {
			credType = docker.GitCredentialHTTP
		} else if argocdGitSSHKeyPath != "" {
			credType = docker.GitCredentialSSH
		}

		cfg := docker.ArgoCDConfig{
			Version:           version,
			Namespace:         argocdNamespace,
			RepoURL:           argocdRepoURL,
			RepoPath:          argocdRepoPath,
			RepoBranch:        argocdRepoBranch,
			AppName:           argocdAppName,
			TargetNamespace:   argocdTargetNamespace,
			CredentialType:    credType,
			HTTPUsername:      argocdGitUsername,
			HTTPPassword:      argocdGitPassword,
			SSHPrivateKeyPath: argocdGitSSHKeyPath,
			IncludeKinderApps: argocdIncludeKinder,
			SkipInitialApp:    argocdSkipApp || argocdRepoURL == "",
		}

		// Load SSH key if provided
		if cfg.SSHPrivateKeyPath != "" {
			keyData, err := os.ReadFile(cfg.SSHPrivateKeyPath)
			if err != nil {
				return fmt.Errorf("failed to read SSH key: %w", err)
			}
			cfg.SSHPrivateKey = string(keyData)
		}

		manifests, err := docker.GenerateArgoCDManifests(cfg)
		if err != nil {
			return fmt.Errorf("failed to generate manifests: %w", err)
		}

		// Print install URL
		Output("# ArgoCD Installation (applied from URL)\n")
		Output("# %s/%s/manifests/install.yaml\n\n", docker.ArgoCDInstallURL, cfg.Version)

		Output("# namespace.yaml\n")
		Output("---\n%s\n", string(manifests.Namespace))

		if len(manifests.RepoSecret) > 0 {
			Output("# repository-secret.yaml (credentials masked)\n")
			Output("---\n")
			Output("# Secret contains Git credentials - not displayed for security\n\n")
		}

		if len(manifests.InitialApp) > 0 {
			Output("# initial-app.yaml\n")
			Output("---\n%s", string(manifests.InitialApp))
		}

		if len(manifests.KinderApps) > 0 {
			Output("\n# kinder-apps.yaml\n")
			Output("---\n%s", string(manifests.KinderApps))
		}

		return nil
	},
}

var argocdPasswordCmd = &cobra.Command{
	Use:   "password",
	Short: "Get the ArgoCD admin password",
	Long:  `Retrieve the initial admin password from the argocd-initial-admin-secret.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()

		cfg := docker.ArgoCDConfig{
			Namespace:      argocdNamespace,
			KubeconfigPath: argocdKubeconfig,
			KubeContext:    argocdContext,
		}

		password, err := docker.GetArgoCDPassword(ctx, cfg)
		if err != nil {
			return fmt.Errorf("failed to get password: %w", err)
		}

		Output("%s\n", password)
		return nil
	},
}

func init() {
	// Common flags for bootstrap and show
	commonFlags := func(cmd *cobra.Command) {
		cmd.Flags().StringVar(&argocdVersion, "version", "", fmt.Sprintf("ArgoCD version to install (default %s)", config.DefaultArgocdVersion))
		cmd.Flags().StringVar(&argocdNamespace, "namespace", docker.ArgoCDNamespace, "ArgoCD namespace")
		cmd.Flags().StringVar(&argocdRepoURL, "repo-url", "", "Git repository URL for initial app")
		cmd.Flags().StringVar(&argocdRepoPath, "repo-path", ".", "Path within the repository")
		cmd.Flags().StringVar(&argocdRepoBranch, "repo-branch", "main", "Branch to track")
		cmd.Flags().StringVar(&argocdAppName, "app-name", "root", "Name of the initial Application")
		cmd.Flags().StringVar(&argocdTargetNamespace, "target-namespace", "default", "Target namespace for deployed resources")
		cmd.Flags().StringVar(&argocdGitUsername, "git-username", "", "Git username for HTTP auth")
		cmd.Flags().StringVar(&argocdGitPassword, "git-password", "", "Git password or token for HTTP auth")
		cmd.Flags().StringVar(&argocdGitSSHKeyPath, "git-ssh-key", "", "Path to SSH private key file")
		cmd.Flags().BoolVar(&argocdIncludeKinder, "include-kinder-apps", false, "Include Applications for trust-bundle and cert-issuer")
		cmd.Flags().BoolVar(&argocdSkipApp, "skip-app", false, "Skip creating the initial application")
	}

	// Setup flags for bootstrap command
	commonFlags(argocdBootstrapCmd)
	argocdBootstrapCmd.Flags().BoolVar(&argocdWait, "wait", false, "Wait for ArgoCD to be ready")
	argocdBootstrapCmd.Flags().DurationVar(&argocdWaitTimeout, "wait-timeout", 5*time.Minute, "Timeout for waiting")
	argocdBootstrapCmd.Flags().StringVar(&argocdKubeconfig, "kubeconfig", "", "Path to kubeconfig file")
	argocdBootstrapCmd.Flags().StringVar(&argocdContext, "context", "", "Kubernetes context to use")

	// Setup flags for show command
	commonFlags(argocdShowCmd)

	// Setup flags for password command
	argocdPasswordCmd.Flags().StringVar(&argocdNamespace, "namespace", docker.ArgoCDNamespace, "ArgoCD namespace")
	argocdPasswordCmd.Flags().StringVar(&argocdKubeconfig, "kubeconfig", "", "Path to kubeconfig file")
	argocdPasswordCmd.Flags().StringVar(&argocdContext, "context", "", "Kubernetes context to use")

	// Add subcommands
	argocdCmd.AddCommand(argocdBootstrapCmd)
	argocdCmd.AddCommand(argocdShowCmd)
	argocdCmd.AddCommand(argocdPasswordCmd)
}
