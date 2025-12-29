package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/go-connections/nat"
)

const (
	// ZotImage is the Docker image for Zot registry
	ZotImage = "ghcr.io/project-zot/zot-linux-amd64:latest"
	// ZotContainerName is the default container name for Zot
	ZotContainerName = "kinder-zot"
	// ZotHostname is the hostname for the Zot container
	ZotHostname = "zot"
)

// ZotConfig holds configuration for the Zot registry container
type ZotConfig struct {
	ContainerName   string
	Hostname        string
	NetworkName     string
	DataDir         string
	Image           string
	RegistryMirrors []string // List of registries to mirror (e.g., "ghcr.io", "registry-1.docker.io")
}

// CreateZotContainer creates and starts a Zot registry container
func CreateZotContainer(ctx context.Context, config ZotConfig) (string, error) {
	// Create Zot data directory
	zotDir := filepath.Join(config.DataDir, "zot")
	zotDataDir := filepath.Join(zotDir, "data")
	if err := os.MkdirAll(zotDataDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create zot data directory: %w", err)
	}

	// Generate Zot config
	configPath := filepath.Join(zotDir, "config.json")
	if err := generateZotConfig(configPath, config.RegistryMirrors); err != nil {
		return "", fmt.Errorf("failed to generate Zot config: %w", err)
	}

	// Build generic container configuration
	containerConfig := ContainerConfig{
		Name:           config.ContainerName,
		Image:          config.Image,
		Hostname:       config.Hostname,
		NetworkName:    config.NetworkName,
		NetworkAliases: []string{config.Hostname},
		Cmd:            []string{"serve", "/etc/zot/config.json"},
		ExposedPorts: nat.PortSet{
			"5000/tcp": struct{}{},
		},
		// NETWORK NOTE: Binding to 0.0.0.0 is required for access via the sslip.io
		// domain (which resolves to 192.0.2.1). Binding to 127.0.0.1 would prevent
		// external access needed for Kind cluster and browser connectivity.
		// For local development, ensure your firewall restricts external access.
		PortBindings: nat.PortMap{
			"5000/tcp": []nat.PortBinding{
				{
					HostIP:   "0.0.0.0",
					HostPort: "5000",
				},
			},
		},
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeBind,
				Source: zotDir,
				Target: "/etc/zot",
			},
			{
				Type:   mount.TypeBind,
				Source: zotDataDir,
				Target: "/var/lib/registry",
			},
		},
		RestartPolicy: container.RestartPolicy{
			Name: "unless-stopped",
		},
	}

	// Use generic CreateContainer function
	containerID, err := CreateContainer(ctx, containerConfig)
	if err != nil {
		return "", fmt.Errorf("failed to create Zot container: %w", err)
	}

	return containerID, nil
}

// RemoveZotContainer stops and removes the Zot container
func RemoveZotContainer(ctx context.Context, containerName string) error {
	return RemoveContainer(ctx, containerName)
}

// WaitForZot waits for the Zot registry to be ready to accept connections
func WaitForZot(ctx context.Context, timeout time.Duration) error {
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost:5000/v2/", nil)
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}

		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
			// Retry
		}
	}

	return fmt.Errorf("timeout waiting for Zot registry to be ready")
}

// zotContentConfig represents the content prefix/destination mapping
type zotContentConfig struct {
	Prefix      string `json:"prefix"`
	Destination string `json:"destination,omitempty"`
}

// zotConfigRegistry represents a registry entry in Zot sync config
type zotConfigRegistry struct {
	URLs       []string           `json:"urls"`
	OnDemand   bool               `json:"onDemand"`
	TLSVerify  bool               `json:"tlsVerify"`
	MaxRetries int                `json:"maxRetries"`
	RetryDelay string             `json:"retryDelay"`
	Content    []zotContentConfig `json:"content,omitempty"`
}

// zotConfigSync represents the sync extension config
type zotConfigSync struct {
	Enable     bool                `json:"enable"`
	Registries []zotConfigRegistry `json:"registries"`
}

// zotConfigExtensions represents the extensions config
type zotConfigExtensions struct {
	Search map[string]bool `json:"search"`
	UI     map[string]bool `json:"ui"`
	Sync   zotConfigSync   `json:"sync"`
}

// zotConfigHTTP represents the HTTP config
type zotConfigHTTP struct {
	Address string   `json:"address"`
	Port    string   `json:"port"`
	Compat  []string `json:"compat,omitempty"` // Docker compatibility modes (e.g., "docker2s2")
}

// zotConfigStorage represents the storage config
type zotConfigStorage struct {
	RootDirectory string `json:"rootDirectory"`
}

// zotConfigLog represents the log config
type zotConfigLog struct {
	Level string `json:"level"`
}

// zotConfigRoot represents the full Zot config
type zotConfigRoot struct {
	DistSpecVersion string              `json:"distSpecVersion"`
	Storage         zotConfigStorage    `json:"storage"`
	HTTP            zotConfigHTTP       `json:"http"`
	Log             zotConfigLog        `json:"log"`
	Extensions      zotConfigExtensions `json:"extensions"`
}

// MirrorPath returns the Zot path prefix for a given upstream registry.
// This is used to namespace images from different registries to avoid collisions.
// e.g., "docker.io" -> "/docker.io", "ghcr.io" -> "/ghcr.io"
func MirrorPath(registry string) string {
	return "/" + registry
}

// generateZotConfig creates a configuration file for Zot with specified registry mirrors
func generateZotConfig(path string, mirrors []string) error {
	// Build registries list from mirrors
	// All images are cached at the root path - no destination prefix needed
	// since containerd sends requests directly to /v2/<repo>/...
	registries := make([]zotConfigRegistry, 0, len(mirrors))
	for _, mirror := range mirrors {
		registries = append(registries, zotConfigRegistry{
			URLs:       []string{"https://" + mirror},
			OnDemand:   true,
			TLSVerify:  true,
			MaxRetries: 3,
			RetryDelay: "5m",
			Content: []zotContentConfig{
				{
					Prefix: "**",
				},
			},
		})
	}

	cfg := zotConfigRoot{
		DistSpecVersion: "1.1.0",
		Storage: zotConfigStorage{
			RootDirectory: "/var/lib/registry",
		},
		HTTP: zotConfigHTTP{
			Address: "0.0.0.0",
			Port:    "5000",
			Compat:  []string{"docker2s2"}, // Enable Docker manifest to OCI conversion
		},
		Log: zotConfigLog{
			Level: "info",
		},
		Extensions: zotConfigExtensions{
			Search: map[string]bool{"enable": true},
			UI:     map[string]bool{"enable": true},
			Sync: zotConfigSync{
				Enable:     true,
				Registries: registries,
			},
		},
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal Zot config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write Zot config: %w", err)
	}

	return nil
}
