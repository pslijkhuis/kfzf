package fzf

import (
	"strings"
	"testing"
	"time"

	"github.com/pslijkhuis/kfzf/internal/config"
	"github.com/pslijkhuis/kfzf/internal/store"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestFormatter_FormatAge(t *testing.T) {
	f := NewFormatter(config.DefaultConfig())

	tests := []struct {
		name     string
		duration time.Duration
		expected string
	}{
		{"30 seconds", 30 * time.Second, "30s"},
		{"5 minutes", 5 * time.Minute, "5m"},
		{"2 hours", 2 * time.Hour, "2h"},
		{"3 days", 3 * 24 * time.Hour, "3d"},
		{"45 days", 45 * 24 * time.Hour, "1M"},
		{"400 days", 400 * 24 * time.Hour, "1y"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			creationTime := time.Now().Add(-tt.duration)
			result := f.formatAge(creationTime)
			if result != tt.expected {
				t.Errorf("formatAge(%v) = %s, want %s", tt.duration, result, tt.expected)
			}
		})
	}
}

func TestFormatter_FormatAgeZero(t *testing.T) {
	f := NewFormatter(config.DefaultConfig())
	result := f.formatAge(time.Time{})
	if result != "<unknown>" {
		t.Errorf("formatAge(zero) = %s, want <unknown>", result)
	}
}

func TestFormatter_TruncateOrPad(t *testing.T) {
	f := NewFormatter(config.DefaultConfig())

	tests := []struct {
		input    string
		width    int
		expected string
	}{
		{"hello", 10, "hello     "},
		{"hello-world-test", 10, "hello-w..."},
		{"abc", 3, "abc"},
		{"ab", 5, "ab   "},
		{"abcdef", 5, "ab..."},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := f.truncateOrPad(tt.input, tt.width)
			if result != tt.expected {
				t.Errorf("truncateOrPad(%q, %d) = %q, want %q", tt.input, tt.width, result, tt.expected)
			}
		})
	}
}

func TestFormatter_ExtractNodeStatus(t *testing.T) {
	f := NewFormatter(config.DefaultConfig())

	tests := []struct {
		name     string
		obj      map[string]interface{}
		expected string
	}{
		{
			name: "Ready node",
			obj: map[string]interface{}{
				"status": map[string]interface{}{
					"conditions": []interface{}{
						map[string]interface{}{
							"type":   "Ready",
							"status": "True",
						},
					},
				},
			},
			expected: "Ready",
		},
		{
			name: "NotReady node",
			obj: map[string]interface{}{
				"status": map[string]interface{}{
					"conditions": []interface{}{
						map[string]interface{}{
							"type":   "Ready",
							"status": "False",
						},
					},
				},
			},
			expected: "NotReady",
		},
		{
			name: "Ready but SchedulingDisabled",
			obj: map[string]interface{}{
				"status": map[string]interface{}{
					"conditions": []interface{}{
						map[string]interface{}{
							"type":   "Ready",
							"status": "True",
						},
					},
				},
				"spec": map[string]interface{}{
					"unschedulable": true,
				},
			},
			expected: "Ready,SchedulingDisabled",
		},
		{
			name:     "No conditions",
			obj:      map[string]interface{}{},
			expected: "Unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := f.extractNodeStatus(tt.obj)
			if result != tt.expected {
				t.Errorf("extractNodeStatus() = %s, want %s", result, tt.expected)
			}
		})
	}
}

func TestFormatter_ExtractNodeRoles(t *testing.T) {
	f := NewFormatter(config.DefaultConfig())

	tests := []struct {
		name     string
		obj      map[string]interface{}
		expected string
	}{
		{
			name: "Control plane node",
			obj: map[string]interface{}{
				"metadata": map[string]interface{}{
					"labels": map[string]interface{}{
						"node-role.kubernetes.io/control-plane": "",
					},
				},
			},
			expected: "control-plane",
		},
		{
			name: "Worker node",
			obj: map[string]interface{}{
				"metadata": map[string]interface{}{
					"labels": map[string]interface{}{
						"node-role.kubernetes.io/worker": "",
					},
				},
			},
			expected: "worker",
		},
		{
			name: "No roles",
			obj: map[string]interface{}{
				"metadata": map[string]interface{}{
					"labels": map[string]interface{}{
						"some-other-label": "value",
					},
				},
			},
			expected: "<none>",
		},
		{
			name:     "No labels",
			obj:      map[string]interface{}{},
			expected: "<none>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := f.extractNodeRoles(tt.obj)
			if result != tt.expected {
				t.Errorf("extractNodeRoles() = %s, want %s", result, tt.expected)
			}
		})
	}
}

