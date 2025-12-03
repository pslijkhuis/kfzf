package store

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestStore_ConcurrentAccess(t *testing.T) {
	s := NewStore()
	gvr := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
	context := "test-context"

	// Number of concurrent goroutines
	numWriters := 10
	numReaders := 20
	numOperations := 100

	var wg sync.WaitGroup
	wg.Add(numWriters + numReaders)

	// Writers: Add resources
	for i := 0; i < numWriters; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				name := fmt.Sprintf("pod-%d-%d", id, j)
				obj := &unstructured.Unstructured{
					Object: map[string]interface{}{
						"metadata": map[string]interface{}{
							"name":      name,
							"namespace": "default",
						},
					},
				}
				s.Add(context, gvr, obj)
				// Simulate mixed operations
				if j%2 == 0 {
					s.Delete(context, gvr, "default", name)
				}
			}
		}(i)
	}

	// Readers: List and Get resources
	for i := 0; i < numReaders; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				_ = s.List(context, gvr, "default")
				_ = s.Count()
				// Small delay to interleave with writers
				time.Sleep(time.Microsecond)
			}
		}(i)
	}

	wg.Wait()
}

func TestStore_BasicOperations(t *testing.T) {
	s := NewStore()
	gvr := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
	context := "test-context"

	// Add a resource
	pod1 := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"metadata": map[string]interface{}{
				"name":      "pod-1",
				"namespace": "default",
			},
		},
	}
	s.Add(context, gvr, pod1)

	// Verify it exists
	res := s.Get(context, gvr, "default", "pod-1")
	if res == nil {
		t.Error("expected pod-1 to exist")
	}
	if res.Name != "pod-1" {
		t.Errorf("expected name pod-1, got %s", res.Name)
	}

	// List should return 1 item
	list := s.List(context, gvr, "default")
	if len(list) != 1 {
		t.Errorf("expected 1 item, got %d", len(list))
	}

	// Add another resource in a different namespace
	pod2 := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"metadata": map[string]interface{}{
				"name":      "pod-2",
				"namespace": "kube-system",
			},
		},
	}
	s.Add(context, gvr, pod2)

	// List "default" should still be 1
	list = s.List(context, gvr, "default")
	if len(list) != 1 {
		t.Errorf("expected 1 item in default, got %d", len(list))
	}

	// List "" (all namespaces) should be 2
	list = s.List(context, gvr, "")
	if len(list) != 2 {
		t.Errorf("expected 2 items total, got %d", len(list))
	}

	// Delete pod-1
	s.Delete(context, gvr, "default", "pod-1")
	res = s.Get(context, gvr, "default", "pod-1")
	if res != nil {
		t.Error("expected pod-1 to be deleted")
	}
}

func TestStore_WatchingStatus(t *testing.T) {
	s := NewStore()
	gvr := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
	context := "test-context"

	if s.IsWatching(context, gvr) {
		t.Error("expected not watching initially")
	}

	s.SetWatching(context, gvr, true)
	if !s.IsWatching(context, gvr) {
		t.Error("expected watching after SetWatching(true)")
	}

	s.SetWatching(context, gvr, false)
	if s.IsWatching(context, gvr) {
		t.Error("expected not watching after SetWatching(false)")
	}
}
