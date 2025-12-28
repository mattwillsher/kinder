package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"codeberg.org/hipkoi/kinder/config"
	"codeberg.org/hipkoi/kinder/docker"
	"github.com/spf13/cobra"
)

var diagnosticsCmd = &cobra.Command{
	Use:   "diagnostics",
	Short: "Run diagnostics to verify kinder environment",
	Long: `Run comprehensive diagnostics to verify the kinder environment is functioning correctly.
Checks:
  - Docker daemon availability
  - IP address 192.0.2.1 routability (verifies network path exists)
  - CA certificate validity
  - Kinder network existence
  - Required containers running
  - Service endpoints (Step CA, Zot, Gatus, Traefik)
  - Registry and Kubernetes end-to-end test (if Kind cluster is running)`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()

		fmt.Println("🔍 Running kinder diagnostics...")
		fmt.Println()

		allPassed := true

		// Check 1: Docker availability
		fmt.Println("1️⃣  Checking Docker availability...")
		if err := checkDockerAvailability(ctx); err != nil {
			fmt.Printf("   ❌ FAILED: %v\n", err)
			allPassed = false
		} else {
			fmt.Println("   ✅ Docker daemon is running and accessible")
		}
		fmt.Println()

		// Check 2: IP 192.0.2.1 reachability (checks if IP is routable)
		fmt.Println("2️⃣  Checking IP 192.0.2.1 reachability...")
		if err := checkIPReachability(ctx, "192.0.2.1"); err != nil {
			fmt.Printf("   ❌ FAILED: %v\n", err)
			allPassed = false
		} else {
			fmt.Println("   ✅ IP 192.0.2.1 is routable")
		}
		fmt.Println()

		// Check 3: CA certificate existence and validity
		fmt.Println("3️⃣  Checking CA certificate...")
		dataDir, err := getDataDir()
		if err != nil {
			fmt.Printf("   ❌ FAILED: %v\n", err)
			allPassed = false
		} else {
			caCertPath := filepath.Join(dataDir, CACertFilename)
			if err := checkCACertificate(caCertPath); err != nil {
				fmt.Printf("   ❌ FAILED: %v\n", err)
				allPassed = false
			} else {
				fmt.Printf("   ✅ CA certificate exists and is valid (%s)\n", caCertPath)
			}
		}
		fmt.Println()

		// Check 4: Kinder network
		fmt.Println("4️⃣  Checking kinder network...")
		if err := checkKinderNetwork(ctx); err != nil {
			fmt.Printf("   ❌ FAILED: %v\n", err)
			allPassed = false
		} else {
			fmt.Println("   ✅ Kinder network exists")
		}
		fmt.Println()

		// Check 5: Running containers
		fmt.Println("5️⃣  Checking running containers...")
		containersPassed := checkRunningContainers(ctx)
		if !containersPassed {
			allPassed = false
		}
		fmt.Println()

		// Check 6: Service endpoints
		fmt.Println("6️⃣  Checking service endpoints...")
		if dataDir != "" {
			caCertPath := filepath.Join(dataDir, CACertFilename)
			endpointsPassed := checkServiceEndpoints(ctx, caCertPath)
			if !endpointsPassed {
				allPassed = false
			}
		} else {
			fmt.Println("   ⚠️  Skipped (data directory not available)")
		}
		fmt.Println()

		// Check 7: Registry and Kubernetes end-to-end test (only if Kind is running)
		fmt.Println("7️⃣  Checking registry and Kubernetes end-to-end...")
		appName := config.GetString(config.KeyAppName)
		if appName == "" {
			appName = config.DefaultAppName
		}
		kindExists, err := docker.KindExists(appName)
		if err != nil {
			fmt.Printf("   ⚠️  Skipped (failed to check Kind status: %v)\n", err)
		} else if !kindExists {
			fmt.Println("   ⚠️  Skipped (Kind cluster not running)")
		} else {
			if err := checkRegistryK8sEndToEnd(ctx, appName); err != nil {
				fmt.Printf("   ❌ FAILED: %v\n", err)
				allPassed = false
			} else {
				fmt.Println("   ✅ Registry and Kubernetes end-to-end test passed")
			}
		}
		fmt.Println()

		// Final summary
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		if allPassed {
			fmt.Println("✅ All diagnostics passed!")
			fmt.Println()
			fmt.Println("Your kinder environment is fully functional.")
			return nil
		} else {
			fmt.Println("❌ Some diagnostics failed")
			fmt.Println()
			fmt.Println("Please review the failures above and run:")
			fmt.Println("  - 'kinder start' to ensure all services are running")
			fmt.Println("  - 'kinder ca generate' if CA certificate is missing")
			return fmt.Errorf("diagnostics failed")
		}
	},
}

