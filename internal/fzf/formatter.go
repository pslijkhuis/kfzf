package fzf

import (
	"fmt"
	"strings"
	"time"

	"github.com/pslijkhuis/kfzf/internal/config"
	"github.com/pslijkhuis/kfzf/internal/store"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ANSI color codes
const (
	colorReset   = "\033[0m"
	colorBold    = "\033[1m"
	colorDim     = "\033[2m"
	colorCyan    = "\033[36m"
	colorGreen   = "\033[32m"
	colorYellow  = "\033[33m"
	colorRed     = "\033[31m"
	colorBlue    = "\033[34m"
	colorMagenta = "\033[35m"
)

// Formatter formats resources for fzf display
type Formatter struct {
	config *config.Config
}

// NewFormatter creates a new formatter
func NewFormatter(cfg *config.Config) *Formatter {
	return &Formatter{config: cfg}
}

// Format formats a list of resources for fzf output (plain text, colors added in shell)
func (f *Formatter) Format(resources []*store.Resource, resourceType string) string {
	if len(resources) == 0 {
		return ""
	}

	resCfg := f.config.GetResourceConfig(resourceType)
	columns := resCfg.Columns

	var buf strings.Builder
	buf.Grow(len(resources) * 100)

	for i, res := range resources {
		if i > 0 {
			buf.WriteByte('\n')
		}
		for j, col := range columns {
			if j > 0 {
				buf.WriteByte('\t')
			}
			value := f.extractField(res.Object, col.Field, res.CreationTimestamp)
			if col.Width > 0 {
				if col.Field == ".metadata.namespace" {
					// Pad namespace but never truncate (needed for -A completion)
					value = f.padOnly(value, col.Width)
				} else {
					value = f.truncateOrPad(value, col.Width)
				}
			}
			buf.WriteString(value)
		}
	}

	return buf.String()
}

// colorize applies ANSI colors based on column name and value
func (f *Formatter) colorize(value, colName string, colIndex int) string {
	trimmed := strings.TrimSpace(value)

	// First column (usually NAME) - cyan and bold
	if colIndex == 0 {
		return colorCyan + colorBold + value + colorReset
	}

	// Namespace column - blue
	if strings.EqualFold(colName, "NAMESPACE") {
		return colorBlue + value + colorReset
	}

	// Status/Phase/Health column - color based on value
	colUpper := strings.ToUpper(colName)
	if colUpper == "STATUS" || colUpper == "PHASE" || colUpper == "HEALTH" {
		switch strings.ToLower(trimmed) {
		case "running", "active", "ready", "true", "bound", "available", "healthy", "provisioned":
			return colorGreen + value + colorReset
		case "pending", "waiting", "unknown", "terminating", "progressing", "suspended", "missing", "degraded":
			return colorYellow + value + colorReset
		case "failed", "error", "crashloopbackoff", "imagepullbackoff", "false", "notready":
			return colorRed + value + colorReset
		case "succeeded", "completed":
			return colorGreen + value + colorReset
		default:
			return colorYellow + value + colorReset
		}
	}

	// Sync column (ArgoCD)
	if colUpper == "SYNC" {
		switch strings.ToLower(trimmed) {
		case "synced":
			return colorGreen + value + colorReset
		case "outofsync":
			return colorYellow + value + colorReset
		case "unknown":
			return colorRed + value + colorReset
		default:
			return colorYellow + value + colorReset
		}
	}

	// Ready column (like 1/1, 3/3)
	if colUpper == "READY" {
		if strings.Contains(trimmed, "/") {
			parts := strings.Split(trimmed, "/")
			if len(parts) == 2 && parts[0] == parts[1] && parts[0] != "0" && parts[0] != "" {
				return colorGreen + value + colorReset
			} else if parts[0] == "0" || parts[0] == "" {
				return colorRed + value + colorReset
			}
			return colorYellow + value + colorReset
		}
	}

	// Age column - dim
	if colUpper == "AGE" {
		return colorDim + value + colorReset
	}

	return value
}

// FormatWithHeader formats resources with a header line
func (f *Formatter) FormatWithHeader(resources []*store.Resource, resourceType string) string {
	if len(resources) == 0 {
		return ""
	}

	resCfg := f.config.GetResourceConfig(resourceType)
	columns := resCfg.Columns

	var buf strings.Builder
	buf.Grow((len(resources) + 1) * 100)

	// Build header
	for j, col := range columns {
		if j > 0 {
			buf.WriteByte('\t')
		}
		header := col.Name
		if col.Width > 0 {
			header = f.truncateOrPad(header, col.Width)
		}
		buf.WriteString(header)
	}

	// Build data lines
	for _, res := range resources {
		buf.WriteByte('\n')
		for j, col := range columns {
			if j > 0 {
				buf.WriteByte('\t')
			}
			value := f.extractField(res.Object, col.Field, res.CreationTimestamp)
			if col.Width > 0 {
				if col.Field == ".metadata.namespace" {
					// Pad namespace but never truncate (needed for -A completion)
					value = f.padOnly(value, col.Width)
				} else {
					value = f.truncateOrPad(value, col.Width)
				}
			}
			buf.WriteString(value)
		}
	}

	return buf.String()
}

// extractField extracts a field value from an unstructured object
func (f *Formatter) extractField(obj *unstructured.Unstructured, field string, creationTime time.Time) string {
	if obj == nil {
		return ""
	}

	// Handle special age field
	if field == ".metadata.creationTimestamp" {
		return f.formatAge(creationTime)
	}

	// Handle special node fields
	switch field {
	case "_nodeStatus":
		return f.extractNodeStatus(obj.Object)
	case "_nodeRoles":
		return f.extractNodeRoles(obj.Object)
	case "_nodeTopology":
		return f.extractNodeTopology(obj.Object)
	case "_capiClusterStatus":
		return f.extractCapiClusterStatus(obj.Object)
	case "_cnpgClusterStatus":
		return f.extractCnpgClusterStatus(obj.Object)
	case "_cnpgClusterReady":
		return f.extractCnpgClusterReady(obj.Object)
	case "_certReady":
		return f.extractCertReady(obj.Object)
	case "_issuerReady":
		return f.extractIssuerReady(obj.Object)
	}

	// Handle ratio fields like ".status.readyReplicas/.spec.replicas"
	if strings.Contains(field, "/") && !strings.HasPrefix(field, ".") {
		// This is a simple path, not a ratio
	} else if strings.Count(field, "/") == 1 && strings.HasPrefix(field, ".") {
		parts := strings.Split(field, "/")
		if len(parts) == 2 {
			num := f.getNestedValue(obj.Object, parts[0])
			denom := f.getNestedValue(obj.Object, parts[1])
			return fmt.Sprintf("%v/%v", num, denom)
		}
	}

	// Handle array access with filter like ".status.conditions[?(@.type==\"Ready\")].status"
	if strings.Contains(field, "[?(") {
		return f.extractFilteredArrayField(obj.Object, field)
	}

	// Handle simple array access like ".spec.rules[*].host"
	if strings.Contains(field, "[*]") {
		return f.extractArrayField(obj.Object, field)
	}

	value := f.getNestedValue(obj.Object, field)
	if value == nil {
		return ""
	}
	if s, ok := value.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", value)
}

// extractNodeStatus returns node status like "Ready" or "Ready,SchedulingDisabled"
func (f *Formatter) extractNodeStatus(obj map[string]interface{}) string {
	var statuses []string

	// Check Ready condition
	conditions := f.getNestedValue(obj, ".status.conditions")
	if condArr, ok := conditions.([]interface{}); ok {
		for _, cond := range condArr {
			if condMap, ok := cond.(map[string]interface{}); ok {
				condType := fmt.Sprintf("%v", condMap["type"])
				condStatus := fmt.Sprintf("%v", condMap["status"])
				if condType == "Ready" {
					if strings.EqualFold(condStatus, "true") {
						statuses = append(statuses, "Ready")
					} else {
						statuses = append(statuses, "NotReady")
					}
					break
				}
			}
		}
	}

	// Check if unschedulable (SchedulingDisabled)
	spec := f.getNestedValue(obj, ".spec")
	if specMap, ok := spec.(map[string]interface{}); ok {
		if unschedulable, ok := specMap["unschedulable"].(bool); ok && unschedulable {
			statuses = append(statuses, "SchedulingDisabled")
		}
	}

	if len(statuses) == 0 {
		return "Unknown"
	}
	return strings.Join(statuses, ",")
}

// extractNodeRoles returns node roles from labels like "control-plane,worker"
func (f *Formatter) extractNodeRoles(obj map[string]interface{}) string {
	var roles []string

	labels := f.getNestedValue(obj, ".metadata.labels")
	if labelMap, ok := labels.(map[string]interface{}); ok {
		for key := range labelMap {
			if strings.HasPrefix(key, "node-role.kubernetes.io/") {
				role := strings.TrimPrefix(key, "node-role.kubernetes.io/")
				if role != "" {
					roles = append(roles, role)
				}
			}
		}
	}

	if len(roles) == 0 {
		return "<none>"
	}
	return strings.Join(roles, ",")
}

// extractNodeTopology returns node topology labels (region/zone)
func (f *Formatter) extractNodeTopology(obj map[string]interface{}) string {
	var parts []string

	labels := f.getNestedValue(obj, ".metadata.labels")
	if labelMap, ok := labels.(map[string]interface{}); ok {
		// Check for standard topology labels
		if region, ok := labelMap["topology.kubernetes.io/region"].(string); ok && region != "" {
			parts = append(parts, region)
		}
		if zone, ok := labelMap["topology.kubernetes.io/zone"].(string); ok && zone != "" {
			parts = append(parts, zone)
		}
		// Fallback to legacy labels
		if len(parts) == 0 {
			if region, ok := labelMap["failure-domain.beta.kubernetes.io/region"].(string); ok && region != "" {
				parts = append(parts, region)
			}
			if zone, ok := labelMap["failure-domain.beta.kubernetes.io/zone"].(string); ok && zone != "" {
				parts = append(parts, zone)
			}
		}
	}

	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, "/")
}

