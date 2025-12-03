package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg == nil {
		t.Fatal("DefaultConfig() returned nil")
	}

	// Check server config
	if cfg.Server.SocketPath == "" {
		t.Error("SocketPath should not be empty")
	}

	// Check that essential resources are configured
	essentialResources := []string{
		"pods",
		"deployments",
		"services",
		"configmaps",
		"secrets",
		"nodes",
		"namespaces",
		"persistentvolumeclaims",
		"persistentvolumes",
		"ingresses",
		"statefulsets",
		"daemonsets",
		"jobs",
		"cronjobs",
		"_default",
	}

	for _, res := range essentialResources {
		if _, ok := cfg.Resources[res]; !ok {
			t.Errorf("DefaultConfig() should have resource %s", res)
		}
	}
}

func TestDefaultConfig_Pods(t *testing.T) {
	cfg := DefaultConfig()
	podsCfg := cfg.Resources["pods"]

	if len(podsCfg.Columns) == 0 {
		t.Fatal("Pods config should have columns")
	}

	// Check NAME column exists
	hasName := false
	hasNamespace := false
	hasStatus := false
	hasAge := false

	for _, col := range podsCfg.Columns {
		switch col.Name {
		case "NAME":
			hasName = true
			if col.Field != ".metadata.name" {
				t.Errorf("NAME field = %s, want .metadata.name", col.Field)
			}
		case "NAMESPACE":
			hasNamespace = true
			if col.Field != ".metadata.namespace" {
				t.Errorf("NAMESPACE field = %s, want .metadata.namespace", col.Field)
			}
		case "STATUS":
			hasStatus = true
			if col.Field != ".status.phase" {
				t.Errorf("STATUS field = %s, want .status.phase", col.Field)
			}
		case "AGE":
			hasAge = true
			if col.Field != ".metadata.creationTimestamp" {
				t.Errorf("AGE field = %s, want .metadata.creationTimestamp", col.Field)
			}
		}
	}

	if !hasName {
		t.Error("Pods config should have NAME column")
	}
	if !hasNamespace {
		t.Error("Pods config should have NAMESPACE column")
	}
	if !hasStatus {
		t.Error("Pods config should have STATUS column")
	}
	if !hasAge {
		t.Error("Pods config should have AGE column")
	}
}

func TestDefaultConfig_Nodes(t *testing.T) {
	cfg := DefaultConfig()
	nodesCfg := cfg.Resources["nodes"]

	if len(nodesCfg.Columns) == 0 {
		t.Fatal("Nodes config should have columns")
	}

	// Check special fields
	hasStatus := false
	hasTopology := false

	for _, col := range nodesCfg.Columns {
		switch col.Name {
		case "STATUS":
			hasStatus = true
			if col.Field != "_nodeStatus" {
				t.Errorf("STATUS field = %s, want _nodeStatus", col.Field)
			}
		case "TOPOLOGY":
			hasTopology = true
			if col.Field != "_nodeTopology" {
				t.Errorf("TOPOLOGY field = %s, want _nodeTopology", col.Field)
			}
		}
	}

	if !hasStatus {
		t.Error("Nodes config should have STATUS column with _nodeStatus")
	}
	if !hasTopology {
		t.Error("Nodes config should have TOPOLOGY column with _nodeTopology")
	}
}

func TestDefaultConfig_Deployments(t *testing.T) {
	cfg := DefaultConfig()
	deploysCfg := cfg.Resources["deployments"]

	// Check READY column uses ratio field
	hasReady := false
	for _, col := range deploysCfg.Columns {
		if col.Name == "READY" {
			hasReady = true
			if col.Field != ".status.readyReplicas/.spec.replicas" {
				t.Errorf("READY field = %s, want .status.readyReplicas/.spec.replicas", col.Field)
			}
		}
	}

	if !hasReady {
		t.Error("Deployments config should have READY column")
	}
}

