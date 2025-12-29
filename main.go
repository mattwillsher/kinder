package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"codeberg.org/hipkoi/kinder/cacert"
	"codeberg.org/hipkoi/kinder/config"
	"codeberg.org/hipkoi/kinder/docker"
	"codeberg.org/hipkoi/kinder/kubernetes"
	"github.com/spf13/cobra"
)

const (
	projectName = "kinder"
)

var (
	// Config file path (can be overridden with --config flag)
	configPath string
	// Data directory path (can be overridden with --data-dir flag)
	dataDir string
	// Verbose flag for increased output
	verbose bool

	// CLI flag variables (these get bound to Viper)
	certPath             string
	keyPath              string
	networkCIDR          string
	networkName          string
	stepCAContainerName  string
	stepCAImage          string
	zotImage             string
	zotContainerName     string
	gatusImage           string
	gatusContainerName   string
	traefikImage         string
	traefikContainerName string
	traefikPort          string
	traefikDomain        string
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:   projectName,
	Short: "A tool to stand up a local kind cluster and support services",
	Long: `kinder is a CLI tool that manages a local Kubernetes development environment
with Kind, including a Step CA certificate authority, Zot registry, Gatus health
dashboard, and Traefik reverse proxy.`,
	CompletionOptions: cobra.CompletionOptions{
		DisableDefaultCmd: false,
	},
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Set verbosity level based on flag
		if verbose {
			SetVerbosity(VerbosityVerbose)
		}

		// Initialize Viper with config file and environment variables
		if err := config.Initialize(configPath); err != nil {
			return fmt.Errorf("failed to initialize config: %w", err)
		}

		// Bind CLI flags to Viper (flags take highest precedence)
		bindFlagsToViper(cmd)

		return nil
	},
	PersistentPostRunE: func(cmd *cobra.Command, args []string) error {
		// Clean up shared Docker client to prevent connection leaks
		return docker.CloseSharedClient()
	},
}

