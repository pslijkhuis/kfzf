package k8s

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/pslijkhuis/kfzf/internal/store"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
)

// WatchManager manages watches for multiple contexts and resource types
type WatchManager struct {
	clientManager *ClientManager
	store         *store.Store
	logger        *slog.Logger

	mu       sync.RWMutex
	watches  map[watchKey]context.CancelFunc
	contexts map[string]bool // contexts being actively watched
}

type watchKey struct {
	context  string
	gvr      schema.GroupVersionResource
}

// NewWatchManager creates a new watch manager
func NewWatchManager(clientManager *ClientManager, store *store.Store, logger *slog.Logger) *WatchManager {
	return &WatchManager{
		clientManager: clientManager,
		store:         store,
		logger:        logger,
		watches:       make(map[watchKey]context.CancelFunc),
		contexts:      make(map[string]bool),
	}
}

// StartWatching starts watching a resource type in a context
func (m *WatchManager) StartWatching(ctx context.Context, contextName string, gvr schema.GroupVersionResource, namespaced bool) error {
	key := watchKey{context: contextName, gvr: gvr}

	m.mu.Lock()
	if _, exists := m.watches[key]; exists {
		m.mu.Unlock()
		return nil // Already watching
	}

	watchCtx, cancel := context.WithCancel(ctx)
	m.watches[key] = cancel
	m.contexts[contextName] = true
	m.mu.Unlock()

	go m.watch(watchCtx, contextName, gvr, namespaced)
	return nil
}

// StopWatching stops watching a resource type in a context and clears cached data
func (m *WatchManager) StopWatching(contextName string, gvr schema.GroupVersionResource) {
	key := watchKey{context: contextName, gvr: gvr}

	m.mu.Lock()
	defer m.mu.Unlock()

	if cancel, exists := m.watches[key]; exists {
		cancel()
		delete(m.watches, key)
		m.store.SetWatching(contextName, gvr, false)
		m.store.Clear(contextName, gvr) // Clear cached data to prevent memory leak
	}
}

// StopAll stops all watches and clears all cached data
func (m *WatchManager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for key, cancel := range m.watches {
		cancel()
		m.store.SetWatching(key.context, key.gvr, false)
		m.store.Clear(key.context, key.gvr) // Clear cached data to prevent memory leak
	}
	m.watches = make(map[watchKey]context.CancelFunc)
	m.contexts = make(map[string]bool)
}

// watch runs the watch loop for a specific resource
func (m *WatchManager) watch(ctx context.Context, contextName string, gvr schema.GroupVersionResource, namespaced bool) {
	// Ensure cleanup when goroutine exits
	defer m.cleanupWatch(contextName, gvr)

	m.logger.Info("starting watch",
		"context", contextName,
		"resource", gvr.Resource,
		"group", gvr.Group,
	)

	backoff := time.Second
	maxBackoff := 5 * time.Minute

	for {
		select {
		case <-ctx.Done():
			m.logger.Info("watch stopped",
				"context", contextName,
				"resource", gvr.Resource,
			)
			return
		default:
		}

		err := m.runWatch(ctx, contextName, gvr, namespaced)
		if err != nil {
			if ctx.Err() != nil {
				return // Context cancelled
			}
			m.logger.Warn("watch error, will retry",
				"context", contextName,
				"resource", gvr.Resource,
				"error", err,
				"backoff", backoff,
			)

			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}

			backoff = min(backoff*2, maxBackoff)
		} else {
			backoff = time.Second // Reset backoff on success
		}
	}
}

// cleanupWatch removes a watch entry when the goroutine exits
func (m *WatchManager) cleanupWatch(contextName string, gvr schema.GroupVersionResource) {
	key := watchKey{context: contextName, gvr: gvr}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Only delete if the entry exists (StopWatching may have already removed it)
	if _, exists := m.watches[key]; exists {
		delete(m.watches, key)
		m.store.SetWatching(contextName, gvr, false)
	}
}

