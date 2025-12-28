package docker

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
	"sigs.k8s.io/kind/pkg/cluster"
	"sigs.k8s.io/kind/pkg/cmd"
	"sigs.k8s.io/kind/pkg/log"
)

// kindEnvMutex protects environment variable operations for Kind cluster creation.
// Kind uses KIND_EXPERIMENTAL_DOCKER_NETWORK env var which is not thread-safe.
var kindEnvMutex sync.Mutex

const (
	// KindClusterName is the default name for the Kind cluster
	KindClusterName = "kinder"
	// KindNodeImage is the default Kind node image
	KindNodeImage = "kindest/node:v1.32.2"
)

// KindConfig holds configuration for creating a Kind cluster
type KindConfig struct {
	// ClusterName is the name of the Kind cluster
	ClusterName string
	// NodeImage is the Kind node image to use
	NodeImage string
	// CACertPath is the path to the CA certificate to trust
	CACertPath string
	// NetworkName is the Docker network to connect to
	NetworkName string
	// RegistryMirrors maps registry hosts to their mirror URLs
	// e.g., "docker.io" -> "http://zot:5000"
	RegistryMirrors map[string]string
	// ZotHostname is the hostname of the Zot registry
	ZotHostname string
	// WorkerNodes is the number of worker nodes (0 = control-plane only)
	WorkerNodes int
	// Verbose enables detailed output from Kind
	Verbose bool
}

// nullLogger implements a no-op logger for Kind
type nullLogger struct{}

func (n nullLogger) Warn(message string)                       {}
func (n nullLogger) Warnf(format string, args ...interface{})  {}
func (n nullLogger) Error(message string)                      {}
func (n nullLogger) Errorf(format string, args ...interface{}) {}
func (n nullLogger) V(level log.Level) log.InfoLogger          { return nullInfoLogger{} }

type nullInfoLogger struct{}

func (n nullInfoLogger) Info(message string)                      {}
func (n nullInfoLogger) Infof(format string, args ...interface{}) {}
func (n nullInfoLogger) Enabled() bool                            { return false }

// StartKind creates and starts a Kind cluster with the given configuration
func StartKind(ctx context.Context, cfg KindConfig) error {
	var provider *cluster.Provider
	if cfg.Verbose {
		provider = cluster.NewProvider(
			cluster.ProviderWithLogger(cmd.NewLogger()),
		)
	} else {
		provider = cluster.NewProvider(
			cluster.ProviderWithLogger(nullLogger{}),
		)
	}

	// Check if cluster already exists
	clusters, err := provider.List()
	if err != nil {
		return fmt.Errorf("failed to list clusters: %w", err)
	}

	for _, c := range clusters {
		if c == cfg.ClusterName {
			return fmt.Errorf("cluster %s already exists", cfg.ClusterName)
		}
	}

	// Build the Kind cluster configuration
	kindConfig, err := buildKindConfig(cfg)
	if err != nil {
		return fmt.Errorf("failed to build Kind config: %w", err)
	}

	// Create the cluster
	nodeImage := cfg.NodeImage
	if nodeImage == "" {
		nodeImage = KindNodeImage
	}

	// Only set KIND_EXPERIMENTAL_DOCKER_NETWORK for non-default networks
	// Kind already defaults to "kind" network, so no env var needed
	// Use mutex to protect env var operations from concurrent access
	kindEnvMutex.Lock()
	if cfg.NetworkName != "" && cfg.NetworkName != "kind" {
		os.Setenv("KIND_EXPERIMENTAL_DOCKER_NETWORK", cfg.NetworkName)
		defer os.Unsetenv("KIND_EXPERIMENTAL_DOCKER_NETWORK")
	}
	defer kindEnvMutex.Unlock()

	if err := provider.Create(
		cfg.ClusterName,
		cluster.CreateWithV1Alpha4Config(kindConfig),
		cluster.CreateWithNodeImage(nodeImage),
		cluster.CreateWithWaitForReady(5*time.Minute),
	); err != nil {
		return fmt.Errorf("failed to create cluster: %w", err)
	}

	// Connect cluster nodes to the kinder network
	// if cfg.NetworkName != "" {
	// 	if err := connectKindToNetwork(ctx, cfg.ClusterName, cfg.NetworkName); err != nil {
	// 		// Log warning but don't fail - cluster is still usable
	// 		fmt.Printf("Warning: failed to connect to network %s: %v\n", cfg.NetworkName, err)
	// 	}
	// }

	return nil
}

