package mcpclient

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SciMate-AI/scicli/internal/config"
	"github.com/SciMate-AI/scicli/internal/version"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

const (
	remoteProtocolVersion = "2025-06-18"
	defaultRequestTimeout = 120 * time.Second
	defaultSSEConnectWait = 90 * time.Second
)

type Client interface {
	Initialize(ctx context.Context, request mcp.InitializeRequest) (*mcp.InitializeResult, error)
	ListTools(ctx context.Context, request mcp.ListToolsRequest) (*mcp.ListToolsResult, error)
	CallTool(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error)
	Close() error
}

type startableClient interface {
	Start(ctx context.Context) error
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error,omitempty"`
}

type rpcMessage struct {
	ID     *json.RawMessage `json:"id,omitempty"`
	Method string           `json:"method,omitempty"`
	Result json.RawMessage  `json:"result,omitempty"`
	Error  *rpcError        `json:"error,omitempty"`
}

func New(server config.MCPServer) (Client, error) {
	switch server.Type {
	case config.MCPStdio, "":
		return client.NewStdioMCPClient(server.Command, server.Env, server.Args...)
	case config.MCPSse:
		return client.NewSSEMCPClient(
			server.URL,
			client.WithHeaders(server.Headers),
			client.WithSSEReadTimeout(defaultRequestTimeout),
		)
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

func EnsureStarted(ctx context.Context, c Client) error {
	if starter, ok := c.(startableClient); ok {
		return starter.Start(ctx)
	}
	return nil
}

type sseMCPClient struct {
	baseURL      string
	headers      map[string]string
	httpClient   *http.Client
	requestID    atomic.Int64
	endpointMu   sync.RWMutex
	endpointURL  string
	endpointChan chan struct{}
	endpointOnce sync.Once
	connectMu    sync.Mutex
	started      bool
	sseBody      io.ReadCloser
	streamCancel context.CancelFunc
	readErrMu    sync.Mutex
	readErr      error
	pendingMu    sync.Mutex
	pending      map[int64]chan rpcResponse
}

type streamableHTTPClient struct {
	url         string
	headers     map[string]string
	httpClient  *http.Client
	nextID      atomic.Int64
	initialized atomic.Bool
	sessionMu   sync.RWMutex
	sessionID   string
}

func NewSSEMCPClient(url string, headers map[string]string) Client {
	return &sseMCPClient{
		baseURL:      strings.TrimSpace(url),
		headers:      cloneHeaders(headers),
		httpClient:   &http.Client{},
		endpointChan: make(chan struct{}),
		pending:      make(map[int64]chan rpcResponse),
	}
}

func NewStreamableHTTPClient(url string, headers map[string]string) Client {
	return &streamableHTTPClient{
		url:        strings.TrimSpace(url),
		headers:    cloneHeaders(headers),
		httpClient: &http.Client{Timeout: defaultRequestTimeout},
	}
}

func (c *sseMCPClient) Start(ctx context.Context) error {
	c.connectMu.Lock()
	defer c.connectMu.Unlock()

	if c.started && c.endpoint() != "" {
		return nil
	}

	waitCtx, cancelWait := withDefaultTimeout(ctx, defaultSSEConnectWait)
	defer cancelWait()
	streamCtx, cancelStream := context.WithCancel(context.Background())

	req, err := http.NewRequestWithContext(streamCtx, http.MethodGet, c.baseURL, nil)
	if err != nil {
		cancelStream()
		return fmt.Errorf("failed to create SSE request: %w", err)
	}
	req.Header.Set("accept", "text/event-stream")
	req.Header.Set("cache-control", "no-cache")
	req.Header.Set("connection", "keep-alive")
	for key, value := range c.headers {
		req.Header.Set(key, value)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		cancelStream()
		return fmt.Errorf("failed to connect to SSE stream: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		cancelStream()
		return buildHTTPError("failed to connect to SSE stream", resp, body)
	}

	c.sseBody = resp.Body
	c.streamCancel = cancelStream
	c.started = true
	go c.readSSE(resp.Body)

	select {
	case <-c.endpointChan:
		return c.lastReadErr()
	case <-waitCtx.Done():
		_ = c.Close()
		if err := c.lastReadErr(); err != nil {
			return err
		}
		return fmt.Errorf("timeout waiting for endpoint")
	}
}

func (c *sseMCPClient) Initialize(ctx context.Context, request mcp.InitializeRequest) (*mcp.InitializeResult, error) {
	if err := c.Start(ctx); err != nil {
		return nil, err
	}
	var result mcp.InitializeResult
	if err := c.call(ctx, "initialize", remoteInitializeParams(request), &result); err != nil {
		return nil, err
	}
	if err := c.notify(ctx, "notifications/initialized", map[string]any{}); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *sseMCPClient) ListTools(ctx context.Context, request mcp.ListToolsRequest) (*mcp.ListToolsResult, error) {
	var result mcp.ListToolsResult
	if err := c.call(ctx, "tools/list", map[string]any{}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *sseMCPClient) CallTool(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var result mcp.CallToolResult
	if err := c.call(ctx, "tools/call", request.Params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *sseMCPClient) Close() error {
	c.connectMu.Lock()
	defer c.connectMu.Unlock()

	if c.sseBody != nil {
		_ = c.sseBody.Close()
		c.sseBody = nil
	}
	if c.streamCancel != nil {
		c.streamCancel()
		c.streamCancel = nil
	}
	c.started = false
	c.setEndpoint("")
	c.endpointChan = make(chan struct{})
	c.endpointOnce = sync.Once{}
	c.readErrMu.Lock()
	c.readErr = nil
	c.readErrMu.Unlock()
	c.pendingMu.Lock()
	for id, ch := range c.pending {
		delete(c.pending, id)
		close(ch)
	}
	c.pendingMu.Unlock()
	return nil
}

func (c *sseMCPClient) call(ctx context.Context, method string, params any, out any) error {
	if err := c.Start(ctx); err != nil {
		return err
	}
	endpoint := c.endpoint()
	if endpoint == "" {
		return fmt.Errorf("MCP messages endpoint not established")
	}

	callCtx, cancel := withDefaultTimeout(ctx, defaultRequestTimeout)
	defer cancel()

	id := c.requestID.Add(1)
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

	resultCh := make(chan rpcResponse, 1)
	c.pendingMu.Lock()
	c.pending[id] = resultCh
	c.pendingMu.Unlock()
	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
	}()

	req, err := http.NewRequestWithContext(callCtx, http.MethodPost, endpoint, bytes.NewReader(body))
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

	contentType := strings.ToLower(resp.Header.Get("content-type"))
	if strings.Contains(contentType, "application/json") {
		bodyBytes, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return fmt.Errorf("failed to read mcp response: %w", readErr)
		}
		if resp.StatusCode >= 400 {
			return buildHTTPError("mcp request failed", resp, bodyBytes)
		}
		return decodeRPCResult(bodyBytes, out)
	}

	if resp.StatusCode >= 400 && resp.StatusCode != http.StatusAccepted {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return buildHTTPError("mcp request failed", resp, bodyBytes)
	}

	select {
	case result, ok := <-resultCh:
		if !ok {
			if err := c.lastReadErr(); err != nil {
				return err
			}
			return fmt.Errorf("MCP connection closed")
		}
		if result.Error != nil {
			return fmt.Errorf("%s", result.Error.Message)
		}
		if out == nil || len(result.Result) == 0 {
			return nil
		}
		if err := json.Unmarshal(result.Result, out); err != nil {
			return fmt.Errorf("failed to decode mcp result: %w", err)
		}
		return nil
	case <-callCtx.Done():
		return fmt.Errorf("mcp request timed out")
	}
}

func (c *sseMCPClient) notify(ctx context.Context, method string, params any) error {
	if err := c.Start(ctx); err != nil {
		return err
	}
	endpoint := c.endpoint()
	if endpoint == "" {
		return fmt.Errorf("MCP messages endpoint not established")
	}

	notifyCtx, cancel := withDefaultTimeout(ctx, defaultRequestTimeout)
	defer cancel()

	payload := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to encode mcp notification: %w", err)
	}
	req, err := http.NewRequestWithContext(notifyCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to build mcp notification: %w", err)
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "application/json, text/event-stream")
	for key, value := range c.headers {
		req.Header.Set(key, value)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("mcp notify failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 && resp.StatusCode != http.StatusAccepted {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return buildHTTPError("mcp notify failed", resp, bodyBytes)
	}
	return nil
}

func (c *sseMCPClient) readSSE(body io.ReadCloser) {
	defer body.Close()

	reader := bufio.NewReader(body)
	var event string
	var dataLines []string

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				c.setReadErr(fmt.Errorf("failed to read SSE stream: %w", err))
			}
			c.endpointOnce.Do(func() {
				close(c.endpointChan)
			})
			return
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			c.handleSSEEvent(event, strings.Join(dataLines, "\n"))
			event = ""
			dataLines = nil
			continue
		}
		switch {
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
}

func (c *sseMCPClient) handleSSEEvent(event string, data string) {
	if strings.TrimSpace(data) == "" {
		return
	}

	switch event {
	case "endpoint":
		c.setEndpoint(resolveURL(c.baseURL, data))
		return
	case "message", "":
		if c.endpoint() == "" {
			if maybe := extractEndpoint(data); maybe != "" {
				c.setEndpoint(resolveURL(c.baseURL, maybe))
				return
			}
		}
	}

	var msg rpcMessage
	if err := json.Unmarshal([]byte(data), &msg); err != nil {
		return
	}
	if msg.ID == nil {
		return
	}
	id, ok := parseNumericID(*msg.ID)
	if !ok {
		return
	}

	c.pendingMu.Lock()
	ch := c.pending[id]
	c.pendingMu.Unlock()
	if ch == nil {
		return
	}
	ch <- rpcResponse{
		Result: msg.Result,
		Error:  msg.Error,
	}
}

func (c *sseMCPClient) setEndpoint(value string) {
	c.endpointMu.Lock()
	c.endpointURL = value
	c.endpointMu.Unlock()
	if value != "" {
		c.endpointOnce.Do(func() {
			close(c.endpointChan)
		})
	}
}

func (c *sseMCPClient) endpoint() string {
	c.endpointMu.RLock()
	defer c.endpointMu.RUnlock()
	return c.endpointURL
}

func (c *sseMCPClient) setReadErr(err error) {
	c.readErrMu.Lock()
	defer c.readErrMu.Unlock()
	if c.readErr == nil {
		c.readErr = err
	}
}

func (c *sseMCPClient) lastReadErr() error {
	c.readErrMu.Lock()
	defer c.readErrMu.Unlock()
	return c.readErr
}

func (c *streamableHTTPClient) Start(ctx context.Context) error {
	return nil
}

func (c *streamableHTTPClient) Initialize(ctx context.Context, request mcp.InitializeRequest) (*mcp.InitializeResult, error) {
	if c.initialized.Load() {
		return &mcp.InitializeResult{}, nil
	}
	var result mcp.InitializeResult
	if err := c.call(ctx, "initialize", remoteInitializeParams(request), &result); err != nil {
		return nil, err
	}
	if err := c.notify(ctx, "notifications/initialized", map[string]any{}); err != nil {
		return nil, err
	}
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
	callCtx, cancel := withDefaultTimeout(ctx, defaultRequestTimeout)
	defer cancel()

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
	req, err := http.NewRequestWithContext(callCtx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to build mcp request: %w", err)
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "application/json, text/event-stream")
	for key, value := range c.headers {
		req.Header.Set(key, value)
	}
	if sessionID := c.getSessionID(); sessionID != "" {
		req.Header.Set("mcp-session-id", sessionID)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("mcp request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return fmt.Errorf("failed to read mcp response: %w", readErr)
	}
	c.setSessionID(resp.Header.Get("mcp-session-id"))
	if resp.StatusCode >= 400 {
		return buildHTTPError("mcp request failed", resp, bodyBytes)
	}

	return decodeResponseBody(resp.Header.Get("content-type"), bodyBytes, out)
}

func (c *streamableHTTPClient) notify(ctx context.Context, method string, params any) error {
	notifyCtx, cancel := withDefaultTimeout(ctx, defaultRequestTimeout)
	defer cancel()

	payload := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to encode mcp notification: %w", err)
	}
	req, err := http.NewRequestWithContext(notifyCtx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to build mcp notification: %w", err)
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "application/json, text/event-stream")
	for key, value := range c.headers {
		req.Header.Set(key, value)
	}
	if sessionID := c.getSessionID(); sessionID != "" {
		req.Header.Set("mcp-session-id", sessionID)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("mcp notify failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	c.setSessionID(resp.Header.Get("mcp-session-id"))
	if resp.StatusCode >= 400 {
		return buildHTTPError("mcp notify failed", resp, bodyBytes)
	}
	return nil
}

func (c *streamableHTTPClient) setSessionID(value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	c.sessionMu.Lock()
	c.sessionID = value
	c.sessionMu.Unlock()
}

func (c *streamableHTTPClient) getSessionID() string {
	c.sessionMu.RLock()
	defer c.sessionMu.RUnlock()
	return c.sessionID
}

func withDefaultTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, timeout)
}

func cloneHeaders(headers map[string]string) map[string]string {
	cloned := map[string]string{}
	for key, value := range headers {
		cloned[key] = value
	}
	return cloned
}

func remoteInitializeParams(request mcp.InitializeRequest) map[string]any {
	protocolVersion := strings.TrimSpace(request.Params.ProtocolVersion)
	if protocolVersion == "" || protocolVersion == mcp.LATEST_PROTOCOL_VERSION {
		protocolVersion = remoteProtocolVersion
	}

	clientInfo := request.Params.ClientInfo
	if strings.TrimSpace(clientInfo.Name) == "" {
		clientInfo.Name = "SciCLI"
	}
	if strings.TrimSpace(clientInfo.Version) == "" {
		clientInfo.Version = version.Version
	}

	capabilities := map[string]any{}
	if request.Params.Capabilities.Experimental != nil {
		capabilities["experimental"] = request.Params.Capabilities.Experimental
	}
	if request.Params.Capabilities.Roots != nil {
		capabilities["roots"] = request.Params.Capabilities.Roots
	}
	if request.Params.Capabilities.Sampling != nil {
		capabilities["sampling"] = request.Params.Capabilities.Sampling
	}
	capabilities["tools"] = map[string]any{}

	return map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    capabilities,
		"clientInfo":      clientInfo,
	}
}

func decodeResponseBody(contentType string, body []byte, out any) error {
	parsed, err := decodeRPCEnvelope(contentType, body)
	if err != nil {
		return err
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

func decodeRPCResult(body []byte, out any) error {
	var parsed rpcResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
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

func decodeRPCEnvelope(contentType string, body []byte) (*rpcResponse, error) {
	payload := bytes.TrimSpace(body)
	if len(payload) == 0 {
		return &rpcResponse{}, nil
	}
	if strings.Contains(strings.ToLower(contentType), "text/event-stream") {
		var err error
		payload, err = extractEventStreamMessage(payload)
		if err != nil {
			return nil, fmt.Errorf("failed to decode mcp response: %w", err)
		}
	}

	var parsed rpcResponse
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return nil, fmt.Errorf("failed to decode mcp response: %w", err)
	}
	return &parsed, nil
}

func extractEventStreamMessage(body []byte) ([]byte, error) {
	scanner := bufio.NewScanner(bytes.NewReader(body))
	var event string
	var dataLines []string

	flush := func() ([]byte, bool) {
		if len(dataLines) == 0 {
			return nil, false
		}
		if event == "" || event == "message" {
			return []byte(strings.Join(dataLines, "\n")), true
		}
		event = ""
		dataLines = nil
		return nil, false
	}

	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if line == "" {
			if data, ok := flush(); ok {
				return data, nil
			}
			continue
		}
		switch {
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if data, ok := flush(); ok {
		return data, nil
	}
	return nil, fmt.Errorf("no message event found")
}

func buildHTTPError(prefix string, resp *http.Response, body []byte) error {
	if parsed, err := decodeRPCEnvelope(resp.Header.Get("content-type"), body); err == nil && parsed.Error != nil {
		return fmt.Errorf("%s: %s", prefix, parsed.Error.Message)
	}
	text := strings.TrimSpace(string(body))
	if text != "" {
		return fmt.Errorf("%s: %s", prefix, text)
	}
	return fmt.Errorf("%s: %s", prefix, resp.Status)
}

func resolveURL(base string, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if parsed, err := url.Parse(value); err == nil && parsed.IsAbs() {
		return parsed.String()
	}
	baseURL, err := url.Parse(base)
	if err != nil {
		return value
	}
	ref, err := url.Parse(value)
	if err != nil {
		return value
	}
	return baseURL.ResolveReference(ref).String()
}

func extractEndpoint(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
		if strings.Contains(trimmed, "messages") {
			return trimmed
		}
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return ""
	}
	for _, key := range []string{"endpoint", "uri", "messageEndpoint"} {
		value, ok := payload[key].(string)
		if ok && strings.Contains(value, "messages") {
			return value
		}
	}
	return ""
}

func parseNumericID(raw json.RawMessage) (int64, bool) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return 0, false
	}
	id, err := strconv.ParseInt(trimmed, 10, 64)
	if err == nil {
		return id, true
	}
	var numeric float64
	if err := json.Unmarshal(raw, &numeric); err == nil {
		return int64(numeric), true
	}
	return 0, false
}
