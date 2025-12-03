package main

import (
	"strings"
	"testing"
)

// CompletionContext represents the parsed state of a kubectl command line
type CompletionContext struct {
	Action        string
	ResourceType  string
	ResourceName  string
	Namespace     string
	Context       string
	Container     string
	AllNamespaces bool
	CompleteType  string // What should be completed next
	CompleteQuery string // Partial input for filtering
}

// ParseCommandLine parses a kubectl command line and returns the completion context
// This mimics the ZSH completion logic in main.go
func ParseCommandLine(cmdline string, cursorAtEnd bool) CompletionContext {
	words := strings.Fields(cmdline)
	ctx := CompletionContext{}

	if len(words) == 0 || (words[0] != "kubectl" && words[0] != "k") {
		return ctx
	}

	// Check if we're completing a partial word
	completingPartial := cursorAtEnd && len(cmdline) > 0 && cmdline[len(cmdline)-1] != ' '

	// Flags that take a value
	flagValues := map[string]string{
		"-n":              "namespace",
		"--namespace":     "namespace",
		"--context":       "context",
		"-c":              "container",
		"--container":     "container",
		"-l":              "label",
		"--selector":      "label",
		"--field-selector": "field_selector",
	}

	// Boolean flags
	boolFlags := map[string]bool{
		"-A": true, "--all-namespaces": true,
		"-w": true, "--watch": true,
	}

	// Actions with implicit pods
	implicitPods := map[string]bool{
		"logs": true, "exec": true, "attach": true, "cp": true, "port-forward": true,
	}

	// Known actions
	knownActions := map[string]bool{
		"get": true, "describe": true, "delete": true, "edit": true,
		"logs": true, "exec": true, "attach": true, "port-forward": true,
		"apply": true, "create": true, "scale": true, "rollout": true,
		"label": true, "annotate": true, "top": true, "events": true,
	}

	i := 1 // Skip "kubectl" or "k"
	for i < len(words) {
		word := words[i]
		var nextWord string
		if i+1 < len(words) {
			nextWord = words[i+1]
		}

		// Check flag with value
		if flagType, ok := flagValues[word]; ok {
			switch flagType {
			case "namespace":
				ctx.Namespace = nextWord
			case "context":
				ctx.Context = nextWord
			case "container":
				ctx.Container = nextWord
			}
			i += 2
			continue
		}

		// Check --flag=value format
		if strings.Contains(word, "=") {
			parts := strings.SplitN(word, "=", 2)
			if flagType, ok := flagValues[parts[0]]; ok {
				switch flagType {
				case "namespace":
					ctx.Namespace = parts[1]
				case "context":
					ctx.Context = parts[1]
				case "container":
					ctx.Container = parts[1]
				}
			}
			i++
			continue
		}

		// Check boolean flags
		if boolFlags[word] {
			if word == "-A" || word == "--all-namespaces" {
				ctx.AllNamespaces = true
			}
			i++
			continue
		}

		// Skip other flags
		if strings.HasPrefix(word, "-") {
			i++
			continue
		}

		// Positional arguments
		if ctx.Action == "" {
			if knownActions[word] {
				ctx.Action = word
			}
			i++
			continue
		}

		if ctx.ResourceType == "" {
			if implicitPods[ctx.Action] {
				// For port-forward, handle svc/ or service/ prefix
				if ctx.Action == "port-forward" && (strings.HasPrefix(word, "svc/") || strings.HasPrefix(word, "service/")) {
					ctx.ResourceType = "services"
					parts := strings.SplitN(word, "/", 2)
					svcName := ""
					if len(parts) > 1 {
						svcName = parts[1]
					}
					// If this is the last word and we're completing partial, or service name is empty
					if (i == len(words)-1 && completingPartial) || svcName == "" {
						// We're completing the service name
						i++
						continue
					}
					ctx.ResourceName = svcName
				} else {
					ctx.ResourceType = "pods"
					// If this is the last word and we're completing partial, don't set resource_name
					if i == len(words)-1 && completingPartial {
						i++
						continue
					}
					ctx.ResourceName = word
				}
			} else {
				// If this is the last word and we're completing partial, don't set resource_type
				if i == len(words)-1 && completingPartial {
					i++
					continue
				}
				ctx.ResourceType = word
			}
			i++
			continue
		}

		if ctx.ResourceName == "" {
			ctx.ResourceName = word
			i++
			continue
		}

		i++
	}

	// Determine what to complete
	var lastWord, secondLast string
	if len(words) >= 1 {
		lastWord = words[len(words)-1]
	}
	if len(words) >= 2 {
		secondLast = words[len(words)-2]
	}

	// Check if completing a flag value
	if !completingPartial {
		// Cursor after space
		switch lastWord {
		case "-n", "--namespace":
			ctx.CompleteType = "namespace"
		case "--context":
			ctx.CompleteType = "context"
		case "-c", "--container":
			ctx.CompleteType = "container"
		case "-l", "--selector":
			ctx.CompleteType = "label"
		case "--field-selector":
			ctx.CompleteType = "field_selector"
		}
	} else {
		// Cursor in middle of word
		switch secondLast {
		case "-n", "--namespace":
			ctx.CompleteType = "namespace"
			ctx.CompleteQuery = lastWord
		case "--context":
			ctx.CompleteType = "context"
			ctx.CompleteQuery = lastWord
		case "-c", "--container":
			ctx.CompleteType = "container"
			ctx.CompleteQuery = lastWord
		case "-l", "--selector":
			ctx.CompleteType = "label"
			ctx.CompleteQuery = lastWord
		case "--field-selector":
			ctx.CompleteType = "field_selector"
			ctx.CompleteQuery = lastWord
		}
	}

	// Set implicit resource type for pod commands
	if ctx.ResourceType == "" && implicitPods[ctx.Action] {
		ctx.ResourceType = "pods"
	}

	// If not completing a flag value, determine based on position
	if ctx.CompleteType == "" {
		if ctx.Action == "" {
			ctx.CompleteType = "action"
		} else if ctx.ResourceType == "" {
			ctx.CompleteType = "resource_type"
			if completingPartial && !strings.HasPrefix(lastWord, "-") {
				ctx.CompleteQuery = lastWord
			}
		} else if ctx.ResourceName != "" && implicitPods[ctx.Action] {
			if ctx.Action == "port-forward" {
				ctx.CompleteType = "port"
			} else {
				ctx.CompleteType = "container"
			}
		} else {
			ctx.CompleteType = "resource"
			if completingPartial && !strings.HasPrefix(lastWord, "-") && ctx.Action != "" {
				ctx.CompleteQuery = lastWord
			}
		}
	}

	return ctx
}

