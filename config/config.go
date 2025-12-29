// Package config provides Viper-based configuration management for kinder.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// Default values for configuration
const (
	DefaultAppName      = "kinder"
	DefaultDomain       = "c0000201.sslip.io"
	DefaultNetworkName  = "kind"
	DefaultNetworkCIDR  = "172.28.28.0/24"
	DefaultBridgeName   = "kindbr0"
	DefaultTraefikPort  = "8443"
	DefaultStepCAImage  = "smallstep/step-ca:latest"
	DefaultZotImage     = "ghcr.io/project-zot/zot-linux-amd64:latest"
	DefaultGatusImage   = "twinproduction/gatus:latest"
	DefaultTraefikImage = "traefik:latest"
	// DefaultArgocdVersion is the latest patch of the previous minor version
	// Current stable: v3.2.x, so default to latest v3.1.x for stability
	DefaultArgocdVersion     = "v3.1.10"
	DefaultArgocdManifestURL = "https://raw.githubusercontent.com/mattwillsher/kinder-argo/refs/heads/main/root-app.yaml"
)

// Config keys for Viper (use these constants to avoid typos)
const (
	KeyAppName           = "appName"
	KeyDataDir           = "dataDir"
	KeyDomain            = "domain"
	KeyNetworkName       = "network.name"
	KeyNetworkCIDR       = "network.cidr"
	KeyNetworkBridge     = "network.bridge"
	KeyTraefikPort       = "traefik.port"
	KeyImagesStepCA      = "images.stepca"
	KeyImagesZot         = "images.zot"
	KeyImagesGatus       = "images.gatus"
	KeyImagesTraefik     = "images.traefik"
	KeyRegistryMirrors   = "registryMirrors"
	KeyCertPath          = "certPath"
	KeyKeyPath           = "keyPath"
	KeyArgocdVersion     = "argocd.version"
	KeyArgocdManifestURL = "argocd.manifestURL"
)

// DefaultRegistryMirrors is the default list of registries to mirror
var DefaultRegistryMirrors = []string{
	"ghcr.io",
	"registry-1.docker.io",
	"quay.io",
	"registry.k8s.io",
}

// NetworkConfig holds network-related configuration
type NetworkConfig struct {
	Name   string `mapstructure:"name" yaml:"name,omitempty"`
	CIDR   string `mapstructure:"cidr" yaml:"cidr,omitempty"`
	Bridge string `mapstructure:"bridge" yaml:"bridge,omitempty"`
}

// TraefikConfig holds Traefik-related configuration
type TraefikConfig struct {
	Port string `mapstructure:"port" yaml:"port,omitempty"`
}

// ArgocdConfig holds ArgoCD-related configuration
type ArgocdConfig struct {
	Version     string `mapstructure:"version" yaml:"version,omitempty"`
	ManifestURL string `mapstructure:"manifestURL" yaml:"manifestURL,omitempty"`
}

// ImagesConfig holds container image configuration
type ImagesConfig struct {
	StepCA  string `mapstructure:"stepca" yaml:"stepca,omitempty"`
	Zot     string `mapstructure:"zot" yaml:"zot,omitempty"`
	Gatus   string `mapstructure:"gatus" yaml:"gatus,omitempty"`
	Traefik string `mapstructure:"traefik" yaml:"traefik,omitempty"`
}

// FileConfig represents the configuration file structure
type FileConfig struct {
	AppName         string        `mapstructure:"appName" yaml:"appName,omitempty"`
	DataDir         string        `mapstructure:"dataDir" yaml:"dataDir,omitempty"`
	Domain          string        `mapstructure:"domain" yaml:"domain,omitempty"`
	Network         NetworkConfig `mapstructure:"network" yaml:"network,omitempty"`
	Traefik         TraefikConfig `mapstructure:"traefik" yaml:"traefik,omitempty"`
	Argocd          ArgocdConfig  `mapstructure:"argocd" yaml:"argocd,omitempty"`
	Images          ImagesConfig  `mapstructure:"images" yaml:"images,omitempty"`
	RegistryMirrors []string      `mapstructure:"registryMirrors" yaml:"registryMirrors,omitempty"`
	CertPath        string        `mapstructure:"certPath" yaml:"certPath,omitempty"`
	KeyPath         string        `mapstructure:"keyPath" yaml:"keyPath,omitempty"`
}

// V is the global Viper instance for kinder configuration
var V *viper.Viper

// Initialize sets up Viper with defaults and loads configuration.
// Call this before using any configuration values.
func Initialize(configPath string) error {
	V = viper.New()

	// Set defaults
	setDefaults(V)

	// Configure environment variable binding
	// KINDER_TRAEFIK_PORT -> traefik.port
	V.SetEnvPrefix("KINDER")
	V.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	V.AutomaticEnv()

	// Set up config file
	if configPath != "" {
		V.SetConfigFile(configPath)
	} else {
		configDir, err := GetConfigDir(DefaultAppName)
		if err != nil {
			return err
		}
		V.SetConfigName("config")
		V.SetConfigType("yaml")
		V.AddConfigPath(configDir)
	}

	// Read config file (ignore "file not found" errors)
	if err := V.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			// Only return error if it's not a "file not found" error
			if !os.IsNotExist(err) {
				return err
			}
		}
	}

	return nil
}

