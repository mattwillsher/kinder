package docker

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"codeberg.org/hipkoi/kinder/cacert"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/go-connections/nat"
)

const (
	// StepCAImage is the Docker image for Step CA
	StepCAImage = "smallstep/step-ca:latest"
	// StepCAContainerName is the default container name for Step CA
	StepCAContainerName = "kinder-step-ca"
	// StepCAHostname is the hostname for the Step CA container
	StepCAHostname = "stepca"
)

// StepCAConfig holds configuration for the Step CA container
type StepCAConfig struct {
	ContainerName string
	Hostname      string
	NetworkName   string
	CACertPath    string
	CAKeyPath     string
	DataDir       string
	Image         string
}

// CreateStepCAContainer creates and starts a Step CA container using the provided root CA
func CreateStepCAContainer(ctx context.Context, config StepCAConfig) (string, error) {
	// Create Step CA data directory
	stepCADir := filepath.Join(config.DataDir, "step-ca")
	if err := os.MkdirAll(stepCADir, 0755); err != nil {
		return "", fmt.Errorf("failed to create step-ca directory: %w", err)
	}

	// Copy CA cert and key to step-ca directory
	caCertDest := filepath.Join(stepCADir, "root_ca.crt")
	caKeyDest := filepath.Join(stepCADir, "root_ca_key")

	if err := CopyFile(config.CACertPath, caCertDest); err != nil {
		return "", fmt.Errorf("failed to copy CA certificate: %w", err)
	}

	if err := CopyFile(config.CAKeyPath, caKeyDest); err != nil {
		return "", fmt.Errorf("failed to copy CA key: %w", err)
	}

	// Set restrictive permissions on the key file
	if err := os.Chmod(caKeyDest, 0600); err != nil {
		return "", fmt.Errorf("failed to set permissions on CA key: %w", err)
	}

	// Create certs directory for intermediate CA
	certsDir := filepath.Join(stepCADir, "certs")
	if err := os.MkdirAll(certsDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create certs directory: %w", err)
	}

	// Create secrets directory for intermediate key
	secretsDir := filepath.Join(stepCADir, "secrets")
	if err := os.MkdirAll(secretsDir, 0700); err != nil {
		return "", fmt.Errorf("failed to create secrets directory: %w", err)
	}

	// Generate intermediate CA certificate and key from root CA
	intermediateCertPath := filepath.Join(certsDir, "intermediate_ca.crt")
	intermediateKeyPath := filepath.Join(secretsDir, "intermediate_ca_key")

	if err := cacert.GenerateIntermediate(config.CACertPath, config.CAKeyPath, intermediateCertPath, intermediateKeyPath); err != nil {
		return "", fmt.Errorf("failed to generate intermediate CA: %w", err)
	}

	// Create password file (empty password for intermediate key)
	// SECURITY NOTE: The intermediate CA key uses an empty password for simplicity
	// in local development. The file is protected with 0600 permissions, restricting
	// access to the current user only. This trade-off is acceptable for local dev
	// environments but should NOT be used in production.
	passwordPath := filepath.Join(secretsDir, "password")
	if err := os.WriteFile(passwordPath, []byte(""), 0600); err != nil {
		return "", fmt.Errorf("failed to create password file: %w", err)
	}

	// Generate Step CA config with ACME
	configPath := filepath.Join(stepCADir, "config", "ca.json")
	stepCADNSNames := []string{config.Hostname, "localhost", "*.localhost"}
	if err := generateStepCAConfigWithACME(configPath, stepCADNSNames); err != nil {
		return "", fmt.Errorf("failed to generate Step CA config: %w", err)
	}

	// Build generic container configuration
	containerConfig := ContainerConfig{
		Name:           config.ContainerName,
		Image:          config.Image,
		Hostname:       config.Hostname,
		NetworkName:    config.NetworkName,
		NetworkAliases: []string{config.Hostname},
		Env: []string{
			"DOCKER_STEPCA_INIT_NAME=kinder",
			"DOCKER_STEPCA_INIT_DNS_NAMES=" + config.Hostname,
			"DOCKER_STEPCA_INIT_PROVISIONER_NAME=kinder-admin",
		},
		ExposedPorts: nat.PortSet{
			"9000/tcp": struct{}{},
		},
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeBind,
				Source: stepCADir,
				Target: "/home/step",
			},
		},
		RestartPolicy: container.RestartPolicy{
			Name: "unless-stopped",
		},
	}

	// Use generic CreateContainer function
	containerID, err := CreateContainer(ctx, containerConfig)
	if err != nil {
		return "", fmt.Errorf("failed to create Step CA container: %w", err)
	}

	return containerID, nil
}

// RemoveStepCAContainer stops and removes the Step CA container
func RemoveStepCAContainer(ctx context.Context, containerName string) error {
	return RemoveContainer(ctx, containerName)
}

// generateStepCAConfigWithACME creates a configuration file for Step CA with ACME enabled
func generateStepCAConfigWithACME(path string, dnsNames []string) error {
	// Create config directory if it doesn't exist
	configDir := filepath.Dir(path)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Build DNS names JSON array
	dnsNamesJSON := ""
	for i, name := range dnsNames {
		if i > 0 {
			dnsNamesJSON += ", "
		}
		dnsNamesJSON += fmt.Sprintf(`"%s"`, name)
	}

	config := fmt.Sprintf(`{
  "root": "/home/step/root_ca.crt",
  "federatedRoots": null,
  "crt": "/home/step/certs/intermediate_ca.crt",
  "key": "/home/step/secrets/intermediate_ca_key",
  "address": ":9000",
  "dnsNames": [%s],
  "logger": {"format": "text"},
  "db": {
    "type": "badger",
    "dataSource": "/home/step/db"
  },
  "authority": {
    "provisioners": [
      {
        "type": "ACME",
        "name": "acme"
      }
    ]
  },
  "tls": {
    "cipherSuites": [
      "TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305",
      "TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256"
    ],
    "minVersion": 1.2,
    "maxVersion": 1.3,
    "renegotiation": false
  }
}`, dnsNamesJSON)

	if err := os.WriteFile(path, []byte(config), 0644); err != nil {
		return fmt.Errorf("failed to write Step CA config: %w", err)
	}

	return nil
}
