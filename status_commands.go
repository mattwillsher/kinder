package main

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"codeberg.org/hipkoi/kinder/config"
	"codeberg.org/hipkoi/kinder/docker"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show status of kinder components",
	Long:  `Display the current status of all kinder components including CA certificate, network, containers, and endpoints.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()

		fmt.Println("kinder status")
		fmt.Println("─────────────────────────────────────────")
		fmt.Println()

		// Get data directory
		dataDir, err := config.GetDataDir()
		if err != nil {
			return fmt.Errorf("failed to get data directory: %w", err)
		}

		// CA Certificate status
		fmt.Println("📜 CA Certificate")
		caCertPath := filepath.Join(dataDir, CACertFilename)
		caStatus := checkCAStatus(caCertPath)
		fmt.Println(caStatus)
		fmt.Println()

		// Network status
		fmt.Println("🌐 Network")
		netStatus := checkNetworkStatus(ctx)
		fmt.Println(netStatus)
		fmt.Println()

		// Container status
		fmt.Println("📦 Containers")
		containerStatus := checkContainerStatus(ctx)
		fmt.Println(containerStatus)

		// Kind cluster status
		fmt.Println("☸️ Kind Cluster")
		kindStatus := checkKindClusterStatus(ctx)
		fmt.Println(kindStatus)
		fmt.Println()

		// ArgoCD status (only if Kind cluster exists)
		fmt.Println("🔄 ArgoCD")
		argocdStatus := checkArgoCDStatus(ctx)
		fmt.Println(argocdStatus)
		fmt.Println()

		// Endpoints
		fmt.Println("🔗 Endpoints")
		endpointsStatus := checkEndpointsStatus(ctx)
		fmt.Println(endpointsStatus)

		return nil
	},
}

func checkCAStatus(certPath string) string {
	// Check if file exists
	info, err := os.Stat(certPath)
	if os.IsNotExist(err) {
		return "   ○ Not generated"
	}
	if err != nil {
		return fmt.Sprintf("   ✗ Error: %v", err)
	}

	// Read and parse certificate
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return fmt.Sprintf("   ✗ Cannot read: %v", err)
	}

	block, _ := pem.Decode(certPEM)
	if block == nil {
		return "   ✗ Invalid PEM format"
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return fmt.Sprintf("   ✗ Cannot parse: %v", err)
	}

	// Check validity
	now := time.Now()
	var status string
	if now.Before(cert.NotBefore) {
		status = "⚠ Not yet valid"
	} else if now.After(cert.NotAfter) {
		status = "✗ Expired"
	} else {
		daysLeft := int(cert.NotAfter.Sub(now).Hours() / 24)
		if daysLeft < 30 {
			status = fmt.Sprintf("⚠ Expires in %d days", daysLeft)
		} else {
			status = fmt.Sprintf("● Valid (%d days remaining)", daysLeft)
		}
	}

	return fmt.Sprintf("   %s\n   Path: %s\n   Modified: %s",
		status,
		certPath,
		info.ModTime().Format("2006-01-02 15:04:05"))
}

func checkNetworkStatus(ctx context.Context) string {
	// Get network name from config (apply same derivation as Config.ApplyDefaults)
	netName := config.GetString(config.KeyNetworkName)
	if netName == "" || netName == config.DefaultNetworkName {
		// Derive from app name, same as Config.ApplyDefaults()
		appName := config.GetString(config.KeyAppName)
		if appName == "" {
			appName = config.DefaultAppName
		}
		netName = appName
	}

	exists, err := docker.NetworkExists(ctx, netName)
	if err != nil {
		return fmt.Sprintf("   ✗ Error checking network: %v", err)
	}

	if exists {
		// Get network details
		netID, _ := docker.GetNetworkID(ctx, netName)
		shortID := netID
		if len(shortID) > 12 {
			shortID = shortID[:12]
		}
		return fmt.Sprintf("   ● %s (ID: %s)", netName, shortID)
	}

	return fmt.Sprintf("   ○ %s (not created)", netName)
}

func checkContainerStatus(ctx context.Context) string {
	containers := []struct {
		name    string
		display string
	}{
		{docker.StepCAContainerName, "Step CA"},
		{docker.ZotContainerName, "Zot Registry"},
		{docker.GatusContainerName, "Gatus"},
		{docker.TraefikContainerName, "Traefik"},
	}

	var result string
	for _, c := range containers {
		exists, err := docker.ContainerExists(ctx, c.name)
		if err != nil {
			result += fmt.Sprintf("   ✗ %-12s Error: %v\n", c.display, err)
			continue
		}

		if exists {
			// Get more details about the container
			status := getContainerState(ctx, c.name)
			result += fmt.Sprintf("   ● %-12s %s\n", c.display, status)
		} else {
			result += fmt.Sprintf("   ○ %-12s not running\n", c.display)
		}
	}

	return result
}

func getContainerState(ctx context.Context, name string) string {
	client, err := docker.GetSharedClient()
	if err != nil {
		return "unknown"
	}

	info, err := client.Raw().ContainerInspect(ctx, name)
	if err != nil {
		return "unknown"
	}

	state := info.State
	if state.Running {
		// Calculate uptime
		startedAt, err := time.Parse(time.RFC3339Nano, state.StartedAt)
		if err == nil {
			uptime := time.Since(startedAt)
			return fmt.Sprintf("running (%s)", formatDuration(uptime))
		}
		return "running"
	}

	if state.Paused {
		return "paused"
	}

	if state.Restarting {
		return "restarting"
	}

	return state.Status
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	return fmt.Sprintf("%dd %dh", days, hours)
}

func checkKindClusterStatus(ctx context.Context) string {
	clusterName := config.GetString(config.KeyAppName)
	if clusterName == "" {
		clusterName = config.DefaultAppName
	}

	exists, err := docker.KindExists(clusterName)
	if err != nil {
		return fmt.Sprintf("   ✗ Error checking cluster: %v", err)
	}

	if !exists {
		return fmt.Sprintf("   ○ %s (not created)", clusterName)
	}

	// Get nodes from Kind API
	nodes, err := docker.GetKindNodes(ctx, clusterName)
	if err != nil {
		return fmt.Sprintf("   ● %s (exists, failed to get nodes: %v)", clusterName, err)
	}

	if len(nodes) == 0 {
		return fmt.Sprintf("   ⚠ %s (exists but no nodes found)", clusterName)
	}

	// Count control-plane vs worker nodes
	var controlPlanes, workers int
	for _, node := range nodes {
		if strings.Contains(node, "control-plane") {
			controlPlanes++
		} else if strings.Contains(node, "worker") {
			workers++
		}
	}

	var result string
	nodeDesc := fmt.Sprintf("%d control-plane", controlPlanes)
	if workers > 0 {
		nodeDesc += fmt.Sprintf(", %d worker", workers)
	}
	result = fmt.Sprintf("   ● %s (%s)\n", clusterName, nodeDesc)

	for _, node := range nodes {
		state := getContainerState(ctx, node)
		// Extract role from node name (e.g., "kinder-control-plane" -> "control-plane")
		role := strings.TrimPrefix(node, clusterName+"-")
		result += fmt.Sprintf("     %-20s %s\n", role, state)
	}

	return result
}

func checkEndpointsStatus(ctx context.Context) string {
	domain := config.GetString(config.KeyDomain)
	if domain == "" {
		domain = config.DefaultDomain
	}
	port := config.GetString(config.KeyTraefikPort)
	if port == "" {
		port = config.DefaultTraefikPort
	}

	// Check if any containers are running to determine if endpoints should be shown
	traefikRunning, _ := docker.ContainerExists(ctx, docker.TraefikContainerName)
	if !traefikRunning {
		return "   ○ Services not running"
	}

	var result string
	endpoints := []struct {
		name string
		url  string
	}{
		{"Traefik Dashboard", fmt.Sprintf("https://traefik.%s:%s", domain, port)},
		{"Step CA", fmt.Sprintf("https://ca.%s:%s", domain, port)},
		{"Zot Registry", fmt.Sprintf("https://registry.%s:%s", domain, port)},
		{"Gatus Dashboard", fmt.Sprintf("https://gatus.%s:%s", domain, port)},
		{"Zot (direct)", "http://localhost:5000"},
	}

	for _, ep := range endpoints {
		result += fmt.Sprintf("   %-18s %s\n", ep.name, ep.url)
	}

	return result
}

func checkArgoCDStatus(ctx context.Context) string {
	clusterName := config.GetString(config.KeyAppName)
	if clusterName == "" {
		clusterName = config.DefaultAppName
	}

	// Check if Kind cluster exists first
	exists, err := docker.KindExists(clusterName)
	if err != nil {
		return fmt.Sprintf("   ✗ Error checking cluster: %v", err)
	}
	if !exists {
		return "   ○ Kind cluster not running"
	}

	kubectlContext := "kind-" + clusterName

	// Check if argocd namespace exists
	nsCmd := exec.CommandContext(ctx, "kubectl", "--context", kubectlContext,
		"get", "namespace", "argocd", "-o", "name")
	if err := nsCmd.Run(); err != nil {
		return "   ○ Not installed (namespace 'argocd' not found)"
	}

	// Get argocd-server deployment status
	deployCmd := exec.CommandContext(ctx, "kubectl", "--context", kubectlContext,
		"get", "deployment", "argocd-server", "-n", "argocd",
		"-o", "jsonpath={.status.availableReplicas}/{.status.replicas}")
	output, err := deployCmd.Output()
	if err != nil {
		return "   ⚠ Installed but argocd-server deployment not found"
	}

	replicas := strings.TrimSpace(string(output))
	if replicas == "" || replicas == "/" {
		return "   ⚠ argocd-server deployment exists but no replicas info"
	}

	// Parse replicas (format: "1/1")
	parts := strings.Split(replicas, "/")
	if len(parts) == 2 && parts[0] == parts[1] && parts[0] != "0" {
		// Get version from argocd-server image tag
		version := getArgoCDVersion(ctx, kubectlContext)
		if version != "" {
			return fmt.Sprintf("   ● Running (%s replicas, %s)", replicas, version)
		}
		return fmt.Sprintf("   ● Running (%s replicas)", replicas)
	}

	return fmt.Sprintf("   ⚠ Degraded (%s replicas available)", replicas)
}

// getArgoCDVersion extracts the ArgoCD version from the argocd-server container image
func getArgoCDVersion(ctx context.Context, kubectlContext string) string {
	cmd := exec.CommandContext(ctx, "kubectl", "--context", kubectlContext,
		"get", "deployment", "argocd-server", "-n", "argocd",
		"-o", "jsonpath={.spec.template.spec.containers[0].image}")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	// Image format: quay.io/argoproj/argocd:v2.9.3 or argoproj/argocd:v2.9.3
	image := strings.TrimSpace(string(output))
	if idx := strings.LastIndex(image, ":"); idx != -1 {
		return image[idx+1:]
	}
	return ""
}
