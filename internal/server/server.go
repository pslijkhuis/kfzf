package server

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/pslijkhuis/kfzf/internal/config"
	"github.com/pslijkhuis/kfzf/internal/fzf"
	"github.com/pslijkhuis/kfzf/internal/k8s"
	"github.com/pslijkhuis/kfzf/internal/store"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const maxConcurrentConnections = 50 // Maximum concurrent connection handlers

// Server is the kfzf daemon that handles completion requests
type Server struct {
	config        *config.Config
	clientManager *k8s.ClientManager
	watchManager  *k8s.WatchManager
	store         *store.Store
	formatter     *fzf.Formatter
	logger        *slog.Logger

	listener  net.Listener
	startTime time.Time

	shutdown bool

	// Semaphore for limiting concurrent connections
	connSemaphore chan struct{}

	// Cache discovered resources per context with access times
	resourceCache       map[string][]k8s.ResourceInfo
	resourceCacheAccess map[string]time.Time
	resourceCacheMu     sync.RWMutex

	// Track which contexts have been initialized with default watches
	initializedContexts       map[string]bool
	initializedContextsAccess map[string]time.Time // Track last access time per context
	initializedContextsMu     sync.RWMutex

	// Track recently accessed resources for suggestions
	recentResources *RecentResources
}

// NewServer creates a new server instance
func NewServer(cfg *config.Config, logger *slog.Logger) (*Server, error) {
	clientManager, err := k8s.NewClientManager()
	if err != nil {
		return nil, fmt.Errorf("failed to create client manager: %w", err)
	}

	resourceStore := store.NewStore()
	watchManager := k8s.NewWatchManager(clientManager, resourceStore, logger)
	formatter := fzf.NewFormatter(cfg)

	return &Server{
		config:                    cfg,
		clientManager:             clientManager,
		watchManager:              watchManager,
		store:                     resourceStore,
		formatter:                 formatter,
		logger:                    logger,
		connSemaphore:             make(chan struct{}, maxConcurrentConnections),
		resourceCache:             make(map[string][]k8s.ResourceInfo),
		resourceCacheAccess:       make(map[string]time.Time),
		initializedContexts:       make(map[string]bool),
		initializedContextsAccess: make(map[string]time.Time),
		recentResources:           NewRecentResources(20), // Track last 20 resources per type
	}, nil
}

// Start starts the server and listens for connections
func (s *Server) Start(ctx context.Context) error {
	socketPath := s.config.Server.SocketPath

	// Ensure socket directory exists
	socketDir := filepath.Dir(socketPath)
	if err := os.MkdirAll(socketDir, 0755); err != nil {
		return fmt.Errorf("failed to create socket directory: %w", err)
	}

	// Remove existing socket file
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove existing socket: %w", err)
	}

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("failed to create unix socket: %w", err)
	}

	s.listener = listener
	s.startTime = time.Now()

	// Set socket permissions
	if err := os.Chmod(socketPath, 0600); err != nil {
		s.logger.Warn("failed to set socket permissions", "error", err)
	}

	s.logger.Info("server started", "socket", socketPath)

	// Start watching common resources for current context
	go s.startDefaultWatches(ctx)

	// Accept connections
	go s.acceptConnections(ctx)

	// Start periodic cleanup of unused resources
	go s.periodicCleanup(ctx)

	// Wait for context cancellation
	<-ctx.Done()

	s.logger.Info("shutting down server")
	s.shutdown = true
	s.watchManager.StopAll()
	_ = s.listener.Close()
	_ = os.Remove(socketPath)

	return nil
}

// startDefaultWatches starts watches for commonly used resources for current context
func (s *Server) startDefaultWatches(ctx context.Context) {
	currentContext := s.clientManager.GetCurrentContext()
	if currentContext == "" {
		s.logger.Warn("no current context set")
		return
	}
	s.initializeContextWatches(ctx, currentContext)
}

