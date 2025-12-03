package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config represents the main configuration structure
type Config struct {
	Server    ServerConfig              `yaml:"server"`
	Resources map[string]ResourceConfig `yaml:"resources"`
}

// ServerConfig holds server-specific settings
type ServerConfig struct {
	SocketPath string `yaml:"socketPath"`
}

// ResourceConfig defines how to display a specific resource type
type ResourceConfig struct {
	// Columns to display in fzf output
	Columns []ColumnConfig `yaml:"columns"`
}

// ColumnConfig defines a single column in the fzf output
type ColumnConfig struct {
	// Name is the column header
	Name string `yaml:"name"`
	// Field is the path to extract from the resource (supports jsonpath-like syntax)
	// Special fields: .metadata.name, .metadata.namespace, .status.phase, .metadata.creationTimestamp
	Field string `yaml:"field"`
	// Width is the fixed width for the column (0 = auto)
	Width int `yaml:"width"`
}

// DefaultConfig returns a sensible default configuration
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			SocketPath: filepath.Join(os.TempDir(), "kfzf.sock"),
		},
		Resources: map[string]ResourceConfig{
			"pods": {
				Columns: []ColumnConfig{
					{Name: "NAME", Field: ".metadata.name", Width: 40},
					{Name: "NAMESPACE", Field: ".metadata.namespace", Width: 40},
					{Name: "STATUS", Field: ".status.phase", Width: 12},
					{Name: "AGE", Field: ".metadata.creationTimestamp", Width: 10},
				},
			},
			"deployments": {
				Columns: []ColumnConfig{
					{Name: "NAME", Field: ".metadata.name", Width: 40},
					{Name: "NAMESPACE", Field: ".metadata.namespace", Width: 40},
					{Name: "READY", Field: ".status.readyReplicas/.spec.replicas", Width: 10},
					{Name: "AGE", Field: ".metadata.creationTimestamp", Width: 10},
				},
			},
			"services": {
				Columns: []ColumnConfig{
					{Name: "NAME", Field: ".metadata.name", Width: 40},
					{Name: "NAMESPACE", Field: ".metadata.namespace", Width: 40},
					{Name: "TYPE", Field: ".spec.type", Width: 12},
					{Name: "CLUSTER-IP", Field: ".spec.clusterIP", Width: 16},
				},
			},
			"configmaps": {
				Columns: []ColumnConfig{
					{Name: "NAME", Field: ".metadata.name", Width: 40},
					{Name: "NAMESPACE", Field: ".metadata.namespace", Width: 40},
					{Name: "AGE", Field: ".metadata.creationTimestamp", Width: 10},
				},
			},
			"secrets": {
				Columns: []ColumnConfig{
					{Name: "NAME", Field: ".metadata.name", Width: 40},
					{Name: "NAMESPACE", Field: ".metadata.namespace", Width: 40},
					{Name: "TYPE", Field: ".type", Width: 30},
					{Name: "AGE", Field: ".metadata.creationTimestamp", Width: 10},
				},
			},
			"nodes": {
				Columns: []ColumnConfig{
					{Name: "NAME", Field: ".metadata.name", Width: 65},
					{Name: "AGE", Field: ".metadata.creationTimestamp", Width: 6},
					{Name: "STATUS", Field: "_nodeStatus", Width: 6},
					{Name: "TOPOLOGY", Field: "_nodeTopology", Width: 25},
				},
			},
			"namespaces": {
				Columns: []ColumnConfig{
					{Name: "NAME", Field: ".metadata.name", Width: 40},
					{Name: "STATUS", Field: ".status.phase", Width: 12},
					{Name: "AGE", Field: ".metadata.creationTimestamp", Width: 10},
				},
			},
			"persistentvolumeclaims": {
				Columns: []ColumnConfig{
					{Name: "NAME", Field: ".metadata.name", Width: 40},
					{Name: "NAMESPACE", Field: ".metadata.namespace", Width: 40},
					{Name: "STATUS", Field: ".status.phase", Width: 10},
					{Name: "CAPACITY", Field: ".spec.resources.requests.storage", Width: 10},
				},
			},
			"persistentvolumes": {
				Columns: []ColumnConfig{
					{Name: "NAME", Field: ".metadata.name", Width: 40},
					{Name: "CAPACITY", Field: ".spec.capacity.storage", Width: 10},
					{Name: "STATUS", Field: ".status.phase", Width: 12},
					{Name: "CLAIM", Field: ".spec.claimRef.name", Width: 30},
				},
			},
			"ingresses": {
				Columns: []ColumnConfig{
					{Name: "NAME", Field: ".metadata.name", Width: 40},
					{Name: "NAMESPACE", Field: ".metadata.namespace", Width: 40},
					{Name: "HOSTS", Field: ".spec.rules[*].host", Width: 40},
				},
			},
			"statefulsets": {
				Columns: []ColumnConfig{
					{Name: "NAME", Field: ".metadata.name", Width: 40},
					{Name: "NAMESPACE", Field: ".metadata.namespace", Width: 40},
					{Name: "READY", Field: ".status.readyReplicas/.spec.replicas", Width: 10},
					{Name: "AGE", Field: ".metadata.creationTimestamp", Width: 10},
				},
			},
			"daemonsets": {
				Columns: []ColumnConfig{
					{Name: "NAME", Field: ".metadata.name", Width: 40},
					{Name: "NAMESPACE", Field: ".metadata.namespace", Width: 40},
					{Name: "DESIRED", Field: ".status.desiredNumberScheduled", Width: 8},
					{Name: "READY", Field: ".status.numberReady", Width: 8},
				},
			},
			"jobs": {
				Columns: []ColumnConfig{
					{Name: "NAME", Field: ".metadata.name", Width: 40},
					{Name: "NAMESPACE", Field: ".metadata.namespace", Width: 40},
					{Name: "COMPLETIONS", Field: ".status.succeeded/.spec.completions", Width: 12},
					{Name: "AGE", Field: ".metadata.creationTimestamp", Width: 10},
				},
			},
			"cronjobs": {
				Columns: []ColumnConfig{
					{Name: "NAME", Field: ".metadata.name", Width: 40},
					{Name: "NAMESPACE", Field: ".metadata.namespace", Width: 40},
					{Name: "SCHEDULE", Field: ".spec.schedule", Width: 20},
					{Name: "SUSPEND", Field: ".spec.suspend", Width: 8},
				},
			},
			// Cluster API clusters
			"clusters.cluster.x-k8s.io": {
				Columns: []ColumnConfig{
					{Name: "NAME", Field: ".metadata.name", Width: 40},
					{Name: "NAMESPACE", Field: ".metadata.namespace", Width: 40},
					{Name: "STATUS", Field: "_capiClusterStatus", Width: 20},
					{Name: "AGE", Field: ".metadata.creationTimestamp", Width: 10},
				},
			},
			// CloudNativePG clusters
			"clusters.postgresql.cnpg.io": {
				Columns: []ColumnConfig{
					{Name: "NAME", Field: ".metadata.name", Width: 40},
					{Name: "NAMESPACE", Field: ".metadata.namespace", Width: 40},
					{Name: "READY", Field: "_cnpgClusterReady", Width: 8},
					{Name: "STATUS", Field: "_cnpgClusterStatus", Width: 28},
					{Name: "AGE", Field: ".metadata.creationTimestamp", Width: 10},
				},
			},
			// ArgoCD Applications
			"applications.argoproj.io": {
				Columns: []ColumnConfig{
					{Name: "NAME", Field: ".metadata.name", Width: 40},
					{Name: "NAMESPACE", Field: ".metadata.namespace", Width: 25},
					{Name: "SYNC", Field: ".status.sync.status", Width: 10},
					{Name: "HEALTH", Field: ".status.health.status", Width: 12},
					{Name: "AGE", Field: ".metadata.creationTimestamp", Width: 10},
				},
			},
			// ArgoCD AppProjects
			"appprojects.argoproj.io": {
				Columns: []ColumnConfig{
					{Name: "NAME", Field: ".metadata.name", Width: 40},
					{Name: "NAMESPACE", Field: ".metadata.namespace", Width: 25},
					{Name: "AGE", Field: ".metadata.creationTimestamp", Width: 10},
				},
			},
			// ArgoCD ApplicationSets
			"applicationsets.argoproj.io": {
				Columns: []ColumnConfig{
					{Name: "NAME", Field: ".metadata.name", Width: 40},
					{Name: "NAMESPACE", Field: ".metadata.namespace", Width: 25},
					{Name: "AGE", Field: ".metadata.creationTimestamp", Width: 10},
				},
			},
			// cert-manager Certificates
			"certificates.cert-manager.io": {
				Columns: []ColumnConfig{
					{Name: "NAME", Field: ".metadata.name", Width: 40},
					{Name: "NAMESPACE", Field: ".metadata.namespace", Width: 40},
					{Name: "READY", Field: "_certReady", Width: 8},
					{Name: "SECRET", Field: ".spec.secretName", Width: 30},
					{Name: "AGE", Field: ".metadata.creationTimestamp", Width: 10},
				},
			},
			// cert-manager ClusterIssuers
			"clusterissuers.cert-manager.io": {
				Columns: []ColumnConfig{
					{Name: "NAME", Field: ".metadata.name", Width: 40},
					{Name: "READY", Field: "_issuerReady", Width: 8},
					{Name: "AGE", Field: ".metadata.creationTimestamp", Width: 10},
				},
			},
			// cert-manager Issuers
			"issuers.cert-manager.io": {
				Columns: []ColumnConfig{
					{Name: "NAME", Field: ".metadata.name", Width: 40},
					{Name: "NAMESPACE", Field: ".metadata.namespace", Width: 40},
					{Name: "READY", Field: "_issuerReady", Width: 8},
					{Name: "AGE", Field: ".metadata.creationTimestamp", Width: 10},
				},
			},
			// Default fallback for any resource type not explicitly configured
			"_default": {
				Columns: []ColumnConfig{
					{Name: "NAME", Field: ".metadata.name", Width: 40},
					{Name: "NAMESPACE", Field: ".metadata.namespace", Width: 40},
					{Name: "AGE", Field: ".metadata.creationTimestamp", Width: 10},
				},
			},
		},
	}
}