// Tests for namespace completion (-n)
func TestCompletion_Namespace(t *testing.T) {
	tests := []struct {
		name        string
		cmdline     string
		cursorAtEnd bool
		wantType    string
		wantQuery   string
	}{
		{
			name:        "kubectl get -n <tab>",
			cmdline:     "kubectl get -n ",
			cursorAtEnd: true,
			wantType:    "namespace",
			wantQuery:   "",
		},
		{
			name:        "kubectl get --namespace <tab>",
			cmdline:     "kubectl get --namespace ",
			cursorAtEnd: true,
			wantType:    "namespace",
			wantQuery:   "",
		},
		{
			name:        "kubectl get -n def<tab>",
			cmdline:     "kubectl get -n def",
			cursorAtEnd: true,
			wantType:    "namespace",
			wantQuery:   "def",
		},
		{
			name:        "kubectl get pods -n <tab>",
			cmdline:     "kubectl get pods -n ",
			cursorAtEnd: true,
			wantType:    "namespace",
			wantQuery:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := ParseCommandLine(tt.cmdline, tt.cursorAtEnd)
			if ctx.CompleteType != tt.wantType {
				t.Errorf("CompleteType = %q, want %q", ctx.CompleteType, tt.wantType)
			}
			if ctx.CompleteQuery != tt.wantQuery {
				t.Errorf("CompleteQuery = %q, want %q", ctx.CompleteQuery, tt.wantQuery)
			}
		})
	}
}

