//go:build integration

package docker

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codeberg.org/hipkoi/kinder/config"
)

// setupTestConfig initializes the config system with a temporary data directory.
// Returns the data directory path and a cleanup function.
func setupTestConfig(t *testing.T, prefix string) (string, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", prefix)
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	// Save original env var
	origDataDir := os.Getenv("KINDER_DATADIR")

	// Set test data directory
	os.Setenv("KINDER_DATADIR", tmpDir)

	// Initialize config system
	if err := config.Initialize(""); err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to initialize config: %v", err)
	}

	cleanup := func() {
		// Restore original env var
		if origDataDir != "" {
			os.Setenv("KINDER_DATADIR", origDataDir)
		} else {
			os.Unsetenv("KINDER_DATADIR")
		}
		os.RemoveAll(tmpDir)
	}

	return tmpDir, cleanup
}

// createTestCACert creates a test CA certificate in the data directory
func createTestCACert(t *testing.T, dataDir string) string {
	t.Helper()

	caCertPath := filepath.Join(dataDir, "ca.crt")
	caCertContent := `-----BEGIN CERTIFICATE-----
MIIBkTCB+wIJAKHBfpegPmC2MAoGCCqGSM49BAMCMBQxEjAQBgNVBAMMCWtpbmRl
ci1jYTAeFw0yNDAxMDEwMDAwMDBaFw0yNTAxMDEwMDAwMDBaMBQxEjAQBgNVBAMM
CWtpbmRlci1jYTBZMBMGByqGSM49AgEGCCqGSM49AwEHA0IABEExample...
-----END CERTIFICATE-----`
	if err := os.WriteFile(caCertPath, []byte(caCertContent), 0644); err != nil {
		t.Fatalf("failed to create CA cert: %v", err)
	}

	return caCertPath
}

// getRegistryMirrorsFromConfig returns the registry mirrors from config
func getRegistryMirrorsFromConfig() map[string]string {
	mirrors := config.GetStringSlice(config.KeyRegistryMirrors)
	if len(mirrors) == 0 {
		mirrors = config.DefaultRegistryMirrors
	}

	registryMirrors := make(map[string]string)
	for _, registry := range mirrors {
		registryMirrors[registry] = "http://zot:5000"
	}
	return registryMirrors
}

// TestCertsDirStructureIntegration tests the full certs.d directory creation
// with actual file system operations and verifies the content is correct.
func TestCertsDirStructureIntegration(t *testing.T) {
	dataDir, cleanup := setupTestConfig(t, "kinder-integration-certs-*")
	defer cleanup()

	caCertPath := createTestCACert(t, dataDir)

	// Use registry mirrors from config
	mirrors := getRegistryMirrorsFromConfig()

	err := createCertsDirStructure(dataDir, caCertPath, mirrors, "zot")
	if err != nil {
		t.Fatalf("createCertsDirStructure failed: %v", err)
	}

	// Verify expected directories and files
	expectedDirs := []string{
		"docker.io", // normalized from registry-1.docker.io
		"ghcr.io",
		"quay.io",
		"registry.k8s.io",
	}

	certsDir := filepath.Join(dataDir, "certs.d")
	for _, dir := range expectedDirs {
		dirPath := filepath.Join(certsDir, dir)

		// Check directory exists
		info, err := os.Stat(dirPath)
		if os.IsNotExist(err) {
			t.Errorf("expected directory %s does not exist", dir)
			continue
		}
		if !info.IsDir() {
			t.Errorf("%s is not a directory", dir)
			continue
		}

		// Check hosts.toml exists and has correct content
		hostsPath := filepath.Join(dirPath, "hosts.toml")
		content, err := os.ReadFile(hostsPath)
		if err != nil {
			t.Errorf("failed to read hosts.toml for %s: %v", dir, err)
			continue
		}

		contentStr := string(content)

		// Verify structure
		if !strings.Contains(contentStr, "server = ") {
			t.Errorf("%s/hosts.toml missing server declaration", dir)
		}
		if !strings.Contains(contentStr, "[host.") {
			t.Errorf("%s/hosts.toml missing host section", dir)
		}
		if !strings.Contains(contentStr, `capabilities = ["pull", "resolve"]`) {
			t.Errorf("%s/hosts.toml missing capabilities", dir)
		}

		// Verify CA cert was copied
		caCertDest := filepath.Join(dirPath, "ca.crt")
		if _, err := os.Stat(caCertDest); os.IsNotExist(err) {
			t.Errorf("CA cert not copied to %s", dir)
		}
	}

	// Verify specific content for docker.io
	dockerHostsPath := filepath.Join(certsDir, "docker.io", "hosts.toml")
	content, _ := os.ReadFile(dockerHostsPath)
	contentStr := string(content)

	if !strings.Contains(contentStr, `server = "https://registry-1.docker.io"`) {
		t.Error("docker.io hosts.toml should use registry-1.docker.io as upstream")
	}
	if !strings.Contains(contentStr, `http://zot:5000/registry-1.docker.io`) {
		t.Error("docker.io hosts.toml should have path prefix /registry-1.docker.io")
	}
}