func TestFormatter_ExtractNodeTopology(t *testing.T) {
	f := NewFormatter(config.DefaultConfig())

	tests := []struct {
		name     string
		obj      map[string]interface{}
		expected string
	}{
		{
			name: "Standard topology labels",
			obj: map[string]interface{}{
				"metadata": map[string]interface{}{
					"labels": map[string]interface{}{
						"topology.kubernetes.io/region": "eu-west-1",
						"topology.kubernetes.io/zone":   "eu-west-1a",
					},
				},
			},
			expected: "eu-west-1/eu-west-1a",
		},
		{
			name: "Legacy topology labels",
			obj: map[string]interface{}{
				"metadata": map[string]interface{}{
					"labels": map[string]interface{}{
						"failure-domain.beta.kubernetes.io/region": "us-east-1",
						"failure-domain.beta.kubernetes.io/zone":   "us-east-1b",
					},
				},
			},
			expected: "us-east-1/us-east-1b",
		},
		{
			name: "Only zone",
			obj: map[string]interface{}{
				"metadata": map[string]interface{}{
					"labels": map[string]interface{}{
						"topology.kubernetes.io/zone": "zone-a",
					},
				},
			},
			expected: "zone-a",
		},
		{
			name: "No topology labels",
			obj: map[string]interface{}{
				"metadata": map[string]interface{}{
					"labels": map[string]interface{}{
						"some-label": "value",
					},
				},
			},
			expected: "-",
		},
		{
			name:     "No labels",
			obj:      map[string]interface{}{},
			expected: "-",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := f.extractNodeTopology(tt.obj)
			if result != tt.expected {
				t.Errorf("extractNodeTopology() = %s, want %s", result, tt.expected)
			}
		})
	}
}

func TestFormatter_ExtractCapiClusterStatus(t *testing.T) {
	f := NewFormatter(config.DefaultConfig())

	tests := []struct {
		name     string
		obj      map[string]interface{}
		expected string
	}{
		{
			name: "Provisioned and Ready",
			obj: map[string]interface{}{
				"status": map[string]interface{}{
					"phase": "Provisioned",
					"conditions": []interface{}{
						map[string]interface{}{
							"type":   "Ready",
							"status": "True",
						},
					},
				},
			},
			expected: "Provisioned",
		},
		{
			name: "Provisioning with reason",
			obj: map[string]interface{}{
				"status": map[string]interface{}{
					"phase": "Provisioning",
					"conditions": []interface{}{
						map[string]interface{}{
							"type":   "Ready",
							"status": "False",
							"reason": "WaitingForInfrastructure",
						},
					},
				},
			},
			expected: "Provisioning (WaitingForInfrastructure)",
		},
		{
			name: "Ready from flags",
			obj: map[string]interface{}{
				"status": map[string]interface{}{
					"controlPlaneReady":    true,
					"infrastructureReady": true,
				},
			},
			expected: "Ready",
		},
		{
			name: "NotReady from flags",
			obj: map[string]interface{}{
				"status": map[string]interface{}{
					"controlPlaneReady":    true,
					"infrastructureReady": false,
				},
			},
			expected: "NotReady",
		},
		{
			name:     "Unknown",
			obj:      map[string]interface{}{},
			expected: "Unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := f.extractCapiClusterStatus(tt.obj)
			if result != tt.expected {
				t.Errorf("extractCapiClusterStatus() = %s, want %s", result, tt.expected)
			}
		})
	}
}