// Tests for resource type completion
func TestCompletion_ResourceType(t *testing.T) {
	tests := []struct {
		name        string
		cmdline     string
		cursorAtEnd bool
		wantType    string
		wantQuery   string
	}{
		{
			name:        "kubectl get <tab>",
			cmdline:     "kubectl get ",
			cursorAtEnd: true,
			wantType:    "resource_type",
			wantQuery:   "",
		},
		{
			name:        "kubectl get po<tab>",
			cmdline:     "kubectl get po",
			cursorAtEnd: true,
			wantType:    "resource_type",
			wantQuery:   "po",
		},
		{
			name:        "kubectl describe <tab>",
			cmdline:     "kubectl describe ",
			cursorAtEnd: true,
			wantType:    "resource_type",
			wantQuery:   "",
		},
		{
			name:        "kubectl delete dep<tab>",
			cmdline:     "kubectl delete dep",
			cursorAtEnd: true,
			wantType:    "resource_type",
			wantQuery:   "dep",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := ParseCommandLine(tt.cmdline, tt.cursorAtEnd)
			if ctx.CompleteType != tt.wantType {
				t.Errorf("CompleteType = %q, want %q", ctx.CompleteType, tt.wantType)
			}
			if ctx.CompleteQuery != tt.wantQuery {
				t.Errorf("CompleteQuery = %q, want %q", ctx.CompleteQuery, tt.wantQuery)
			}
		})
	}
}

// Tests for resource name completion
func TestCompletion_ResourceName(t *testing.T) {
	tests := []struct {
		name         string
		cmdline      string
		cursorAtEnd  bool
		wantType     string
		wantResource string
		wantQuery    string
	}{
		{
			name:         "kubectl get pods <tab>",
			cmdline:      "kubectl get pods ",
			cursorAtEnd:  true,
			wantType:     "resource",
			wantResource: "pods",
			wantQuery:    "",
		},
		{
			name:         "kubectl get pods my-<tab>",
			cmdline:      "kubectl get pods my-",
			cursorAtEnd:  true,
			wantType:     "resource",
			wantResource: "pods",
			wantQuery:    "my-",
		},
		{
			name:         "kubectl get deployments <tab>",
			cmdline:      "kubectl get deployments ",
			cursorAtEnd:  true,
			wantType:     "resource",
			wantResource: "deployments",
			wantQuery:    "",
		},
		{
			name:         "kubectl get svc -n kube-system <tab>",
			cmdline:      "kubectl get svc -n kube-system ",
			cursorAtEnd:  true,
			wantType:     "resource",
			wantResource: "svc",
			wantQuery:    "",
		},
		{
			name:         "kubectl describe pod nginx<tab>",
			cmdline:      "kubectl describe pod nginx",
			cursorAtEnd:  true,
			wantType:     "resource",
			wantResource: "pod",
			wantQuery:    "nginx",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := ParseCommandLine(tt.cmdline, tt.cursorAtEnd)
			if ctx.CompleteType != tt.wantType {
				t.Errorf("CompleteType = %q, want %q", ctx.CompleteType, tt.wantType)
			}
			if ctx.ResourceType != tt.wantResource {
				t.Errorf("ResourceType = %q, want %q", ctx.ResourceType, tt.wantResource)
			}
			if ctx.CompleteQuery != tt.wantQuery {
				t.Errorf("CompleteQuery = %q, want %q", ctx.CompleteQuery, tt.wantQuery)
			}
		})
	}
}