// extractCapiClusterStatus returns CAPI cluster status from conditions and phase
func (f *Formatter) extractCapiClusterStatus(obj map[string]interface{}) string {
	// First check the phase
	phase := f.getNestedValue(obj, ".status.phase")
	if phaseStr, ok := phase.(string); ok && phaseStr != "" {
		// Check Ready condition for more detail
		conditions := f.getNestedValue(obj, ".status.conditions")
		if condArr, ok := conditions.([]interface{}); ok {
			for _, cond := range condArr {
				if condMap, ok := cond.(map[string]interface{}); ok {
					condType := fmt.Sprintf("%v", condMap["type"])
					condStatus := fmt.Sprintf("%v", condMap["status"])
					if condType == "Ready" {
						if strings.EqualFold(condStatus, "true") {
							return phaseStr
						}
						// Not ready - check reason
						if reason, ok := condMap["reason"].(string); ok && reason != "" {
							return phaseStr + " (" + reason + ")"
						}
						return phaseStr + " (NotReady)"
					}
				}
			}
		}
		return phaseStr
	}

	// Fallback to checking controlPlaneReady
	cpReady := f.getNestedValue(obj, ".status.controlPlaneReady")
	infraReady := f.getNestedValue(obj, ".status.infrastructureReady")

	if cpReady == true && infraReady == true {
		return "Ready"
	}
	if cpReady == false || infraReady == false {
		return "NotReady"
	}

	return "Unknown"
}