// StopKind deletes a Kind cluster
func StopKind(cfg KindConfig) error {
	var provider *cluster.Provider
	if cfg.Verbose {
		provider = cluster.NewProvider(
			cluster.ProviderWithLogger(cmd.NewLogger()),
		)
	} else {
		provider = cluster.NewProvider(
			cluster.ProviderWithLogger(nullLogger{}),
		)
	}

	if err := provider.Delete(cfg.ClusterName, ""); err != nil {
		return fmt.Errorf("failed to delete cluster: %w", err)
	}

	return nil
}

// KindExists checks if a Kind cluster exists
func KindExists(clusterName string) (bool, error) {
	provider := cluster.NewProvider()

	clusters, err := provider.List()
	if err != nil {
		return false, fmt.Errorf("failed to list clusters: %w", err)
	}

	for _, c := range clusters {
		if c == clusterName {
			return true, nil
		}
	}

	return false, nil
}

// GetKindKubeconfig returns the kubeconfig for a Kind cluster
func GetKindKubeconfig(clusterName string) (string, error) {
	provider := cluster.NewProvider()

	kubeconfig, err := provider.KubeConfig(clusterName, false)
	if err != nil {
		return "", fmt.Errorf("failed to get kubeconfig: %w", err)
	}

	return kubeconfig, nil
}

// buildKindConfig creates the Kind cluster configuration
func buildKindConfig(cfg KindConfig) (*v1alpha4.Cluster, error) {
	config := &v1alpha4.Cluster{
		TypeMeta: v1alpha4.TypeMeta{
			Kind:       "Cluster",
			APIVersion: "kind.x-k8s.io/v1alpha4",
		},
		Name: cfg.ClusterName,
	}

	// Build containerd config patches for registry mirrors
	containerdPatches := buildContainerdPatches(cfg)
	if len(containerdPatches) > 0 {
		config.ContainerdConfigPatches = containerdPatches
	}

	// Create the certs.d directory structure with hosts.toml files
	dataDir := filepath.Dir(cfg.CACertPath)
	if len(cfg.RegistryMirrors) > 0 || cfg.ZotHostname != "" {
		if err := createCertsDirStructure(dataDir, cfg.CACertPath, cfg.RegistryMirrors, cfg.ZotHostname); err != nil {
			return nil, fmt.Errorf("failed to create certs.d structure: %w", err)
		}
	}

	// Define common mounts for all nodes
	var extraMounts []v1alpha4.Mount

	// Mount CA certificate for system-wide trust
	if cfg.CACertPath != "" {
		extraMounts = append(extraMounts, v1alpha4.Mount{
			HostPath:      cfg.CACertPath,
			ContainerPath: "/etc/ssl/certs/kinder-ca.crt",
			Readonly:      true,
		})

		// Mount the certs.d directory for containerd registry configuration
		certsDir := filepath.Join(dataDir, "certs.d")
		if _, err := os.Stat(certsDir); err == nil {
			extraMounts = append(extraMounts, v1alpha4.Mount{
				HostPath:      certsDir,
				ContainerPath: "/etc/containerd/certs.d",
				Readonly:      true,
			})
		}
	}

	// Create control plane node
	controlPlane := v1alpha4.Node{
		Role:        v1alpha4.ControlPlaneRole,
		ExtraMounts: extraMounts,
	}
	config.Nodes = append(config.Nodes, controlPlane)

	// Add worker nodes with the same mounts
	for i := 0; i < cfg.WorkerNodes; i++ {
		worker := v1alpha4.Node{
			Role:        v1alpha4.WorkerRole,
			ExtraMounts: extraMounts,
		}
		config.Nodes = append(config.Nodes, worker)
	}

	return config, nil
}