// initializeContextWatches starts default watches for a specific context if not already initialized
func (s *Server) initializeContextWatches(ctx context.Context, contextName string) {
	s.initializedContextsMu.Lock()
	// Always update access time
	s.initializedContextsAccess[contextName] = time.Now()

	// Check if already initialized
	if s.initializedContexts[contextName] {
		s.initializedContextsMu.Unlock()
		return
	}
	s.initializedContexts[contextName] = true
	s.initializedContextsMu.Unlock()

	s.logger.Info("initializing watches for new context", "context", contextName)

	// Common resources to watch by default
	defaultResources := []struct {
		gvr        schema.GroupVersionResource
		namespaced bool
	}{
		{schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}, true},
		{schema.GroupVersionResource{Group: "", Version: "v1", Resource: "services"}, true},
		{schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}, true},
		{schema.GroupVersionResource{Group: "", Version: "v1", Resource: "secrets"}, true},
		{schema.GroupVersionResource{Group: "", Version: "v1", Resource: "namespaces"}, false},
		{schema.GroupVersionResource{Group: "", Version: "v1", Resource: "nodes"}, false},
		{schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}, true},
		{schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"}, true},
		{schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "daemonsets"}, true},
	}

	for _, res := range defaultResources {
		if err := s.watchManager.StartWatching(ctx, contextName, res.gvr, res.namespaced); err != nil {
			s.logger.Warn("failed to start watch",
				"context", contextName,
				"resource", res.gvr.Resource,
				"error", err,
			)
		}
	}
}

// acceptConnections accepts incoming connections
func (s *Server) acceptConnections(ctx context.Context) {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if s.shutdown {
				return
			}
			s.logger.Error("failed to accept connection", "error", err)
			continue
		}

		// Acquire semaphore slot (blocks if at max connections)
		select {
		case s.connSemaphore <- struct{}{}:
			go func() {
				defer func() { <-s.connSemaphore }() // Release slot when done
				s.handleConnection(ctx, conn)
			}()
		case <-ctx.Done():
			_ = conn.Close()
			return
		}
	}
}

// handleConnection handles a single client connection
func (s *Server) handleConnection(ctx context.Context, conn net.Conn) {
	defer func() { _ = conn.Close() }()

	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("panic in handleConnection", "error", r)
			s.sendError(conn, "internal server error")
		}
	}()

	// Set read deadline
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	reader := bufio.NewReader(conn)
	data, err := reader.ReadBytes('\n')
	if err != nil {
		// EOF is expected when client disconnects without sending data (e.g., health checks)
		if err.Error() != "EOF" {
			s.logger.Error("failed to read request", "error", err)
		}
		return
	}

	req, err := DecodeRequest(data)
	if err != nil {
		s.logger.Error("failed to decode request", "error", err)
		s.sendError(conn, "invalid request format")
		return
	}

	var resp *Response

	switch req.Type {
	case RequestTypeComplete:
		resp = s.handleComplete(ctx, req)
	case RequestTypeContainers:
		resp = s.handleContainers(req)
	case RequestTypePorts:
		resp = s.handlePorts(req)
	case RequestTypeLabels:
		resp = s.handleLabels(ctx, req)
	case RequestTypeFieldValues:
		resp = s.handleFieldValues(ctx, req)
	case RequestTypeStatus:
		resp = s.handleStatus()
	case RequestTypeRefresh:
		resp = s.handleRefresh()
	case RequestTypeWatch:
		resp = s.handleWatch(ctx, req)
	case RequestTypeStopWatch:
		resp = s.handleStopWatch(req)
	case RequestTypeRecordRecent:
		resp = s.handleRecordRecent(req)
	case RequestTypeGetRecent:
		resp = s.handleGetRecent(req)
	default:
		resp = &Response{Success: false, Error: "unknown request type"}
	}

	respData, err := EncodeResponse(resp)
	if err != nil {
		s.logger.Error("failed to encode response", "error", err)
		return
	}

	_, _ = conn.Write(respData)
}