// Tests for label completion (-l)
func TestCompletion_Labels(t *testing.T) {
	tests := []struct {
		name         string
		cmdline      string
		cursorAtEnd  bool
		wantType     string
		wantResource string
		wantQuery    string
	}{
		{
			name:         "kubectl get pods -l <tab>",
			cmdline:      "kubectl get pods -l ",
			cursorAtEnd:  true,
			wantType:     "label",
			wantResource: "pods",
			wantQuery:    "",
		},
		{
			name:         "kubectl get pods --selector <tab>",
			cmdline:      "kubectl get pods --selector ",
			cursorAtEnd:  true,
			wantType:     "label",
			wantResource: "pods",
			wantQuery:    "",
		},
		{
			name:         "kubectl get pods -l app=<tab>",
			cmdline:      "kubectl get pods -l app=",
			cursorAtEnd:  true,
			wantType:     "label",
			wantResource: "pods",
			wantQuery:    "app=",
		},
		{
			name:         "kubectl get deployments -l env<tab>",
			cmdline:      "kubectl get deployments -l env",
			cursorAtEnd:  true,
			wantType:     "label",
			wantResource: "deployments",
			wantQuery:    "env",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := ParseCommandLine(tt.cmdline, tt.cursorAtEnd)
			if ctx.CompleteType != tt.wantType {
				t.Errorf("CompleteType = %q, want %q", ctx.CompleteType, tt.wantType)
			}
			if ctx.ResourceType != tt.wantResource {
				t.Errorf("ResourceType = %q, want %q", ctx.ResourceType, tt.wantResource)
			}
			if ctx.CompleteQuery != tt.wantQuery {
				t.Errorf("CompleteQuery = %q, want %q", ctx.CompleteQuery, tt.wantQuery)
			}
		})
	}
}

// Tests for field selector completion (--field-selector)
func TestCompletion_FieldSelector(t *testing.T) {
	tests := []struct {
		name         string
		cmdline      string
		cursorAtEnd  bool
		wantType     string
		wantResource string
		wantQuery    string
	}{
		{
			name:         "kubectl get pods --field-selector <tab>",
			cmdline:      "kubectl get pods --field-selector ",
			cursorAtEnd:  true,
			wantType:     "field_selector",
			wantResource: "pods",
			wantQuery:    "",
		},
		{
			name:         "kubectl get pods --field-selector status.phase=<tab>",
			cmdline:      "kubectl get pods --field-selector status.phase=",
			cursorAtEnd:  true,
			wantType:     "field_selector",
			wantResource: "pods",
			wantQuery:    "status.phase=",
		},
		{
			name:         "kubectl get pods --field-selector spec.node<tab>",
			cmdline:      "kubectl get pods --field-selector spec.node",
			cursorAtEnd:  true,
			wantType:     "field_selector",
			wantResource: "pods",
			wantQuery:    "spec.node",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := ParseCommandLine(tt.cmdline, tt.cursorAtEnd)
			if ctx.CompleteType != tt.wantType {
				t.Errorf("CompleteType = %q, want %q", ctx.CompleteType, tt.wantType)
			}
			if ctx.ResourceType != tt.wantResource {
				t.Errorf("ResourceType = %q, want %q", ctx.ResourceType, tt.wantResource)
			}
			if ctx.CompleteQuery != tt.wantQuery {
				t.Errorf("CompleteQuery = %q, want %q", ctx.CompleteQuery, tt.wantQuery)
			}
		})
	}
}

// Tests for implicit pods commands (logs, exec, etc.)
func TestCompletion_ImplicitPods(t *testing.T) {
	tests := []struct {
		name         string
		cmdline      string
		cursorAtEnd  bool
		wantType     string
		wantResource string
		wantPodName  string
	}{
		{
			name:         "kubectl logs <tab>",
			cmdline:      "kubectl logs ",
			cursorAtEnd:  true,
			wantType:     "resource",
			wantResource: "pods",
			wantPodName:  "",
		},
		{
			name:         "kubectl logs my-pod <tab>",
			cmdline:      "kubectl logs my-pod ",
			cursorAtEnd:  true,
			wantType:     "container",
			wantResource: "pods",
			wantPodName:  "my-pod",
		},
		{
			name:         "kubectl exec <tab>",
			cmdline:      "kubectl exec ",
			cursorAtEnd:  true,
			wantType:     "resource",
			wantResource: "pods",
			wantPodName:  "",
		},
		{
			name:         "kubectl exec my-pod <tab>",
			cmdline:      "kubectl exec my-pod ",
			cursorAtEnd:  true,
			wantType:     "container",
			wantResource: "pods",
			wantPodName:  "my-pod",
		},
		{
			name:         "kubectl attach <tab>",
			cmdline:      "kubectl attach ",
			cursorAtEnd:  true,
			wantType:     "resource",
			wantResource: "pods",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := ParseCommandLine(tt.cmdline, tt.cursorAtEnd)
			if ctx.CompleteType != tt.wantType {
				t.Errorf("CompleteType = %q, want %q", ctx.CompleteType, tt.wantType)
			}
			if ctx.ResourceType != tt.wantResource {
				t.Errorf("ResourceType = %q, want %q", ctx.ResourceType, tt.wantResource)
			}
			if tt.wantPodName != "" && ctx.ResourceName != tt.wantPodName {
				t.Errorf("ResourceName = %q, want %q", ctx.ResourceName, tt.wantPodName)
			}
		})
	}
}