// buildContainerdPatches creates containerd configuration patches for registry mirrors.
// This only sets the config_path to enable the directory-based hosts.toml configuration.
// The actual mirror configuration is in the hosts.toml files created by createCertsDirStructure.
func buildContainerdPatches(cfg KindConfig) []string {
	if len(cfg.RegistryMirrors) == 0 && cfg.ZotHostname == "" {
		return nil
	}

	// Only set the config_path to enable directory-based registry configuration.
	// The old-style registry.mirrors.* config is deprecated and conflicts with config_path.
	patch := `[plugins."io.containerd.grpc.v1.cri".registry]
  config_path = "/etc/containerd/certs.d"
`
	return []string{patch}
}

// createCertsDirStructure creates the certs.d directory structure with hosts.toml files
// for each registry mirror. This is the new containerd registry configuration format.
func createCertsDirStructure(dataDir string, caCertPath string, mirrors map[string]string, zotHostname string) error {
	certsDir := filepath.Join(dataDir, "certs.d")

	// Clean existing certs.d directory to ensure fresh configuration
	if err := os.RemoveAll(certsDir); err != nil {
		return fmt.Errorf("failed to remove old certs.d directory: %w", err)
	}

	// Read CA cert content for copying to registry directories
	var caCertData []byte
	var err error
	if caCertPath != "" {
		caCertData, err = os.ReadFile(caCertPath)
		if err != nil {
			return fmt.Errorf("failed to read CA certificate: %w", err)
		}
	}

	// Create hosts.toml for direct access to Zot registry (zot:5000)
	// This allows pulling images pushed directly to the local registry
	if zotHostname != "" {
		zotAddr := zotHostname + ":5000"
		zotDir := filepath.Join(certsDir, zotAddr)
		if err := os.MkdirAll(zotDir, 0755); err != nil {
			return fmt.Errorf("failed to create directory for %s: %w", zotAddr, err)
		}

		// Configure HTTP access to Zot (no TLS)
		hostsToml := fmt.Sprintf(`server = "http://%s"

[host."http://%s"]
  capabilities = ["pull", "resolve", "push"]
  skip_verify = true
`, zotAddr, zotAddr)

		hostsPath := filepath.Join(zotDir, "hosts.toml")
		if err := os.WriteFile(hostsPath, []byte(hostsToml), 0644); err != nil {
			return fmt.Errorf("failed to write hosts.toml for %s: %w", zotAddr, err)
		}

		// Create hosts.toml for localhost:5000 -> zot:5000 mapping
		// This allows pulling images using localhost:5000 from inside Kind nodes
		localhostDir := filepath.Join(certsDir, "localhost:5000")
		if err := os.MkdirAll(localhostDir, 0755); err != nil {
			return fmt.Errorf("failed to create directory for localhost:5000: %w", err)
		}

		// Redirect localhost:5000 to zot:5000 (the actual registry on Docker network)
		localhostHostsToml := fmt.Sprintf(`server = "http://%s"

[host."http://%s"]
  capabilities = ["pull", "resolve", "push"]
  skip_verify = true
`, zotAddr, zotAddr)

		localhostHostsPath := filepath.Join(localhostDir, "hosts.toml")
		if err := os.WriteFile(localhostHostsPath, []byte(localhostHostsToml), 0644); err != nil {
			return fmt.Errorf("failed to write hosts.toml for localhost:5000: %w", err)
		}
	}

	// Create hosts.toml for each registry
	for registry, mirrorURL := range mirrors {
		// Normalize the registry name (e.g., registry-1.docker.io -> docker.io)
		normalizedName := normalizeRegistryName(registry)

		registryDir := filepath.Join(certsDir, normalizedName)
		if err := os.MkdirAll(registryDir, 0755); err != nil {
			return fmt.Errorf("failed to create directory for %s: %w", normalizedName, err)
		}

		// Determine the upstream server URL based on registry
		upstreamServer := getUpstreamServer(registry)

		// Build the mirror URL with the path prefix using the original registry name
		// e.g., "http://zot:5000" + "/registry-1.docker.io" -> "http://zot:5000/registry-1.docker.io"
		// This keeps the Zot storage path consistent with what's configured in Zot sync
		mirrorWithPath := mirrorURL + MirrorPath(registry)

		// Create hosts.toml
		hostsToml := fmt.Sprintf(`server = "%s"

[host."%s"]
  capabilities = ["pull", "resolve"]
`, upstreamServer, mirrorWithPath)

		hostsPath := filepath.Join(registryDir, "hosts.toml")
		if err := os.WriteFile(hostsPath, []byte(hostsToml), 0644); err != nil {
			return fmt.Errorf("failed to write hosts.toml for %s: %w", normalizedName, err)
		}

		// Copy CA cert to registry directory for TLS verification
		if len(caCertData) > 0 {
			caCertDest := filepath.Join(registryDir, "ca.crt")
			if err := os.WriteFile(caCertDest, caCertData, 0644); err != nil {
				return fmt.Errorf("failed to write CA cert for %s: %w", normalizedName, err)
			}
		}
	}

	return nil
}

