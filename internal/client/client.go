package client

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/pslijkhuis/kfzf/internal/config"
	"github.com/pslijkhuis/kfzf/internal/server"
)

// Client communicates with the kfzf server
type Client struct {
	socketPath string
}

// NewClient creates a new client
func NewClient(cfg *config.Config) *Client {
	return &Client{
		socketPath: cfg.Server.SocketPath,
	}
}

// NewClientWithSocket creates a client with a specific socket path
func NewClientWithSocket(socketPath string) *Client {
	return &Client{
		socketPath: socketPath,
	}
}

// IsServerRunning checks if the server is running
func (c *Client) IsServerRunning() bool {
	conn, err := net.DialTimeout("unix", c.socketPath, time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// Complete requests completions from the server
func (c *Client) Complete(ctx, namespace, resourceType string) (string, error) {
	req := &server.Request{
		Type:         server.RequestTypeComplete,
		Context:      ctx,
		Namespace:    namespace,
		ResourceType: resourceType,
	}

	resp, err := c.sendRequest(req)
	if err != nil {
		return "", err
	}

	if !resp.Success {
		return "", fmt.Errorf("server error: %s", resp.Error)
	}

	return resp.Output, nil
}

// Status gets the server status
func (c *Client) Status() (*server.StatusInfo, error) {
	req := &server.Request{
		Type: server.RequestTypeStatus,
	}

	resp, err := c.sendRequest(req)
	if err != nil {
		return nil, err
	}

	if !resp.Success {
		return nil, fmt.Errorf("server error: %s", resp.Error)
	}

	return resp.Status, nil
}

// Refresh tells the server to refresh its kubeconfig
func (c *Client) Refresh() error {
	req := &server.Request{
		Type: server.RequestTypeRefresh,
	}

	resp, err := c.sendRequest(req)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("server error: %s", resp.Error)
	}

	return nil
}

// Watch tells the server to watch specific resource types
func (c *Client) Watch(ctx string, resourceTypes []string) error {
	req := &server.Request{
		Type:          server.RequestTypeWatch,
		Context:       ctx,
		ResourceTypes: resourceTypes,
	}

	resp, err := c.sendRequest(req)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("server error: %s", resp.Error)
	}

	return nil
}

// Containers returns container names for a pod from cache
func (c *Client) Containers(ctx, namespace, podName string) (string, error) {
	req := &server.Request{
		Type:      server.RequestTypeContainers,
		Context:   ctx,
		Namespace: namespace,
		PodName:   podName,
	}

	resp, err := c.sendRequest(req)
	if err != nil {
		return "", err
	}

	if !resp.Success {
		return "", fmt.Errorf("server error: %s", resp.Error)
	}

	return resp.Output, nil
}

// Ports returns container ports for a pod or service from cache
func (c *Client) Ports(ctx, namespace, resourceType, resourceName string) (string, error) {
	req := &server.Request{
		Type:         server.RequestTypePorts,
		Context:      ctx,
		Namespace:    namespace,
		ResourceType: resourceType,
		PodName:      resourceName,
	}

	resp, err := c.sendRequest(req)
	if err != nil {
		return "", err
	}

	if !resp.Success {
		return "", fmt.Errorf("server error: %s", resp.Error)
	}

	return resp.Output, nil
}

// Labels returns unique label key=value pairs for a resource type from cache
func (c *Client) Labels(ctx, namespace, resourceType string) (string, error) {
	req := &server.Request{
		Type:         server.RequestTypeLabels,
		Context:      ctx,
		Namespace:    namespace,
		ResourceType: resourceType,
	}

	resp, err := c.sendRequest(req)
	if err != nil {
		return "", err
	}

	if !resp.Success {
		return "", fmt.Errorf("server error: %s", resp.Error)
	}

	return resp.Output, nil
}

