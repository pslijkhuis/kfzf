package server

import (
	"encoding/json"
)

// RequestType identifies the type of request
type RequestType string

const (
	RequestTypeComplete       RequestType = "complete"
	RequestTypeContainers     RequestType = "containers"
	RequestTypePorts          RequestType = "ports"
	RequestTypeLabels         RequestType = "labels"
	RequestTypeFieldValues    RequestType = "field_values"
	RequestTypeStatus         RequestType = "status"
	RequestTypeRefresh        RequestType = "refresh"
	RequestTypeWatch          RequestType = "watch"
	RequestTypeStopWatch      RequestType = "stop_watch"
	RequestTypeRecordRecent   RequestType = "record_recent"
	RequestTypeGetRecent      RequestType = "get_recent"
)

// Request represents a client request to the server
type Request struct {
	Type RequestType `json:"type"`

	// For complete requests
	Context      string `json:"context,omitempty"`
	Namespace    string `json:"namespace,omitempty"`
	ResourceType string `json:"resource_type,omitempty"`

	// For containers request
	PodName string `json:"pod_name,omitempty"`

	// For field_values request
	FieldName string `json:"field_name,omitempty"`

	// For watch requests
	ResourceTypes []string `json:"resource_types,omitempty"`

	// For record_recent request
	ResourceName string `json:"resource_name,omitempty"`
}

// Response represents a server response
type Response struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`

	// For complete responses
	Output string `json:"output,omitempty"`

	// For status responses
	Status *StatusInfo `json:"status,omitempty"`
}

// StatusInfo contains server status information
type StatusInfo struct {
	Uptime           string                       `json:"uptime"`
	ResourceCount    int                          `json:"resource_count"`
	WatchedResources map[string][]string          `json:"watched_resources"`
	ResourceStats    map[string]map[string]int    `json:"resource_stats"`
}

// EncodeRequest encodes a request to JSON with newline delimiter
func EncodeRequest(req *Request) ([]byte, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// DecodeRequest decodes a JSON request
func DecodeRequest(data []byte) (*Request, error) {
	var req Request
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, err
	}
	return &req, nil
}

// EncodeResponse encodes a response to JSON with newline delimiter
func EncodeResponse(resp *Response) ([]byte, error) {
	data, err := json.Marshal(resp)
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// DecodeResponse decodes a JSON response
func DecodeResponse(data []byte) (*Response, error) {
	var resp Response
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