// Tests for container completion (-c)
func TestCompletion_Container(t *testing.T) {
	tests := []struct {
		name        string
		cmdline     string
		cursorAtEnd bool
		wantType    string
		wantPodName string
		wantQuery   string
	}{
		{
			name:        "kubectl logs my-pod -c <tab>",
			cmdline:     "kubectl logs my-pod -c ",
			cursorAtEnd: true,
			wantType:    "container",
			wantPodName: "my-pod",
			wantQuery:   "",
		},
		{
			name:        "kubectl logs my-pod --container <tab>",
			cmdline:     "kubectl logs my-pod --container ",
			cursorAtEnd: true,
			wantType:    "container",
			wantPodName: "my-pod",
			wantQuery:   "",
		},
		{
			name:        "kubectl exec my-pod -c ngi<tab>",
			cmdline:     "kubectl exec my-pod -c ngi",
			cursorAtEnd: true,
			wantType:    "container",
			wantPodName: "my-pod",
			wantQuery:   "ngi",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := ParseCommandLine(tt.cmdline, tt.cursorAtEnd)
			if ctx.CompleteType != tt.wantType {
				t.Errorf("CompleteType = %q, want %q", ctx.CompleteType, tt.wantType)
			}
			if ctx.ResourceName != tt.wantPodName {
				t.Errorf("ResourceName = %q, want %q", ctx.ResourceName, tt.wantPodName)
			}
			if ctx.CompleteQuery != tt.wantQuery {
				t.Errorf("CompleteQuery = %q, want %q", ctx.CompleteQuery, tt.wantQuery)
			}
		})
	}
}

// Tests for port-forward completion
func TestCompletion_PortForward(t *testing.T) {
	tests := []struct {
		name         string
		cmdline      string
		cursorAtEnd  bool
		wantType     string
		wantResource string
		wantName     string
		wantNS       string
	}{
		{
			name:         "kubectl port-forward <tab>",
			cmdline:      "kubectl port-forward ",
			cursorAtEnd:  true,
			wantType:     "resource",
			wantResource: "pods",
			wantName:     "",
		},
		{
			name:         "kubectl port-forward my-pod <tab>",
			cmdline:      "kubectl port-forward my-pod ",
			cursorAtEnd:  true,
			wantType:     "port",
			wantResource: "pods",
			wantName:     "my-pod",
		},
		{
			name:         "kubectl port-forward svc/my-svc <tab>",
			cmdline:      "kubectl port-forward svc/my-svc ",
			cursorAtEnd:  true,
			wantType:     "port",
			wantResource: "services",
			wantName:     "my-svc",
		},
		{
			name:         "kubectl port-forward service/my-service <tab>",
			cmdline:      "kubectl port-forward service/my-service ",
			cursorAtEnd:  true,
			wantType:     "port",
			wantResource: "services",
			wantName:     "my-service",
		},
		{
			name:         "kubectl port-forward -n kube-system svc/<tab>",
			cmdline:      "kubectl port-forward -n kube-system svc/",
			cursorAtEnd:  true,
			wantType:     "resource",
			wantResource: "services",
			wantName:     "",
			wantNS:       "kube-system",
		},
		{
			name:         "kubectl port-forward -n kube-system service/<tab>",
			cmdline:      "kubectl port-forward -n kube-system service/",
			cursorAtEnd:  true,
			wantType:     "resource",
			wantResource: "services",
			wantName:     "",
			wantNS:       "kube-system",
		},
		{
			name:         "kubectl port-forward -n default svc/core<tab>",
			cmdline:      "kubectl port-forward -n default svc/core",
			cursorAtEnd:  true,
			wantType:     "resource",
			wantResource: "services",
			wantName:     "",
			wantNS:       "default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := ParseCommandLine(tt.cmdline, tt.cursorAtEnd)
			if ctx.CompleteType != tt.wantType {
				t.Errorf("CompleteType = %q, want %q", ctx.CompleteType, tt.wantType)
			}
			if ctx.ResourceType != tt.wantResource {
				t.Errorf("ResourceType = %q, want %q", ctx.ResourceType, tt.wantResource)
			}
			if ctx.ResourceName != tt.wantName {
				t.Errorf("ResourceName = %q, want %q", ctx.ResourceName, tt.wantName)
			}
			if tt.wantNS != "" && ctx.Namespace != tt.wantNS {
				t.Errorf("Namespace = %q, want %q", ctx.Namespace, tt.wantNS)
			}
		})
	}
}