// ConfigPath returns the default config file path
func ConfigPath() string {
	if xdgConfig := os.Getenv("XDG_CONFIG_HOME"); xdgConfig != "" {
		return filepath.Join(xdgConfig, "kfzf", "config.yaml")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "kfzf", "config.yaml")
}

// CachePath returns the default cache directory path
func CachePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "kfzf")
}

// Load loads configuration from the default path, merging with defaults
func Load() (*Config, error) {
	return LoadFrom(ConfigPath())
}

// LoadFrom loads configuration from a specific path, merging with defaults
func LoadFrom(path string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Return defaults if no config file exists
			return cfg, nil
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	userCfg := &Config{}
	if err := yaml.Unmarshal(data, userCfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Merge user config with defaults
	if userCfg.Server.SocketPath != "" {
		cfg.Server.SocketPath = userCfg.Server.SocketPath
	}

	// Merge resource configurations
	for resource, resCfg := range userCfg.Resources {
		cfg.Resources[resource] = resCfg
	}

	return cfg, nil
}

// Save saves the configuration to the default path
func (c *Config) Save() error {
	return c.SaveTo(ConfigPath())
}

// SaveTo saves the configuration to a specific path
func (c *Config) SaveTo(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// GetResourceConfig returns the configuration for a resource type, falling back to default
func (c *Config) GetResourceConfig(resourceType string) ResourceConfig {
	if cfg, ok := c.Resources[resourceType]; ok {
		return cfg
	}
	return c.Resources["_default"]
}