// handleComplete handles a completion request
func (s *Server) handleComplete(ctx context.Context, req *Request) *Response {
	contextName := req.Context
	if contextName == "" {
		contextName = s.clientManager.GetCurrentContext()
	}

	// Initialize default watches for this context if it's a new context
	// This runs in the background to not block the current request
	go s.initializeContextWatches(ctx, contextName)

	// Use the explicitly provided namespace, or empty string to get all namespaces
	namespace := req.Namespace

	resourceType := k8s.NormalizeResourceName(req.ResourceType)

	// Get or discover the GVR for this resource type
	gvr, namespaced, err := s.resolveGVR(contextName, resourceType)
	if err != nil {
		return &Response{Success: false, Error: err.Error()}
	}

	// Ensure we're watching this resource
	if !s.watchManager.IsWatching(contextName, *gvr) {
		if err := s.watchManager.StartWatching(ctx, contextName, *gvr, namespaced); err != nil {
			return &Response{Success: false, Error: fmt.Sprintf("failed to start watch: %v", err)}
		}
	}
	
	// Wait for data to be populated
	s.waitForSync(contextName, *gvr, 1*time.Second)

	// Get resources from store
	// If namespace is empty and resource is namespaced, return ALL namespaced resources
	var resources []*store.Resource
	if namespaced {
		resources = s.store.ListNamespaced(contextName, *gvr, namespace)
	} else {
		resources = s.store.ListClusterScoped(contextName, *gvr)
	}

	// Sort by name using slices.SortFunc (faster than sort.Slice)
	slices.SortFunc(resources, func(a, b *store.Resource) int {
		return strings.Compare(a.Name, b.Name)
	})

	output := s.formatter.Format(resources, resourceType)

	return &Response{
		Success: true,
		Output:  output,
	}
}

// handleContainers returns container names for a pod from cache
func (s *Server) handleContainers(req *Request) *Response {
	contextName := req.Context
	if contextName == "" {
		contextName = s.clientManager.GetCurrentContext()
	}

	namespace := req.Namespace
	podName := req.PodName

	if podName == "" {
		return &Response{Success: false, Error: "pod_name is required"}
	}

	// Get the pod from the store
	podsGVR := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
	pod := s.store.Get(contextName, podsGVR, namespace, podName)

	if pod == nil || pod.Object == nil {
		return &Response{Success: false, Error: "pod not found in cache"}
	}

	type containerInfo struct {
		name   string
		isInit bool
	}
	var containers []containerInfo

	// Extract container names from spec.containers
	if spec, ok := pod.Object.Object["spec"].(map[string]interface{}); ok {
		if containerList, ok := spec["containers"].([]interface{}); ok {
			for _, c := range containerList {
				if container, ok := c.(map[string]interface{}); ok {
					if name, ok := container["name"].(string); ok {
						containers = append(containers, containerInfo{name: name, isInit: false})
					}
				}
			}
		}
		// Also get init containers
		if initContainerList, ok := spec["initContainers"].([]interface{}); ok {
			for _, c := range initContainerList {
				if container, ok := c.(map[string]interface{}); ok {
					if name, ok := container["name"].(string); ok {
						containers = append(containers, containerInfo{name: name, isInit: true})
					}
				}
			}
		}
	}

	if len(containers) == 0 {
		return &Response{Success: false, Error: "no containers found"}
	}

	// Format: name<tab>type (init containers shown with "init" indicator)
	var buf strings.Builder
	buf.Grow(len(containers) * 32) // Pre-allocate reasonable capacity
	for _, c := range containers {
		buf.WriteString(c.name)
		if c.isInit {
			buf.WriteString("\t\033[33m(init)\033[0m\n")
		} else {
			buf.WriteByte('\n')
		}
	}

	return &Response{
		Success: true,
		Output:  buf.String(),
	}
}