// runWatch performs a single watch iteration (list + watch)
func (m *WatchManager) runWatch(ctx context.Context, contextName string, gvr schema.GroupVersionResource, namespaced bool) error {
	client, err := m.clientManager.GetClient(contextName)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	// Get the dynamic resource interface
	var resourceClient interface {
		List(ctx context.Context, opts metav1.ListOptions) (*unstructured.UnstructuredList, error)
		Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error)
	}

	if namespaced {
		// Watch all namespaces
		resourceClient = client.DynamicClient.Resource(gvr)
	} else {
		resourceClient = client.DynamicClient.Resource(gvr)
	}

	// Initial list to populate the store
	list, err := resourceClient.List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list resources: %w", err)
	}

	// Clear existing resources and add new ones
	m.store.Clear(contextName, gvr)
	for i := range list.Items {
		m.store.Add(contextName, gvr, &list.Items[i])
	}
	m.store.SetWatching(contextName, gvr, true)

	m.logger.Info("initial list complete",
		"context", contextName,
		"resource", gvr.Resource,
		"count", len(list.Items),
	)

	// Start watching from the resource version of the list
	resourceVersion := list.GetResourceVersion()
	watcher, err := resourceClient.Watch(ctx, metav1.ListOptions{
		ResourceVersion: resourceVersion,
	})
	if err != nil {
		return fmt.Errorf("failed to start watch: %w", err)
	}
	defer watcher.Stop()

	// Process watch events
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event, ok := <-watcher.ResultChan():
			if !ok {
				return fmt.Errorf("watch channel closed")
			}

			obj, ok := event.Object.(*unstructured.Unstructured)
			if !ok {
				continue
			}

			switch event.Type {
			case watch.Added:
				m.store.Add(contextName, gvr, obj)
				m.logger.Debug("resource added",
					"context", contextName,
					"resource", gvr.Resource,
					"name", obj.GetName(),
					"namespace", obj.GetNamespace(),
				)
			case watch.Modified:
				// Update store directly - skip expensive diff for memory efficiency
				m.store.Add(contextName, gvr, obj)
				m.logger.Debug("resource modified",
					"context", contextName,
					"resource", gvr.Resource,
					"name", obj.GetName(),
					"namespace", obj.GetNamespace(),
				)
			case watch.Deleted:
				m.store.Delete(contextName, gvr, obj.GetNamespace(), obj.GetName())
				m.logger.Debug("resource deleted",
					"context", contextName,
					"resource", gvr.Resource,
					"name", obj.GetName(),
					"namespace", obj.GetNamespace(),
				)
			case watch.Error:
				m.logger.Warn("watch error event",
					"context", contextName,
					"resource", gvr.Resource,
					"object", event.Object,
				)
			}
		}
	}
}

// IsWatching returns whether a resource is being watched
func (m *WatchManager) IsWatching(contextName string, gvr schema.GroupVersionResource) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	key := watchKey{context: contextName, gvr: gvr}
	_, exists := m.watches[key]
	return exists
}

// WatchedResources returns all currently watched resources
func (m *WatchManager) WatchedResources() map[string][]schema.GroupVersionResource {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string][]schema.GroupVersionResource)
	for key := range m.watches {
		result[key.context] = append(result[key.context], key.gvr)
	}
	return result
}

// ActiveContexts returns a list of contexts that have active watches
func (m *WatchManager) ActiveContexts() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	contexts := make([]string, 0, len(m.contexts))
	for ctx := range m.contexts {
		contexts = append(contexts, ctx)
	}
	return contexts
}

// StopContext stops all watches for a specific context and clears its data
func (m *WatchManager) StopContext(contextName string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Find and stop all watches for this context
	for key, cancel := range m.watches {
		if key.context == contextName {
			cancel()
			m.store.SetWatching(key.context, key.gvr, false)
			m.store.Clear(key.context, key.gvr)
			delete(m.watches, key)
		}
	}
	delete(m.contexts, contextName)

	// Also clear any remaining context data from the store
	m.store.ClearContext(contextName)
}

// ANSI color codes for diff output
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorCyan   = "\033[36m"
	colorDim    = "\033[2m"
)

// skipFields contains fields to ignore when diffing objects (always change or too noisy)
var diffSkipFields = map[string]bool{
	"metadata.resourceVersion":   true,
	"metadata.managedFields":     true,
	"metadata.generation":        true,
	"metadata.uid":               true,
	"metadata.creationTimestamp": true,
}

// diffObjects compares two unstructured objects and returns a list of changes
func diffObjects(old, new map[string]interface{}) []string {
	var changes []string
	diffMaps("", old, new, &changes)
	return changes
}

