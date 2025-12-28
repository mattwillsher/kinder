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

// containerInfo holds display information for a started container
type containerInfo struct {
	Name        string
	Hostname    string
	ContainerID string
	IPAddress   string
	Network     string
	ExtraInfo   []string // Additional lines to display
}

// printContainerInfo prints formatted container information (only in verbose mode)
func printContainerInfo(info containerInfo) {
	Verbose("  Container Name: %s\n", info.Name)
	Verbose("  Hostname: %s\n", info.Hostname)
	Verbose("  Container ID: %s\n", info.ContainerID)
	Verbose("  IP Address: %s\n", info.IPAddress)
	Verbose("  Network: %s\n", info.Network)
	for _, extra := range info.ExtraInfo {
		Verbose("  %s\n", extra)
	}
}

// checkPrerequisites checks if network exists
func checkPrerequisites(ctx context.Context, networkName string) error {
	networkExists, err := docker.NetworkExists(ctx, networkName)
	if err != nil {
		return fmt.Errorf("failed to check if network exists: %w", err)
	}
	if !networkExists {
		return fmt.Errorf("network '%s' does not exist. Create it first with 'kinder network create'", networkName)
	}
	return nil
}

// getContainerIPSafe retrieves container IP or returns "unknown" on error
func getContainerIPSafe(ctx context.Context, containerName, networkName string) string {
	ip, err := docker.GetContainerIP(ctx, containerName, networkName)
	if err != nil {
		return "unknown"
	}
	return ip
}

// stopContainerSafe stops a container and prints appropriate message
func stopContainerSafe(ctx context.Context, containerName string, stopFunc func(context.Context, string) error) {
	exists, err := docker.ContainerExists(ctx, containerName)
	if err != nil || !exists {
		Verbose("Container '%s' does not exist\n", containerName)
		return
	}

	if err := stopFunc(ctx, containerName); err != nil {
		Verbose("  ⚠️  Warning: %v\n", err)
		return
	}

	Verbose("%s container '%s' stopped and removed successfully\n", containerName, containerName)
}

var containerCmd = &cobra.Command{
	Use:   "container",
	Short: "Manage containers",
	Long:  `Commands for managing kinder service containers.`,
}

var containerStartCmd = &cobra.Command{
	Use:   "start [service]",
	Short: "Start a container",
	Long: `Start a kinder service container.

Available services: stepca, zot, gatus, traefik, kind`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		service := args[0]
		ctx := context.Background()

		switch service {
		case "stepca":
			return startStepCA(ctx)
		case "zot":
			return startZot(ctx)
		case "gatus":
			return startGatus(ctx)
		case "traefik":
			return startTraefik(ctx)
		case "kind":
			return startKind(ctx)
		default:
			return fmt.Errorf("unknown service: %s. Available services: stepca, zot, gatus, traefik, kind", service)
		}
	},
}

var containerStopCmd = &cobra.Command{
	Use:   "stop [service]",
	Short: "Stop a container",
	Long: `Stop and remove a kinder service container.

Available services: stepca, zot, gatus, traefik, kind`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		service := args[0]
		ctx := context.Background()

		switch service {
		case "stepca":
			return stopStepCA(ctx)
		case "zot":
			return stopZot(ctx)
		case "gatus":
			return stopGatus(ctx)
		case "traefik":
			return stopTraefik(ctx)
		case "kind":
			return stopKind(ctx)
		default:
			return fmt.Errorf("unknown service: %s. Available services: stepca, zot, gatus, traefik, kind", service)
		}
	},
}

// Step CA functions
func startStepCA(ctx context.Context) error {
	if certPath == "" {
		dataDir, err := getDataDir()
		if err != nil {
			return fmt.Errorf("failed to get data directory: %w", err)
		}
		certPath = filepath.Join(dataDir, CACertFilename)
	}

	if keyPath == "" {
		dataDir, err := getDataDir()
		if err != nil {
			return fmt.Errorf("failed to get data directory: %w", err)
		}
		keyPath = filepath.Join(dataDir, CAKeyFilename)
	}

	if _, err := os.Stat(certPath); os.IsNotExist(err) {
		return fmt.Errorf("CA certificate not found at %s. Run 'kinder ca generate' first", certPath)
	}
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		return fmt.Errorf("CA key not found at %s. Run 'kinder ca generate' first", keyPath)
	}

	if err := checkPrerequisites(ctx, networkName); err != nil {
		return err
	}

	dataDir, err := getDataDir()
	if err != nil {
		return fmt.Errorf("failed to get data directory: %w", err)
	}

	config := docker.StepCAConfig{
		ContainerName: stepCAContainerName,
		Hostname:      docker.StepCAHostname,
		NetworkName:   networkName,
		CACertPath:    certPath,
		CAKeyPath:     keyPath,
		DataDir:       dataDir,
		Image:         stepCAImage,
	}

	containerID, err := docker.CreateStepCAContainer(ctx, config)
	if err != nil {
		return fmt.Errorf("failed to create Step CA container: %w", err)
	}

	Verbose("Step CA container started successfully:\n")
	printContainerInfo(containerInfo{
		Name:        stepCAContainerName,
		Hostname:    docker.StepCAHostname,
		ContainerID: containerID,
		IPAddress:   getContainerIPSafe(ctx, stepCAContainerName, networkName),
		Network:     networkName,
	})

	return nil
}

func stopStepCA(ctx context.Context) error {
	stopContainerSafe(ctx, stepCAContainerName, docker.RemoveStepCAContainer)
	return nil
}