// normalizeRegistryName returns the canonical name for a registry as used by containerd.
// This ensures hosts.toml directories match what containerd expects.
func normalizeRegistryName(registry string) string {
	// registry-1.docker.io is the actual server, but containerd uses "docker.io"
	if registry == "registry-1.docker.io" {
		return "docker.io"
	}
	return registry
}

// getUpstreamServer returns the canonical server URL for a registry
func getUpstreamServer(registry string) string {
	// docker.io uses registry-1.docker.io as the actual server
	if registry == "docker.io" || registry == "registry-1.docker.io" {
		return "https://registry-1.docker.io"
	}
	return "https://" + registry
}

// connectKindToNetwork connects Kind cluster nodes to a Docker network
func connectKindToNetwork(ctx context.Context, clusterName, networkName string) error {
	c, err := GetSharedClient()
	if err != nil {
		return err
	}
	cli := c.Raw()

	// Get all containers for this Kind cluster
	// Kind containers are named: <cluster>-control-plane, <cluster>-worker, etc.
	containers, err := cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	prefix := clusterName + "-"
	for _, container := range containers {
		for _, name := range container.Names {
			// Container names have leading /
			name = strings.TrimPrefix(name, "/")
			if strings.HasPrefix(name, prefix) {
				// Check if already connected
				inspect, err := cli.ContainerInspect(ctx, container.ID)
				if err != nil {
					continue
				}

				alreadyConnected := false
				for netName := range inspect.NetworkSettings.Networks {
					if netName == networkName {
						alreadyConnected = true
						break
					}
				}

				if !alreadyConnected {
					if err := cli.NetworkConnect(ctx, networkName, container.ID, nil); err != nil {
						return fmt.Errorf("failed to connect %s to network %s: %w", name, networkName, err)
					}
				}
			}
		}
	}

	return nil
}

// GetKindNodes returns information about Kind cluster nodes.
// Kind nodes follow the naming pattern: <cluster>-control-plane[N] or <cluster>-worker[N]
func GetKindNodes(ctx context.Context, clusterName string) ([]string, error) {
	c, err := GetSharedClient()
	if err != nil {
		return nil, err
	}
	cli := c.Raw()

	containers, err := cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	var nodes []string
	controlPlanePrefix := clusterName + "-control-plane"
	workerPrefix := clusterName + "-worker"

	for _, container := range containers {
		for _, name := range container.Names {
			name = strings.TrimPrefix(name, "/")
			// Kind nodes are either control-plane or worker nodes
			if strings.HasPrefix(name, controlPlanePrefix) || strings.HasPrefix(name, workerPrefix) {
				nodes = append(nodes, name)
			}
		}
	}

	return nodes, nil
}