// handlePorts returns container ports for a pod or service from cache
func (s *Server) handlePorts(req *Request) *Response {
	contextName := req.Context
	if contextName == "" {
		contextName = s.clientManager.GetCurrentContext()
	}

	namespace := req.Namespace
	podName := req.PodName
	resourceType := req.ResourceType // Can be "pods" or "services"

	if podName == "" {
		return &Response{Success: false, Error: "pod_name is required"}
	}

	// If resource type is services, get service ports
	if resourceType == "services" || resourceType == "service" || resourceType == "svc" {
		return s.handleServicePorts(contextName, namespace, podName)
	}

	// Get the pod from the store
	podsGVR := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
	pod := s.store.Get(contextName, podsGVR, namespace, podName)

	if pod == nil || pod.Object == nil {
		return &Response{Success: false, Error: "pod not found in cache"}
	}

	type portInfo struct {
		containerPort int64
		protocol      string
		containerName string
		portName      string
	}
	var ports []portInfo

	// Extract container ports from spec.containers
	if spec, ok := pod.Object.Object["spec"].(map[string]interface{}); ok {
		if containerList, ok := spec["containers"].([]interface{}); ok {
			for _, c := range containerList {
				if container, ok := c.(map[string]interface{}); ok {
					containerName := ""
					if name, ok := container["name"].(string); ok {
						containerName = name
					}
					if portsList, ok := container["ports"].([]interface{}); ok {
						for _, p := range portsList {
							if port, ok := p.(map[string]interface{}); ok {
								pi := portInfo{containerName: containerName}
								pi.containerPort = extractInt64(port["containerPort"])
								if proto, ok := port["protocol"].(string); ok {
									pi.protocol = proto
								} else {
									pi.protocol = "TCP"
								}
								if pn, ok := port["name"].(string); ok {
									pi.portName = pn
								}
								if pi.containerPort > 0 {
									ports = append(ports, pi)
								}
							}
						}
					}
				}
			}
		}
	}

	if len(ports) == 0 {
		return &Response{Success: false, Error: "no ports found"}
	}

	// Format: port<tab>protocol<tab>container<tab>name
	var buf strings.Builder
	buf.Grow(len(ports) * 48) // Pre-allocate reasonable capacity
	for _, p := range ports {
		portName := p.portName
		if portName == "" {
			portName = "-"
		}
		fmt.Fprintf(&buf, "%d\t%s\t%s\t%s\n", p.containerPort, p.protocol, p.containerName, portName)
	}

	return &Response{
		Success: true,
		Output:  buf.String(),
	}
}

// handleServicePorts returns service ports from cache
func (s *Server) handleServicePorts(contextName, namespace, serviceName string) *Response {
	svcGVR := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "services"}
	svc := s.store.Get(contextName, svcGVR, namespace, serviceName)

	if svc == nil || svc.Object == nil {
		return &Response{Success: false, Error: "service not found in cache"}
	}

	type portInfo struct {
		port       int64
		targetPort interface{}
		protocol   string
		name       string
	}
	var ports []portInfo

	// Extract service ports from spec.ports
	if spec, ok := svc.Object.Object["spec"].(map[string]interface{}); ok {
		if portsList, ok := spec["ports"].([]interface{}); ok {
			for _, p := range portsList {
				if port, ok := p.(map[string]interface{}); ok {
					pi := portInfo{}
					pi.port = extractInt64(port["port"])
					pi.targetPort = port["targetPort"]
					if proto, ok := port["protocol"].(string); ok {
						pi.protocol = proto
					} else {
						pi.protocol = "TCP"
					}
					if pn, ok := port["name"].(string); ok {
						pi.name = pn
					}
					if pi.port > 0 {
						ports = append(ports, pi)
					}
				}
			}
		}
	}

	if len(ports) == 0 {
		return &Response{Success: false, Error: "no ports found"}
	}

	// Format: port<tab>targetPort<tab>protocol<tab>name
	var buf strings.Builder
	buf.Grow(len(ports) * 48)
	for _, p := range ports {
		portName := p.name
		if portName == "" {
			portName = "-"
		}
		targetPort := fmt.Sprintf("%v", p.targetPort)
		if targetPort == "" || targetPort == "<nil>" {
			targetPort = "-"
		}
		fmt.Fprintf(&buf, "%d\t%s\t%s\t%s\n", p.port, targetPort, p.protocol, portName)
	}

	return &Response{
		Success: true,
		Output:  buf.String(),
	}
}

