package docker

import (
	"context"
	"fmt"
	"net"

	"github.com/docker/docker/api/types/network"
)

const (
	// DefaultNetworkCIDR is the default CIDR for the kinder network
	DefaultNetworkCIDR = "172.28.28.0/24"
	// DefaultNetworkName is the name of the docker network
	DefaultNetworkName = "kind"
)

// NetworkConfig holds configuration for creating a docker network
type NetworkConfig struct {
	Name       string
	CIDR       string
	Driver     string
	BridgeName string
}

// CreateNetwork creates a docker network with the specified configuration
func CreateNetwork(ctx context.Context, config NetworkConfig) (string, error) {
	c, err := GetSharedClient()
	if err != nil {
		return "", err
	}
	cli := c.Raw()

	// Check if network already exists
	networks, err := cli.NetworkList(ctx, network.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to list networks: %w", err)
	}

	for _, net := range networks {
		if net.Name == config.Name {
			return net.ID, fmt.Errorf("network %s already exists", config.Name)
		}
	}

	// Parse CIDR to derive gateway and IP range
	gateway, ipRange, err := deriveNetworkConfig(config.CIDR)
	if err != nil {
		return "", fmt.Errorf("failed to parse network CIDR: %w", err)
	}

	// Create network with restricted IP range for containers
	// Container DHCP uses first half of subnet, second half reserved for MetalLB
	ipamConfig := network.IPAMConfig{
		Subnet:  config.CIDR,
		IPRange: ipRange,
		Gateway: gateway,
	}

	enableIPv6 := false
	// Use bridge name from config, fallback to network name + "br0"
	bridgeName := config.BridgeName
	if bridgeName == "" {
		bridgeName = config.Name + "br0"
	}

	networkCreate := network.CreateOptions{
		Driver: config.Driver,
		IPAM: &network.IPAM{
			Config: []network.IPAMConfig{ipamConfig},
		},
		EnableIPv6: &enableIPv6,
		Options: map[string]string{
			"com.docker.network.bridge.name": bridgeName,
		},
	}

	resp, err := cli.NetworkCreate(ctx, config.Name, networkCreate)
	if err != nil {
		return "", fmt.Errorf("failed to create network: %w", err)
	}

	return resp.ID, nil
}

// RemoveNetwork removes a docker network by name
func RemoveNetwork(ctx context.Context, name string) error {
	c, err := GetSharedClient()
	if err != nil {
		return err
	}

	if err := c.Raw().NetworkRemove(ctx, name); err != nil {
		return fmt.Errorf("failed to remove network: %w", err)
	}

	return nil
}

// NetworkExists checks if a docker network exists
func NetworkExists(ctx context.Context, name string) (bool, error) {
	c, err := GetSharedClient()
	if err != nil {
		return false, err
	}

	networks, err := c.Raw().NetworkList(ctx, network.ListOptions{})
	if err != nil {
		return false, fmt.Errorf("failed to list networks: %w", err)
	}

	for _, net := range networks {
		if net.Name == name {
			return true, nil
		}
	}

	return false, nil
}

// GetNetworkID returns the ID of a network by name
func GetNetworkID(ctx context.Context, name string) (string, error) {
	c, err := GetSharedClient()
	if err != nil {
		return "", err
	}

	networks, err := c.Raw().NetworkList(ctx, network.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to list networks: %w", err)
	}

	for _, net := range networks {
		if net.Name == name {
			return net.ID, nil
		}
	}

	return "", fmt.Errorf("network %s not found", name)
}

// deriveNetworkConfig calculates gateway and IP range from a CIDR.
// Gateway is set to the first usable IP (e.g., x.x.x.1).
// IP range is set to the first half of the subnet (adds 1 to prefix length).
func deriveNetworkConfig(cidr string) (gateway string, ipRange string, err error) {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", "", fmt.Errorf("invalid CIDR %s: %w", cidr, err)
	}

	// Calculate gateway (first usable IP: network + 1)
	gatewayIP := make(net.IP, len(ipNet.IP))
	copy(gatewayIP, ipNet.IP)
	gatewayIP[len(gatewayIP)-1]++
	gateway = gatewayIP.String()

	// Calculate IP range (first half of subnet)
	// Increase prefix length by 1 to get first half
	ones, bits := ipNet.Mask.Size()
	if ones >= bits-1 {
		// Subnet too small to split, use entire range
		ipRange = cidr
	} else {
		ipRange = fmt.Sprintf("%s/%d", ipNet.IP.String(), ones+1)
	}

	return gateway, ipRange, nil
}
