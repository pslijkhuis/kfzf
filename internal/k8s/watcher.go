package k8s

import (
	"context"
	"fmt"
	"log/slog"
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

// pruneObject removes large fields that aren't needed for completion
// This significantly reduces memory usage for secrets, configmaps, etc.
func pruneObject(obj *unstructured.Unstructured) {
	o := obj.Object

	// Remove data/binaryData from secrets and configmaps (can be huge)
	delete(o, "data")
	delete(o, "binaryData")
	delete(o, "stringData")

	// Remove managedFields from metadata (verbose, not needed)
	if metadata, ok := o["metadata"].(map[string]interface{}); ok {
		delete(metadata, "managedFields")
		// Remove annotations we don't need (can be large, e.g., last-applied-config)
		if annotations, ok := metadata["annotations"].(map[string]interface{}); ok {
			delete(annotations, "kubectl.kubernetes.io/last-applied-configuration")
		}
	}

	// For pods, prune container env/volumeMounts which can be large
	if spec, ok := o["spec"].(map[string]interface{}); ok {
		pruneContainerList(spec, "containers")
		pruneContainerList(spec, "initContainers")
		// Remove volumes (not needed for completion)
		delete(spec, "volumes")
	}
}

// pruneContainerList prunes unnecessary fields from containers
func pruneContainerList(spec map[string]interface{}, key string) {
	containers, ok := spec[key].([]interface{})
	if !ok {
		return
	}
	for _, c := range containers {
		container, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		// Keep: name, image, ports
		// Remove: env, envFrom, volumeMounts, resources, etc.
		delete(container, "env")
		delete(container, "envFrom")
		delete(container, "volumeMounts")
		delete(container, "resources")
		delete(container, "livenessProbe")
		delete(container, "readinessProbe")
		delete(container, "startupProbe")
		delete(container, "lifecycle")
		delete(container, "securityContext")
		delete(container, "command")
		delete(container, "args")
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
		pruneObject(&list.Items[i])
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
				pruneObject(obj)
				m.store.Add(contextName, gvr, obj)
				m.logger.Debug("resource added",
					"context", contextName,
					"resource", gvr.Resource,
					"name", obj.GetName(),
					"namespace", obj.GetNamespace(),
				)
			case watch.Modified:
				// Update store directly - skip expensive diff for memory efficiency
				pruneObject(obj)
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