// handleLabels returns unique label keys and values for a resource type from cache
func (s *Server) handleLabels(ctx context.Context, req *Request) *Response {
	contextName := req.Context
	if contextName == "" {
		contextName = s.clientManager.GetCurrentContext()
	}

	// Initialize default watches for this context if it's a new context
	go s.initializeContextWatches(ctx, contextName)

	namespace := req.Namespace
	resourceType := k8s.NormalizeResourceName(req.ResourceType)

	// Get or discover the GVR for this resource type
	gvr, namespaced, err := s.resolveGVR(contextName, resourceType)
	if err != nil {
		return &Response{Success: false, Error: err.Error()}
	}

	// Ensure we're watching this resource
	if !s.watchManager.IsWatching(contextName, *gvr) {
		if err := s.watchManager.StartWatching(ctx, contextName, *gvr, namespaced); err != nil {
			return &Response{Success: false, Error: fmt.Sprintf("failed to start watch: %v", err)}
		}
	}
	
	// Wait for data to be populated
	s.waitForSync(contextName, *gvr, 1*time.Second)

	// Get resources from store
	var resources []*store.Resource
	if namespaced {
		resources = s.store.ListNamespaced(contextName, *gvr, namespace)
	} else {
		resources = s.store.ListClusterScoped(contextName, *gvr)
	}

	// Collect unique label key=value pairs
	// Use map of maps to avoid repeated string concatenations
	labelsMap := make(map[string]map[string]struct{})
	count := 0

	for _, res := range resources {
		if res.Object == nil {
			continue
		}
		if metadata, ok := res.Object.Object["metadata"].(map[string]interface{}); ok {
			if labels, ok := metadata["labels"].(map[string]interface{}); ok {
				for key, value := range labels {
					if strVal, ok := value.(string); ok {
						if _, exists := labelsMap[key]; !exists {
							labelsMap[key] = make(map[string]struct{})
						}
						if _, exists := labelsMap[key][strVal]; !exists {
							labelsMap[key][strVal] = struct{}{}
							count++
						}
					}
				}
			}
		}
	}

	// Convert to sorted list
	labelList := make([]string, 0, count)
	for key, values := range labelsMap {
		for val := range values {
			labelList = append(labelList, key+"="+val)
		}
	}
	slices.Sort(labelList)

	var buf strings.Builder
	buf.Grow(len(labelList) * 32)
	for _, label := range labelList {
		buf.WriteString(label)
		buf.WriteByte('\n')
	}

	return &Response{
		Success: true,
		Output:  buf.String(),
	}
}