// extractCnpgClusterStatus returns CNPG cluster status/phase
func (f *Formatter) extractCnpgClusterStatus(obj map[string]interface{}) string {
	// Check the phase field first
	phase := f.getNestedValue(obj, ".status.phase")
	if phaseStr, ok := phase.(string); ok && phaseStr != "" {
		return phaseStr
	}

	// Fall back to Ready condition
	conditions := f.getNestedValue(obj, ".status.conditions")
	if condArr, ok := conditions.([]interface{}); ok {
		for _, cond := range condArr {
			if condMap, ok := cond.(map[string]interface{}); ok {
				condType := fmt.Sprintf("%v", condMap["type"])
				condStatus := fmt.Sprintf("%v", condMap["status"])
				if condType == "Ready" {
					if strings.EqualFold(condStatus, "true") {
						return "Ready"
					}
					if reason, ok := condMap["reason"].(string); ok && reason != "" {
						return reason
					}
					return "NotReady"
				}
			}
		}
	}

	return "Unknown"
}

// extractCnpgClusterReady returns CNPG ready instances as "ready/total"
func (f *Formatter) extractCnpgClusterReady(obj map[string]interface{}) string {
	readyInstances := f.getNestedValue(obj, ".status.readyInstances")
	totalInstances := f.getNestedValue(obj, ".spec.instances")

	ready := "0"
	total := "0"

	if r, ok := readyInstances.(int64); ok {
		ready = fmt.Sprintf("%d", r)
	} else if r, ok := readyInstances.(float64); ok {
		ready = fmt.Sprintf("%d", int(r))
	}

	if t, ok := totalInstances.(int64); ok {
		total = fmt.Sprintf("%d", t)
	} else if t, ok := totalInstances.(float64); ok {
		total = fmt.Sprintf("%d", int(t))
	}

	return ready + "/" + total
}