// Tests for context completion (--context)
func TestCompletion_Context(t *testing.T) {
	tests := []struct {
		name        string
		cmdline     string
		cursorAtEnd bool
		wantType    string
		wantQuery   string
	}{
		{
			name:        "kubectl --context <tab>",
			cmdline:     "kubectl --context ",
			cursorAtEnd: true,
			wantType:    "context",
			wantQuery:   "",
		},
		{
			name:        "kubectl --context prod<tab>",
			cmdline:     "kubectl --context prod",
			cursorAtEnd: true,
			wantType:    "context",
			wantQuery:   "prod",
		},
		{
			name:        "kubectl get pods --context <tab>",
			cmdline:     "kubectl get pods --context ",
			cursorAtEnd: true,
			wantType:    "context",
			wantQuery:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := ParseCommandLine(tt.cmdline, tt.cursorAtEnd)
			if ctx.CompleteType != tt.wantType {
				t.Errorf("CompleteType = %q, want %q", ctx.CompleteType, tt.wantType)
			}
			if ctx.CompleteQuery != tt.wantQuery {
				t.Errorf("CompleteQuery = %q, want %q", ctx.CompleteQuery, tt.wantQuery)
			}
		})
	}
}

// Tests for all-namespaces mode (-A)
func TestCompletion_AllNamespaces(t *testing.T) {
	tests := []struct {
		name              string
		cmdline           string
		cursorAtEnd       bool
		wantAllNamespaces bool
		wantType          string
	}{
		{
			name:              "kubectl get pods -A <tab>",
			cmdline:           "kubectl get pods -A ",
			cursorAtEnd:       true,
			wantAllNamespaces: true,
			wantType:          "resource",
		},
		{
			name:              "kubectl get pods --all-namespaces <tab>",
			cmdline:           "kubectl get pods --all-namespaces ",
			cursorAtEnd:       true,
			wantAllNamespaces: true,
			wantType:          "resource",
		},
		{
			name:              "kubectl get deployments -A <tab>",
			cmdline:           "kubectl get deployments -A ",
			cursorAtEnd:       true,
			wantAllNamespaces: true,
			wantType:          "resource",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := ParseCommandLine(tt.cmdline, tt.cursorAtEnd)
			if ctx.AllNamespaces != tt.wantAllNamespaces {
				t.Errorf("AllNamespaces = %v, want %v", ctx.AllNamespaces, tt.wantAllNamespaces)
			}
			if ctx.CompleteType != tt.wantType {
				t.Errorf("CompleteType = %q, want %q", ctx.CompleteType, tt.wantType)
			}
		})
	}
}

