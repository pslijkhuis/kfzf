package k8s

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
)

// ClientManager manages Kubernetes clients for multiple contexts
type ClientManager struct {
	mu            sync.RWMutex
	kubeConfig    api.Config
	clients       map[string]*ContextClient
	clientAccess  map[string]int64 // last access time (unix timestamp) per context
	configLoader  clientcmd.ClientConfig
	loadingRules  *clientcmd.ClientConfigLoadingRules

	// Cached current context with file modification tracking
	cachedCurrentContext string
	configModTime        time.Time
	configPath           string
}

// ContextClient holds clients for a specific context
type ContextClient struct {
	Context         string
	DynamicClient   dynamic.Interface
	DiscoveryClient discovery.DiscoveryInterface
	RestConfig      *rest.Config
	Namespace       string // default namespace for this context
}

// NewClientManager creates a new client manager
func NewClientManager() (*ClientManager, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	configLoader := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)

	rawConfig, err := configLoader.RawConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load kubeconfig: %w", err)
	}

	// Get the kubeconfig path and its modification time
	configPath := KubeconfigPath()
	var modTime time.Time
	if stat, err := os.Stat(configPath); err == nil {
		modTime = stat.ModTime()
	}

	return &ClientManager{
		kubeConfig:           rawConfig,
		clients:              make(map[string]*ContextClient),
		clientAccess:         make(map[string]int64),
		configLoader:         configLoader,
		loadingRules:         loadingRules,
		cachedCurrentContext: rawConfig.CurrentContext,
		configModTime:        modTime,
		configPath:           configPath,
	}, nil
}

// GetClient returns a client for the specified context
func (m *ClientManager) GetClient(contextName string) (*ContextClient, error) {
	now := time.Now().Unix()

	m.mu.RLock()
	if client, ok := m.clients[contextName]; ok {
		m.mu.RUnlock()
		// Update access time (write lock needed)
		m.mu.Lock()
		m.clientAccess[contextName] = now
		m.mu.Unlock()
		return client, nil
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check after acquiring write lock
	if client, ok := m.clients[contextName]; ok {
		m.clientAccess[contextName] = now
		return client, nil
	}

	client, err := m.createClient(contextName)
	if err != nil {
		return nil, err
	}

	m.clients[contextName] = client
	m.clientAccess[contextName] = now
	return client, nil
}

// GetCurrentContext returns the name of the current context
// It re-reads from the kubeconfig file only if the file has been modified
func (m *ClientManager) GetCurrentContext() string {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if kubeconfig file has been modified
	stat, err := os.Stat(m.configPath)
	if err != nil {
		// Can't stat file, return cached value
		return m.cachedCurrentContext
	}

	// If file hasn't changed, return cached value
	if !stat.ModTime().After(m.configModTime) {
		return m.cachedCurrentContext
	}

	// File changed, reload config
	rawConfig, err := m.configLoader.RawConfig()
	if err != nil {
		return m.cachedCurrentContext
	}

	// Update cache
	m.configModTime = stat.ModTime()
	m.cachedCurrentContext = rawConfig.CurrentContext
	m.kubeConfig = rawConfig

	return m.cachedCurrentContext
}

// ListContexts returns all available context names
func (m *ClientManager) ListContexts() []string {
	contexts := make([]string, 0, len(m.kubeConfig.Contexts))
	for name := range m.kubeConfig.Contexts {
		contexts = append(contexts, name)
	}
	return contexts
}

// createClient creates a new client for the specified context
func (m *ClientManager) createClient(contextName string) (*ContextClient, error) {
	// Create config for specific context
	configOverrides := &clientcmd.ConfigOverrides{
		CurrentContext: contextName,
	}

	contextConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		m.loadingRules,
		configOverrides,
	)

	restConfig, err := contextConfig.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to create rest config for context %s: %w", contextName, err)
	}

	// Increase QPS and Burst for watch operations
	restConfig.QPS = 50
	restConfig.Burst = 100

	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic client for context %s: %w", contextName, err)
	}

	discoveryClient, err := discovery.NewDiscoveryClientForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create discovery client for context %s: %w", contextName, err)
	}

	namespace, _, err := contextConfig.Namespace()
	if err != nil {
		namespace = "default"
	}

	return &ContextClient{
		Context:         contextName,
		DynamicClient:   dynamicClient,
		DiscoveryClient: discoveryClient,
		RestConfig:      restConfig,
		Namespace:       namespace,
	}, nil
}

// RefreshConfig reloads the kubeconfig from disk
func (m *ClientManager) RefreshConfig() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	rawConfig, err := m.configLoader.RawConfig()
	if err != nil {
		return fmt.Errorf("failed to reload kubeconfig: %w", err)
	}

	m.kubeConfig = rawConfig
	// Clear cached clients as contexts may have changed
	m.clients = make(map[string]*ContextClient)
	m.clientAccess = make(map[string]int64)

	return nil
}

// CleanupUnusedClients removes clients that haven't been accessed within the given duration
func (m *ClientManager) CleanupUnusedClients(maxAge time.Duration) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	cutoff := time.Now().Add(-maxAge).Unix()
	removed := 0

	for contextName, lastAccess := range m.clientAccess {
		if lastAccess < cutoff {
			delete(m.clients, contextName)
			delete(m.clientAccess, contextName)
			removed++
		}
	}

	return removed
}

// ClientCount returns the number of cached clients
func (m *ClientManager) ClientCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.clients)
}

// GetContextNamespace returns the default namespace for a context
func (m *ClientManager) GetContextNamespace(contextName string) string {
	if ctx, ok := m.kubeConfig.Contexts[contextName]; ok {
		if ctx.Namespace != "" {
			return ctx.Namespace
		}
	}
	return "default"
}

// KubeconfigPath returns the path to the kubeconfig file
// It respects the KUBECONFIG environment variable
func KubeconfigPath() string {
	// Check KUBECONFIG environment variable first
	if kubeconfig := os.Getenv("KUBECONFIG"); kubeconfig != "" {
		// KUBECONFIG can contain multiple paths separated by :
		// Return the first one for modification time checking
		paths := filepath.SplitList(kubeconfig)
		if len(paths) > 0 && paths[0] != "" {
			return paths[0]
		}
	}
	// Fall back to default
	if kubeconfig := clientcmd.RecommendedHomeFile; kubeconfig != "" {
		return kubeconfig
	}
	return filepath.Join(clientcmd.RecommendedConfigDir, clientcmd.RecommendedFileName)
}