// FieldValues returns unique values for a field selector
func (c *Client) FieldValues(ctx, namespace, resourceType, fieldName string) (string, error) {
	req := &server.Request{
		Type:         server.RequestTypeFieldValues,
		Context:      ctx,
		Namespace:    namespace,
		ResourceType: resourceType,
		FieldName:    fieldName,
	}

	resp, err := c.sendRequest(req)
	if err != nil {
		return "", err
	}

	if !resp.Success {
		return "", fmt.Errorf("server error: %s", resp.Error)
	}

	return resp.Output, nil
}

// StopWatch tells the server to stop watching specific resource types
func (c *Client) StopWatch(ctx string, resourceTypes []string) error {
	req := &server.Request{
		Type:          server.RequestTypeStopWatch,
		Context:       ctx,
		ResourceTypes: resourceTypes,
	}

	resp, err := c.sendRequest(req)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("server error: %s", resp.Error)
	}

	return nil
}

// RecordRecent records a recently accessed resource
func (c *Client) RecordRecent(ctx, namespace, resourceType, resourceName string) error {
	req := &server.Request{
		Type:         server.RequestTypeRecordRecent,
		Context:      ctx,
		Namespace:    namespace,
		ResourceType: resourceType,
		ResourceName: resourceName,
	}

	resp, err := c.sendRequest(req)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("server error: %s", resp.Error)
	}

	return nil
}

// GetRecent returns recently accessed resources
func (c *Client) GetRecent(ctx, namespace, resourceType string) (string, error) {
	req := &server.Request{
		Type:         server.RequestTypeGetRecent,
		Context:      ctx,
		Namespace:    namespace,
		ResourceType: resourceType,
	}

	resp, err := c.sendRequest(req)
	if err != nil {
		return "", err
	}

	if !resp.Success {
		return "", fmt.Errorf("server error: %s", resp.Error)
	}

	return resp.Output, nil
}

// sendRequest sends a request to the server and returns the response
func (c *Client) sendRequest(req *server.Request) (*server.Response, error) {
	conn, err := net.DialTimeout("unix", c.socketPath, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to server: %w", err)
	}
	defer func() { _ = conn.Close() }()

	// Set write deadline
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))

	data, err := server.EncodeRequest(req)
	if err != nil {
		return nil, fmt.Errorf("failed to encode request: %w", err)
	}

	if _, err := conn.Write(data); err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	// Set read deadline
	_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))

	reader := bufio.NewReader(conn)
	respData, err := reader.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	resp, err := server.DecodeResponse(respData)
	if err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return resp, nil
}

// CompleteWithFzf gets completions and pipes them through fzf
func (c *Client) CompleteWithFzf(ctx, namespace, resourceType string, fzfOpts []string) (string, error) {
	output, err := c.Complete(ctx, namespace, resourceType)
	if err != nil {
		return "", err
	}

	if output == "" {
		return "", nil
	}

	// Run fzf
	args := []string{
		"--ansi",
		"--no-hscroll",
		"--delimiter=\t",
		"--nth=1",          // Search only the name column
		"--with-nth=1..",   // Display all columns
		"--select-1",       // Auto-select if only one match
		"--exit-0",         // Exit if no match
	}
	args = append(args, fzfOpts...)

	cmd := exec.Command("fzf", args...)
	cmd.Stdin = strings.NewReader(output)
	cmd.Stderr = os.Stderr

	result, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			// fzf returns exit code 1 when user presses ESC or Ctrl-C
			if exitErr.ExitCode() == 1 || exitErr.ExitCode() == 130 {
				return "", nil
			}
		}
		return "", fmt.Errorf("fzf failed: %w", err)
	}

	// Extract the name (first column) from the selected line
	line := string(result)
	if line == "" {
		return "", nil
	}

	// Split by tab and return first field (the name)
	for i, ch := range line {
		if ch == '\t' || ch == ' ' {
			return line[:i], nil
		}
	}

	// No tab found, return trimmed line
	return trimNewline(line), nil
}

func trimNewline(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}