func TestDefaultConfig_Ingresses(t *testing.T) {
	cfg := DefaultConfig()
	ingressCfg := cfg.Resources["ingresses"]

	// Check HOSTS column uses array field
	hasHosts := false
	for _, col := range ingressCfg.Columns {
		if col.Name == "HOSTS" {
			hasHosts = true
			if col.Field != ".spec.rules[*].host" {
				t.Errorf("HOSTS field = %s, want .spec.rules[*].host", col.Field)
			}
		}
	}

	if !hasHosts {
		t.Error("Ingresses config should have HOSTS column")
	}
}

func TestDefaultConfig_CRDs(t *testing.T) {
	cfg := DefaultConfig()

	crds := []string{
		"clusters.cluster.x-k8s.io",
		"clusters.postgresql.cnpg.io",
		"applications.argoproj.io",
		"appprojects.argoproj.io",
		"applicationsets.argoproj.io",
	}

	for _, crd := range crds {
		if _, ok := cfg.Resources[crd]; !ok {
			t.Errorf("DefaultConfig() should have CRD %s", crd)
		}
	}
}

func TestDefaultConfig_CapiCluster(t *testing.T) {
	cfg := DefaultConfig()
	capiCfg := cfg.Resources["clusters.cluster.x-k8s.io"]

	hasStatus := false
	for _, col := range capiCfg.Columns {
		if col.Name == "STATUS" {
			hasStatus = true
			if col.Field != "_capiClusterStatus" {
				t.Errorf("STATUS field = %s, want _capiClusterStatus", col.Field)
			}
		}
	}

	if !hasStatus {
		t.Error("CAPI cluster config should have STATUS column with _capiClusterStatus")
	}
}

func TestDefaultConfig_CnpgCluster(t *testing.T) {
	cfg := DefaultConfig()
	cnpgCfg := cfg.Resources["clusters.postgresql.cnpg.io"]

	hasReady := false
	hasStatus := false

	for _, col := range cnpgCfg.Columns {
		switch col.Name {
		case "READY":
			hasReady = true
			if col.Field != "_cnpgClusterReady" {
				t.Errorf("READY field = %s, want _cnpgClusterReady", col.Field)
			}
		case "STATUS":
			hasStatus = true
			if col.Field != "_cnpgClusterStatus" {
				t.Errorf("STATUS field = %s, want _cnpgClusterStatus", col.Field)
			}
		}
	}

	if !hasReady {
		t.Error("CNPG cluster config should have READY column")
	}
	if !hasStatus {
		t.Error("CNPG cluster config should have STATUS column")
	}
}

func TestDefaultConfig_ArgoCD(t *testing.T) {
	cfg := DefaultConfig()
	argoCfg := cfg.Resources["applications.argoproj.io"]

	hasSync := false
	hasHealth := false

	for _, col := range argoCfg.Columns {
		switch col.Name {
		case "SYNC":
			hasSync = true
			if col.Field != ".status.sync.status" {
				t.Errorf("SYNC field = %s, want .status.sync.status", col.Field)
			}
		case "HEALTH":
			hasHealth = true
			if col.Field != ".status.health.status" {
				t.Errorf("HEALTH field = %s, want .status.health.status", col.Field)
			}
		}
	}

	if !hasSync {
		t.Error("ArgoCD Application config should have SYNC column")
	}
	if !hasHealth {
		t.Error("ArgoCD Application config should have HEALTH column")
	}
}

func TestGetResourceConfig(t *testing.T) {
	cfg := DefaultConfig()

	// Known resource
	podsCfg := cfg.GetResourceConfig("pods")
	if len(podsCfg.Columns) == 0 {
		t.Error("GetResourceConfig(pods) should return pod config")
	}

	// Unknown resource should return default
	unknownCfg := cfg.GetResourceConfig("unknownresource")
	defaultCfg := cfg.Resources["_default"]
	if len(unknownCfg.Columns) != len(defaultCfg.Columns) {
		t.Error("GetResourceConfig(unknown) should return _default config")
	}
}

func TestConfigPath(t *testing.T) {
	path := ConfigPath()
	if path == "" {
		t.Error("ConfigPath() should not return empty string")
	}
	if !filepath.IsAbs(path) {
		t.Error("ConfigPath() should return absolute path")
	}
	if !contains(path, "kfzf") {
		t.Error("ConfigPath() should contain 'kfzf'")
	}
	if !contains(path, "config.yaml") {
		t.Error("ConfigPath() should contain 'config.yaml'")
	}
}

