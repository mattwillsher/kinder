package main

import (
	"fmt"
	"path/filepath"

	"codeberg.org/hipkoi/kinder/config"
	"codeberg.org/hipkoi/kinder/docker"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Config holds all configuration for kinder operations
type Config struct {
	// Application name (used for container prefixes, network name, CA org)
	AppName string

	// Certificate paths
	CertPath string
	KeyPath  string

	// Network configuration
	NetworkName string
	NetworkCIDR string
	BridgeName  string

	// Container names
	StepCAContainerName  string
	ZotContainerName     string
	GatusContainerName   string
	TraefikContainerName string

	// Container images
	StepCAImage  string
	ZotImage     string
	GatusImage   string
	TraefikImage string

	// Traefik configuration
	TraefikPort   string
	TraefikDomain string

	// Registry mirrors for Zot pull-through cache
	RegistryMirrors []string

	// Data directory (computed)
	DataDir string
}

// NewConfig creates a Config with default values
func NewConfig() *Config {
	return &Config{
		AppName:              config.DefaultAppName,
		NetworkName:          config.DefaultNetworkName,
		NetworkCIDR:          config.DefaultNetworkCIDR,
		BridgeName:           config.DefaultBridgeName,
		StepCAContainerName:  docker.StepCAContainerName,
		ZotContainerName:     docker.ZotContainerName,
		GatusContainerName:   docker.GatusContainerName,
		TraefikContainerName: docker.TraefikContainerName,
		StepCAImage:          config.DefaultStepCAImage,
		ZotImage:             config.DefaultZotImage,
		GatusImage:           config.DefaultGatusImage,
		TraefikImage:         config.DefaultTraefikImage,
		TraefikPort:          config.DefaultTraefikPort,
		TraefikDomain:        config.DefaultDomain,
		RegistryMirrors:      config.DefaultRegistryMirrors,
	}
}

// NewConfigFromFile creates a Config by loading from the YAML config file.
// Falls back to defaults for any missing values.
func NewConfigFromFile(fileCfg *config.FileConfig) *Config {
	cfg := NewConfig()

	// Apply file config values (non-empty values override defaults)
	if fileCfg.AppName != "" {
		cfg.AppName = fileCfg.AppName
	}
	if fileCfg.Domain != "" {
		cfg.TraefikDomain = fileCfg.Domain
	}
	if fileCfg.Network.Name != "" {
		cfg.NetworkName = fileCfg.Network.Name
	}
	if fileCfg.Network.CIDR != "" {
		cfg.NetworkCIDR = fileCfg.Network.CIDR
	}
	if fileCfg.Network.Bridge != "" {
		cfg.BridgeName = fileCfg.Network.Bridge
	}
	if fileCfg.Traefik.Port != "" {
		cfg.TraefikPort = fileCfg.Traefik.Port
	}
	if fileCfg.Images.StepCA != "" {
		cfg.StepCAImage = fileCfg.Images.StepCA
	}
	if fileCfg.Images.Zot != "" {
		cfg.ZotImage = fileCfg.Images.Zot
	}
	if fileCfg.Images.Gatus != "" {
		cfg.GatusImage = fileCfg.Images.Gatus
	}
	if fileCfg.Images.Traefik != "" {
		cfg.TraefikImage = fileCfg.Images.Traefik
	}
	if len(fileCfg.RegistryMirrors) > 0 {
		cfg.RegistryMirrors = fileCfg.RegistryMirrors
	}

	return cfg
}

// EnsureDefaults fills in any empty fields with sensible defaults.
// This should be called after flag parsing but before use.
func (c *Config) EnsureDefaults() error {
	// Ensure app name is set
	if c.AppName == "" {
		c.AppName = config.DefaultAppName
	}

	// Get data directory if not set (uses app name)
	if c.DataDir == "" {
		dataDir, err := getDataDirForApp(c.AppName)
		if err != nil {
			return err
		}
		c.DataDir = dataDir
	}

	// Set default cert paths if not provided
	if c.CertPath == "" {
		c.CertPath = filepath.Join(c.DataDir, CACertFilename)
	}
	if c.KeyPath == "" {
		c.KeyPath = filepath.Join(c.DataDir, CAKeyFilename)
	}

	// Derive network name from app name if using default
	if c.NetworkName == "" || c.NetworkName == config.DefaultNetworkName {
		c.NetworkName = c.AppName
	}

	// Derive bridge name from app name if using default
	if c.BridgeName == "" || c.BridgeName == config.DefaultBridgeName {
		c.BridgeName = c.AppName + "br0"
	}

	// Derive container names from app name
	if c.StepCAContainerName == "" || c.StepCAContainerName == docker.StepCAContainerName {
		c.StepCAContainerName = c.AppName + "-step-ca"
	}
	if c.ZotContainerName == "" || c.ZotContainerName == docker.ZotContainerName {
		c.ZotContainerName = c.AppName + "-zot"
	}
	if c.GatusContainerName == "" || c.GatusContainerName == docker.GatusContainerName {
		c.GatusContainerName = c.AppName + "-gatus"
	}
	if c.TraefikContainerName == "" || c.TraefikContainerName == docker.TraefikContainerName {
		c.TraefikContainerName = c.AppName + "-traefik"
	}

	// Ensure registry mirrors has defaults
	if len(c.RegistryMirrors) == 0 {
		c.RegistryMirrors = config.DefaultRegistryMirrors
	}

	return nil
}

// AllContainerNames returns all container names for iteration
func (c *Config) AllContainerNames() []string {
	return []string{
		c.StepCAContainerName,
		c.ZotContainerName,
		c.GatusContainerName,
		c.TraefikContainerName,
	}
}

// bindFlagsToViper binds CLI flag values to Viper.
// This is called after flag parsing to override config file/env values.
func bindFlagsToViper(cmd *cobra.Command) {
	// Visit all flags (including persistent flags from parent commands)
	cmd.Flags().VisitAll(bindFlag)
	cmd.PersistentFlags().VisitAll(bindFlag)
}

// bindFlag binds a single flag to Viper if it was explicitly set
func bindFlag(f *pflag.Flag) {
	if !f.Changed {
		return
	}

	// Map flag names to Viper keys
	key := flagToViperKey(f.Name)
	if key != "" {
		config.Set(key, f.Value.String())
	}
}

// flagToViperKey maps CLI flag names to Viper configuration keys
func flagToViperKey(flagName string) string {
	mapping := map[string]string{
		"cert":           config.KeyCertPath,
		"key":            config.KeyKeyPath,
		"data-dir":       config.KeyDataDir,
		"network":        config.KeyNetworkName,
		"cidr":           config.KeyNetworkCIDR,
		"domain":         config.KeyDomain,
		"traefik-port":   config.KeyTraefikPort,
		"traefik-domain": config.KeyDomain,
		"port":           config.KeyTraefikPort,
		"stepca-image":   config.KeyImagesStepCA,
		"zot-image":      config.KeyImagesZot,
		"gatus-image":    config.KeyImagesGatus,
		"traefik-image":  config.KeyImagesTraefik,
		"image":          "", // Context-dependent, handled separately
	}
	return mapping[flagName]
}

// buildConfigFromFlags creates a Config from Viper configuration.
// Viper already handles the precedence: CLI flags > env vars > config file > defaults.
func buildConfigFromFlags() (*Config, error) {
	// Get the merged configuration from Viper
	fileCfg, err := config.Get()
	if err != nil {
		return nil, fmt.Errorf("failed to get configuration: %w", err)
	}

	// Create Config from Viper values
	cfg := NewConfigFromFile(fileCfg)

	// Apply any remaining CLI flag overrides that aren't in Viper
	// (for flags like container names that use a different naming pattern)
	if stepCAContainerName != "" && stepCAContainerName != docker.StepCAContainerName {
		cfg.StepCAContainerName = stepCAContainerName
	}
	if zotContainerName != "" && zotContainerName != docker.ZotContainerName {
		cfg.ZotContainerName = zotContainerName
	}
	if gatusContainerName != "" && gatusContainerName != docker.GatusContainerName {
		cfg.GatusContainerName = gatusContainerName
	}
	if traefikContainerName != "" && traefikContainerName != docker.TraefikContainerName {
		cfg.TraefikContainerName = traefikContainerName
	}

	// Apply cert/key paths from Viper if set
	if certPath := config.GetString(config.KeyCertPath); certPath != "" {
		cfg.CertPath = certPath
	}
	if keyPath := config.GetString(config.KeyKeyPath); keyPath != "" {
		cfg.KeyPath = keyPath
	}

	if err := cfg.EnsureDefaults(); err != nil {
		return nil, err
	}

	return cfg, nil
}
