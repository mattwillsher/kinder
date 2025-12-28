package docker

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/go-connections/nat"
)

const (
	// GatusImage is the Docker image for Gatus
	GatusImage = "twinproduction/gatus:latest"
	// GatusContainerName is the default container name for Gatus
	GatusContainerName = "kinder-gatus"
	// GatusHostname is the hostname for the Gatus container
	GatusHostname = "gatus"
)

// GatusConfig holds configuration for the Gatus health dashboard container
type GatusConfig struct {
	ContainerName string
	Hostname      string
	NetworkName   string
	DataDir       string
	Image         string
}

// CreateGatusContainer creates and starts a Gatus health dashboard container
func CreateGatusContainer(ctx context.Context, config GatusConfig) (string, error) {
	// Create Gatus data directory
	gatusDir := filepath.Join(config.DataDir, "gatus")
	if err := os.MkdirAll(gatusDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create gatus directory: %w", err)
	}

	// Generate Gatus config
	configPath := filepath.Join(gatusDir, "config.yaml")
	if err := generateGatusConfig(configPath); err != nil {
		return "", fmt.Errorf("failed to generate Gatus config: %w", err)
	}

	// Path to CA certificate
	caCertPath := filepath.Join(config.DataDir, "ca.crt")

	// Build generic container configuration
	containerConfig := ContainerConfig{
		Name:           config.ContainerName,
		Image:          config.Image,
		Hostname:       config.Hostname,
		NetworkName:    config.NetworkName,
		NetworkAliases: []string{config.Hostname},
		ExposedPorts: nat.PortSet{
			"8080/tcp": struct{}{},
		},
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeBind,
				Source: configPath,
				Target: "/config/config.yaml",
			},
			{
				Type:   mount.TypeBind,
				Source: caCertPath,
				Target: "/etc/ssl/certs/kinder-ca.crt",
			},
		},
		RestartPolicy: container.RestartPolicy{
			Name: "unless-stopped",
		},
	}

	// Use generic CreateContainer function
	containerID, err := CreateContainer(ctx, containerConfig)
	if err != nil {
		return "", fmt.Errorf("failed to create Gatus container: %w", err)
	}

	return containerID, nil
}

// RemoveGatusContainer stops and removes the Gatus container
func RemoveGatusContainer(ctx context.Context, containerName string) error {
	return RemoveContainer(ctx, containerName)
}

// generateGatusConfig creates a configuration file for Gatus
func generateGatusConfig(path string) error {
	config := `# Gatus configuration for kinder
endpoints:
  - name: Step CA
    url: "https://stepca:9000/health"
    interval: 30s
    conditions:
      - "[STATUS] == 200"

  - name: Zot Registry
    url: "http://zot:5000/v2/"
    interval: 30s
    conditions:
      - "[STATUS] == 200"

  - name: Kubernetes API
    url: "https://kinder-control-plane:6443/livez"
    interval: 30s
    client:
      insecure: true
    conditions:
      - "[STATUS] == 200"

web:
  port: 8080
`

	if err := os.WriteFile(path, []byte(config), 0644); err != nil {
		return fmt.Errorf("failed to write Gatus config: %w", err)
	}

	return nil
}