// Zot functions
func startZot(ctx context.Context) error {
	if err := checkPrerequisites(ctx, networkName); err != nil {
		return err
	}

	dataDir, err := getDataDir()
	if err != nil {
		return fmt.Errorf("failed to get data directory: %w", err)
	}

	zotCfg := docker.ZotConfig{
		ContainerName:   zotContainerName,
		Hostname:        docker.ZotHostname,
		NetworkName:     networkName,
		DataDir:         dataDir,
		Image:           zotImage,
		RegistryMirrors: config.DefaultRegistryMirrors,
	}

	containerID, err := docker.CreateZotContainer(ctx, zotCfg)
	if err != nil {
		return fmt.Errorf("failed to create Zot container: %w", err)
	}

	Verbose("Zot registry container started successfully:\n")
	printContainerInfo(containerInfo{
		Name:        zotContainerName,
		Hostname:    docker.ZotHostname,
		ContainerID: containerID,
		IPAddress:   getContainerIPSafe(ctx, zotContainerName, networkName),
		Network:     networkName,
		ExtraInfo:   []string{"Registry URL: http://localhost:5000"},
	})

	return nil
}

func stopZot(ctx context.Context) error {
	stopContainerSafe(ctx, zotContainerName, docker.RemoveZotContainer)
	return nil
}

// Gatus functions
func startGatus(ctx context.Context) error {
	if err := checkPrerequisites(ctx, networkName); err != nil {
		return err
	}

	dataDir, err := getDataDir()
	if err != nil {
		return fmt.Errorf("failed to get data directory: %w", err)
	}

	config := docker.GatusConfig{
		ContainerName: gatusContainerName,
		Hostname:      docker.GatusHostname,
		NetworkName:   networkName,
		DataDir:       dataDir,
		Image:         gatusImage,
	}

	containerID, err := docker.CreateGatusContainer(ctx, config)
	if err != nil {
		return fmt.Errorf("failed to create Gatus container: %w", err)
	}

	Verbose("Gatus health dashboard container started successfully:\n")
	printContainerInfo(containerInfo{
		Name:        gatusContainerName,
		Hostname:    docker.GatusHostname,
		ContainerID: containerID,
		IPAddress:   getContainerIPSafe(ctx, gatusContainerName, networkName),
		Network:     networkName,
	})

	return nil
}

func stopGatus(ctx context.Context) error {
	stopContainerSafe(ctx, gatusContainerName, docker.RemoveGatusContainer)
	return nil
}

// Traefik functions
func startTraefik(ctx context.Context) error {
	if err := checkPrerequisites(ctx, networkName); err != nil {
		return err
	}

	dataDir, err := getDataDir()
	if err != nil {
		return fmt.Errorf("failed to get data directory: %w", err)
	}

	config := docker.TraefikConfig{
		ContainerName: traefikContainerName,
		Hostname:      docker.TraefikHostname,
		NetworkName:   networkName,
		DataDir:       dataDir,
		Image:         traefikImage,
		Port:          traefikPort,
		Domain:        traefikDomain,
	}

	containerID, err := docker.CreateTraefikContainer(ctx, config)
	if err != nil {
		return fmt.Errorf("failed to create Traefik container: %w", err)
	}

	Verbose("Traefik reverse proxy container started successfully:\n")
	printContainerInfo(containerInfo{
		Name:        traefikContainerName,
		Hostname:    docker.TraefikHostname,
		ContainerID: containerID,
		IPAddress:   getContainerIPSafe(ctx, traefikContainerName, networkName),
		Network:     networkName,
		ExtraInfo:   []string{"Dashboard: http://localhost:8080"},
	})

	return nil
}

func stopTraefik(ctx context.Context) error {
	stopContainerSafe(ctx, traefikContainerName, docker.RemoveTraefikContainer)
	return nil
}

// Kind cluster functions
func startKind(ctx context.Context) error {
	// Get data directory for CA cert path
	dataDir, err := getDataDir()
	if err != nil {
		return fmt.Errorf("failed to get data directory: %w", err)
	}

	appName := config.GetString(config.KeyAppName)
	if appName == "" {
		appName = config.DefaultAppName
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
		Verbose:         IsVerbose(),
	}

	// Check if cluster already exists
	exists, err := docker.KindExists(kindCfg.ClusterName)
	if err != nil {
		return fmt.Errorf("failed to check cluster status: %w", err)
	}

	if exists {
		Verbose("Kind cluster '%s' already exists\n", kindCfg.ClusterName)
		return nil
	}

	if kindCfg.WorkerNodes > 0 {
		Verbose("Creating Kind cluster '%s' with %d worker nodes...\n", kindCfg.ClusterName, kindCfg.WorkerNodes)
	} else {
		Verbose("Creating Kind cluster '%s'...\n", kindCfg.ClusterName)
	}

	if err := docker.StartKind(ctx, kindCfg); err != nil {
		return fmt.Errorf("failed to start Kind cluster: %w", err)
	}

	Verbose("Kind cluster '%s' created\n", kindCfg.ClusterName)
	return nil
}

func stopKind(ctx context.Context) error {
	appName := config.GetString(config.KeyAppName)
	if appName == "" {
		appName = config.DefaultAppName
	}

	// Check if cluster exists
	exists, err := docker.KindExists(appName)
	if err != nil {
		return fmt.Errorf("failed to check cluster status: %w", err)
	}

	if !exists {
		Verbose("Kind cluster '%s' does not exist\n", appName)
		return nil
	}

	kindCfg := docker.KindConfig{
		ClusterName: appName,
		Verbose:     IsVerbose(),
	}

	if err := docker.StopKind(kindCfg); err != nil {
		return fmt.Errorf("failed to stop Kind cluster: %w", err)
	}

	Verbose("Kind cluster '%s' deleted\n", appName)
	return nil
}