func TestConfigPath_XDG(t *testing.T) {
	// Save original and restore after test
	original := os.Getenv("XDG_CONFIG_HOME")
	defer os.Setenv("XDG_CONFIG_HOME", original)

	os.Setenv("XDG_CONFIG_HOME", "/custom/config")
	path := ConfigPath()

	if path != "/custom/config/kfzf/config.yaml" {
		t.Errorf("ConfigPath() with XDG = %s, want /custom/config/kfzf/config.yaml", path)
	}
}

func TestCachePath(t *testing.T) {
	path := CachePath()
	if path == "" {
		t.Error("CachePath() should not return empty string")
	}
	if !filepath.IsAbs(path) {
		t.Error("CachePath() should return absolute path")
	}
	if !contains(path, "kfzf") {
		t.Error("CachePath() should contain 'kfzf'")
	}
	if !contains(path, ".cache") {
		t.Error("CachePath() should contain '.cache'")
	}
}

func TestLoadFrom_NonExistent(t *testing.T) {
	cfg, err := LoadFrom("/nonexistent/path/config.yaml")
	if err != nil {
		t.Errorf("LoadFrom(nonexistent) should not error, got %v", err)
	}
	if cfg == nil {
		t.Error("LoadFrom(nonexistent) should return default config")
	}
	// Should have default resources
	if _, ok := cfg.Resources["pods"]; !ok {
		t.Error("LoadFrom(nonexistent) should return config with pods")
	}
}

func TestLoadFrom_Valid(t *testing.T) {
	// Create temp config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
server:
  socketPath: /tmp/custom.sock
resources:
  customresource:
    columns:
      - name: NAME
        field: .metadata.name
        width: 40
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write temp config: %v", err)
	}

	cfg, err := LoadFrom(configPath)
	if err != nil {
		t.Fatalf("LoadFrom() error = %v", err)
	}

	// Check custom socket path
	if cfg.Server.SocketPath != "/tmp/custom.sock" {
		t.Errorf("SocketPath = %s, want /tmp/custom.sock", cfg.Server.SocketPath)
	}

	// Check custom resource is added
	if _, ok := cfg.Resources["customresource"]; !ok {
		t.Error("Custom resource should be in config")
	}

	// Check default resources are still present (merged)
	if _, ok := cfg.Resources["pods"]; !ok {
		t.Error("Default pods resource should still be present")
	}
}

func TestLoadFrom_Invalid(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Invalid YAML
	if err := os.WriteFile(configPath, []byte("invalid: yaml: content: ["), 0644); err != nil {
		t.Fatalf("Failed to write temp config: %v", err)
	}

	_, err := LoadFrom(configPath)
	if err == nil {
		t.Error("LoadFrom(invalid) should return error")
	}
}

func TestSaveTo(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Server.SocketPath = "/tmp/test.sock"

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "subdir", "config.yaml")

	err := cfg.SaveTo(configPath)
	if err != nil {
		t.Fatalf("SaveTo() error = %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("SaveTo() should create config file")
	}

	// Verify content can be loaded back
	loaded, err := LoadFrom(configPath)
	if err != nil {
		t.Fatalf("LoadFrom(saved) error = %v", err)
	}

	if loaded.Server.SocketPath != "/tmp/test.sock" {
		t.Errorf("Loaded SocketPath = %s, want /tmp/test.sock", loaded.Server.SocketPath)
	}
}

func TestColumnConfig(t *testing.T) {
	col := ColumnConfig{
		Name:  "TEST",
		Field: ".test.field",
		Width: 20,
	}

	if col.Name != "TEST" {
		t.Errorf("Name = %s, want TEST", col.Name)
	}
	if col.Field != ".test.field" {
		t.Errorf("Field = %s, want .test.field", col.Field)
	}
	if col.Width != 20 {
		t.Errorf("Width = %d, want 20", col.Width)
	}
}

func contains(s, substr string) bool {
	return filepath.Base(s) == substr || filepath.Dir(s) == substr || len(s) > 0 && (s[0:len(substr)] == substr || s[len(s)-len(substr):] == substr) || findSubstring(s, substr)
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