// diffMaps recursively compares two maps and collects differences
func diffMaps(prefix string, old, new map[string]interface{}, changes *[]string) {
	// Check for modified and added keys
	for key, newVal := range new {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}

		if diffSkipFields[path] {
			continue
		}

		oldVal, exists := old[key]
		if !exists {
			*changes = append(*changes, fmt.Sprintf("%s[+]%s %s%s%s = %s%v%s", colorGreen, colorReset, colorCyan, path, colorReset, colorGreen, formatValue(newVal), colorReset))
			continue
		}

		if !reflect.DeepEqual(oldVal, newVal) {
			// If both are maps, recurse
			oldMap, oldIsMap := oldVal.(map[string]interface{})
			newMap, newIsMap := newVal.(map[string]interface{})
			if oldIsMap && newIsMap {
				diffMaps(path, oldMap, newMap, changes)
				continue
			}

			// If both are slices, diff them specially
			oldSlice, oldIsSlice := oldVal.([]interface{})
			newSlice, newIsSlice := newVal.([]interface{})
			if oldIsSlice && newIsSlice {
				sliceChanges := diffSlices(path, oldSlice, newSlice)
				*changes = append(*changes, sliceChanges...)
				continue
			}

			*changes = append(*changes, fmt.Sprintf("%s[~]%s %s%s%s: %s%v%s → %s%v%s", colorYellow, colorReset, colorCyan, path, colorReset, colorRed, formatValue(oldVal), colorReset, colorGreen, formatValue(newVal), colorReset))
		}
	}

	// Check for removed keys
	for key, oldVal := range old {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}

		if diffSkipFields[path] {
			continue
		}

		if _, exists := new[key]; !exists {
			*changes = append(*changes, fmt.Sprintf("%s[-]%s %s%s%s = %s%v%s", colorRed, colorReset, colorCyan, path, colorReset, colorRed, formatValue(oldVal), colorReset))
		}
	}
}

// diffSlices compares two slices and returns meaningful changes
func diffSlices(path string, old, new []interface{}) []string {
	changes := make([]string, 0, 4) // Pre-allocate for typical case

	// For condition arrays, compare by type field
	if strings.Contains(path, "conditions") {
		oldByType := indexConditionsByType(old)
		newByType := indexConditionsByType(new)

		for condType, newCond := range newByType {
			oldCond, exists := oldByType[condType]
			if !exists {
				changes = append(changes, fmt.Sprintf("%s[+]%s %s%s[%s]%s", colorGreen, colorReset, colorCyan, path, condType, colorReset))
				continue
			}
			// Compare status field
			oldStatus := getMapField(oldCond, "status")
			newStatus := getMapField(newCond, "status")
			if oldStatus != newStatus {
				changes = append(changes, fmt.Sprintf("%s[~]%s %s%s[%s].status%s: %s%v%s → %s%v%s", colorYellow, colorReset, colorCyan, path, condType, colorReset, colorRed, oldStatus, colorReset, colorGreen, newStatus, colorReset))
			}
			// Compare reason field if it changed
			oldReason := getMapField(oldCond, "reason")
			newReason := getMapField(newCond, "reason")
			if oldReason != newReason && newReason != "" {
				changes = append(changes, fmt.Sprintf("%s[~]%s %s%s[%s].reason%s: %s%v%s → %s%v%s", colorYellow, colorReset, colorCyan, path, condType, colorReset, colorRed, oldReason, colorReset, colorGreen, newReason, colorReset))
			}
		}

		for condType := range oldByType {
			if _, exists := newByType[condType]; !exists {
				changes = append(changes, fmt.Sprintf("%s[-]%s %s%s[%s]%s", colorRed, colorReset, colorCyan, path, condType, colorReset))
			}
		}
		return changes
	}

	// For container arrays, compare by name
	if strings.Contains(path, "containers") {
		oldByName := indexByName(old)
		newByName := indexByName(new)

		for name, newContainer := range newByName {
			oldContainer, exists := oldByName[name]
			if !exists {
				changes = append(changes, fmt.Sprintf("%s[+]%s %s%s[%s]%s", colorGreen, colorReset, colorCyan, path, name, colorReset))
				continue
			}
			// Check image changes
			oldImage := getMapField(oldContainer, "image")
			newImage := getMapField(newContainer, "image")
			if oldImage != newImage {
				changes = append(changes, fmt.Sprintf("%s[~]%s %s%s[%s].image%s: %s%v%s → %s%v%s", colorYellow, colorReset, colorCyan, path, name, colorReset, colorRed, oldImage, colorReset, colorGreen, newImage, colorReset))
			}
		}

		for name := range oldByName {
			if _, exists := newByName[name]; !exists {
				changes = append(changes, fmt.Sprintf("%s[-]%s %s%s[%s]%s", colorRed, colorReset, colorCyan, path, name, colorReset))
			}
		}
		return changes
	}

	// For containerStatuses, compare by name and show state changes
	if strings.Contains(path, "containerStatuses") {
		oldByName := indexByName(old)
		newByName := indexByName(new)

		for name, newStatus := range newByName {
			oldStatus, exists := oldByName[name]
			if !exists {
				changes = append(changes, fmt.Sprintf("%s[+]%s %s%s[%s]%s", colorGreen, colorReset, colorCyan, path, name, colorReset))
				continue
			}
			// Check ready state
			oldReady := getMapField(oldStatus, "ready")
			newReady := getMapField(newStatus, "ready")
			if oldReady != newReady {
				changes = append(changes, fmt.Sprintf("%s[~]%s %s%s[%s].ready%s: %s%v%s → %s%v%s", colorYellow, colorReset, colorCyan, path, name, colorReset, colorRed, oldReady, colorReset, colorGreen, newReady, colorReset))
			}
			// Check restart count
			oldRestarts := getMapField(oldStatus, "restartCount")
			newRestarts := getMapField(newStatus, "restartCount")
			if oldRestarts != newRestarts {
				changes = append(changes, fmt.Sprintf("%s[~]%s %s%s[%s].restartCount%s: %s%v%s → %s%v%s", colorYellow, colorReset, colorCyan, path, name, colorReset, colorRed, oldRestarts, colorReset, colorGreen, newRestarts, colorReset))
			}
			// Check state changes
			oldState := getContainerState(oldStatus)
			newState := getContainerState(newStatus)
			if oldState != newState {
				changes = append(changes, fmt.Sprintf("%s[~]%s %s%s[%s].state%s: %s%v%s → %s%v%s", colorYellow, colorReset, colorCyan, path, name, colorReset, colorRed, oldState, colorReset, colorGreen, newState, colorReset))
			}
		}
		return changes
	}

	// Generic slice comparison - just report size change or that it changed
	if len(old) != len(new) {
		changes = append(changes, fmt.Sprintf("%s[~]%s %s%s%s: len %s%d%s → %s%d%s", colorYellow, colorReset, colorCyan, path, colorReset, colorRed, len(old), colorReset, colorGreen, len(new), colorReset))
	} else if !reflect.DeepEqual(old, new) {
		changes = append(changes, fmt.Sprintf("%s[~]%s %s%s%s: %s(changed)%s", colorYellow, colorReset, colorCyan, path, colorReset, colorDim, colorReset))
	}
	return changes
}

