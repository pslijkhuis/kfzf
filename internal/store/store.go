package store

import (
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Resource represents a cached Kubernetes resource
type Resource struct {
	Name              string
	Namespace         string
	GVR               schema.GroupVersionResource
	Object            *unstructured.Unstructured
	CreationTimestamp time.Time
}

// ResourceKey uniquely identifies a resource
type ResourceKey struct {
	Context   string
	GVR       schema.GroupVersionResource
	Namespace string
	Name      string
}

// Store is an in-memory store for Kubernetes resources
type Store struct {
	mu sync.RWMutex
	// resources indexed by context -> GVR -> namespace -> name
	resources map[string]map[schema.GroupVersionResource]map[string]map[string]*Resource
	// Track which contexts/resources are being watched
	watching map[string]map[schema.GroupVersionResource]bool
}

// NewStore creates a new resource store
func NewStore() *Store {
	return &Store{
		resources: make(map[string]map[schema.GroupVersionResource]map[string]map[string]*Resource),
		watching:  make(map[string]map[schema.GroupVersionResource]bool),
	}
}

// Add adds or updates a resource in the store
// Note: The object is stored directly without deep copy for memory efficiency.
// Callers should not modify the object after calling Add.
func (s *Store) Add(context string, gvr schema.GroupVersionResource, obj *unstructured.Unstructured) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.resources[context] == nil {
		s.resources[context] = make(map[schema.GroupVersionResource]map[string]map[string]*Resource)
	}
	if s.resources[context][gvr] == nil {
		s.resources[context][gvr] = make(map[string]map[string]*Resource)
	}

	namespace := obj.GetNamespace()
	if namespace == "" {
		namespace = "_cluster" // marker for cluster-scoped resources
	}

	if s.resources[context][gvr][namespace] == nil {
		s.resources[context][gvr][namespace] = make(map[string]*Resource)
	}

	var creationTime time.Time
	if ct := obj.GetCreationTimestamp(); !ct.IsZero() {
		creationTime = ct.Time
	}

	// Store the object directly without deep copy for memory efficiency.
	// The watch API provides new object instances for each event, so this is safe.
	s.resources[context][gvr][namespace][obj.GetName()] = &Resource{
		Name:              obj.GetName(),
		Namespace:         obj.GetNamespace(),
		GVR:               gvr,
		Object:            obj,
		CreationTimestamp: creationTime,
	}
}

// Delete removes a resource from the store
func (s *Store) Delete(context string, gvr schema.GroupVersionResource, namespace, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if namespace == "" {
		namespace = "_cluster"
	}

	if s.resources[context] == nil {
		return
	}
	if s.resources[context][gvr] == nil {
		return
	}
	if s.resources[context][gvr][namespace] == nil {
		return
	}

	delete(s.resources[context][gvr][namespace], name)
}

// List returns all resources matching the criteria
func (s *Store) List(context string, gvr schema.GroupVersionResource, namespace string) []*Resource {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.resources[context] == nil || s.resources[context][gvr] == nil {
		return nil
	}

	// If namespace is empty, return all (for cluster-scoped or all-namespaces query)
	if namespace == "" {
		// Count total resources first for pre-allocation
		total := 0
		for _, nsResources := range s.resources[context][gvr] {
			total += len(nsResources)
		}
		result := make([]*Resource, 0, total)
		for _, nsResources := range s.resources[context][gvr] {
			for _, res := range nsResources {
				result = append(result, res)
			}
		}
		return result
	}

	// Return resources from specific namespace
	nsKey := namespace
	if namespace == "_cluster" || namespace == "" {
		nsKey = "_cluster"
	}

	nsResources := s.resources[context][gvr][nsKey]
	if nsResources == nil {
		return nil
	}
	result := make([]*Resource, 0, len(nsResources))
	for _, res := range nsResources {
		result = append(result, res)
	}
	return result
}

// ListNamespaced returns all resources from a specific namespace
func (s *Store) ListNamespaced(context string, gvr schema.GroupVersionResource, namespace string) []*Resource {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.resources[context] == nil || s.resources[context][gvr] == nil {
		return nil
	}

	if namespace == "" {
		// Count total namespaced resources first for pre-allocation
		total := 0
		for ns, nsResources := range s.resources[context][gvr] {
			if ns == "_cluster" {
				continue
			}
			total += len(nsResources)
		}
		result := make([]*Resource, 0, total)
		for ns, nsResources := range s.resources[context][gvr] {
			if ns == "_cluster" {
				continue // skip cluster-scoped
			}
			for _, res := range nsResources {
				result = append(result, res)
			}
		}
		return result
	}

	nsResources := s.resources[context][gvr][namespace]
	if nsResources == nil {
		return nil
	}
	result := make([]*Resource, 0, len(nsResources))
	for _, res := range nsResources {
		result = append(result, res)
	}
	return result
}

// ListClusterScoped returns all cluster-scoped resources
func (s *Store) ListClusterScoped(context string, gvr schema.GroupVersionResource) []*Resource {
	return s.List(context, gvr, "_cluster")
}

// Get returns a specific resource
func (s *Store) Get(context string, gvr schema.GroupVersionResource, namespace, name string) *Resource {
	s.mu.RLock()
	defer s.mu.RUnlock()

	nsKey := namespace
	if nsKey == "" {
		nsKey = "_cluster"
	}

	if s.resources[context] == nil ||
		s.resources[context][gvr] == nil ||
		s.resources[context][gvr][nsKey] == nil {
		return nil
	}

	return s.resources[context][gvr][nsKey][name]
}

// Clear removes all resources for a context and GVR
func (s *Store) Clear(context string, gvr schema.GroupVersionResource) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.resources[context] != nil {
		delete(s.resources[context], gvr)
	}
}

// ClearContext removes all resources for a context
func (s *Store) ClearContext(context string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.resources, context)
	delete(s.watching, context)
}

// SetWatching marks a resource type as being watched
func (s *Store) SetWatching(context string, gvr schema.GroupVersionResource, watching bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.watching[context] == nil {
		s.watching[context] = make(map[schema.GroupVersionResource]bool)
	}
	s.watching[context][gvr] = watching
}

// IsWatching returns whether a resource type is being watched
func (s *Store) IsWatching(context string, gvr schema.GroupVersionResource) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.watching[context] == nil {
		return false
	}
	return s.watching[context][gvr]
}

// Stats returns statistics about the store
func (s *Store) Stats() map[string]map[string]int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := make(map[string]map[string]int)

	for ctx, gvrMap := range s.resources {
		stats[ctx] = make(map[string]int)
		for gvr, nsMap := range gvrMap {
			count := 0
			for _, resources := range nsMap {
				count += len(resources)
			}
			stats[ctx][gvr.Resource] = count
		}
	}

	return stats
}

// Count returns the total number of resources in the store
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	count := 0
	for _, gvrMap := range s.resources {
		for _, nsMap := range gvrMap {
			for _, resources := range nsMap {
				count += len(resources)
			}
		}
	}
	return count
}