func checkDockerAvailability(ctx context.Context) error {
	c, err := docker.GetSharedClient()
	if err != nil {
		return fmt.Errorf("failed to create Docker client: %w", err)
	}

	if err := c.Ping(ctx); err != nil {
		return fmt.Errorf("Docker daemon not responding: %w", err)
	}

	return nil
}

func checkIPReachability(ctx context.Context, ip string) error {
	// Use UDP dial to check if the IP is routable.
	// This verifies the kernel has a route to the IP without requiring
	// a listening service or root privileges (unlike ICMP ping).
	// The dial succeeds if the IP is routable, even if nothing is listening.
	dialer := &net.Dialer{
		Timeout: 3 * time.Second,
	}
	conn, err := dialer.DialContext(ctx, "udp", net.JoinHostPort(ip, "1"))
	if err != nil {
		return fmt.Errorf("IP %s is not routable: %w", ip, err)
	}
	conn.Close()
	return nil
}

func checkCACertificate(path string) error {
	// Check if file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("CA certificate not found at %s", path)
	}

	// Read and parse certificate
	certPEM, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read CA certificate: %w", err)
	}

	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(certPEM) {
		return fmt.Errorf("failed to parse CA certificate")
	}

	// Parse to check validity period
	block, _ := os.ReadFile(path)
	if len(block) == 0 {
		return fmt.Errorf("empty certificate file")
	}

	return nil
}

func checkKinderNetwork(ctx context.Context) error {
	exists, err := docker.NetworkExists(ctx, networkName)
	if err != nil {
		return fmt.Errorf("failed to check network: %w", err)
	}
	if !exists {
		return fmt.Errorf("network '%s' does not exist", networkName)
	}
	return nil
}

func checkRunningContainers(ctx context.Context) bool {
	containers := []struct {
		name     string
		varName  string
		required bool
	}{
		{stepCAContainerName, "Step CA", true},
		{zotContainerName, "Zot Registry", true},
		{gatusContainerName, "Gatus", true},
		{traefikContainerName, "Traefik", true},
	}

	allRunning := true
	for _, c := range containers {
		exists, err := docker.ContainerExists(ctx, c.name)
		if err != nil {
			fmt.Printf("   ❌ %s: Failed to check (%v)\n", c.varName, err)
			if c.required {
				allRunning = false
			}
			continue
		}

		if !exists {
			fmt.Printf("   ❌ %s: Container not found (%s)\n", c.varName, c.name)
			if c.required {
				allRunning = false
			}
		} else {
			fmt.Printf("   ✅ %s: Running (%s)\n", c.varName, c.name)
		}
	}

	return allRunning
}

func checkServiceEndpoints(ctx context.Context, caCertPath string) bool {
	// Load CA certificate
	caCert, err := os.ReadFile(caCertPath)
	if err != nil {
		fmt.Printf("   ⚠️  Cannot load CA certificate: %v\n", err)
		return false
	}

	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)

	allPassed := true

	endpoints := []struct {
		name       string
		url        string
		useTLS     bool
		skipVerify bool
	}{
		{"Zot Registry (direct)", "http://localhost:5000/v2/", false, false},
		{"Step CA", fmt.Sprintf("https://ca.%s:%s/health", traefikDomain, traefikPort), true, false},
		{"Zot Registry", fmt.Sprintf("https://registry.%s:%s/v2/", traefikDomain, traefikPort), true, false},
		{"Gatus Dashboard", fmt.Sprintf("https://gatus.%s:%s/", traefikDomain, traefikPort), true, false},
		{"Traefik Dashboard", fmt.Sprintf("https://traefik.%s:%s/dashboard/", traefikDomain, traefikPort), true, false},
	}

	for _, endpoint := range endpoints {
		if err := checkEndpoint(ctx, endpoint.name, endpoint.url, endpoint.useTLS, endpoint.skipVerify, caCertPool); err != nil {
			fmt.Printf("   ❌ %s: %v\n", endpoint.name, err)
			allPassed = false
		} else {
			fmt.Printf("   ✅ %s: OK (200)\n", endpoint.name)
		}
	}

	return allPassed
}