// setDefaults configures all default values
func setDefaults(v *viper.Viper) {
	v.SetDefault(KeyAppName, DefaultAppName)
	v.SetDefault(KeyDomain, DefaultDomain)
	v.SetDefault(KeyNetworkName, DefaultNetworkName)
	v.SetDefault(KeyNetworkCIDR, DefaultNetworkCIDR)
	v.SetDefault(KeyNetworkBridge, DefaultBridgeName)
	v.SetDefault(KeyTraefikPort, DefaultTraefikPort)
	v.SetDefault(KeyArgocdVersion, DefaultArgocdVersion)
	v.SetDefault(KeyArgocdManifestURL, DefaultArgocdManifestURL)
	v.SetDefault(KeyImagesStepCA, DefaultStepCAImage)
	v.SetDefault(KeyImagesZot, DefaultZotImage)
	v.SetDefault(KeyImagesGatus, DefaultGatusImage)
	v.SetDefault(KeyImagesTraefik, DefaultTraefikImage)
	v.SetDefault(KeyRegistryMirrors, DefaultRegistryMirrors)
}

// GetDataDir returns the data directory for kinder.
// Checks config/env first, then falls back to XDG_DATA_HOME, then ~/.local/share.
func GetDataDir() (string, error) {
	// Check if explicitly configured via Viper (config file or env var)
	if V != nil {
		if dataDir := V.GetString(KeyDataDir); dataDir != "" {
			return dataDir, nil
		}
	}

	// Fall back to XDG standard
	appName := DefaultAppName
	if V != nil {
		if name := V.GetString(KeyAppName); name != "" {
			appName = name
		}
	}

	var baseDir string
	if xdgData := os.Getenv("XDG_DATA_HOME"); xdgData != "" {
		baseDir = xdgData
	} else {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		baseDir = filepath.Join(homeDir, ".local", "share")
	}

	return filepath.Join(baseDir, appName), nil
}

// GetConfigDir returns the configuration directory path following XDG Base Directory spec.
// It checks XDG_CONFIG_HOME first, then falls back to ~/.config
func GetConfigDir(appName string) (string, error) {
	var baseDir string

	if xdgConfig := os.Getenv("XDG_CONFIG_HOME"); xdgConfig != "" {
		baseDir = xdgConfig
	} else {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		baseDir = filepath.Join(homeDir, ".config")
	}

	return filepath.Join(baseDir, appName), nil
}

// GetConfigPath returns the full path to the config file
func GetConfigPath(appName string) (string, error) {
	configDir, err := GetConfigDir(appName)
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "config.yaml"), nil
}

// Get returns the current configuration as a FileConfig struct.
// Returns an error if unmarshaling fails.
func Get() (*FileConfig, error) {
	if V == nil {
		// Initialize with defaults if not yet initialized
		V = viper.New()
		setDefaults(V)
	}

	cfg := &FileConfig{}
	if err := V.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}
	return cfg, nil
}

// MustGet returns the current configuration, panicking on error.
// Use only when configuration is guaranteed to be valid (after successful Initialize).
func MustGet() *FileConfig {
	cfg, err := Get()
	if err != nil {
		panic(fmt.Sprintf("config.MustGet: %v", err))
	}
	return cfg
}

// GetString returns a string configuration value
func GetString(key string) string {
	if V == nil {
		return ""
	}
	return V.GetString(key)
}

// GetStringSlice returns a string slice configuration value
func GetStringSlice(key string) []string {
	if V == nil {
		return nil
	}
	return V.GetStringSlice(key)
}

// Set sets a configuration value (useful for CLI flag overrides)
func Set(key string, value interface{}) {
	if V == nil {
		V = viper.New()
		setDefaults(V)
	}
	V.Set(key, value)
}

// LoadConfigFromDefaultPath loads configuration from the default XDG path.
// Deprecated: Use Initialize() instead.
func LoadConfigFromDefaultPath() (*FileConfig, error) {
	if err := Initialize(""); err != nil {
		return nil, err
	}
	return Get()
}

// LoadConfig loads configuration from the specified path.
// Deprecated: Use Initialize(path) instead.
func LoadConfig(path string) (*FileConfig, error) {
	if err := Initialize(path); err != nil {
		return nil, err
	}
	return Get()
}

// ApplyDefaults fills in any missing values with sensible defaults
func (c *FileConfig) ApplyDefaults() {
	if c.AppName == "" {
		c.AppName = DefaultAppName
	}
	if c.Domain == "" {
		c.Domain = DefaultDomain
	}
	if c.Network.Name == "" {
		c.Network.Name = c.AppName
	}
	if c.Network.CIDR == "" {
		c.Network.CIDR = DefaultNetworkCIDR
	}
	if c.Network.Bridge == "" {
		c.Network.Bridge = c.AppName + "br0"
	}
	if c.Traefik.Port == "" {
		c.Traefik.Port = DefaultTraefikPort
	}
	if c.Argocd.Version == "" {
		c.Argocd.Version = DefaultArgocdVersion
	}
	if c.Argocd.ManifestURL == "" {
		c.Argocd.ManifestURL = DefaultArgocdManifestURL
	}
	if c.Images.StepCA == "" {
		c.Images.StepCA = DefaultStepCAImage
	}
	if c.Images.Zot == "" {
		c.Images.Zot = DefaultZotImage
	}
	if c.Images.Gatus == "" {
		c.Images.Gatus = DefaultGatusImage
	}
	if c.Images.Traefik == "" {
		c.Images.Traefik = DefaultTraefikImage
	}
	if len(c.RegistryMirrors) == 0 {
		c.RegistryMirrors = DefaultRegistryMirrors
	}
}

// ContainerName returns a container name with the app name prefix
func (c *FileConfig) ContainerName(service string) string {
	return c.AppName + "-" + service
}

// ConfigFile returns the config file path that was used, or empty if none
func ConfigFile() string {
	if V == nil {
		return ""
	}
	return V.ConfigFileUsed()
}
