package mcpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/opencode-ai/opencode/internal/config"
	"github.com/opencode-ai/opencode/internal/version"
)

type Client interface {
	Initialize(ctx context.Context, request mcp.InitializeRequest) (*mcp.InitializeResult, error)
	ListTools(ctx context.Context, request mcp.ListToolsRequest) (*mcp.ListToolsResult, error)
	CallTool(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error)
	Close() error
}

func New(server config.MCPServer) (Client, error) {
	switch server.Type {
	case config.MCPStdio, "":
		return client.NewStdioMCPClient(server.Command, server.Env, server.Args...)
	case config.MCPSse:
		return client.NewSSEMCPClient(server.URL, client.WithHeaders(server.Headers))
	case config.MCPStreamableHTTP:
		return NewStreamableHTTPClient(server.URL, server.Headers), nil
	default:
		return nil, fmt.Errorf("unsupported mcp transport: %s", server.Type)
	}
}

func DefaultInitializeRequest() mcp.InitializeRequest {
	req := mcp.InitializeRequest{}
	req.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	req.Params.ClientInfo = mcp.Implementation{
		Name:    "SciCLI",
		Version: version.Version,
	}
	return req
}

type streamableHTTPClient struct {
	url         string
	headers     map[string]string
	httpClient  *http.Client
	nextID      atomic.Int64
	initialized atomic.Bool
}

type rpcResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func NewStreamableHTTPClient(url string, headers map[string]string) Client {
	copyHeaders := map[string]string{}
	for key, value := range headers {
		copyHeaders[key] = value
	}
	return &streamableHTTPClient{
		url:        strings.TrimSpace(url),
		headers:    copyHeaders,
		httpClient: &http.Client{Timeout: 45 * time.Second},
	}
}

func (c *streamableHTTPClient) Initialize(ctx context.Context, request mcp.InitializeRequest) (*mcp.InitializeResult, error) {
	if c.initialized.Load() {
		result := &mcp.InitializeResult{}
		return result, nil
	}
	var result mcp.InitializeResult
	if err := c.call(ctx, "initialize", request.Params, &result); err != nil {
		return nil, err
	}
	_ = c.notify(ctx, "notifications/initialized", map[string]any{})
	c.initialized.Store(true)
	return &result, nil
}

func (c *streamableHTTPClient) ListTools(ctx context.Context, request mcp.ListToolsRequest) (*mcp.ListToolsResult, error) {
	if !c.initialized.Load() {
		if _, err := c.Initialize(ctx, DefaultInitializeRequest()); err != nil {
			return nil, err
		}
	}
	var result mcp.ListToolsResult
	if err := c.call(ctx, "tools/list", map[string]any{}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *streamableHTTPClient) CallTool(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if !c.initialized.Load() {
		if _, err := c.Initialize(ctx, DefaultInitializeRequest()); err != nil {
			return nil, err
		}
	}
	var result mcp.CallToolResult
	if err := c.call(ctx, "tools/call", request.Params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *streamableHTTPClient) Close() error {
	return nil
}

func (c *streamableHTTPClient) call(ctx context.Context, method string, params any, out any) error {
	id := c.nextID.Add(1)
	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to encode mcp request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to build mcp request: %w", err)
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "application/json, text/event-stream")
	for key, value := range c.headers {
		req.Header.Set(key, value)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("mcp request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("mcp request failed: %s", resp.Status)
	}

	var parsed rpcResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return fmt.Errorf("failed to decode mcp response: %w", err)
	}
	if parsed.Error != nil {
		return fmt.Errorf("%s", parsed.Error.Message)
	}
	if out == nil || len(parsed.Result) == 0 {
		return nil
	}
	if err := json.Unmarshal(parsed.Result, out); err != nil {
		return fmt.Errorf("failed to decode mcp result: %w", err)
	}
	return nil
}

func (c *streamableHTTPClient) notify(ctx context.Context, method string, params any) error {
	payload := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "application/json, text/event-stream")
	for key, value := range c.headers {
		req.Header.Set(key, value)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("mcp notify failed: %s", resp.Status)
	}
	return nil
}