var completionCmd = &cobra.Command{
	Use:   "completion [bash|zsh|fish|powershell]",
	Short: "Generate completion script",
	Long: `To load completions:

Bash:

  $ source <(kinder completion bash)

  # To load completions for each session, execute once:
  # Linux:
  $ kinder completion bash > /etc/bash_completion.d/kinder
  # macOS:
  $ kinder completion bash > $(brew --prefix)/etc/bash_completion.d/kinder

Zsh:

  # If shell completion is not already enabled in your environment,
  # you will need to enable it.  You can execute the following once:

  $ echo "autoload -U compinit; compinit" >> ~/.zshrc

  # To load completions for each session, execute once:
  $ kinder completion zsh > "${fpath[1]}/_kinder"

  # You will need to start a new shell for this setup to take effect.

Fish:

  $ kinder completion fish | source

  # To load completions for each session, execute once:
  $ kinder completion fish > ~/.config/fish/completions/kinder.fish

PowerShell:

  PS> kinder completion powershell | Out-String | Invoke-Expression

  # To load completions for every new session, run:
  PS> kinder completion powershell > kinder.ps1
  # and source this file from your PowerShell profile.
`,
	DisableFlagsInUseLine: true,
	ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
	Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
	RunE: func(cmd *cobra.Command, args []string) error {
		var err error
		switch args[0] {
		case "bash":
			err = cmd.Root().GenBashCompletion(os.Stdout)
		case "zsh":
			err = cmd.Root().GenZshCompletion(os.Stdout)
		case "fish":
			err = cmd.Root().GenFishCompletion(os.Stdout, true)
		case "powershell":
			err = cmd.Root().GenPowerShellCompletionWithDesc(os.Stdout)
		}
		return err
	},
}

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start all kinder services",
	Long:  `Start the network and all kinder service containers in the correct order.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()

		// Set defaults for Traefik configuration
		if traefikPort == "" {
			traefikPort = docker.DefaultTraefikPort
		}
		if traefikDomain == "" {
			traefikDomain = docker.DefaultTraefikDomain
		}

		// Get data directory
		dataDir, err := getDataDir()
		if err != nil {
			return fmt.Errorf("failed to get data directory: %w", err)
		}

		// Set default cert paths if not provided
		if certPath == "" {
			certPath = filepath.Join(dataDir, CACertFilename)
		}
		if keyPath == "" {
			keyPath = filepath.Join(dataDir, CAKeyFilename)
		}

		Header("Starting kinder...")
		if !IsVerbose() {
			BlankLine()
		}

		// Check if CA certificate exists, generate if not
		ProgressStart("🔐", "CA certificate")
		if _, err := os.Stat(certPath); os.IsNotExist(err) {
			// Ensure the directory exists
			certDir := filepath.Dir(certPath)
			if err := os.MkdirAll(certDir, 0755); err != nil {
				ProgressDone(false, err.Error())
				return fmt.Errorf("failed to create directory %s: %w", certDir, err)
			}

			keyDir := filepath.Dir(keyPath)
			if err := os.MkdirAll(keyDir, 0755); err != nil {
				ProgressDone(false, err.Error())
				return fmt.Errorf("failed to create directory %s: %w", keyDir, err)
			}

			// Generate the CA certificate with domain constraints
			if err := cacert.GenerateCAWithDomain(certPath, keyPath, traefikDomain); err != nil {
				ProgressDone(false, err.Error())
				return fmt.Errorf("failed to generate CA certificate: %w", err)
			}
			ProgressDone(true, "generated")
		} else {
			ProgressDone(true, "exists")
		}
		Verbose("\n")

		// Step 1: Create network
		ProgressStart("📡", "Network")
		networkExists, err := docker.NetworkExists(ctx, networkName)
		if err != nil {
			ProgressDone(false, err.Error())
			return fmt.Errorf("failed to check if network exists: %w", err)
		}

		if !networkExists {
			netConfig := docker.NetworkConfig{
				Name:       networkName,
				CIDR:       networkCIDR,
				Driver:     "bridge",
				BridgeName: networkName + "br0",
			}
			networkID, err := docker.CreateNetwork(ctx, netConfig)
			if err != nil {
				ProgressDone(false, err.Error())
				return fmt.Errorf("failed to create network: %w", err)
			}
			ProgressDone(true, fmt.Sprintf("Created '%s' (ID: %s, CIDR: %s)", networkName, networkID[:12], networkCIDR))
		} else {
			ProgressDone(true, fmt.Sprintf("'%s' exists", networkName))
		}
		Verbose("\n")

		// Step 2: Start Step CA
		ProgressStart("🔐", "Step CA")
		if err := startStepCA(ctx); err != nil {
			ProgressDone(false, err.Error())
			return fmt.Errorf("failed to start Step CA: %w", err)
		}
		ProgressDone(true, "Running")
		Verbose("\n")

		// Step 3: Start Zot Registry
		ProgressStart("📦", "Zot Registry")
		if err := startZot(ctx); err != nil {
			ProgressDone(false, err.Error())
			return fmt.Errorf("failed to start Zot: %w", err)
		}
		// Wait for Zot to be ready before pushing images
		if err := docker.WaitForZot(ctx, 30*time.Second); err != nil {
			ProgressDone(false, err.Error())
			return fmt.Errorf("Zot registry not ready: %w", err)
		}
		ProgressDone(true, "Running")
		Verbose("\n")

		// Step 3.5: Push trust bundle to registry
		ProgressStart("🔐", "Trust Bundle")
		if err := pushTrustBundle(ctx, certPath); err != nil {
			ProgressDone(false, err.Error())
			return fmt.Errorf("failed to push trust bundle: %w", err)
		}
		ProgressDone(true, "Pushed")
		Verbose("\n")

		// Step 3.6: Push cert-manager issuer to registry
		ProgressStart("📜", "Cert Issuer")
		if err := pushCertManagerIssuer(ctx, certPath); err != nil {
			ProgressDone(false, err.Error())
			return fmt.Errorf("failed to push cert-manager issuer: %w", err)
		}
		ProgressDone(true, "Pushed")
		Verbose("\n")

		// Step 4: Start Gatus
		ProgressStart("📊", "Gatus")
		if err := startGatus(ctx); err != nil {
			ProgressDone(false, err.Error())
			return fmt.Errorf("failed to start Gatus: %w", err)
		}
		ProgressDone(true, "Running")
		Verbose("\n")

		// Step 5: Start Traefik
		ProgressStart("🔀", "Traefik")
		if err := startTraefik(ctx); err != nil {
			ProgressDone(false, err.Error())
			return fmt.Errorf("failed to start Traefik: %w", err)
		}
		ProgressDone(true, "Running")
		Verbose("\n")

		// Step 6: Start Kind cluster
		ProgressStart("☸️", "Kind cluster")
		if err := startKind(ctx); err != nil {
			ProgressDone(false, err.Error())
			return fmt.Errorf("failed to start Kind: %w", err)
		}
		ProgressDone(true, "Running")
		Verbose("\n")

		// Step 7: Bootstrap ArgoCD
		ProgressStart("🐙", "ArgoCD")
		if err := bootstrapArgoCD(ctx); err != nil {
			ProgressDone(false, err.Error())
			return fmt.Errorf("failed to bootstrap ArgoCD: %w", err)
		}
		ProgressDone(true, "Running")

		BlankLine()

		appName := config.GetString(config.KeyAppName)
		if appName == "" {
			appName = config.DefaultAppName
		}

		Success("All services started")
		BlankLine()
		Header("Endpoints:")
		ServiceInfo("Traefik", fmt.Sprintf("https://traefik.%s:%s", traefikDomain, traefikPort))
		ServiceInfo("Step CA", fmt.Sprintf("https://ca.%s:%s", traefikDomain, traefikPort))
		ServiceInfo("Registry", fmt.Sprintf("https://registry.%s:%s", traefikDomain, traefikPort))
		ServiceInfo("Gatus", fmt.Sprintf("https://gatus.%s:%s", traefikDomain, traefikPort))
		ServiceInfo("Zot (direct)", "http://localhost:5000")
		ServiceInfo("ArgoCD", "kubectl port-forward svc/argocd-server -n argocd 8080:443")
		BlankLine()
		Output("kubectl cluster-info --context kind-%s\n", appName)

		return nil
	},
}

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop all kinder services",
	Long:  `Stop and remove all kinder service containers and network.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()

		Header("Stopping kinder...")
		if !IsVerbose() {
			BlankLine()
		}

		// Collect errors but continue stopping all services (best effort)
		var errs []string

		// Step 1: Stop Kind cluster first (before other services)
		ProgressStart("☸️", "Kind cluster")
		if err := stopKind(ctx); err != nil {
			ProgressDone(false, err.Error())
			errs = append(errs, fmt.Sprintf("kind: %v", err))
		} else {
			ProgressDone(true, "Stopped")
		}
		Verbose("\n")

		// Step 2: Stop Traefik
		ProgressStart("🔀", "Traefik")
		if err := stopTraefik(ctx); err != nil {
			ProgressDone(false, err.Error())
			errs = append(errs, fmt.Sprintf("traefik: %v", err))
		} else {
			ProgressDone(true, "Stopped")
		}
		Verbose("\n")

		// Step 3: Stop Gatus
		ProgressStart("📊", "Gatus")
		if err := stopGatus(ctx); err != nil {
			ProgressDone(false, err.Error())
			errs = append(errs, fmt.Sprintf("gatus: %v", err))
		} else {
			ProgressDone(true, "Stopped")
		}
		Verbose("\n")

		// Step 4: Stop Zot
		ProgressStart("📦", "Zot Registry")
		if err := stopZot(ctx); err != nil {
			ProgressDone(false, err.Error())
			errs = append(errs, fmt.Sprintf("zot: %v", err))
		} else {
			ProgressDone(true, "Stopped")
		}
		Verbose("\n")

		// Step 5: Stop Step CA
		ProgressStart("🔐", "Step CA")
		if err := stopStepCA(ctx); err != nil {
			ProgressDone(false, err.Error())
			errs = append(errs, fmt.Sprintf("stepca: %v", err))
		} else {
			ProgressDone(true, "Stopped")
		}
		Verbose("\n")

		// Step 6: Remove network
		ProgressStart("📡", "Network")
		networkExists, err := docker.NetworkExists(ctx, networkName)
		if err != nil {
			ProgressDone(false, err.Error())
			errs = append(errs, fmt.Sprintf("network check: %v", err))
		} else if networkExists {
			if err := docker.RemoveNetwork(ctx, networkName); err != nil {
				ProgressDone(false, err.Error())
				errs = append(errs, fmt.Sprintf("network remove: %v", err))
			} else {
				ProgressDone(true, "Removed")
			}
		} else {
			ProgressDone(true, "Not present")
		}

		BlankLine()

		// Return combined error if any failures occurred
		if len(errs) > 0 {
			Success("Stop completed with errors")
			return fmt.Errorf("some services failed to stop: %s", fmt.Sprintf("[%s]", joinErrors(errs)))
		}

		Success("All services stopped")
		return nil
	},
}

var restartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart all kinder services",
	Long:  `Restart all kinder service containers to apply configuration changes.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()

		Header("Restarting kinder...")
		if !IsVerbose() {
			BlankLine()
		}

		// Stop containers (but not the network)
		Verbose("Stopping services...\n")

		// Step 1: Stop Kind cluster first
		ProgressStart("☸️", "Kind cluster")
		if err := stopKind(ctx); err != nil {
			ProgressDone(false, err.Error())
		} else {
			ProgressDone(true, "Stopped")
		}
		Verbose("\n")

		// Step 2: Stop Traefik
		ProgressStart("🔀", "Traefik")
		if err := stopTraefik(ctx); err != nil {
			ProgressDone(false, err.Error())
		} else {
			ProgressDone(true, "Stopped")
		}
		Verbose("\n")

		// Step 3: Stop Gatus
		ProgressStart("📊", "Gatus")
		if err := stopGatus(ctx); err != nil {
			ProgressDone(false, err.Error())
		} else {
			ProgressDone(true, "Stopped")
		}
		Verbose("\n")

		// Step 4: Stop Zot
		ProgressStart("📦", "Zot Registry")
		if err := stopZot(ctx); err != nil {
			ProgressDone(false, err.Error())
		} else {
			ProgressDone(true, "Stopped")
		}
		Verbose("\n")

		// Step 5: Stop Step CA
		ProgressStart("🔐", "Step CA")
		if err := stopStepCA(ctx); err != nil {
			ProgressDone(false, err.Error())
		} else {
			ProgressDone(true, "Stopped")
		}
		Verbose("\n")

		Verbose("Starting services...\n")

		// Start containers with updated configs
		// Step 1: Start Step CA
		ProgressStart("🔐", "Step CA")
		if err := startStepCA(ctx); err != nil {
			ProgressDone(false, err.Error())
			return fmt.Errorf("failed to start Step CA: %w", err)
		}
		ProgressDone(true, "Running")
		Verbose("\n")

		// Step 2: Start Zot Registry
		ProgressStart("📦", "Zot Registry")
		if err := startZot(ctx); err != nil {
			ProgressDone(false, err.Error())
			return fmt.Errorf("failed to start Zot: %w", err)
		}
		// Wait for Zot to be ready before pushing images
		if err := docker.WaitForZot(ctx, 30*time.Second); err != nil {
			ProgressDone(false, err.Error())
			return fmt.Errorf("Zot registry not ready: %w", err)
		}
		ProgressDone(true, "Running")
		Verbose("\n")

		// Step 2.5: Push trust bundle
		ProgressStart("🔐", "Trust Bundle")
		if err := pushTrustBundle(ctx, certPath); err != nil {
			ProgressDone(false, err.Error())
			return fmt.Errorf("failed to push trust bundle: %w", err)
		}
		ProgressDone(true, "Pushed")
		Verbose("\n")

		// Step 2.6: Push cert-manager issuer
		ProgressStart("📜", "Cert Issuer")
		if err := pushCertManagerIssuer(ctx, certPath); err != nil {
			ProgressDone(false, err.Error())
			return fmt.Errorf("failed to push cert-manager issuer: %w", err)
		}
		ProgressDone(true, "Pushed")
		Verbose("\n")

		// Step 3: Start Gatus
		ProgressStart("📊", "Gatus")
		if err := startGatus(ctx); err != nil {
			ProgressDone(false, err.Error())
			return fmt.Errorf("failed to start Gatus: %w", err)
		}
		ProgressDone(true, "Running")
		Verbose("\n")

		// Step 4: Start Traefik
		ProgressStart("🔀", "Traefik")
		if err := startTraefik(ctx); err != nil {
			ProgressDone(false, err.Error())
			return fmt.Errorf("failed to start Traefik: %w", err)
		}
		ProgressDone(true, "Running")
		Verbose("\n")

		// Step 5: Start Kind cluster
		ProgressStart("☸️", "Kind cluster")
		if err := startKind(ctx); err != nil {
			ProgressDone(false, err.Error())
			return fmt.Errorf("failed to start Kind: %w", err)
		}
		ProgressDone(true, "Running")
		Verbose("\n")

		// Step 6: Bootstrap ArgoCD
		ProgressStart("🐙", "ArgoCD")
		if err := bootstrapArgoCD(ctx); err != nil {
			ProgressDone(false, err.Error())
			return fmt.Errorf("failed to bootstrap ArgoCD: %w", err)
		}
		ProgressDone(true, "Running")

		BlankLine()

		appName := config.GetString(config.KeyAppName)
		if appName == "" {
			appName = config.DefaultAppName
		}

		Success("All services restarted")
		BlankLine()
		Header("Endpoints:")
		ServiceInfo("Traefik", fmt.Sprintf("https://traefik.%s:%s", traefikDomain, traefikPort))
		ServiceInfo("Step CA", fmt.Sprintf("https://ca.%s:%s", traefikDomain, traefikPort))
		ServiceInfo("Registry", fmt.Sprintf("https://registry.%s:%s", traefikDomain, traefikPort))
		ServiceInfo("Gatus", fmt.Sprintf("https://gatus.%s:%s", traefikDomain, traefikPort))
		ServiceInfo("Zot (direct)", "http://localhost:5000")
		ServiceInfo("ArgoCD", "kubectl port-forward svc/argocd-server -n argocd 8080:443")
		BlankLine()
		Output("kubectl cluster-info --context kind-%s\n", appName)

		return nil
	},
}

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Remove all kinder data",
	Long:  `Remove all kinder configuration and data files. This will delete the CA certificate, container data, and all generated configurations.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()

		cfg, err := buildConfigFromFlags()
		if err != nil {
			return fmt.Errorf("failed to build config: %w", err)
		}

		// Check if any containers are running
		runningContainers := []string{}
		for _, name := range cfg.AllContainerNames() {
			exists, err := docker.ContainerExists(ctx, name)
			if err == nil && exists {
				runningContainers = append(runningContainers, name)
			}
		}

		if len(runningContainers) > 0 {
			Error("❌ Cannot clean while containers are running\n")
			ErrorLn()
			ErrorLn("Running containers:")
			for _, name := range runningContainers {
				Error("  - %s\n", name)
			}
			ErrorLn()
			ErrorLn("Please run 'kinder stop' first to stop all containers and clean data.")
			return fmt.Errorf("containers are still running")
		}

		// Check if directory exists
		if _, err := os.Stat(cfg.DataDir); os.IsNotExist(err) {
			Output("No kinder data found to clean\n")
			return nil
		}

		Section("🧹", "Cleaning kinder data")
		Verbose("Directory: %s\n", cfg.DataDir)
		BlankLine()

		// Remove the entire data directory
		if err := os.RemoveAll(cfg.DataDir); err != nil {
			return fmt.Errorf("failed to remove data directory: %w", err)
		}

		Success("All kinder data removed successfully!")

		return nil
	},
}

