package main

import (
	"context"

	"github.com/spf13/cobra"
)

// Step CA commands
var stepCACmd = &cobra.Command{
	Use:   "stepca",
	Short: "Manage Step CA container",
	Long:  `Commands for managing the Step CA certificate authority container.`,
}

var stepCAStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start Step CA container",
	Long:  `Start the Step CA container using the generated root CA certificate.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return startStepCA(context.Background())
	},
}

var stepCAStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop and remove Step CA container",
	Long:  `Stop and remove the Step CA container.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return stopStepCA(context.Background())
	},
}

// Zot commands
var zotCmd = &cobra.Command{
	Use:   "zot",
	Short: "Manage Zot registry container",
	Long:  `Commands for managing the Zot container registry.`,
}

var zotStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start Zot registry container",
	Long:  `Start the Zot container registry.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return startZot(context.Background())
	},
}

var zotStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop and remove Zot container",
	Long:  `Stop and remove the Zot registry container.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return stopZot(context.Background())
	},
}

// Gatus commands
var gatusCmd = &cobra.Command{
	Use:   "gatus",
	Short: "Manage Gatus health dashboard container",
	Long:  `Commands for managing the Gatus health dashboard.`,
}

var gatusStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start Gatus health dashboard container",
	Long:  `Start the Gatus health dashboard container.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return startGatus(context.Background())
	},
}

var gatusStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop and remove Gatus container",
	Long:  `Stop and remove the Gatus health dashboard container.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return stopGatus(context.Background())
	},
}

// Traefik commands
var traefikCmd = &cobra.Command{
	Use:   "traefik",
	Short: "Manage Traefik reverse proxy container",
	Long:  `Commands for managing the Traefik reverse proxy.`,
}

var traefikStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start Traefik reverse proxy container",
	Long:  `Start the Traefik reverse proxy container.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return startTraefik(context.Background())
	},
}

var traefikStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop and remove Traefik container",
	Long:  `Stop and remove the Traefik reverse proxy container.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return stopTraefik(context.Background())
	},
}
