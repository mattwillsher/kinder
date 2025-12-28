package main

import (
	"fmt"
	"os"

	"codeberg.org/hipkoi/kinder/config"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage kinder configuration",
	Long:  `Commands for managing and viewing kinder configuration.`,
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show current configuration",
	Long: `Display the current configuration in YAML format.

This shows the effective configuration including:
- Values from the config file (if present)
- Environment variable overrides
- CLI flag overrides
- Default values for any unset options

The output can be redirected to create a config file:
  kinder config show > ~/.config/kinder/config.yaml`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Get the current configuration from Viper
		cfg, err := config.Get()
		if err != nil {
			return fmt.Errorf("failed to get configuration: %w", err)
		}

		// Apply defaults to ensure all fields are populated
		cfg.ApplyDefaults()

		// Get effective data directory (may come from env var or config)
		dataDir, _ := config.GetDataDir()
		cfg.DataDir = dataDir

		// Marshal to YAML
		output, err := yaml.Marshal(cfg)
		if err != nil {
			return fmt.Errorf("failed to marshal configuration: %w", err)
		}

		// Print header comment
		fmt.Println("# kinder configuration")
		fmt.Println("# Generated with current settings including defaults")
		if configFile := config.ConfigFile(); configFile != "" {
			fmt.Printf("# Loaded from: %s\n", configFile)
		}
		fmt.Println()

		// Print the YAML
		fmt.Print(string(output))

		return nil
	},
}

var configPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Show configuration file path",
	Long:  `Display the path where kinder looks for its configuration file.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		configPath, err := config.GetConfigPath(config.DefaultAppName)
		if err != nil {
			return fmt.Errorf("failed to get config path: %w", err)
		}

		fmt.Println("Configuration file path:", configPath)

		// Check if file exists
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			fmt.Println("Status: Not found (using defaults)")
		} else {
			fmt.Println("Status: Found")
		}

		// Show if a config file was loaded
		if loadedPath := config.ConfigFile(); loadedPath != "" {
			fmt.Println("Loaded from:", loadedPath)
		}

		return nil
	},
}

var configInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Create a default configuration file",
	Long: `Create a configuration file with default values.

This creates a new config file at the default location with all
options set to their default values. You can then edit this file
to customize your configuration.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		configPath, err := config.GetConfigPath(config.DefaultAppName)
		if err != nil {
			return fmt.Errorf("failed to get config path: %w", err)
		}

		// Check if file already exists
		if _, err := os.Stat(configPath); err == nil {
			return fmt.Errorf("config file already exists at %s (use --force to overwrite)", configPath)
		}

		// Get configuration with defaults
		cfg, err := config.Get()
		if err != nil {
			return fmt.Errorf("failed to get configuration: %w", err)
		}
		cfg.ApplyDefaults()

		// Marshal to YAML
		output, err := yaml.Marshal(cfg)
		if err != nil {
			return fmt.Errorf("failed to marshal configuration: %w", err)
		}

		// Create directory if needed
		configDir, err := config.GetConfigDir(config.DefaultAppName)
		if err != nil {
			return fmt.Errorf("failed to get config directory: %w", err)
		}
		if err := os.MkdirAll(configDir, 0755); err != nil {
			return fmt.Errorf("failed to create config directory: %w", err)
		}

		// Write config file with header
		file, err := os.Create(configPath)
		if err != nil {
			return fmt.Errorf("failed to create config file: %w", err)
		}
		defer file.Close()

		fmt.Fprintln(file, "# kinder configuration")
		fmt.Fprintln(file, "# See 'kinder config show' for current effective configuration")
		fmt.Fprintln(file)
		file.Write(output)

		fmt.Printf("Created config file at: %s\n", configPath)
		return nil
	},
}
