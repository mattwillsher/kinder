package docker

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/docker/docker/client"
)

// Client wraps the Docker client with lifecycle management
type Client struct {
	cli *client.Client
}

var (
	sharedClient *Client
	clientMu     sync.Mutex
)

// getDockerHostFromContext retrieves the Docker host from the current Docker context.
// Returns empty string if context cannot be determined or DOCKER_HOST is already set.
func getDockerHostFromContext() string {
	// If DOCKER_HOST is already set, don't override it
	if os.Getenv("DOCKER_HOST") != "" {
		return ""
	}

	// Try to get the current context's Docker host using docker CLI
	cmd := exec.Command("docker", "context", "inspect", "--format", "{{.Endpoints.docker.Host}}")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	host := strings.TrimSpace(string(output))
	if host == "" || host == "unix:///var/run/docker.sock" {
		// Default socket, no need to override
		return ""
	}

	return host
}

// NewClient creates a new Docker client wrapper
func NewClient() (*Client, error) {
	opts := []client.Opt{client.FromEnv, client.WithAPIVersionNegotiation()}

	// Check if we need to use a specific Docker host from context
	if host := getDockerHostFromContext(); host != "" {
		opts = append(opts, client.WithHost(host))
	}

	cli, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %w", err)
	}
	return &Client{cli: cli}, nil
}

// GetSharedClient returns a shared Docker client instance.
// The client is created on first call and reused for subsequent calls.
// This reduces connection overhead when performing multiple Docker operations.
func GetSharedClient() (*Client, error) {
	clientMu.Lock()
	defer clientMu.Unlock()

	if sharedClient != nil {
		return sharedClient, nil
	}

	c, err := NewClient()
	if err != nil {
		return nil, err
	}
	sharedClient = c
	return sharedClient, nil
}

// Close closes the Docker client connection
func (c *Client) Close() error {
	if c.cli != nil {
		return c.cli.Close()
	}
	return nil
}

// CloseSharedClient closes the shared client if it exists.
// Call this during application shutdown.
func CloseSharedClient() error {
	clientMu.Lock()
	defer clientMu.Unlock()

	if sharedClient != nil {
		err := sharedClient.Close()
		sharedClient = nil
		return err
	}
	return nil
}

// Ping verifies the Docker daemon is accessible
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.cli.Ping(ctx)
	return err
}

// Raw returns the underlying Docker client for advanced operations.
// Use sparingly - prefer adding methods to Client instead.
func (c *Client) Raw() *client.Client {
	return c.cli
}