func init() {
	// Global flags
	rootCmd.PersistentFlags().StringVar(&configPath, "config", "", "Path to config file (default: $XDG_CONFIG_HOME/kinder/config.yaml)")
	rootCmd.PersistentFlags().StringVar(&dataDir, "data-dir", "", "Path to data directory (default: $XDG_DATA_HOME/kinder)")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output")

	// Setup flags for generate command
	generateCmd.Flags().StringVar(&certPath, "cert", "", "Path to save the CA certificate (default: $XDG_DATA_HOME/kinder/ca.crt)")
	generateCmd.Flags().StringVar(&keyPath, "key", "", "Path to save the CA private key (default: $XDG_DATA_HOME/kinder/ca.key)")
	generateCmd.Flags().StringVar(&traefikDomain, "domain", docker.DefaultTraefikDomain, "Domain for name constraints")

	// Setup flags for print command
	printCmd.Flags().StringVar(&certPath, "cert", "", "Path to the CA certificate (default: $XDG_DATA_HOME/kinder/ca.crt)")
	printCmd.Flags().StringVar(&keyPath, "key", "", "Path to the CA private key (default: $XDG_DATA_HOME/kinder/ca.key)")

	// Add commands to ca
	caCmd.AddCommand(generateCmd)
	caCmd.AddCommand(printCmd)

	// Setup flags for network commands
	networkCreateCmd.Flags().StringVar(&networkCIDR, "cidr", docker.DefaultNetworkCIDR, "CIDR for the network")
	networkCreateCmd.Flags().StringVar(&networkName, "name", docker.DefaultNetworkName, "Name of the network")

	networkRemoveCmd.Flags().StringVar(&networkName, "name", docker.DefaultNetworkName, "Name of the network to remove")

	// Add commands to network
	networkCmd.AddCommand(networkCreateCmd)
	networkCmd.AddCommand(networkRemoveCmd)

	// Setup flags for Step CA commands
	stepCAStartCmd.Flags().StringVar(&certPath, "cert", "", "Path to the CA certificate (default: $XDG_DATA_HOME/kinder/ca.crt)")
	stepCAStartCmd.Flags().StringVar(&keyPath, "key", "", "Path to the CA private key (default: $XDG_DATA_HOME/kinder/ca.key)")
	stepCAStartCmd.Flags().StringVar(&networkName, "network", docker.DefaultNetworkName, "Docker network name")
	stepCAStartCmd.Flags().StringVar(&stepCAContainerName, "name", docker.StepCAContainerName, "Container name")
	stepCAStartCmd.Flags().StringVar(&stepCAImage, "image", docker.StepCAImage, "Step CA Docker image")

	stepCAStopCmd.Flags().StringVar(&stepCAContainerName, "name", docker.StepCAContainerName, "Container name")

	// Add commands to stepca
	stepCACmd.AddCommand(stepCAStartCmd)
	stepCACmd.AddCommand(stepCAStopCmd)

	// Setup flags for Zot commands
	zotStartCmd.Flags().StringVar(&networkName, "network", docker.DefaultNetworkName, "Docker network name")
	zotStartCmd.Flags().StringVar(&zotContainerName, "name", docker.ZotContainerName, "Container name")
	zotStartCmd.Flags().StringVar(&zotImage, "image", docker.ZotImage, "Zot Docker image")

	zotStopCmd.Flags().StringVar(&zotContainerName, "name", docker.ZotContainerName, "Container name")

	// Add commands to zot
	zotCmd.AddCommand(zotStartCmd)
	zotCmd.AddCommand(zotStopCmd)

	// Setup flags for Gatus commands
	gatusStartCmd.Flags().StringVar(&networkName, "network", docker.DefaultNetworkName, "Docker network name")
	gatusStartCmd.Flags().StringVar(&gatusContainerName, "name", docker.GatusContainerName, "Container name")
	gatusStartCmd.Flags().StringVar(&gatusImage, "image", docker.GatusImage, "Gatus Docker image")

	gatusStopCmd.Flags().StringVar(&gatusContainerName, "name", docker.GatusContainerName, "Container name")

	// Add commands to gatus
	gatusCmd.AddCommand(gatusStartCmd)
	gatusCmd.AddCommand(gatusStopCmd)

	// Setup flags for Traefik commands
	traefikStartCmd.Flags().StringVar(&networkName, "network", docker.DefaultNetworkName, "Docker network name")
	traefikStartCmd.Flags().StringVar(&traefikContainerName, "name", docker.TraefikContainerName, "Container name")
	traefikStartCmd.Flags().StringVar(&traefikImage, "image", docker.TraefikImage, "Traefik Docker image")
	traefikStartCmd.Flags().StringVar(&traefikPort, "port", docker.DefaultTraefikPort, "Localhost HTTPS port")
	traefikStartCmd.Flags().StringVar(&traefikDomain, "domain", docker.DefaultTraefikDomain, "Base domain for services")

	traefikStopCmd.Flags().StringVar(&traefikContainerName, "name", docker.TraefikContainerName, "Container name")

	// Add commands to traefik
	traefikCmd.AddCommand(traefikStartCmd)
	traefikCmd.AddCommand(traefikStopCmd)

	// Setup flags for container commands
	containerStartCmd.Flags().StringVar(&certPath, "cert", "", "Path to the CA certificate (for stepca)")
	containerStartCmd.Flags().StringVar(&keyPath, "key", "", "Path to the CA private key (for stepca)")
	containerStartCmd.Flags().StringVar(&networkName, "network", docker.DefaultNetworkName, "Docker network name")
	containerStartCmd.Flags().StringVar(&stepCAContainerName, "stepca-name", docker.StepCAContainerName, "Step CA container name")
	containerStartCmd.Flags().StringVar(&zotContainerName, "zot-name", docker.ZotContainerName, "Zot container name")
	containerStartCmd.Flags().StringVar(&gatusContainerName, "gatus-name", docker.GatusContainerName, "Gatus container name")
	containerStartCmd.Flags().StringVar(&traefikContainerName, "traefik-name", docker.TraefikContainerName, "Traefik container name")
	containerStartCmd.Flags().StringVar(&stepCAImage, "stepca-image", docker.StepCAImage, "Step CA Docker image")
	containerStartCmd.Flags().StringVar(&zotImage, "zot-image", docker.ZotImage, "Zot Docker image")
	containerStartCmd.Flags().StringVar(&gatusImage, "gatus-image", docker.GatusImage, "Gatus Docker image")
	containerStartCmd.Flags().StringVar(&traefikImage, "traefik-image", docker.TraefikImage, "Traefik Docker image")

	containerStopCmd.Flags().StringVar(&stepCAContainerName, "stepca-name", docker.StepCAContainerName, "Step CA container name")
	containerStopCmd.Flags().StringVar(&zotContainerName, "zot-name", docker.ZotContainerName, "Zot container name")
	containerStopCmd.Flags().StringVar(&gatusContainerName, "gatus-name", docker.GatusContainerName, "Gatus container name")
	containerStopCmd.Flags().StringVar(&traefikContainerName, "traefik-name", docker.TraefikContainerName, "Traefik container name")

	// Add commands to container
	containerCmd.AddCommand(containerStartCmd)
	containerCmd.AddCommand(containerStopCmd)

	// Setup flags for start command
	startCmd.Flags().StringVar(&certPath, "cert", "", "Path to the CA certificate (default: $XDG_DATA_HOME/kinder/ca.crt)")
	startCmd.Flags().StringVar(&keyPath, "key", "", "Path to the CA private key (default: $XDG_DATA_HOME/kinder/ca.key)")
	startCmd.Flags().StringVar(&networkName, "network", docker.DefaultNetworkName, "Docker network name")
	startCmd.Flags().StringVar(&networkCIDR, "cidr", docker.DefaultNetworkCIDR, "Network CIDR")
	startCmd.Flags().StringVar(&traefikPort, "traefik-port", docker.DefaultTraefikPort, "Traefik localhost HTTPS port")
	startCmd.Flags().StringVar(&traefikDomain, "traefik-domain", docker.DefaultTraefikDomain, "Traefik base domain for services")
	startCmd.Flags().IntVar(&kindWorkerNodes, "workers", 0, "Number of Kind worker nodes (0 = control-plane only)")
	startCmd.Flags().StringVar(&kindNodeImage, "node-image", kubernetes.KindNodeImage, "Kind node image to use")

	// Setup flags for stop command
	stopCmd.Flags().StringVar(&networkName, "network", docker.DefaultNetworkName, "Docker network name")

	// Setup flags for restart command
	restartCmd.Flags().StringVar(&certPath, "cert", "", "Path to the CA certificate (default: $XDG_DATA_HOME/kinder/ca.crt)")
	restartCmd.Flags().StringVar(&keyPath, "key", "", "Path to the CA private key (default: $XDG_DATA_HOME/kinder/ca.key)")
	restartCmd.Flags().StringVar(&networkName, "network", docker.DefaultNetworkName, "Docker network name")
	restartCmd.Flags().StringVar(&networkCIDR, "cidr", docker.DefaultNetworkCIDR, "Network CIDR")
	restartCmd.Flags().StringVar(&traefikPort, "traefik-port", docker.DefaultTraefikPort, "Traefik localhost HTTPS port")
	restartCmd.Flags().StringVar(&traefikDomain, "traefik-domain", docker.DefaultTraefikDomain, "Traefik base domain for services")
	restartCmd.Flags().IntVar(&kindWorkerNodes, "workers", 0, "Number of Kind worker nodes (0 = control-plane only)")
	restartCmd.Flags().StringVar(&kindNodeImage, "node-image", kubernetes.KindNodeImage, "Kind node image to use")

	// Add commands to config
	configCmd.AddCommand(configShowCmd)
	configCmd.AddCommand(configPathCmd)
	configCmd.AddCommand(configInitCmd)

	// Setup flags for Kind commands
	kindStartCmd.Flags().IntVar(&kindWorkerNodes, "workers", 0, "Number of worker nodes (0 = control-plane only)")
	kindStartCmd.Flags().StringVar(&kindNodeImage, "node-image", kubernetes.KindNodeImage, "Kind node image to use")

	// Add commands to kind
	kindCmd.AddCommand(kindStartCmd)
	kindCmd.AddCommand(kindStopCmd)
	kindCmd.AddCommand(kindStatusCmd)
	kindCmd.AddCommand(kindKubeconfigCmd)

	// Add all commands to root
	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(stopCmd)
	rootCmd.AddCommand(restartCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(cleanCmd)
	rootCmd.AddCommand(diagnosticsCmd)
	rootCmd.AddCommand(caCmd)
	rootCmd.AddCommand(networkCmd)
	rootCmd.AddCommand(containerCmd)
	rootCmd.AddCommand(kindCmd)
	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(trustBundleCmd)
	rootCmd.AddCommand(certIssuerCmd)
	rootCmd.AddCommand(argocdCmd)
	rootCmd.AddCommand(completionCmd)
}

// getDataDir returns the appropriate data directory based on XDG_DATA_HOME or fallback
