package docker

import (
	"context"
	"testing"
)

func TestNetworkConfig(t *testing.T) {
	config := NetworkConfig{
		Name:   "test-network",
		CIDR:   "172.28.28.0/24",
		Driver: "bridge",
	}

	if config.Name != "test-network" {
		t.Errorf("expected name 'test-network', got '%s'", config.Name)
	}

	if config.CIDR != "172.28.28.0/24" {
		t.Errorf("expected CIDR '172.28.28.0/24', got '%s'", config.CIDR)
	}

	if config.Driver != "bridge" {
		t.Errorf("expected driver 'bridge', got '%s'", config.Driver)
	}
}

func TestDefaultConstants(t *testing.T) {
	if DefaultNetworkCIDR != "172.28.28.0/24" {
		t.Errorf("expected DefaultNetworkCIDR '172.28.28.0/24', got '%s'", DefaultNetworkCIDR)
	}

	if DefaultNetworkName != "kind" {
		t.Errorf("expected DefaultNetworkName 'kind', got '%s'", DefaultNetworkName)
	}
}

func TestDeriveNetworkConfig(t *testing.T) {
	tests := []struct {
		name            string
		cidr            string
		expectedGateway string
		expectedIPRange string
		expectError     bool
	}{
		{
			name:            "standard /24 network",
			cidr:            "172.28.28.0/24",
			expectedGateway: "172.28.28.1",
			expectedIPRange: "172.28.28.0/25",
			expectError:     false,
		},
		{
			name:            "10.0.0.0/16 network",
			cidr:            "10.0.0.0/16",
			expectedGateway: "10.0.0.1",
			expectedIPRange: "10.0.0.0/17",
			expectError:     false,
		},
		{
			name:            "192.168.1.0/24 network",
			cidr:            "192.168.1.0/24",
			expectedGateway: "192.168.1.1",
			expectedIPRange: "192.168.1.0/25",
			expectError:     false,
		},
		{
			name:        "invalid CIDR",
			cidr:        "invalid",
			expectError: true,
		},
		{
			name:        "malformed CIDR",
			cidr:        "172.28.28.0",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gateway, ipRange, err := deriveNetworkConfig(tt.cidr)

			if tt.expectError {
				if err == nil {
					t.Error("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if gateway != tt.expectedGateway {
				t.Errorf("expected gateway '%s', got '%s'", tt.expectedGateway, gateway)
			}

			if ipRange != tt.expectedIPRange {
				t.Errorf("expected IP range '%s', got '%s'", tt.expectedIPRange, ipRange)
			}
		})
	}
}

// TestCreateAndRemoveNetwork tests the full lifecycle of creating and removing a network
// Note: This test requires Docker to be running and will create/remove a real network
func TestCreateAndRemoveNetwork(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	testNetworkName := "kinder-test-network"
	// Use a less common CIDR to avoid conflicts with existing networks
	testCIDR := "172.31.255.0/24"

	// Cleanup any existing test network
	_ = RemoveNetwork(ctx, testNetworkName)

	// Test network creation
	config := NetworkConfig{
		Name:       testNetworkName,
		CIDR:       testCIDR,
		Driver:     "bridge",
		BridgeName: "kindertestbr0",
	}

	networkID, err := CreateNetwork(ctx, config)
	if err != nil {
		t.Fatalf("failed to create network: %v", err)
	}

	if networkID == "" {
		t.Error("expected non-empty network ID")
	}

	// Test that network exists
	exists, err := NetworkExists(ctx, testNetworkName)
	if err != nil {
		t.Fatalf("failed to check network existence: %v", err)
	}

	if !exists {
		t.Error("network should exist after creation")
	}

	// Test getting network ID
	retrievedID, err := GetNetworkID(ctx, testNetworkName)
	if err != nil {
		t.Fatalf("failed to get network ID: %v", err)
	}

	if retrievedID != networkID {
		t.Errorf("expected network ID '%s', got '%s'", networkID, retrievedID)
	}

	// Test creating duplicate network (should return error)
	_, err = CreateNetwork(ctx, config)
	if err == nil {
		t.Error("expected error when creating duplicate network")
	}

	// Test network removal
	err = RemoveNetwork(ctx, testNetworkName)
	if err != nil {
		t.Fatalf("failed to remove network: %v", err)
	}

	// Test that network no longer exists
	exists, err = NetworkExists(ctx, testNetworkName)
	if err != nil {
		t.Fatalf("failed to check network existence after removal: %v", err)
	}

	if exists {
		t.Error("network should not exist after removal")
	}
}

func TestNetworkExists_Nonexistent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	exists, err := NetworkExists(ctx, "nonexistent-network-12345")
	if err != nil {
		t.Fatalf("failed to check network existence: %v", err)
	}

	if exists {
		t.Error("nonexistent network should not exist")
	}
}

func TestGetNetworkID_Nonexistent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	_, err := GetNetworkID(ctx, "nonexistent-network-12345")
	if err == nil {
		t.Error("expected error when getting ID of nonexistent network")
	}
}