func TestFormatter_ExtractCnpgClusterStatus(t *testing.T) {
	f := NewFormatter(config.DefaultConfig())

	tests := []struct {
		name     string
		obj      map[string]interface{}
		expected string
	}{
		{
			name: "Cluster healthy",
			obj: map[string]interface{}{
				"status": map[string]interface{}{
					"phase": "Cluster in healthy state",
				},
			},
			expected: "Cluster in healthy state",
		},
		{
			name: "Ready condition",
			obj: map[string]interface{}{
				"status": map[string]interface{}{
					"conditions": []interface{}{
						map[string]interface{}{
							"type":   "Ready",
							"status": "True",
						},
					},
				},
			},
			expected: "Ready",
		},
		{
			name: "NotReady with reason",
			obj: map[string]interface{}{
				"status": map[string]interface{}{
					"conditions": []interface{}{
						map[string]interface{}{
							"type":   "Ready",
							"status": "False",
							"reason": "ReplicaFailure",
						},
					},
				},
			},
			expected: "ReplicaFailure",
		},
		{
			name:     "Unknown",
			obj:      map[string]interface{}{},
			expected: "Unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := f.extractCnpgClusterStatus(tt.obj)
			if result != tt.expected {
				t.Errorf("extractCnpgClusterStatus() = %s, want %s", result, tt.expected)
			}
		})
	}
}

func TestFormatter_ExtractCnpgClusterReady(t *testing.T) {
	f := NewFormatter(config.DefaultConfig())

	tests := []struct {
		name     string
		obj      map[string]interface{}
		expected string
	}{
		{
			name: "3/3 ready",
			obj: map[string]interface{}{
				"status": map[string]interface{}{
					"readyInstances": float64(3),
				},
				"spec": map[string]interface{}{
					"instances": float64(3),
				},
			},
			expected: "3/3",
		},
		{
			name: "1/3 ready",
			obj: map[string]interface{}{
				"status": map[string]interface{}{
					"readyInstances": float64(1),
				},
				"spec": map[string]interface{}{
					"instances": float64(3),
				},
			},
			expected: "1/3",
		},
		{
			name:     "No data",
			obj:      map[string]interface{}{},
			expected: "0/0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := f.extractCnpgClusterReady(tt.obj)
			if result != tt.expected {
				t.Errorf("extractCnpgClusterReady() = %s, want %s", result, tt.expected)
			}
		})
	}
}

func TestFormatter_GetNestedValue(t *testing.T) {
	f := NewFormatter(config.DefaultConfig())

	obj := map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":      "test-pod",
			"namespace": "default",
			"labels": map[string]interface{}{
				"app": "nginx",
			},
		},
		"spec": map[string]interface{}{
			"replicas": 3,
		},
		"status": map[string]interface{}{
			"phase": "Running",
		},
	}

	tests := []struct {
		path     string
		expected interface{}
	}{
		{".metadata.name", "test-pod"},
		{".metadata.namespace", "default"},
		{".metadata.labels.app", "nginx"},
		{".spec.replicas", 3},
		{".status.phase", "Running"},
		{".nonexistent", ""},
		{".metadata.nonexistent", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := f.getNestedValue(obj, tt.path)
			if result != tt.expected {
				t.Errorf("getNestedValue(%s) = %v, want %v", tt.path, result, tt.expected)
			}
		})
	}
}

func TestFormatter_ExtractArrayField(t *testing.T) {
	f := NewFormatter(config.DefaultConfig())

	obj := map[string]interface{}{
		"spec": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{
					"host": "app1.example.com",
				},
				map[string]interface{}{
					"host": "app2.example.com",
				},
			},
		},
	}

	result := f.extractArrayField(obj, ".spec.rules[*].host")
	expected := "app1.example.com,app2.example.com"
	if result != expected {
		t.Errorf("extractArrayField() = %s, want %s", result, expected)
	}
}

func TestFormatter_ExtractFilteredArrayField(t *testing.T) {
	f := NewFormatter(config.DefaultConfig())

	obj := map[string]interface{}{
		"status": map[string]interface{}{
			"conditions": []interface{}{
				map[string]interface{}{
					"type":   "PodScheduled",
					"status": "True",
				},
				map[string]interface{}{
					"type":   "Ready",
					"status": "True",
				},
				map[string]interface{}{
					"type":   "ContainersReady",
					"status": "True",
				},
			},
		},
	}

	result := f.extractFilteredArrayField(obj, `.status.conditions[?(@.type=="Ready")].status`)
	expected := "True"
	if result != expected {
		t.Errorf("extractFilteredArrayField() = %s, want %s", result, expected)
	}
}