// extractCertReady returns cert-manager Certificate ready status
func (f *Formatter) extractCertReady(obj map[string]interface{}) string {
	conditions := f.getNestedValue(obj, ".status.conditions")
	if condArr, ok := conditions.([]interface{}); ok {
		for _, cond := range condArr {
			if condMap, ok := cond.(map[string]interface{}); ok {
				condType := fmt.Sprintf("%v", condMap["type"])
				condStatus := fmt.Sprintf("%v", condMap["status"])
				if condType == "Ready" {
					if strings.EqualFold(condStatus, "true") {
						return "True"
					}
					return "False"
				}
			}
		}
	}
	return "Unknown"
}

// extractIssuerReady returns cert-manager Issuer/ClusterIssuer ready status
func (f *Formatter) extractIssuerReady(obj map[string]interface{}) string {
	conditions := f.getNestedValue(obj, ".status.conditions")
	if condArr, ok := conditions.([]interface{}); ok {
		for _, cond := range condArr {
			if condMap, ok := cond.(map[string]interface{}); ok {
				condType := fmt.Sprintf("%v", condMap["type"])
				condStatus := fmt.Sprintf("%v", condMap["status"])
				if condType == "Ready" {
					if strings.EqualFold(condStatus, "true") {
						return "True"
					}
					return "False"
				}
			}
		}
	}
	return "Unknown"
}

// getNestedValue extracts a nested value from a map using dot notation
// Optimized to avoid allocations from strings.Split
func (f *Formatter) getNestedValue(obj map[string]interface{}, path string) interface{} {
	if path == "" || obj == nil {
		return ""
	}

	// Remove leading dot
	if len(path) > 0 && path[0] == '.' {
		path = path[1:]
	}

	var current interface{} = obj
	
	start := 0
	for i := 0; i <= len(path); i++ {
		if i == len(path) || path[i] == '.' {
			if start == i {
				start = i + 1
				continue // Empty key, shouldn't happen with correct paths
			}
			
			key := path[start:i]
			start = i + 1

			// Traverse
			m, ok := current.(map[string]interface{})
			if !ok {
				return "" // Cannot traverse further
			}
			
			val, exists := m[key]
			if !exists {
				return ""
			}
			current = val
		}
	}

	return current
}