// handleFieldValues returns unique values for a field (for field selector completion)
func (s *Server) handleFieldValues(ctx context.Context, req *Request) *Response {
	contextName := req.Context
	if contextName == "" {
		contextName = s.clientManager.GetCurrentContext()
	}

	go s.initializeContextWatches(ctx, contextName)

	namespace := req.Namespace
	resourceType := k8s.NormalizeResourceName(req.ResourceType)
	fieldName := req.FieldName

	if fieldName == "" {
		return &Response{Success: false, Error: "field_name is required"}
	}

	gvr, namespaced, err := s.resolveGVR(contextName, resourceType)
	if err != nil {
		return &Response{Success: false, Error: err.Error()}
	}

	if !s.watchManager.IsWatching(contextName, *gvr) {
		if err := s.watchManager.StartWatching(ctx, contextName, *gvr, namespaced); err != nil {
			return &Response{Success: false, Error: fmt.Sprintf("failed to start watch: %v", err)}
		}
	}
	
	// Wait for data to be populated
	s.waitForSync(contextName, *gvr, 1*time.Second)

	var resources []*store.Resource
	if namespaced {
		resources = s.store.ListNamespaced(contextName, *gvr, namespace)
	} else {
		resources = s.store.ListClusterScoped(contextName, *gvr)
	}

	// Map of supported field selectors and their paths
	fieldPaths := map[string]string{
		"metadata.name":             ".metadata.name",
		"metadata.namespace":        ".metadata.namespace",
		"spec.nodeName":             ".spec.nodeName",
		"spec.restartPolicy":        ".spec.restartPolicy",
		"spec.schedulerName":        ".spec.schedulerName",
		"spec.serviceAccountName":   ".spec.serviceAccountName",
		"status.phase":              ".status.phase",
		"status.podIP":              ".status.podIP",
		"status.nominatedNodeName":  ".status.nominatedNodeName",
	}

	fieldPath, ok := fieldPaths[fieldName]
	if !ok {
		return &Response{Success: false, Error: fmt.Sprintf("unsupported field selector: %s", fieldName)}
	}

	// Collect unique values
	valueSet := make(map[string]bool)
	for _, res := range resources {
		if res.Object == nil {
			continue
		}
		val := getNestedString(res.Object.Object, fieldPath)
		if val != "" {
			valueSet[val] = true
		}
	}

	values := make([]string, 0, len(valueSet))
	for v := range valueSet {
		values = append(values, v)
	}
	slices.Sort(values)

	var buf strings.Builder
	buf.Grow(len(values) * (len(fieldName) + 16))
	for _, v := range values {
		buf.WriteString(fieldName)
		buf.WriteByte('=')
		buf.WriteString(v)
		buf.WriteByte('\n')
	}

	return &Response{
		Success: true,
		Output:  buf.String(),
	}
}

// extractInt64 extracts an int64 from interface{} (handles both int64 and float64)
func extractInt64(val interface{}) int64 {
	if i, ok := val.(int64); ok {
		return i
	}
	if f, ok := val.(float64); ok {
		return int64(f)
	}
	return 0
}

// getNestedString extracts a string value from nested map using dot notation
// Optimized to avoid strings.Split allocations
func getNestedString(obj map[string]interface{}, path string) string {
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
				continue // Empty key
			}
			
			key := path[start:i]
			start = i + 1

			// Traverse
			m, ok := current.(map[string]interface{})
			if !ok {
				return ""
			}
			
			val, exists := m[key]
			if !exists {
				return ""
			}
			current = val
		}
	}

	if s, ok := current.(string); ok {
		return s
	}
	return ""
}

// handleStatus handles a status request
func (s *Server) handleStatus() *Response {
	watched := s.watchManager.WatchedResources()
	watchedStrings := make(map[string][]string)
	for ctx, gvrs := range watched {
		for _, gvr := range gvrs {
			watchedStrings[ctx] = append(watchedStrings[ctx], gvr.Resource)
		}
	}

	return &Response{
		Success: true,
		Status: &StatusInfo{
			Uptime:           time.Since(s.startTime).Round(time.Second).String(),
			ResourceCount:    s.store.Count(),
			WatchedResources: watchedStrings,
			ResourceStats:    s.store.Stats(),
		},
	}
}

// handleRefresh handles a refresh request
func (s *Server) handleRefresh() *Response {
	if err := s.clientManager.RefreshConfig(); err != nil {
		return &Response{Success: false, Error: err.Error()}
	}

	// Stop all watches and clear all cached data
	s.watchManager.StopAll()

	// Clear resource cache
	s.resourceCacheMu.Lock()
	s.resourceCache = make(map[string][]k8s.ResourceInfo)
	s.resourceCacheAccess = make(map[string]time.Time)
	s.resourceCacheMu.Unlock()

	// Clear initialized contexts tracking
	s.initializedContextsMu.Lock()
	s.initializedContexts = make(map[string]bool)
	s.initializedContextsAccess = make(map[string]time.Time)
	s.initializedContextsMu.Unlock()

	// Clear recent resources
	s.recentResources.Clear()

	return &Response{Success: true}
}