// indexConditionsByType indexes a conditions array by the "type" field
func indexConditionsByType(conditions []interface{}) map[string]map[string]interface{} {
	result := make(map[string]map[string]interface{}, len(conditions))
	for _, c := range conditions {
		if cond, ok := c.(map[string]interface{}); ok {
			if t, ok := cond["type"].(string); ok {
				result[t] = cond
			}
		}
	}
	return result
}

// indexByName indexes an array by the "name" field
func indexByName(items []interface{}) map[string]map[string]interface{} {
	result := make(map[string]map[string]interface{}, len(items))
	for _, item := range items {
		if m, ok := item.(map[string]interface{}); ok {
			if name, ok := m["name"].(string); ok {
				result[name] = m
			}
		}
	}
	return result
}

// getMapField safely gets a field from a map
func getMapField(m map[string]interface{}, field string) interface{} {
	if m == nil {
		return nil
	}
	return m[field]
}

// getContainerState returns the current state of a container (running, waiting, terminated)
func getContainerState(status map[string]interface{}) string {
	state, ok := status["state"].(map[string]interface{})
	if !ok {
		return "unknown"
	}
	if _, ok := state["running"]; ok {
		return "running"
	}
	if w, ok := state["waiting"].(map[string]interface{}); ok {
		if reason, ok := w["reason"].(string); ok {
			return "waiting:" + reason
		}
		return "waiting"
	}
	if t, ok := state["terminated"].(map[string]interface{}); ok {
		if reason, ok := t["reason"].(string); ok {
			return "terminated:" + reason
		}
		return "terminated"
	}
	return "unknown"
}

// formatValue formats a value for display, truncating long values
func formatValue(v interface{}) string {
	if v == nil {
		return "<nil>"
	}
	s := fmt.Sprintf("%v", v)
	if s == "" {
		return "<empty>"
	}
	if len(s) > 60 {
		return s[:57] + "..."
	}
	return s
}