func TestFormatter_Colorize(t *testing.T) {
	f := NewFormatter(config.DefaultConfig())

	tests := []struct {
		name     string
		value    string
		colName  string
		colIndex int
		hasColor bool
	}{
		{"First column (NAME)", "my-pod", "NAME", 0, true},
		{"Namespace column", "default", "NAMESPACE", 1, true},
		{"Status Running", "Running", "STATUS", 2, true},
		{"Status Pending", "Pending", "STATUS", 2, true},
		{"Status Failed", "Failed", "STATUS", 2, true},
		{"Ready 3/3", "3/3", "READY", 2, true},
		{"Ready 0/3", "0/3", "READY", 2, true},
		{"Age column", "5d", "AGE", 3, true},
		{"Sync Synced", "Synced", "SYNC", 2, true},
		{"Sync OutOfSync", "OutOfSync", "SYNC", 2, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := f.colorize(tt.value, tt.colName, tt.colIndex)
			hasEscape := strings.Contains(result, "\033[")
			if hasEscape != tt.hasColor {
				t.Errorf("colorize(%s, %s, %d) has color=%v, want %v", tt.value, tt.colName, tt.colIndex, hasEscape, tt.hasColor)
			}
		})
	}
}

func TestFormatter_Format(t *testing.T) {
	cfg := config.DefaultConfig()
	f := NewFormatter(cfg)

	resources := []*store.Resource{
		{
			Object: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"name":      "pod-1",
						"namespace": "default",
					},
					"status": map[string]interface{}{
						"phase": "Running",
					},
				},
			},
			CreationTimestamp: time.Now().Add(-1 * time.Hour),
		},
		{
			Object: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"name":      "pod-2",
						"namespace": "kube-system",
					},
					"status": map[string]interface{}{
						"phase": "Pending",
					},
				},
			},
			CreationTimestamp: time.Now().Add(-2 * time.Hour),
		},
	}

	result := f.Format(resources, "pods")

	// Check that both pods are in output
	if !strings.Contains(result, "pod-1") {
		t.Error("Format() should contain pod-1")
	}
	if !strings.Contains(result, "pod-2") {
		t.Error("Format() should contain pod-2")
	}
	if !strings.Contains(result, "default") {
		t.Error("Format() should contain namespace default")
	}
	if !strings.Contains(result, "kube-system") {
		t.Error("Format() should contain namespace kube-system")
	}

	// Output should be plain text (coloring is done in ZSH before fzf)
	if strings.Contains(result, "\033[") {
		t.Error("Format() should NOT contain ANSI color codes - coloring is done in shell")
	}

	// Check that lines are separated by newlines
	lines := strings.Split(result, "\n")
	if len(lines) != 2 {
		t.Errorf("Format() should have 2 lines, got %d", len(lines))
	}
}

func TestFormatter_FormatEmpty(t *testing.T) {
	f := NewFormatter(config.DefaultConfig())
	result := f.Format([]*store.Resource{}, "pods")
	if result != "" {
		t.Errorf("Format(empty) = %q, want empty string", result)
	}
}

func TestFormatter_FormatWithHeader(t *testing.T) {
	cfg := config.DefaultConfig()
	f := NewFormatter(cfg)

	resources := []*store.Resource{
		{
			Object: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"name":      "svc-1",
						"namespace": "default",
					},
					"spec": map[string]interface{}{
						"type":      "ClusterIP",
						"clusterIP": "10.0.0.1",
					},
				},
			},
			CreationTimestamp: time.Now().Add(-1 * time.Hour),
		},
	}

	result := f.FormatWithHeader(resources, "services")

	// Check header line
	if !strings.Contains(result, "NAME") {
		t.Error("FormatWithHeader() should contain NAME header")
	}
	if !strings.Contains(result, "NAMESPACE") {
		t.Error("FormatWithHeader() should contain NAMESPACE header")
	}
	if !strings.Contains(result, "TYPE") {
		t.Error("FormatWithHeader() should contain TYPE header")
	}

	// Check data
	if !strings.Contains(result, "svc-1") {
		t.Error("FormatWithHeader() should contain svc-1")
	}
	if !strings.Contains(result, "ClusterIP") {
		t.Error("FormatWithHeader() should contain ClusterIP")
	}
}

func TestFormatter_RatioField(t *testing.T) {
	f := NewFormatter(config.DefaultConfig())

	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"status": map[string]interface{}{
				"readyReplicas": float64(2),
			},
			"spec": map[string]interface{}{
				"replicas": float64(3),
			},
		},
	}

	result := f.extractField(obj, ".status.readyReplicas/.spec.replicas", time.Time{})
	expected := "2/3"
	if result != expected {
		t.Errorf("extractField(ratio) = %s, want %s", result, expected)
	}
}
