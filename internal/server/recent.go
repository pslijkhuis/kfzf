package server

import "sync"

const maxRecentKeys = 100 // Maximum number of unique context/namespace/resourceType combinations

// RecentResources tracks recently accessed resources for suggestions
type RecentResources struct {
	mu       sync.RWMutex
	maxItems int
	maxKeys  int
	items    map[string][]string // key: "context/namespace/resourceType" -> list of names
	keyOrder []string            // Track key insertion order for LRU eviction
}

// NewRecentResources creates a new RecentResources tracker
func NewRecentResources(max int) *RecentResources {
	return &RecentResources{
		maxItems: max,
		maxKeys:  maxRecentKeys,
		items:    make(map[string][]string),
		keyOrder: make([]string, 0, maxRecentKeys),
	}
}

// Add adds a resource to the recent list, moving it to the front if it already exists
func (r *RecentResources) Add(context, namespace, resourceType, name string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := context + "/" + namespace + "/" + resourceType

	// Check if this is a new key
	list, exists := r.items[key]

	// Find and remove if already exists (in-place)
	foundIdx := -1
	for i, item := range list {
		if item == name {
			foundIdx = i
			break
		}
	}

	if foundIdx >= 0 {
		// Remove by shifting elements (avoids allocation)
		copy(list[foundIdx:], list[foundIdx+1:])
		list = list[:len(list)-1]
	}

	// Add to front - reuse slice if possible
	if cap(list) > len(list) {
		// Shift right and insert at front
		list = list[:len(list)+1]
		copy(list[1:], list[:len(list)-1])
		list[0] = name
	} else {
		// Need to allocate new slice with appropriate capacity
		newCap := r.maxItems
		if len(list)+1 > newCap {
			newCap = len(list) + 1
		}
		newList := make([]string, len(list)+1, newCap)
		newList[0] = name
		copy(newList[1:], list)
		list = newList
	}

	// Trim to max
	if len(list) > r.maxItems {
		list = list[:r.maxItems]
	}

	r.items[key] = list

	// Update key order for LRU tracking
	if !exists {
		// New key - add to order and possibly evict oldest
		if len(r.keyOrder) >= r.maxKeys {
			// Evict oldest key
			oldestKey := r.keyOrder[0]
			// Shift slice instead of reslicing to allow GC of old elements
			copy(r.keyOrder, r.keyOrder[1:])
			r.keyOrder = r.keyOrder[:len(r.keyOrder)-1]
			delete(r.items, oldestKey)
		}
		r.keyOrder = append(r.keyOrder, key)
	} else {
		// Existing key - move to end (most recently used)
		r.moveKeyToEnd(key)
	}
}

// moveKeyToEnd moves a key to the end of the order slice (most recently used)
func (r *RecentResources) moveKeyToEnd(key string) {
	for i, k := range r.keyOrder {
		if k == key {
			// Shift elements left and place key at end (avoids allocation)
			copy(r.keyOrder[i:], r.keyOrder[i+1:])
			r.keyOrder[len(r.keyOrder)-1] = key
			return
		}
	}
}

// Get returns the list of recently accessed resources for the given key
func (r *RecentResources) Get(context, namespace, resourceType string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	key := context + "/" + namespace + "/" + resourceType
	if items, ok := r.items[key]; ok {
		// Return a copy to avoid race conditions
		result := make([]string, len(items))
		copy(result, items)
		return result
	}
	return nil
}

// Clear removes all recent resources
func (r *RecentResources) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items = make(map[string][]string)
	r.keyOrder = make([]string, 0, r.maxKeys)
}
