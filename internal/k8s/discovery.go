package k8s

import (
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// ResourceInfo contains information about a Kubernetes resource type
type ResourceInfo struct {
	GVR        schema.GroupVersionResource
	Kind       string
	Namespaced bool
	ShortNames []string
	Verbs      []string
}

// DiscoverResources discovers all available API resources in a cluster
func DiscoverResources(client *ContextClient) ([]ResourceInfo, error) {
	_, apiResourceLists, err := client.DiscoveryClient.ServerGroupsAndResources()
	if err != nil {
		// Some resources may fail to discover, but we can still use the ones that work
		if !strings.Contains(err.Error(), "unable to retrieve") {
			return nil, fmt.Errorf("failed to discover API resources: %w", err)
		}
	}

	var resources []ResourceInfo

	for _, apiResourceList := range apiResourceLists {
		gv, err := schema.ParseGroupVersion(apiResourceList.GroupVersion)
		if err != nil {
			continue
		}

		for _, apiResource := range apiResourceList.APIResources {
			// Skip subresources (e.g., pods/log, pods/status)
			if strings.Contains(apiResource.Name, "/") {
				continue
			}

			// Check if we can list and watch this resource
			if !containsVerb(apiResource.Verbs, "list") || !containsVerb(apiResource.Verbs, "watch") {
				continue
			}

			resources = append(resources, ResourceInfo{
				GVR: schema.GroupVersionResource{
					Group:    gv.Group,
					Version:  gv.Version,
					Resource: apiResource.Name,
				},
				Kind:       apiResource.Kind,
				Namespaced: apiResource.Namespaced,
				ShortNames: apiResource.ShortNames,
				Verbs:      apiResource.Verbs,
			})
		}
	}

	return resources, nil
}

// FindResource finds a resource by name, short name, kind, or resource.group format
func FindResource(resources []ResourceInfo, name string) *ResourceInfo {
	name = strings.ToLower(name)

	// Check if name is in resource.group format (e.g., clusters.cluster.x-k8s.io)
	if strings.Contains(name, ".") {
		parts := strings.SplitN(name, ".", 2)
		resourceName := parts[0]
		groupName := parts[1]

		for i := range resources {
			if strings.ToLower(resources[i].GVR.Resource) == resourceName &&
				strings.ToLower(resources[i].GVR.Group) == groupName {
				return &resources[i]
			}
		}
		// If not found with exact group match, fall through to try other matches
	}

	// First, try exact match on resource name
	for i := range resources {
		if strings.ToLower(resources[i].GVR.Resource) == name {
			return &resources[i]
		}
	}

	// Try short names
	for i := range resources {
		for _, shortName := range resources[i].ShortNames {
			if strings.ToLower(shortName) == name {
				return &resources[i]
			}
		}
	}

	// Try kind (singular form)
	for i := range resources {
		if strings.ToLower(resources[i].Kind) == name {
			return &resources[i]
		}
	}

	return nil
}

// GetPreferredGVR returns the preferred GVR for common resource types
// This helps avoid ambiguity when multiple API groups provide the same resource
func GetPreferredGVR(resourceName string) *schema.GroupVersionResource {
	preferred := map[string]schema.GroupVersionResource{
		"pods":                   {Group: "", Version: "v1", Resource: "pods"},
		"po":                     {Group: "", Version: "v1", Resource: "pods"},
		"services":               {Group: "", Version: "v1", Resource: "services"},
		"svc":                    {Group: "", Version: "v1", Resource: "services"},
		"nodes":                  {Group: "", Version: "v1", Resource: "nodes"},
		"no":                     {Group: "", Version: "v1", Resource: "nodes"},
		"namespaces":             {Group: "", Version: "v1", Resource: "namespaces"},
		"ns":                     {Group: "", Version: "v1", Resource: "namespaces"},
		"configmaps":             {Group: "", Version: "v1", Resource: "configmaps"},
		"cm":                     {Group: "", Version: "v1", Resource: "configmaps"},
		"secrets":                {Group: "", Version: "v1", Resource: "secrets"},
		"persistentvolumes":      {Group: "", Version: "v1", Resource: "persistentvolumes"},
		"pv":                     {Group: "", Version: "v1", Resource: "persistentvolumes"},
		"persistentvolumeclaims": {Group: "", Version: "v1", Resource: "persistentvolumeclaims"},
		"pvc":                    {Group: "", Version: "v1", Resource: "persistentvolumeclaims"},
		"serviceaccounts":        {Group: "", Version: "v1", Resource: "serviceaccounts"},
		"sa":                     {Group: "", Version: "v1", Resource: "serviceaccounts"},
		"events":                 {Group: "", Version: "v1", Resource: "events"},
		"ev":                     {Group: "", Version: "v1", Resource: "events"},
		"endpoints":              {Group: "", Version: "v1", Resource: "endpoints"},
		"ep":                     {Group: "", Version: "v1", Resource: "endpoints"},
		"deployments":            {Group: "apps", Version: "v1", Resource: "deployments"},
		"deploy":                 {Group: "apps", Version: "v1", Resource: "deployments"},
		"replicasets":            {Group: "apps", Version: "v1", Resource: "replicasets"},
		"rs":                     {Group: "apps", Version: "v1", Resource: "replicasets"},
		"statefulsets":           {Group: "apps", Version: "v1", Resource: "statefulsets"},
		"sts":                    {Group: "apps", Version: "v1", Resource: "statefulsets"},
		"daemonsets":             {Group: "apps", Version: "v1", Resource: "daemonsets"},
		"ds":                     {Group: "apps", Version: "v1", Resource: "daemonsets"},
		"jobs":                   {Group: "batch", Version: "v1", Resource: "jobs"},
		"cronjobs":               {Group: "batch", Version: "v1", Resource: "cronjobs"},
		"cj":                     {Group: "batch", Version: "v1", Resource: "cronjobs"},
		"ingresses":              {Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"},
		"ing":                    {Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"},
		"networkpolicies":        {Group: "networking.k8s.io", Version: "v1", Resource: "networkpolicies"},
		"netpol":                 {Group: "networking.k8s.io", Version: "v1", Resource: "networkpolicies"},
	}

	name := strings.ToLower(resourceName)
	if gvr, ok := preferred[name]; ok {
		return &gvr
	}
	return nil
}

// ResourceAliases maps common names to their canonical resource names
var ResourceAliases = map[string]string{
	"po":          "pods",
	"pod":         "pods",
	"svc":         "services",
	"service":     "services",
	"no":          "nodes",
	"node":        "nodes",
	"ns":          "namespaces",
	"namespace":   "namespaces",
	"cm":          "configmaps",
	"configmap":   "configmaps",
	"secret":      "secrets",
	"pv":          "persistentvolumes",
	"pvc":         "persistentvolumeclaims",
	"sa":          "serviceaccounts",
	"ev":          "events",
	"event":       "events",
	"ep":          "endpoints",
	"deploy":      "deployments",
	"deployment":  "deployments",
	"rs":          "replicasets",
	"replicaset":  "replicasets",
	"sts":         "statefulsets",
	"statefulset": "statefulsets",
	"ds":          "daemonsets",
	"daemonset":   "daemonsets",
	"cj":          "cronjobs",
	"cronjob":     "cronjobs",
	"job":         "jobs",
	"ing":         "ingresses",
	"ingress":     "ingresses",
	"netpol":      "networkpolicies",
	// ArgoCD resources
	"app":            "applications.argoproj.io",
	"application":    "applications.argoproj.io",
	"applications":   "applications.argoproj.io",
	"appproj":        "appprojects.argoproj.io",
	"appproject":     "appprojects.argoproj.io",
	"appprojects":    "appprojects.argoproj.io",
	"appset":         "applicationsets.argoproj.io",
	"applicationset": "applicationsets.argoproj.io",
	"appsets":        "applicationsets.argoproj.io",
}

// NormalizeResourceName returns the canonical name for a resource
func NormalizeResourceName(name string) string {
	name = strings.ToLower(name)
	if canonical, ok := ResourceAliases[name]; ok {
		return canonical
	}
	return name
}

// containsVerb checks if a verb list contains a specific verb
func containsVerb(verbs metav1.Verbs, verb string) bool {
	for _, v := range verbs {
		if v == verb {
			return true
		}
	}
	return false
}
