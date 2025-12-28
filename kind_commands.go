package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"codeberg.org/hipkoi/kinder/config"
	"codeberg.org/hipkoi/kinder/docker"
	"github.com/spf13/cobra"
)

var (
	kindWorkerNodes int
	kindNodeImage   string
)

var kindCmd = &cobra.Command{
	Use:   "kind",
	Short: "Manage Kind Kubernetes cluster",
	Long:  `Commands for managing the Kind Kubernetes cluster that uses kinder infrastructure.`,
}

var kindStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the Kind cluster",
	Long: `Start a Kind Kubernetes cluster configured to:
- Trust the kinder CA certificate
- Use Zot registry as a pull-through cache for container images
- Connect to the kinder Docker network`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		return startKindCluster(ctx)
	},
}

var kindStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop and delete the Kind cluster",
	Long:  `Stop and delete the Kind Kubernetes cluster.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return stopKindCluster()
	},
}

var kindStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show Kind cluster status",
	Long:  `Display the status of the Kind Kubernetes cluster.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		return showKindStatus(ctx)
	},
}

var kindKubeconfigCmd = &cobra.Command{
	Use:   "kubeconfig",
	Short: "Print kubeconfig for the Kind cluster",
	Long:  `Print the kubeconfig needed to connect to the Kind cluster.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		clusterName := config.GetString(config.KeyAppName)
		if clusterName == "" {
			clusterName = docker.KindClusterName
		}

		kubeconfig, err := docker.GetKindKubeconfig(clusterName)
		if err != nil {
			return fmt.Errorf("failed to get kubeconfig: %w", err)
		}

		fmt.Println(kubeconfig)
		return nil
	},
}

func startKindCluster(ctx context.Context) error {
	// Get configuration
	dataDir, err := config.GetDataDir()
	if err != nil {
		return fmt.Errorf("failed to get data directory: %w", err)
	}

	appName := config.GetString(config.KeyAppName)
	if appName == "" {
		appName = config.DefaultAppName
	}

	networkName := config.GetString(config.KeyNetworkName)
	if networkName == "" {
		networkName = appName
	}

	// Check if CA certificate exists
	caCertPath := filepath.Join(dataDir, CACertFilename)
	if _, err := os.Stat(caCertPath); os.IsNotExist(err) {
		return fmt.Errorf("CA certificate not found at %s - run 'kinder start' first", caCertPath)
	}

	kindCfg := docker.KindConfig{
		ClusterName:     appName,
		NodeImage:       kindNodeImage,
		CACertPath:      caCertPath,
		NetworkName:     networkName,
		RegistryMirrors: buildRegistryMirrorMap(),
		ZotHostname:     "zot",
		WorkerNodes:     kindWorkerNodes,
	}

	// Check if cluster already exists
	exists, err := docker.KindExists(kindCfg.ClusterName)
	if err != nil {
		return fmt.Errorf("failed to check cluster status: %w", err)
	}

	if exists {
		fmt.Printf("  ✓ Kind cluster '%s' already exists\n", kindCfg.ClusterName)
		return nil
	}

	fmt.Printf("Creating Kind cluster '%s'...\n", kindCfg.ClusterName)
	if err := docker.StartKind(ctx, kindCfg); err != nil {
		return fmt.Errorf("failed to start Kind cluster: %w", err)
	}

	fmt.Printf("  ✓ Kind cluster '%s' created\n", kindCfg.ClusterName)
	fmt.Println()
	fmt.Println("To use the cluster:")
	fmt.Printf("  export KUBECONFIG=\"$(kind get kubeconfig-path --name=%s)\"\n", kindCfg.ClusterName)
	fmt.Println("  # or")
	fmt.Printf("  kubectl cluster-info --context kind-%s\n", kindCfg.ClusterName)

	return nil
}

func stopKindCluster() error {
	appName := config.GetString(config.KeyAppName)
	if appName == "" {
		appName = config.DefaultAppName
	}

	kindCfg := docker.KindConfig{
		ClusterName: appName,
	}

	// Check if cluster exists
	exists, err := docker.KindExists(kindCfg.ClusterName)
	if err != nil {
		return fmt.Errorf("failed to check cluster status: %w", err)
	}

	if !exists {
		fmt.Printf("  ✓ Kind cluster '%s' does not exist\n", kindCfg.ClusterName)
		return nil
	}

	fmt.Printf("Deleting Kind cluster '%s'...\n", kindCfg.ClusterName)
	if err := docker.StopKind(kindCfg); err != nil {
		return fmt.Errorf("failed to stop Kind cluster: %w", err)
	}

	fmt.Printf("  ✓ Kind cluster '%s' deleted\n", kindCfg.ClusterName)
	return nil
}

func showKindStatus(ctx context.Context) error {
	appName := config.GetString(config.KeyAppName)
	if appName == "" {
		appName = config.DefaultAppName
	}

	exists, err := docker.KindExists(appName)
	if err != nil {
		return fmt.Errorf("failed to check cluster status: %w", err)
	}

	if !exists {
		fmt.Printf("Kind cluster '%s': not running\n", appName)
		return nil
	}

	// Get nodes
	nodes, err := docker.GetKindNodes(ctx, appName)
	if err != nil {
		fmt.Printf("Kind cluster '%s': running (failed to get nodes: %v)\n", appName, err)
		return nil
	}

	fmt.Printf("Kind cluster '%s': running\n", appName)
	fmt.Println("Nodes:")
	for _, node := range nodes {
		fmt.Printf("  - %s\n", node)
	}

	return nil
}