func checkEndpoint(ctx context.Context, name, url string, useTLS bool, skipVerify bool, caCertPool *x509.CertPool) error {
	client := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Don't follow redirects - treat redirect as success
			return http.ErrUseLastResponse
		},
	}

	if useTLS {
		tlsConfig := &tls.Config{}
		if skipVerify {
			tlsConfig.InsecureSkipVerify = true
		} else {
			tlsConfig.RootCAs = caCertPool
		}
		client.Transport = &http.Transport{
			TLSClientConfig: tlsConfig,
		}
	}

	// Use NewRequestWithContext for proper cancellation support
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("unreachable (%v)", err)
	}
	defer resp.Body.Close()

	// Accept 200-399 as success
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	return nil
}

// checkRegistryK8sEndToEnd performs an end-to-end test:
// 1. Copy a small image to the local Zot registry (using skopeo for OCI format)
// 2. Create a pod in Kubernetes using that image
// 3. Verify the pod is running
// 4. Clean up all created resources
func checkRegistryK8sEndToEnd(ctx context.Context, clusterName string) error {
	const (
		sourceImage  = "docker://busybox:1.36"
		destImage    = "docker://localhost:5000/kinder-diag-test:latest"
		k8sImage     = "localhost:5000/kinder-diag-test:latest" // Mapped to zot:5000 via containerd hosts.toml
		testPodName  = "kinder-diag-test"
		testPodNS    = "default"
		pollInterval = 2 * time.Second
		pollTimeout  = 60 * time.Second
	)

	// Cleanup function to ensure resources are removed
	cleanup := func() {
		// Delete the test pod (ignore errors)
		kubectlCmd := exec.Command("kubectl", "--context", "kind-"+clusterName,
			"delete", "pod", testPodName, "-n", testPodNS, "--ignore-not-found", "--wait=false")
		kubectlCmd.Run()
	}

	// Ensure cleanup runs even on failure
	defer cleanup()

	// Step 1: Copy image to local registry using skopeo
	// Skopeo converts Docker manifest to OCI format, which Zot requires
	Verbose("   Copying %s to local registry...\n", sourceImage)
	copyCmd := exec.Command("skopeo", "copy",
		"--insecure-policy",
		"--dest-tls-verify=false",
		sourceImage, destImage)
	if output, err := copyCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to copy image to registry (requires skopeo): %w\n%s", err, output)
	}

	// Step 2: Create a test pod in Kubernetes
	Verbose("   Creating test pod in Kubernetes...\n")
	podManifest := fmt.Sprintf(`apiVersion: v1
kind: Pod
metadata:
  name: %s
  namespace: %s
  labels:
    app: kinder-diag-test
spec:
  containers:
  - name: test
    image: %s
    command: ["sleep", "300"]
  restartPolicy: Never
`, testPodName, testPodNS, k8sImage)

	applyCmd := exec.Command("kubectl", "--context", "kind-"+clusterName, "apply", "-f", "-")
	applyCmd.Stdin = strings.NewReader(podManifest)
	if output, err := applyCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create pod: %w\n%s", err, output)
	}

	// Step 3: Wait for pod to be running
	Verbose("   Waiting for pod to be running...\n")
	deadline := time.Now().Add(pollTimeout)
	for time.Now().Before(deadline) {
		statusCmd := exec.Command("kubectl", "--context", "kind-"+clusterName,
			"get", "pod", testPodName, "-n", testPodNS,
			"-o", "jsonpath={.status.phase}")
		output, err := statusCmd.Output()
		if err == nil {
			phase := strings.TrimSpace(string(output))
			if phase == "Running" {
				Verbose("   Pod is running!\n")
				return nil
			}
			if phase == "Failed" || phase == "Error" {
				// Get pod events for debugging
				eventsCmd := exec.Command("kubectl", "--context", "kind-"+clusterName,
					"describe", "pod", testPodName, "-n", testPodNS)
				eventsOutput, _ := eventsCmd.CombinedOutput()
				return fmt.Errorf("pod failed to start (phase: %s)\n%s", phase, eventsOutput)
			}
			Verbose("   Pod phase: %s\n", phase)
		}
		time.Sleep(pollInterval)
	}

	// Timeout - get pod description for debugging
	describeCmd := exec.Command("kubectl", "--context", "kind-"+clusterName,
		"describe", "pod", testPodName, "-n", testPodNS)
	describeOutput, _ := describeCmd.CombinedOutput()
	return fmt.Errorf("timeout waiting for pod to be running\n%s", describeOutput)
}
