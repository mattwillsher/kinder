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
	// TraefikImage is the Docker image for Traefik
	TraefikImage = "traefik:latest"
	// TraefikContainerName is the default container name for Traefik
	TraefikContainerName = "kinder-traefik"
	// TraefikHostname is the hostname for the Traefik container
	TraefikHostname = "traefik"
	// DefaultTraefikPort is the default HTTPS port for Traefik on localhost
	DefaultTraefikPort = "8443"
	// DefaultTraefikDomain is the default sslip.io domain for Traefik
	DefaultTraefikDomain = "c0000201.sslip.io"
)

// TraefikConfig holds configuration for the Traefik reverse proxy container
type TraefikConfig struct {
	ContainerName string
	Hostname      string
	NetworkName   string
	DataDir       string
	Image         string
	Port          string // Localhost HTTPS port (default: 8443)
	Domain        string // Base domain for services (default: c0000201.sslip.io)
}

// CreateTraefikContainer creates and starts a Traefik reverse proxy container
func CreateTraefikContainer(ctx context.Context, config TraefikConfig) (string, error) {
	// Set defaults
	if config.Port == "" {
		config.Port = DefaultTraefikPort
	}
	if config.Domain == "" {
		config.Domain = DefaultTraefikDomain
	}

	// Create Traefik data directory
	traefikDir := filepath.Join(config.DataDir, "traefik")
	if err := os.MkdirAll(traefikDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create traefik directory: %w", err)
	}

	// Copy CA certificate to Traefik directory
	caCertSrc := filepath.Join(config.DataDir, "ca.crt")
	caCertDest := filepath.Join(traefikDir, "ca.crt")
	if err := CopyFile(caCertSrc, caCertDest); err != nil {
		return "", fmt.Errorf("failed to copy CA certificate: %w", err)
	}

	// Generate Traefik static config
	staticConfigPath := filepath.Join(traefikDir, "traefik.yaml")
	if err := generateTraefikStaticConfig(staticConfigPath); err != nil {
		return "", fmt.Errorf("failed to generate Traefik static config: %w", err)
	}

	// Generate Traefik dynamic config
	dynamicConfigPath := filepath.Join(traefikDir, "dynamic.yaml")
	if err := generateTraefikDynamicConfig(dynamicConfigPath, config.Domain); err != nil {
		return "", fmt.Errorf("failed to generate Traefik dynamic config: %w", err)
	}

	// Build generic container configuration
	containerConfig := ContainerConfig{
		Name:           config.ContainerName,
		Image:          config.Image,
		Hostname:       config.Hostname,
		NetworkName:    config.NetworkName,
		NetworkAliases: []string{config.Hostname},
		Cmd: []string{
			"--configFile=/etc/traefik/traefik.yaml",
		},
		ExposedPorts: nat.PortSet{
			"80/tcp":  struct{}{},
			"443/tcp": struct{}{},
		},
		PortBindings: nat.PortMap{
			"80/tcp": []nat.PortBinding{
				{
					HostPort: "80",
				},
			},
			"443/tcp": []nat.PortBinding{
				{
					HostPort: config.Port,
				},
			},
		},
		Env: []string{
			"SSL_CERT_FILE=/etc/traefik/ca.crt",
		},
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeBind,
				Source: traefikDir,
				Target: "/etc/traefik",
			},
		},
		RestartPolicy: container.RestartPolicy{
			Name: "unless-stopped",
		},
	}

	// Use generic CreateContainer function
	containerID, err := CreateContainer(ctx, containerConfig)
	if err != nil {
		return "", fmt.Errorf("failed to create Traefik container: %w", err)
	}

	return containerID, nil
}

// RemoveTraefikContainer stops and removes the Traefik container
func RemoveTraefikContainer(ctx context.Context, containerName string) error {
	return RemoveContainer(ctx, containerName)
}

// generateTraefikStaticConfig creates the static configuration file for Traefik
func generateTraefikStaticConfig(path string) error {
	config := `# Traefik static configuration for kinder
api:
  dashboard: true

entryPoints:
  web:
    address: ":80"
    http:
      redirections:
        entryPoint:
          to: websecure
          scheme: https
  websecure:
    address: ":443"

certificatesResolvers:
  stepca:
    acme:
      email: admin@localhost
      storage: /etc/traefik/acme.json
      caServer: https://stepca:9000/acme/acme/directory
      certificatesDuration: 2160
      httpChallenge:
        entryPoint: web
      caCertificates:
        - /etc/traefik/ca.crt

providers:
  file:
    filename: /etc/traefik/dynamic.yaml
    watch: true

log:
  level: INFO
`

	if err := os.WriteFile(path, []byte(config), 0644); err != nil {
		return fmt.Errorf("failed to write Traefik static config: %w", err)
	}

	return nil
}

// generateTraefikDynamicConfig creates the dynamic configuration file for Traefik
func generateTraefikDynamicConfig(path, domain string) error {
	config := fmt.Sprintf(`# Traefik dynamic configuration for kinder
http:
  routers:
    traefik-router:
      rule: "Host(`+"`traefik.%s`"+`)"
      service: api@internal
      entryPoints:
        - websecure
      tls:
        certResolver: stepca

    zot-router:
      rule: "Host(`+"`registry.%s`"+`)"
      service: zot-service
      entryPoints:
        - websecure
      tls:
        certResolver: stepca

    gatus-router:
      rule: "Host(`+"`gatus.%s`"+`)"
      service: gatus-service
      entryPoints:
        - websecure
      tls:
        certResolver: stepca

    stepca-router:
      rule: "Host(`+"`ca.%s`"+`)"
      service: stepca-service
      entryPoints:
        - websecure
      tls:
        certResolver: stepca

  services:
    zot-service:
      loadBalancer:
        servers:
          - url: "http://zot:5000"

    gatus-service:
      loadBalancer:
        servers:
          - url: "http://gatus:8080"

    stepca-service:
      loadBalancer:
        servers:
          - url: "https://stepca:9000"
        serversTransport: stepca-transport

  serversTransports:
    stepca-transport: {}
`, domain, domain, domain, domain)

	if err := os.WriteFile(path, []byte(config), 0644); err != nil {
		return fmt.Errorf("failed to write Traefik dynamic config: %w", err)
	}

	return nil
}