// Tests for combined flags
func TestCompletion_CombinedFlags(t *testing.T) {
	tests := []struct {
		name         string
		cmdline      string
		cursorAtEnd  bool
		wantNS       string
		wantCtx      string
		wantType     string
		wantResource string
	}{
		{
			name:         "kubectl get pods -n kube-system --context prod <tab>",
			cmdline:      "kubectl get pods -n kube-system --context prod ",
			cursorAtEnd:  true,
			wantNS:       "kube-system",
			wantCtx:      "prod",
			wantType:     "resource",
			wantResource: "pods",
		},
		{
			name:         "kubectl --context dev get deployments -n default <tab>",
			cmdline:      "kubectl --context dev get deployments -n default ",
			cursorAtEnd:  true,
			wantNS:       "default",
			wantCtx:      "dev",
			wantType:     "resource",
			wantResource: "deployments",
		},
		{
			name:         "kubectl -n monitoring get pods -l <tab>",
			cmdline:      "kubectl -n monitoring get pods -l ",
			cursorAtEnd:  true,
			wantNS:       "monitoring",
			wantType:     "label",
			wantResource: "pods",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := ParseCommandLine(tt.cmdline, tt.cursorAtEnd)
			if ctx.Namespace != tt.wantNS {
				t.Errorf("Namespace = %q, want %q", ctx.Namespace, tt.wantNS)
			}
			if ctx.Context != tt.wantCtx {
				t.Errorf("Context = %q, want %q", ctx.Context, tt.wantCtx)
			}
			if ctx.CompleteType != tt.wantType {
				t.Errorf("CompleteType = %q, want %q", ctx.CompleteType, tt.wantType)
			}
			if ctx.ResourceType != tt.wantResource {
				t.Errorf("ResourceType = %q, want %q", ctx.ResourceType, tt.wantResource)
			}
		})
	}
}

// Tests for k alias
func TestCompletion_KAlias(t *testing.T) {
	tests := []struct {
		name        string
		cmdline     string
		cursorAtEnd bool
		wantType    string
	}{
		{
			name:        "k get <tab>",
			cmdline:     "k get ",
			cursorAtEnd: true,
			wantType:    "resource_type",
		},
		{
			name:        "k get pods <tab>",
			cmdline:     "k get pods ",
			cursorAtEnd: true,
			wantType:    "resource",
		},
		{
			name:        "k logs <tab>",
			cmdline:     "k logs ",
			cursorAtEnd: true,
			wantType:    "resource",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := ParseCommandLine(tt.cmdline, tt.cursorAtEnd)
			if ctx.CompleteType != tt.wantType {
				t.Errorf("CompleteType = %q, want %q", ctx.CompleteType, tt.wantType)
			}
		})
	}
}

// Tests for events completion (special namespace case)
func TestCompletion_Events(t *testing.T) {
	tests := []struct {
		name         string
		cmdline      string
		cursorAtEnd  bool
		wantType     string
		wantResource string
	}{
		{
			name:         "kubectl get namespaces <tab>",
			cmdline:      "kubectl get namespaces ",
			cursorAtEnd:  true,
			wantType:     "resource",
			wantResource: "namespaces",
		},
		{
			name:         "kubectl events -n <tab>",
			cmdline:      "kubectl events -n ",
			cursorAtEnd:  true,
			wantType:     "namespace",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := ParseCommandLine(tt.cmdline, tt.cursorAtEnd)
			if ctx.CompleteType != tt.wantType {
				t.Errorf("CompleteType = %q, want %q", ctx.CompleteType, tt.wantType)
			}
			if tt.wantResource != "" && ctx.ResourceType != tt.wantResource {
				t.Errorf("ResourceType = %q, want %q", ctx.ResourceType, tt.wantResource)
			}
		})
	}
}

// Edge case tests
func TestCompletion_EdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		cmdline     string
		cursorAtEnd bool
		wantType    string
	}{
		{
			name:        "empty kubectl",
			cmdline:     "kubectl ",
			cursorAtEnd: true,
			wantType:    "action",
		},
		{
			name:        "non-kubectl command",
			cmdline:     "ls -la ",
			cursorAtEnd: true,
			wantType:    "",
		},
		{
			name:        "kubectl only",
			cmdline:     "kubectl",
			cursorAtEnd: true,
			wantType:    "action",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := ParseCommandLine(tt.cmdline, tt.cursorAtEnd)
			if ctx.CompleteType != tt.wantType {
				t.Errorf("CompleteType = %q, want %q", ctx.CompleteType, tt.wantType)
			}
		})
	}
}