// TestZotConfigIntegration tests the full Zot configuration generation
func TestZotConfigIntegration(t *testing.T) {
	dataDir, cleanup := setupTestConfig(t, "kinder-integration-zot-*")
	defer cleanup()

	configPath := filepath.Join(dataDir, "zot", "config.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		t.Fatalf("failed to create zot directory: %v", err)
	}

	// Use registry mirrors from config
	mirrors := config.GetStringSlice(config.KeyRegistryMirrors)
	if len(mirrors) == 0 {
		mirrors = config.DefaultRegistryMirrors
	}

	err := generateZotConfig(configPath, mirrors)
	if err != nil {
		t.Fatalf("generateZotConfig failed: %v", err)
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}

	contentStr := string(content)

	// Verify all mirrors have content prefixes
	for _, mirror := range mirrors {
		expectedDest := `"destination": "/` + mirror + `"`
		if !strings.Contains(contentStr, expectedDest) {
			t.Errorf("Zot config missing destination for %s", mirror)
		}
	}

	// Verify sync is enabled
	if !strings.Contains(contentStr, `"enable": true`) {
		t.Error("Zot config should have sync enabled")
	}

	// Verify onDemand is set
	if !strings.Contains(contentStr, `"onDemand": true`) {
		t.Error("Zot config should have onDemand enabled")
	}
}

