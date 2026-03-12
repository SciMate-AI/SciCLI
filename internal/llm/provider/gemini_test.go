package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/SciMate-AI/scicli/internal/llm/models"
	"github.com/SciMate-AI/scicli/internal/message"
	"google.golang.org/genai"
)

func TestGeminiSendPersistsThoughtSignatureAcrossTurns(t *testing.T) {
	t.Parallel()

	requestBodies := make([]map[string]any, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		requestBodies = append(requestBodies, body)

		w.Header().Set("Content-Type", "application/json")
		switch len(requestBodies) {
		case 1:
			fmt.Fprint(w, `{
				"candidates":[{
					"content":{"role":"model","parts":[
						{"thought":true,"text":"plan","thoughtSignature":"sig-1"},
						{"functionCall":{"name":"read_file","args":{"path":"go.mod"}}}
					]},
					"finishReason":"STOP"
				}],
				"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5}
			}`)
		default:
			fmt.Fprint(w, `{
				"candidates":[{
					"content":{"role":"model","parts":[{"text":"done"}]},
					"finishReason":"STOP"
				}],
				"usageMetadata":{"promptTokenCount":20,"candidatesTokenCount":3}
			}`)
		}
	}))
	defer server.Close()

	client, err := genai.NewClient(context.Background(), &genai.ClientConfig{
		APIKey:     "test-key",
		Backend:    genai.BackendGeminiAPI,
		HTTPClient: server.Client(),
		HTTPOptions: genai.HTTPOptions{
			BaseURL:    server.URL,
			APIVersion: "v1beta",
		},
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	g := &geminiClient{
		providerOptions: providerClientOptions{
			model: models.Model{
				Provider: models.ProviderGemini,
				APIModel: "gemini-2.5-pro",
			},
			maxTokens:     512,
			systemMessage: "system",
		},
		client: client,
	}

	userMsg := message.Message{
		Role:  message.User,
		Parts: []message.ContentPart{message.TextContent{Text: "hi"}},
	}

	firstResp, err := g.send(context.Background(), []message.Message{userMsg}, nil)
	if err != nil {
		t.Fatalf("first send() error = %v", err)
	}
	if firstResp.GeminiRawContent == nil {
		t.Fatal("expected GeminiRawContent in first response")
	}
	if got := firstResp.GeminiRawContent.Parts[0]["thoughtSignature"]; got != "sig-1" {
		t.Fatalf("expected thoughtSignature sig-1, got %#v", got)
	}
	if len(firstResp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(firstResp.ToolCalls))
	}

	assistantMsg := message.Message{Role: message.Assistant}
	assistantMsg.SetGeminiRawContent(*firstResp.GeminiRawContent)
	assistantMsg.SetToolCalls(firstResp.ToolCalls)

	toolMsg := message.Message{
		Role: message.Tool,
		Parts: []message.ContentPart{
			message.ToolResult{
				ToolCallID: firstResp.ToolCalls[0].ID,
				Name:       firstResp.ToolCalls[0].Name,
				Content:    `{"ok":true}`,
			},
		},
	}

	if _, err := g.send(context.Background(), []message.Message{userMsg, assistantMsg, toolMsg}, nil); err != nil {
		t.Fatalf("second send() error = %v", err)
	}

	if len(requestBodies) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(requestBodies))
	}

	contents, ok := requestBodies[1]["contents"].([]any)
	if !ok || len(contents) < 2 {
		t.Fatalf("unexpected contents payload: %#v", requestBodies[1]["contents"])
	}
	assistantContent, ok := contents[1].(map[string]any)
	if !ok {
		t.Fatalf("assistant content is not an object: %#v", contents[1])
	}
	parts, ok := assistantContent["parts"].([]any)
	if !ok {
		t.Fatalf("assistant parts are not a list: %#v", assistantContent["parts"])
	}

	foundSignature := slices.ContainsFunc(parts, func(part any) bool {
		partMap, ok := part.(map[string]any)
		return ok && partMap["thoughtSignature"] == "sig-1"
	})
	if !foundSignature {
		t.Fatalf("second request is missing thoughtSignature: %#v", parts)
	}
}

func TestGeminiStreamAggregatesThoughtSignature(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		fmt.Fprint(w, "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"thought\":true,\"text\":\"pla\",\"thoughtSignature\":\"sig-1\"}]}}]}\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"thought\":true,\"text\":\"n\",\"thoughtSignature\":\"sig-1\"},{\"functionCall\":{\"name\":\"read_file\",\"args\":{\"path\":\"go.mod\"}}}],\"role\":\"model\"},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":10,\"candidatesTokenCount\":5}}\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	client, err := genai.NewClient(context.Background(), &genai.ClientConfig{
		APIKey:     "test-key",
		Backend:    genai.BackendGeminiAPI,
		HTTPClient: server.Client(),
		HTTPOptions: genai.HTTPOptions{
			BaseURL:    server.URL,
			APIVersion: "v1beta",
		},
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	g := &geminiClient{
		providerOptions: providerClientOptions{
			model: models.Model{
				Provider: models.ProviderGemini,
				APIModel: "gemini-2.5-pro",
			},
			maxTokens: 512,
		},
		client: client,
	}

	userMsg := message.Message{
		Role:  message.User,
		Parts: []message.ContentPart{message.TextContent{Text: "hi"}},
	}

	var complete *ProviderResponse
	var thinking string
	for event := range g.stream(context.Background(), []message.Message{userMsg}, nil) {
		switch event.Type {
		case EventThinkingDelta:
			thinking += event.Content
		case EventComplete:
			complete = event.Response
		case EventError:
			t.Fatalf("stream error = %v", event.Error)
		}
	}

	if thinking != "plan" {
		t.Fatalf("expected aggregated thinking delta 'plan', got %q", thinking)
	}
	if complete == nil {
		t.Fatal("expected complete response")
	}
	if complete.GeminiRawContent == nil {
		t.Fatal("expected GeminiRawContent in complete response")
	}
	if len(complete.GeminiRawContent.Parts) != 2 {
		t.Fatalf("expected 2 aggregated raw parts, got %d", len(complete.GeminiRawContent.Parts))
	}
	if complete.GeminiRawContent.Parts[0]["thoughtSignature"] != "sig-1" {
		t.Fatalf("expected thoughtSignature sig-1, got %#v", complete.GeminiRawContent.Parts[0]["thoughtSignature"])
	}
	if complete.GeminiRawContent.Parts[0]["text"] != "plan" {
		t.Fatalf("expected merged thought text 'plan', got %#v", complete.GeminiRawContent.Parts[0]["text"])
	}
	if len(complete.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(complete.ToolCalls))
	}
}