// handleWatch handles a watch request
func (s *Server) handleWatch(ctx context.Context, req *Request) *Response {
	contextName := req.Context
	if contextName == "" {
		contextName = s.clientManager.GetCurrentContext()
	}

	for _, resourceType := range req.ResourceTypes {
		resourceType = k8s.NormalizeResourceName(resourceType)
		gvr, namespaced, err := s.resolveGVR(contextName, resourceType)
		if err != nil {
			s.logger.Warn("failed to resolve resource type",
				"type", resourceType,
				"error", err,
			)
			continue
		}

		if err := s.watchManager.StartWatching(ctx, contextName, *gvr, namespaced); err != nil {
			s.logger.Warn("failed to start watch",
				"resource", resourceType,
				"error", err,
			)
		}
	}

	return &Response{Success: true}
}

// handleStopWatch handles a stop watch request
func (s *Server) handleStopWatch(req *Request) *Response {
	contextName := req.Context
	if contextName == "" {
		contextName = s.clientManager.GetCurrentContext()
	}

	for _, resourceType := range req.ResourceTypes {
		resourceType = k8s.NormalizeResourceName(resourceType)
		gvr, _, err := s.resolveGVR(contextName, resourceType)
		if err != nil {
			continue
		}
		s.watchManager.StopWatching(contextName, *gvr)
	}

	return &Response{Success: true}
}

// handleRecordRecent records a recently accessed resource
func (s *Server) handleRecordRecent(req *Request) *Response {
	contextName := req.Context
	if contextName == "" {
		contextName = s.clientManager.GetCurrentContext()
	}

	namespace := req.Namespace
	resourceType := k8s.NormalizeResourceName(req.ResourceType)
	resourceName := req.ResourceName

	if resourceName == "" {
		return &Response{Success: false, Error: "resource_name is required"}
	}

	s.recentResources.Add(contextName, namespace, resourceType, resourceName)
	return &Response{Success: true}
}

// handleGetRecent returns recently accessed resources
func (s *Server) handleGetRecent(req *Request) *Response {
	contextName := req.Context
	if contextName == "" {
		contextName = s.clientManager.GetCurrentContext()
	}

	namespace := req.Namespace
	resourceType := k8s.NormalizeResourceName(req.ResourceType)

	recent := s.recentResources.Get(contextName, namespace, resourceType)
	if len(recent) == 0 {
		return &Response{Success: true, Output: ""}
	}

	output := strings.Join(recent, "\n")
	return &Response{Success: true, Output: output}
}

// periodicCleanup runs periodic maintenance tasks
func (s *Server) periodicCleanup(ctx context.Context) {
	// Run cleanup every 5 minutes
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Cleanup clients not used in the last 30 minutes
			if removed := s.clientManager.CleanupUnusedClients(30 * time.Minute); removed > 0 {
				s.logger.Info("cleaned up unused clients", "count", removed)
			}

			// Cleanup resource cache entries older than 1 hour
			s.cleanupResourceCache(1 * time.Hour)

			// Cleanup contexts not accessed in the last hour
			s.cleanupOldContexts(1 * time.Hour)
		}
	}
}

// resolveGVR resolves a resource type name to a GVR
func (s *Server) resolveGVR(contextName, resourceType string) (*schema.GroupVersionResource, bool, error) {
	// Try preferred GVR first
	if gvr := k8s.GetPreferredGVR(resourceType); gvr != nil {
		// Determine if namespaced from known resources
		namespaced := isKnownNamespaced(resourceType)
		return gvr, namespaced, nil
	}

	// Fall back to discovery
	resources, err := s.getResourceInfo(contextName)
	if err != nil {
		return nil, false, fmt.Errorf("failed to discover resources: %w", err)
	}

	resInfo := k8s.FindResource(resources, resourceType)
	if resInfo == nil {
		return nil, false, fmt.Errorf("unknown resource type: %s", resourceType)
	}

	return &resInfo.GVR, resInfo.Namespaced, nil
}