// TestKindClusterCreation tests actual Kind cluster creation with registry config.
// This test requires Docker and will create/delete a real Kind cluster.
func TestKindClusterCreation(t *testing.T) {
	if os.Getenv("KINDER_SKIP_KIND_TEST") != "" {
		t.Skip("Skipping Kind cluster test (KINDER_SKIP_KIND_TEST set)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	dataDir, cleanup := setupTestConfig(t, "kinder-kind-integration-*")
	defer cleanup()

	caCertPath := createTestCACert(t, dataDir)

	// Use a unique cluster name to avoid conflicts
	clusterName := "kinder-integration-test"

	// Get registry mirrors from config
	registryMirrors := getRegistryMirrorsFromConfig()

	cfg := KindConfig{
		ClusterName:     clusterName,
		NodeImage:       KindNodeImage,
		CACertPath:      caCertPath,
		NetworkName:     "bridge", // Use default bridge for test
		RegistryMirrors: registryMirrors,
		ZotHostname:     "zot",
		WorkerNodes:     0, // Control plane only for faster test
	}

	// Ensure cleanup even on failure
	defer func() {
		t.Log("Cleaning up Kind cluster...")
		StopKind(KindConfig{ClusterName: clusterName})
	}()

	// Delete any existing cluster with the same name
	if exists, _ := KindExists(clusterName); exists {
		t.Log("Deleting existing test cluster...")
		if err := StopKind(cfg); err != nil {
			t.Logf("Warning: failed to delete existing cluster: %v", err)
		}
		time.Sleep(2 * time.Second)
	}

	// Create the cluster
	t.Log("Creating Kind cluster...")
	if err := StartKind(ctx, cfg); err != nil {
		t.Fatalf("failed to create Kind cluster: %v", err)
	}

	// Verify cluster exists
	exists, err := KindExists(clusterName)
	if err != nil {
		t.Fatalf("failed to check cluster existence: %v", err)
	}
	if !exists {
		t.Fatal("cluster should exist after creation")
	}

	// Get kubeconfig to verify cluster is accessible
	kubeconfig, err := GetKindKubeconfig(clusterName)
	if err != nil {
		t.Fatalf("failed to get kubeconfig: %v", err)
	}
	if kubeconfig == "" {
		t.Error("kubeconfig should not be empty")
	}
	if !strings.Contains(kubeconfig, "apiVersion") {
		t.Error("kubeconfig should contain apiVersion")
	}

	// Verify certs.d was created correctly using config.GetDataDir()
	verifyDataDir, err := config.GetDataDir()
	if err != nil {
		t.Fatalf("failed to get data dir from config: %v", err)
	}
	certsDir := filepath.Join(verifyDataDir, "certs.d")
	if _, err := os.Stat(certsDir); os.IsNotExist(err) {
		t.Error("certs.d directory was not created")
	}

	// Check for expected registry directories
	expectedDirs := []string{"docker.io", "ghcr.io"}
	for _, dir := range expectedDirs {
		dirPath := filepath.Join(certsDir, dir)
		if _, err := os.Stat(dirPath); os.IsNotExist(err) {
			t.Errorf("expected registry directory %s not found", dir)
		}
	}

	t.Log("Kind cluster created successfully with registry configuration")
}

// TestKindClusterWithWorkers tests Kind cluster creation with worker nodes
func TestKindClusterWithWorkers(t *testing.T) {
	if os.Getenv("KINDER_SKIP_KIND_TEST") != "" {
		t.Skip("Skipping Kind cluster test (KINDER_SKIP_KIND_TEST set)")
	}
	if os.Getenv("KINDER_SKIP_WORKER_TEST") != "" {
		t.Skip("Skipping worker node test (KINDER_SKIP_WORKER_TEST set)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	dataDir, cleanup := setupTestConfig(t, "kinder-worker-integration-*")
	defer cleanup()

	caCertPath := createTestCACert(t, dataDir)

	clusterName := "kinder-worker-test"

	// Use a subset of registry mirrors for faster test
	registryMirrors := map[string]string{
		"ghcr.io": "http://zot:5000",
	}

	cfg := KindConfig{
		ClusterName:     clusterName,
		NodeImage:       KindNodeImage,
		CACertPath:      caCertPath,
		NetworkName:     "bridge",
		RegistryMirrors: registryMirrors,
		ZotHostname:     "zot",
		WorkerNodes:     1, // One worker node
	}

	defer func() {
		t.Log("Cleaning up Kind cluster with workers...")
		StopKind(KindConfig{ClusterName: clusterName})
	}()

	if exists, _ := KindExists(clusterName); exists {
		t.Log("Deleting existing test cluster...")
		StopKind(cfg)
		time.Sleep(2 * time.Second)
	}

	t.Log("Creating Kind cluster with 1 worker node...")
	if err := StartKind(ctx, cfg); err != nil {
		t.Fatalf("failed to create Kind cluster with workers: %v", err)
	}

	// Get nodes
	nodes, err := GetKindNodes(ctx, clusterName)
	if err != nil {
		t.Fatalf("failed to get nodes: %v", err)
	}

	// Should have 2 nodes: control-plane + 1 worker
	if len(nodes) != 2 {
		t.Errorf("expected 2 nodes, got %d: %v", len(nodes), nodes)
	}

	// Verify node names
	hasControlPlane := false
	hasWorker := false
	for _, node := range nodes {
		if strings.Contains(node, "control-plane") {
			hasControlPlane = true
		}
		if strings.Contains(node, "worker") {
			hasWorker = true
		}
	}

	if !hasControlPlane {
		t.Error("missing control-plane node")
	}
	if !hasWorker {
		t.Error("missing worker node")
	}

	t.Log("Kind cluster with workers created successfully")
}

// TestBuildKindConfigIntegration tests the full config building process
func TestBuildKindConfigIntegration(t *testing.T) {
	dataDir, cleanup := setupTestConfig(t, "kinder-buildconfig-integration-*")
	defer cleanup()

	caCertPath := createTestCACert(t, dataDir)

	// Get registry mirrors from config
	registryMirrors := getRegistryMirrorsFromConfig()

	cfg := KindConfig{
		ClusterName:     "integration-test",
		NodeImage:       KindNodeImage,
		CACertPath:      caCertPath,
		NetworkName:     "test-network",
		RegistryMirrors: registryMirrors,
		ZotHostname:     "zot",
		WorkerNodes:     2,
	}

	kindConfig, err := buildKindConfig(cfg)
	if err != nil {
		t.Fatalf("buildKindConfig failed: %v", err)
	}

	// Verify cluster configuration
	if kindConfig.Name != "integration-test" {
		t.Errorf("unexpected cluster name: %s", kindConfig.Name)
	}

	// Verify node count: 1 control-plane + 2 workers
	if len(kindConfig.Nodes) != 3 {
		t.Errorf("expected 3 nodes, got %d", len(kindConfig.Nodes))
	}

	// Verify containerd patches exist and have correct format
	if len(kindConfig.ContainerdConfigPatches) == 0 {
		t.Error("expected containerd patches")
	}

	patch := kindConfig.ContainerdConfigPatches[0]
	if !strings.Contains(patch, `config_path = "/etc/containerd/certs.d"`) {
		t.Error("patch should set config_path")
	}
	// Should NOT contain deprecated format
	if strings.Contains(patch, "registry.mirrors") {
		t.Error("patch should not contain deprecated registry.mirrors format")
	}

	// Verify certs.d directory structure using config.GetDataDir()
	verifyDataDir, err := config.GetDataDir()
	if err != nil {
		t.Fatalf("failed to get data dir from config: %v", err)
	}
	certsDir := filepath.Join(verifyDataDir, "certs.d")

	// Check all expected directories exist
	expectedDirs := map[string]string{
		"docker.io":       "registry-1.docker.io", // normalized name -> original for path
		"ghcr.io":         "ghcr.io",
		"quay.io":         "quay.io",
		"registry.k8s.io": "registry.k8s.io",
	}

	for normalizedName, originalName := range expectedDirs {
		dirPath := filepath.Join(certsDir, normalizedName)
		hostsPath := filepath.Join(dirPath, "hosts.toml")

		content, err := os.ReadFile(hostsPath)
		if err != nil {
			t.Errorf("failed to read hosts.toml for %s: %v", normalizedName, err)
			continue
		}

		contentStr := string(content)

		// Verify mirror URL includes path prefix with original name
		expectedMirror := "http://zot:5000/" + originalName
		if !strings.Contains(contentStr, expectedMirror) {
			t.Errorf("%s/hosts.toml should contain mirror %s, got:\n%s",
				normalizedName, expectedMirror, contentStr)
		}
	}

	// Verify all nodes have correct mounts
	for i, node := range kindConfig.Nodes {
		if len(node.ExtraMounts) < 2 {
			t.Errorf("node %d should have at least 2 mounts, got %d", i, len(node.ExtraMounts))
		}

		hasCACert := false
		hasCertsD := false
		for _, mount := range node.ExtraMounts {
			if mount.ContainerPath == "/etc/ssl/certs/kinder-ca.crt" {
				hasCACert = true
				if !mount.Readonly {
					t.Errorf("CA cert mount should be readonly")
				}
			}
			if mount.ContainerPath == "/etc/containerd/certs.d" {
				hasCertsD = true
				if !mount.Readonly {
					t.Errorf("certs.d mount should be readonly")
				}
			}
		}

		if !hasCACert {
			t.Errorf("node %d missing CA cert mount", i)
		}
		if !hasCertsD {
			t.Errorf("node %d missing certs.d mount", i)
		}
	}
}
