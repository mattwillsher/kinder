package docker

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/go-connections/nat"
)

// ContainerConfig holds generic configuration for creating a Docker container
type ContainerConfig struct {
	// Container identification
	Name     string
	Image    string
	Hostname string

	// Network configuration
	NetworkName    string
	NetworkAliases []string

	// Environment variables
	Env []string

	// Port exposures
	ExposedPorts nat.PortSet
	PortBindings nat.PortMap

	// Volume mounts
	Mounts []mount.Mount

	// Restart policy
	RestartPolicy container.RestartPolicy

	// Additional container config options
	WorkingDir string
	User       string
	Cmd        []string
	Entrypoint []string
}

// CreateContainer creates and starts a Docker container with the specified configuration
// If the container already exists, returns its ID without error (idempotent)
func CreateContainer(ctx context.Context, config ContainerConfig) (string, error) {
	c, err := GetSharedClient()
	if err != nil {
		return "", err
	}
	cli := c.Raw()

	// Check if container already exists
	exists, err := ContainerExists(ctx, config.Name)
	if err != nil {
		return "", fmt.Errorf("failed to check if container exists: %w", err)
	}
	if exists {
		// Get existing container and start if not running
		inspect, err := cli.ContainerInspect(ctx, config.Name)
		if err != nil {
			return "", fmt.Errorf("failed to inspect existing container: %w", err)
		}
		if !inspect.State.Running {
			if err := cli.ContainerStart(ctx, inspect.ID, container.StartOptions{}); err != nil {
				return "", fmt.Errorf("failed to start existing container: %w", err)
			}
		}
		return inspect.ID, nil
	}

	// Pull the image
	reader, err := cli.ImagePull(ctx, config.Image, image.PullOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to pull image: %w", err)
	}
	defer reader.Close()
	// Consume the pull output and check for errors
	if _, err := io.Copy(io.Discard, reader); err != nil {
		return "", fmt.Errorf("failed to complete image pull: %w", err)
	}

	// Create container configuration
	containerConfig := &container.Config{
		Image:        config.Image,
		Hostname:     config.Hostname,
		Env:          config.Env,
		ExposedPorts: config.ExposedPorts,
		WorkingDir:   config.WorkingDir,
		User:         config.User,
	}

	if len(config.Cmd) > 0 {
		containerConfig.Cmd = config.Cmd
	}

	if len(config.Entrypoint) > 0 {
		containerConfig.Entrypoint = config.Entrypoint
	}

	// Create host configuration
	hostConfig := &container.HostConfig{
		Mounts:        config.Mounts,
		RestartPolicy: config.RestartPolicy,
		PortBindings:  config.PortBindings,
	}

	// Create network configuration
	var networkConfig *network.NetworkingConfig
	if config.NetworkName != "" {
		networkConfig = &network.NetworkingConfig{
			EndpointsConfig: map[string]*network.EndpointSettings{
				config.NetworkName: {
					Aliases: config.NetworkAliases,
				},
			},
		}
	}

	// Create container
	resp, err := cli.ContainerCreate(
		ctx,
		containerConfig,
		hostConfig,
		networkConfig,
		nil,
		config.Name,
	)
	if err != nil {
		return "", fmt.Errorf("failed to create container: %w", err)
	}

	// Start container
	if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return "", fmt.Errorf("failed to start container: %w", err)
	}

	return resp.ID, nil
}

// RemoveContainer stops and removes a Docker container
func RemoveContainer(ctx context.Context, containerName string) error {
	c, err := GetSharedClient()
	if err != nil {
		return err
	}
	cli := c.Raw()

	// Stop container
	timeout := 10
	stopOptions := container.StopOptions{
		Timeout: &timeout,
	}
	if err := cli.ContainerStop(ctx, containerName, stopOptions); err != nil {
		// Continue even if stop fails (container might not be running)
		fmt.Fprintf(os.Stderr, "Warning: failed to stop container: %v\n", err)
	}

	// Remove container
	if err := cli.ContainerRemove(ctx, containerName, container.RemoveOptions{
		Force: true,
	}); err != nil {
		return fmt.Errorf("failed to remove container: %w", err)
	}

	return nil
}

// ContainerExists checks if a container exists
func ContainerExists(ctx context.Context, containerName string) (bool, error) {
	c, err := GetSharedClient()
	if err != nil {
		return false, err
	}

	containers, err := c.Raw().ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return false, fmt.Errorf("failed to list containers: %w", err)
	}

	for _, ctr := range containers {
		for _, name := range ctr.Names {
			// Docker prefixes container names with "/"
			if name == "/"+containerName || name == containerName {
				return true, nil
			}
		}
	}

	return false, nil
}

// CopyFile copies a file from src to dst
func CopyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	return err
}

// GetContainerIP retrieves the IP address of a container on a specific network
func GetContainerIP(ctx context.Context, containerName, networkName string) (string, error) {
	c, err := GetSharedClient()
	if err != nil {
		return "", err
	}

	containerJSON, err := c.Raw().ContainerInspect(ctx, containerName)
	if err != nil {
		return "", fmt.Errorf("failed to inspect container: %w", err)
	}

	if containerJSON.NetworkSettings == nil {
		return "", fmt.Errorf("container has no network settings")
	}

	if networkSettings, ok := containerJSON.NetworkSettings.Networks[networkName]; ok {
		return networkSettings.IPAddress, nil
	}

	return "", fmt.Errorf("container not connected to network %s", networkName)
}
