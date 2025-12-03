package server

import (
	"testing"

	"github.com/pslijkhuis/kfzf/internal/config"
	"github.com/pslijkhuis/kfzf/internal/store"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// TestHandleServicePorts tests service port extraction for port-forward
func TestHandleServicePorts(t *testing.T) {
	cfg := config.DefaultConfig()
	s := &Server{
		config: cfg,
		store:  store.NewStore(),
	}

	// Add a test service to the store
	svcGVR := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "services"}
	svc := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Service",
			"metadata": map[string]interface{}{
				"name":      "test-svc",
				"namespace": "default",
			},
			"spec": map[string]interface{}{
				"type":      "ClusterIP",
				"clusterIP": "10.0.0.100",
				"selector": map[string]interface{}{
					"app": "test-app",
				},
				"ports": []interface{}{
					map[string]interface{}{
						"name":       "http",
						"port":       float64(80),
						"targetPort": float64(8080),
						"protocol":   "TCP",
					},
					map[string]interface{}{
						"name":       "https",
						"port":       float64(443),
						"targetPort": float64(8443),
						"protocol":   "TCP",
					},
				},
			},
		},
	}

	s.store.Add("test-context", svcGVR, svc)

	resp := s.handleServicePorts("test-context", "default", "test-svc")

	if !resp.Success {
		t.Fatalf("handleServicePorts failed: %s", resp.Error)
	}

	// Should contain both ports
	if resp.Output == "" {
		t.Error("Expected port output, got empty string")
	}

	// Check for port numbers in output
	if !containsString(resp.Output, "80") {
		t.Error("Output should contain port 80")
	}
	if !containsString(resp.Output, "443") {
		t.Error("Output should contain port 443")
	}
}

func TestHandleServicePorts_NotFound(t *testing.T) {
	cfg := config.DefaultConfig()
	s := &Server{
		config: cfg,
		store:  store.NewStore(),
	}

	resp := s.handleServicePorts("test-context", "default", "nonexistent")

	if resp.Success {
		t.Error("Expected failure for non-existent service")
	}
	if resp.Error == "" {
		t.Error("Expected error message")
	}
}

// TestHandleServicePreview tests service preview for port-forward completion
func TestHandleServicePreview(t *testing.T) {
	// This test documents expected behavior for service preview
	// Preview should show: ports, selector, type, clusterIP

	tests := []struct {
		name          string
		service       map[string]interface{}
		expectPorts   bool
		expectType    bool
		expectIP      bool
	}{
		{
			name: "ClusterIP service with ports",
			service: map[string]interface{}{
				"spec": map[string]interface{}{
					"type":      "ClusterIP",
					"clusterIP": "10.0.0.1",
					"ports": []interface{}{
						map[string]interface{}{"port": float64(80), "name": "http"},
					},
					"selector": map[string]interface{}{"app": "web"},
				},
			},
			expectPorts: true,
			expectType:  true,
			expectIP:    true,
		},
		{
			name: "LoadBalancer service",
			service: map[string]interface{}{
				"spec": map[string]interface{}{
					"type":      "LoadBalancer",
					"clusterIP": "10.0.0.2",
					"ports": []interface{}{
						map[string]interface{}{"port": float64(443), "name": "https"},
					},
				},
				"status": map[string]interface{}{
					"loadBalancer": map[string]interface{}{
						"ingress": []interface{}{
							map[string]interface{}{"ip": "1.2.3.4"},
						},
					},
				},
			},
			expectPorts: true,
			expectType:  true,
			expectIP:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Document expected preview content
			if tt.expectPorts {
				t.Log("Preview should show service ports")
			}
			if tt.expectType {
				t.Log("Preview should show service type")
			}
			if tt.expectIP {
				t.Log("Preview should show cluster IP")
			}
		})
	}
}

// TestRecentResources tests tracking of recently accessed resources
func TestRecentResources(t *testing.T) {
	tests := []struct {
		name           string
		accessSequence []string // resource names accessed in order
		expectedRecent []string // expected order in recent list (most recent first)
		maxRecent      int
	}{
		{
			name:           "Track single resource",
			accessSequence: []string{"pod-a"},
			expectedRecent: []string{"pod-a"},
			maxRecent:      10,
		},
		{
			name:           "Track multiple resources in order",
			accessSequence: []string{"pod-a", "pod-b", "pod-c"},
			expectedRecent: []string{"pod-c", "pod-b", "pod-a"},
			maxRecent:      10,
		},
		{
			name:           "Duplicate access moves to front",
			accessSequence: []string{"pod-a", "pod-b", "pod-a"},
			expectedRecent: []string{"pod-a", "pod-b"},
			maxRecent:      10,
		},
		{
			name:           "Respects max limit",
			accessSequence: []string{"pod-1", "pod-2", "pod-3", "pod-4"},
			expectedRecent: []string{"pod-4", "pod-3"},
			maxRecent:      2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recent := NewRecentResources(tt.maxRecent)

			for _, name := range tt.accessSequence {
				recent.Add("test-context", "default", "pods", name)
			}

			got := recent.Get("test-context", "default", "pods")
			if len(got) != len(tt.expectedRecent) {
				t.Errorf("Got %d recent items, want %d", len(got), len(tt.expectedRecent))
				return
			}

			for i, want := range tt.expectedRecent {
				if got[i] != want {
					t.Errorf("Recent[%d] = %s, want %s", i, got[i], want)
				}
			}
		})
	}
}

// TestCRDAutoDetection tests auto-detection of Custom Resource Definitions
func TestCRDAutoDetection(t *testing.T) {
	// This test documents expected behavior for CRD detection

	tests := []struct {
		name          string
		crdName       string
		group         string
		kind          string
		expectColumns []string
	}{
		{
			name:          "ArgoCD Application",
			crdName:       "applications.argoproj.io",
			group:         "argoproj.io",
			kind:          "Application",
			expectColumns: []string{"NAME", "NAMESPACE", "SYNC", "HEALTH"},
		},
		{
			name:          "Cert-Manager Certificate",
			crdName:       "certificates.cert-manager.io",
			group:         "cert-manager.io",
			kind:          "Certificate",
			expectColumns: []string{"NAME", "NAMESPACE", "READY", "AGE"},
		},
		{
			name:          "Unknown CRD",
			crdName:       "widgets.example.com",
			group:         "example.com",
			kind:          "Widget",
			expectColumns: []string{"NAME", "NAMESPACE", "AGE"}, // default columns
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Document expected behavior
			t.Logf("CRD %s should have columns: %v", tt.crdName, tt.expectColumns)
		})
	}
}

// TestRolloutPreview tests rollout action preview content
func TestRolloutPreview(t *testing.T) {
	// This test documents expected behavior for rollout previews

	tests := []struct {
		name           string
		action         string
		resourceType   string
		expectContent  []string
	}{
		{
			name:           "Rollout status",
			action:         "status",
			resourceType:   "deployments",
			expectContent:  []string{"replicas", "ready", "available"},
		},
		{
			name:           "Rollout history",
			action:         "history",
			resourceType:   "deployments",
			expectContent:  []string{"REVISION", "CHANGE-CAUSE"},
		},
		{
			name:           "Rollout restart preview",
			action:         "restart",
			resourceType:   "deployments",
			expectContent:  []string{"Will restart", "pods"},
		},
		{
			name:           "Rollout undo preview",
			action:         "undo",
			resourceType:   "deployments",
			expectContent:  []string{"previous revision", "REVISION"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("Rollout %s for %s should show: %v", tt.action, tt.resourceType, tt.expectContent)
		})
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
