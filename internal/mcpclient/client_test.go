package mcpclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

type fakeClient struct{}

func (f *fakeClient) Initialize(ctx context.Context, request mcp.InitializeRequest) (*mcp.InitializeResult, error) {
	return &mcp.InitializeResult{}, nil
}

func (f *fakeClient) ListTools(ctx context.Context, request mcp.ListToolsRequest) (*mcp.ListToolsResult, error) {
	return &mcp.ListToolsResult{}, nil
}

func (f *fakeClient) CallTool(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return &mcp.CallToolResult{}, nil
}

func (f *fakeClient) Close() error {
	return nil
}

type fakeStartClient struct {
	fakeClient
	started bool
}

func (f *fakeStartClient) Start(ctx context.Context) error {
	f.started = true
	return nil
}

func TestEnsureStartedStartsStartableClients(t *testing.T) {
	client := &fakeStartClient{}

	if err := EnsureStarted(context.Background(), client); err != nil {
		t.Fatalf("EnsureStarted() error = %v", err)
	}
	if !client.started {
		t.Fatal("EnsureStarted() did not call Start on a startable client")
	}
}

func TestEnsureStartedSkipsNonStartableClients(t *testing.T) {
	client := &fakeClient{}

	if err := EnsureStarted(context.Background(), client); err != nil {
		t.Fatalf("EnsureStarted() error = %v", err)
	}
}

func TestExtractEventStreamMessage(t *testing.T) {
	payload := []byte("event: message\r\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"ok\":true}}\r\n\r\n")

	result, err := extractEventStreamMessage(payload)
	if err != nil {
		t.Fatalf("extractEventStreamMessage() error = %v", err)
	}
	if string(result) != "{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"ok\":true}}" {
		t.Fatalf("unexpected payload: %s", result)
	}
}

func TestStreamableHTTPClientTracksSessionAndDecodesSSE(t *testing.T) {
	sessionID := "session-123"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}

		switch req.Method {
		case "initialize":
			if got := req.Params["protocolVersion"]; got != remoteProtocolVersion {
				t.Fatalf("protocolVersion = %v, want %s", got, remoteProtocolVersion)
			}
			capabilities, ok := req.Params["capabilities"].(map[string]any)
			if !ok {
				t.Fatalf("capabilities missing: %#v", req.Params["capabilities"])
			}
			if _, ok := capabilities["tools"]; !ok {
				t.Fatalf("tools capability missing: %#v", capabilities)
			}
			w.Header().Set("content-type", "text/event-stream")
			w.Header().Set("mcp-session-id", sessionID)
			_, _ = w.Write([]byte("event: message\n"))
			_, _ = w.Write([]byte("data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"protocolVersion\":\"2025-06-18\",\"capabilities\":{\"tools\":{\"listChanged\":false}},\"serverInfo\":{\"name\":\"rdkit\",\"version\":\"1.0.0\"}}}\n\n"))
		case "notifications/initialized":
			if got := r.Header.Get("mcp-session-id"); got != sessionID {
				t.Fatalf("notifications/initialized mcp-session-id = %q, want %q", got, sessionID)
			}
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			if got := r.Header.Get("mcp-session-id"); got != sessionID {
				t.Fatalf("tools/list mcp-session-id = %q, want %q", got, sessionID)
			}
			w.Header().Set("content-type", "text/event-stream")
			w.Header().Set("mcp-session-id", sessionID)
			_, _ = w.Write([]byte("event: message\n"))
			_, _ = w.Write([]byte("data: {\"jsonrpc\":\"2.0\",\"id\":2,\"result\":{\"tools\":[{\"name\":\"embed_molecule\",\"description\":\"Embed a molecule\",\"inputSchema\":{\"type\":\"object\",\"properties\":{}}}]}}\n\n"))
		default:
			t.Fatalf("unexpected method %q", req.Method)
		}
	}))
	defer server.Close()

	client := NewStreamableHTTPClient(server.URL, nil)
	tools, err := client.ListTools(context.Background(), mcp.ListToolsRequest{})
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	if len(tools.Tools) != 1 || tools.Tools[0].Name != "embed_molecule" {
		t.Fatalf("unexpected tools response: %#v", tools.Tools)
	}
}

func TestDecodeRPCResultParsesCallToolResult(t *testing.T) {
	body := []byte(`{"jsonrpc":"2.0","id":3,"result":{"content":[{"type":"text","text":"hello world"}],"structuredContent":{"formula":"C8H10N4O2"},"isError":false}}`)

	var result mcp.CallToolResult
	if err := decodeRPCResult(body, &result); err != nil {
		t.Fatalf("decodeRPCResult() error = %v", err)
	}
	if result.IsError {
		t.Fatal("expected IsError to be false")
	}
	if len(result.Content) != 1 {
		t.Fatalf("len(result.Content) = %d, want 1", len(result.Content))
	}
	text, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("unexpected content type: %T", result.Content[0])
	}
	if text.Text != "hello world" {
		t.Fatalf("text.Text = %q, want %q", text.Text, "hello world")
	}
}

func TestDecodeResponseBodyParsesCallToolResultFromSSE(t *testing.T) {
	body := []byte("event: message\n" +
		"data: {\"jsonrpc\":\"2.0\",\"id\":3,\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"caffeine\"}],\"structuredContent\":{\"formula\":\"C8H10N4O2\"},\"isError\":false}}\n\n")

	var result mcp.CallToolResult
	if err := decodeResponseBody("text/event-stream", body, &result); err != nil {
		t.Fatalf("decodeResponseBody() error = %v", err)
	}
	if len(result.Content) != 1 {
		t.Fatalf("len(result.Content) = %d, want 1", len(result.Content))
	}
	text, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("unexpected content type: %T", result.Content[0])
	}
	if text.Text != "caffeine" {
		t.Fatalf("text.Text = %q, want %q", text.Text, "caffeine")
	}
}
