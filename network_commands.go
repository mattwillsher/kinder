package main

import (
	"context"
	"fmt"

	"codeberg.org/hipkoi/kinder/docker"
	"github.com/spf13/cobra"
)

var networkCmd = &cobra.Command{
	Use:   "network",
	Short: "Manage Docker networks",
	Long:  `Commands for creating and managing Docker networks for kinder.`,
}

var networkCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create Docker network",
	Long:  `Create a Docker network for kinder services with configurable CIDR.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()

		// Check if network already exists
		exists, err := docker.NetworkExists(ctx, networkName)
		if err != nil {
			return fmt.Errorf("failed to check if network exists: %w", err)
		}

		if exists {
			fmt.Printf("Network '%s' already exists\n", networkName)
			return nil
		}

		// Create network
		config := docker.NetworkConfig{
			Name:       networkName,
			CIDR:       networkCIDR,
			Driver:     "bridge",
			BridgeName: networkName + "br0",
		}

		networkID, err := docker.CreateNetwork(ctx, config)
		if err != nil {
			return fmt.Errorf("failed to create network: %w", err)
		}

		fmt.Printf("Network created successfully:\n")
		fmt.Printf("  Name: %s\n", networkName)
		fmt.Printf("  CIDR: %s\n", networkCIDR)
		fmt.Printf("  ID: %s\n", networkID)

		return nil
	},
}

var networkRemoveCmd = &cobra.Command{
	Use:   "remove",
	Short: "Remove Docker network",
	Long:  `Remove the Docker network used by kinder.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()

		// Check if network exists
		exists, err := docker.NetworkExists(ctx, networkName)
		if err != nil {
			return fmt.Errorf("failed to check if network exists: %w", err)
		}

		if !exists {
			fmt.Printf("Network '%s' does not exist\n", networkName)
			return nil
		}

		// Remove network
		if err := docker.RemoveNetwork(ctx, networkName); err != nil {
			return fmt.Errorf("failed to remove network: %w", err)
		}

		fmt.Printf("Network '%s' removed successfully\n", networkName)

		return nil
	},
}