// getResourceInfo gets cached or discovers resource info for a context
func (s *Server) getResourceInfo(contextName string) ([]k8s.ResourceInfo, error) {
	// Fast path: check cache with read lock
	s.resourceCacheMu.RLock()
	cached, ok := s.resourceCache[contextName]
	s.resourceCacheMu.RUnlock()

	if ok {
		// Update access time - needs lock
		s.resourceCacheMu.Lock()
		s.resourceCacheAccess[contextName] = time.Now()
		s.resourceCacheMu.Unlock()
		return cached, nil
	}

	// Slow path: discovery
	// Note: We don't lock here to allow concurrent discovery and not block other requests.
	client, err := s.clientManager.GetClient(contextName)
	if err != nil {
		return nil, err
	}

	resources, err := k8s.DiscoverResources(client)
	if err != nil {
		return nil, err
	}

	// Save to cache
	s.resourceCacheMu.Lock()
	s.resourceCache[contextName] = resources
	s.resourceCacheAccess[contextName] = time.Now()
	s.resourceCacheMu.Unlock()

	return resources, nil
}

// waitForSync waits for a resource to be synced (watched) in the store
func (s *Server) waitForSync(contextName string, gvr schema.GroupVersionResource, timeout time.Duration) {
	// Fast check first
	if s.store.IsWatching(contextName, gvr) {
		return
	}

	// Poll until synced or timeout
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	timeoutCh := time.After(timeout)

	for {
		select {
		case <-ticker.C:
			if s.store.IsWatching(contextName, gvr) {
				return
			}
		case <-timeoutCh:
			return
		}
	}
}

// cleanupResourceCache removes resource cache entries older than maxAge
func (s *Server) cleanupResourceCache(maxAge time.Duration) {
	s.resourceCacheMu.Lock()
	defer s.resourceCacheMu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	for contextName, lastAccess := range s.resourceCacheAccess {
		if lastAccess.Before(cutoff) {
			delete(s.resourceCache, contextName)
			delete(s.resourceCacheAccess, contextName)
			s.logger.Debug("cleaned up resource cache", "context", contextName)
		}
	}
}

// cleanupOldContexts removes context data for contexts not accessed within maxAge
func (s *Server) cleanupOldContexts(maxAge time.Duration) {
	s.initializedContextsMu.Lock()
	defer s.initializedContextsMu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	currentContext := s.clientManager.GetCurrentContext()

	for contextName, lastAccess := range s.initializedContextsAccess {
		// Never clean up the current context
		if contextName == currentContext {
			continue
		}

		if lastAccess.Before(cutoff) {
			// Stop all watches and clear store data for this context
			s.watchManager.StopContext(contextName)

			// Clean up initialized contexts tracking
			delete(s.initializedContexts, contextName)
			delete(s.initializedContextsAccess, contextName)

			s.logger.Info("cleaned up old context", "context", contextName)
		}
	}
}

// sendError sends an error response
func (s *Server) sendError(conn net.Conn, msg string) {
	resp := &Response{Success: false, Error: msg}
	data, _ := EncodeResponse(resp)
	_, _ = conn.Write(data)
}

// isKnownNamespaced returns whether a resource type is namespaced
func isKnownNamespaced(resourceType string) bool {
	clusterScoped := map[string]bool{
		"nodes":                      true,
		"namespaces":                 true,
		"persistentvolumes":          true,
		"clusterroles":               true,
		"clusterrolebindings":        true,
		"customresourcedefinitions":  true,
		"storageclasses":             true,
		"priorityclasses":            true,
		"csidrivers":                 true,
		"csinodes":                   true,
		"volumeattachments":          true,
		"ingressclasses":             true,
		"runtimeclasses":             true,
	}

	return !clusterScoped[k8s.NormalizeResourceName(resourceType)]
}