// extractArrayField handles fields with [*] array access
func (f *Formatter) extractArrayField(obj map[string]interface{}, field string) string {
	// Split on [*]
	parts := strings.Split(field, "[*]")
	if len(parts) != 2 {
		return ""
	}

	arrayPath := parts[0]
	fieldPath := strings.TrimPrefix(parts[1], ".")

	arrayValue := f.getNestedValue(obj, arrayPath)
	arr, ok := arrayValue.([]interface{})
	if !ok {
		return ""
	}

	var values []string
	for _, item := range arr {
		if itemMap, ok := item.(map[string]interface{}); ok {
			if fieldPath != "" {
				val := f.getNestedValue(itemMap, "."+fieldPath)
				if val != nil && val != "" {
					values = append(values, fmt.Sprintf("%v", val))
				}
			}
		}
	}

	return strings.Join(values, ",")
}

// extractFilteredArrayField handles JSONPath-like filtered array access
func (f *Formatter) extractFilteredArrayField(obj map[string]interface{}, field string) string {
	// Parse something like ".status.conditions[?(@.type==\"Ready\")].status"
	// Find the array path
	bracketIdx := strings.Index(field, "[?(")
	if bracketIdx == -1 {
		return ""
	}

	arrayPath := field[:bracketIdx]
	remaining := field[bracketIdx:]

	// Find the filter condition
	endBracket := strings.Index(remaining, ")]")
	if endBracket == -1 {
		return ""
	}

	filterExpr := remaining[3:endBracket] // Skip "[?(" and get until ")"
	fieldAfter := strings.TrimPrefix(remaining[endBracket+2:], ".")

	// Parse filter expression like "@.type==\"Ready\""
	filterParts := strings.Split(filterExpr, "==")
	if len(filterParts) != 2 {
		return ""
	}

	filterField := strings.TrimPrefix(filterParts[0], "@.")
	filterValue := strings.Trim(filterParts[1], "\"")

	// Get the array
	arrayValue := f.getNestedValue(obj, arrayPath)
	arr, ok := arrayValue.([]interface{})
	if !ok {
		return ""
	}

	// Find matching item
	for _, item := range arr {
		if itemMap, ok := item.(map[string]interface{}); ok {
			if val := f.getNestedValue(itemMap, "."+filterField); fmt.Sprintf("%v", val) == filterValue {
				if fieldAfter != "" {
					result := f.getNestedValue(itemMap, "."+fieldAfter)
					return fmt.Sprintf("%v", result)
				}
				return fmt.Sprintf("%v", itemMap)
			}
		}
	}

	return ""
}

// formatAge formats a duration as a human-readable age string
func (f *Formatter) formatAge(t time.Time) string {
	if t.IsZero() {
		return "<unknown>"
	}

	duration := time.Since(t)

	if duration < time.Minute {
		return fmt.Sprintf("%ds", int(duration.Seconds()))
	}
	if duration < time.Hour {
		return fmt.Sprintf("%dm", int(duration.Minutes()))
	}
	if duration < 24*time.Hour {
		return fmt.Sprintf("%dh", int(duration.Hours()))
	}
	days := int(duration.Hours() / 24)
	if days < 30 {
		return fmt.Sprintf("%dd", days)
	}
	if days < 365 {
		return fmt.Sprintf("%dM", days/30)
	}
	return fmt.Sprintf("%dy", days/365)
}

// truncateOrPad truncates or pads a string to a fixed width
func (f *Formatter) truncateOrPad(s string, width int) string {
	if len(s) > width {
		if width > 3 {
			return s[:width-3] + "..."
		}
		return s[:width]
	}
	return s + strings.Repeat(" ", width-len(s))
}

// padOnly pads a string to at least the given width (never truncates)
func (f *Formatter) padOnly(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}