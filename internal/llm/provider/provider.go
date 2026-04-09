package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"github.com/SciMate-AI/scicli/internal/llm/models"
	"github.com/SciMate-AI/scicli/internal/llm/tools"
	"github.com/SciMate-AI/scicli/internal/message"
)

type EventType string

const maxRetries = 8
const retryBaseDelay = 3 * time.Second
const retryMaxDelay = 30 * time.Second

const (
	EventContentStart  EventType = "content_start"
	EventToolUseStart  EventType = "tool_use_start"
	EventToolUseDelta  EventType = "tool_use_delta"
	EventToolUseStop   EventType = "tool_use_stop"
	EventContentDelta  EventType = "content_delta"
	EventThinkingDelta EventType = "thinking_delta"
	EventContentStop   EventType = "content_stop"
	EventComplete      EventType = "complete"
	EventError         EventType = "error"
	EventWarning       EventType = "warning"
)

type TokenUsage struct {
	InputTokens         int64
	OutputTokens        int64
	CacheCreationTokens int64
	CacheReadTokens     int64
}

type ProviderResponse struct {
	Content          string
	ToolCalls        []message.ToolCall
	Usage            TokenUsage
	FinishReason     message.FinishReason
	GeminiRawContent *message.GeminiRawContent
}

type ProviderEvent struct {
	Type EventType

	Content  string
	Thinking string
	Response *ProviderResponse
	ToolCall *message.ToolCall
	Error    error
}
type Provider interface {
	SendMessages(ctx context.Context, messages []message.Message, tools []tools.BaseTool) (*ProviderResponse, error)

	StreamResponse(ctx context.Context, messages []message.Message, tools []tools.BaseTool) <-chan ProviderEvent

	Model() models.Model
}

type providerClientOptions struct {
	apiKey        string
	model         models.Model
	maxTokens     int64
	systemMessage string

	anthropicOptions []AnthropicOption
	openaiOptions    []OpenAIOption
	geminiOptions    []GeminiOption
	bedrockOptions   []BedrockOption
	copilotOptions   []CopilotOption
}

type ProviderClientOption func(*providerClientOptions)

type ProviderClient interface {
	send(ctx context.Context, messages []message.Message, tools []tools.BaseTool) (*ProviderResponse, error)
	stream(ctx context.Context, messages []message.Message, tools []tools.BaseTool) <-chan ProviderEvent
}

type baseProvider[C ProviderClient] struct {
	options providerClientOptions
	client  C
}

func NewProvider(providerName models.ModelProvider, opts ...ProviderClientOption) (Provider, error) {
	clientOptions := providerClientOptions{}
	for _, o := range opts {
		o(&clientOptions)
	}
	switch providerName {
	case models.ProviderCopilot:
		return &baseProvider[CopilotClient]{
			options: clientOptions,
			client:  newCopilotClient(clientOptions),
		}, nil
	case models.ProviderAnthropic:
		return &baseProvider[AnthropicClient]{
			options: clientOptions,
			client:  newAnthropicClient(clientOptions),
		}, nil
	case models.ProviderOpenAI:
		return &baseProvider[OpenAIClient]{
			options: clientOptions,
			client:  newOpenAIClient(clientOptions),
		}, nil
	case models.ProviderGemini:
		return &baseProvider[GeminiClient]{
			options: clientOptions,
			client:  newGeminiClient(clientOptions),
		}, nil
	case models.ProviderBedrock:
		return &baseProvider[BedrockClient]{
			options: clientOptions,
			client:  newBedrockClient(clientOptions),
		}, nil
	case models.ProviderGROQ:
		clientOptions.openaiOptions = append(clientOptions.openaiOptions,
			WithOpenAIBaseURL("https://api.groq.com/openai/v1"),
		)
		return &baseProvider[OpenAIClient]{
			options: clientOptions,
			client:  newOpenAIClient(clientOptions),
		}, nil
	case models.ProviderAzure:
		return &baseProvider[AzureClient]{
			options: clientOptions,
			client:  newAzureClient(clientOptions),
		}, nil
	case models.ProviderVertexAI:
		return &baseProvider[VertexAIClient]{
			options: clientOptions,
			client:  newVertexAIClient(clientOptions),
		}, nil
	case models.ProviderOpenRouter:
		clientOptions.openaiOptions = append(clientOptions.openaiOptions,
			WithOpenAIBaseURL("https://openrouter.ai/api/v1"),
			WithOpenAIExtraHeaders(map[string]string{
				"X-Title": "SciCLI",
			}),
		)
		return &baseProvider[OpenAIClient]{
			options: clientOptions,
			client:  newOpenAIClient(clientOptions),
		}, nil
	case models.ProviderXAI:
		clientOptions.openaiOptions = append(clientOptions.openaiOptions,
			WithOpenAIBaseURL("https://api.x.ai/v1"),
		)
		return &baseProvider[OpenAIClient]{
			options: clientOptions,
			client:  newOpenAIClient(clientOptions),
		}, nil
	case models.ProviderOpenAICompatible:
		return &baseProvider[OpenAIClient]{
			options: clientOptions,
			client:  newOpenAIClient(clientOptions),
		}, nil
	case models.ProviderLocal:
		clientOptions.openaiOptions = append(clientOptions.openaiOptions,
			WithOpenAIBaseURL(os.Getenv("LOCAL_ENDPOINT")),
		)
		return &baseProvider[OpenAIClient]{
			options: clientOptions,
			client:  newOpenAIClient(clientOptions),
		}, nil
	case models.ProviderMock:
		// TODO: implement mock client for test
		panic("not implemented")
	}
	return nil, fmt.Errorf("provider not supported: %s", providerName)
}

func (p *baseProvider[C]) cleanMessages(messages []message.Message) (cleaned []message.Message) {
	for _, msg := range messages {
		// The message has no content
		if len(msg.Parts) == 0 {
			continue
		}
		cleaned = append(cleaned, msg)
	}
	return
}

func (p *baseProvider[C]) SendMessages(ctx context.Context, messages []message.Message, tools []tools.BaseTool) (*ProviderResponse, error) {
	messages = p.cleanMessages(messages)
	return p.client.send(ctx, messages, tools)
}

func (p *baseProvider[C]) Model() models.Model {
	return p.options.model
}

func (p *baseProvider[C]) StreamResponse(ctx context.Context, messages []message.Message, tools []tools.BaseTool) <-chan ProviderEvent {
	messages = p.cleanMessages(messages)
	return p.client.stream(ctx, messages, tools)
}

func WithAPIKey(apiKey string) ProviderClientOption {
	return func(options *providerClientOptions) {
		options.apiKey = apiKey
	}
}

func WithModel(model models.Model) ProviderClientOption {
	return func(options *providerClientOptions) {
		options.model = model
	}
}

func WithMaxTokens(maxTokens int64) ProviderClientOption {
	return func(options *providerClientOptions) {
		options.maxTokens = maxTokens
	}
}

func WithSystemMessage(systemMessage string) ProviderClientOption {
	return func(options *providerClientOptions) {
		options.systemMessage = systemMessage
	}
}

func WithAnthropicOptions(anthropicOptions ...AnthropicOption) ProviderClientOption {
	return func(options *providerClientOptions) {
		options.anthropicOptions = anthropicOptions
	}
}

func WithOpenAIOptions(openaiOptions ...OpenAIOption) ProviderClientOption {
	return func(options *providerClientOptions) {
		options.openaiOptions = openaiOptions
	}
}

func WithGeminiOptions(geminiOptions ...GeminiOption) ProviderClientOption {
	return func(options *providerClientOptions) {
		options.geminiOptions = geminiOptions
	}
}

func WithBedrockOptions(bedrockOptions ...BedrockOption) ProviderClientOption {
	return func(options *providerClientOptions) {
		options.bedrockOptions = bedrockOptions
	}
}

func WithCopilotOptions(copilotOptions ...CopilotOption) ProviderClientOption {
	return func(options *providerClientOptions) {
		options.copilotOptions = copilotOptions
	}
}

func shouldRetryTransientNetworkError(attempts int, err error) (bool, int64, error) {
	if !isTransientNetworkError(err) {
		return false, 0, nil
	}
	if attempts > maxRetries {
		return false, 0, fmt.Errorf("maximum retry attempts reached after transient network errors: %d retries", maxRetries)
	}
	return true, retryDelayMs(attempts, nil), nil
}

func retryDelayMs(attempts int, retryAfterValues []string) int64 {
	delay := retryBaseDelay
	for i := 1; i < attempts; i++ {
		delay *= 2
		if delay >= retryMaxDelay {
			delay = retryMaxDelay
			break
		}
	}
	if len(retryAfterValues) > 0 {
		var retrySeconds int
		if _, err := fmt.Sscanf(strings.TrimSpace(retryAfterValues[0]), "%d", &retrySeconds); err == nil && retrySeconds > 0 {
			delay = time.Duration(retrySeconds) * time.Second
		}
	}
	if delay > retryMaxDelay {
		delay = retryMaxDelay
	}
	return delay.Milliseconds()
}

func formatRetryLog(err error, attempts int, afterMs int64) string {
	return fmt.Sprintf("%s... attempt %d of %d (waiting %s)", retryReason(err), attempts, maxRetries, (time.Duration(afterMs) * time.Millisecond).Round(time.Second))
}

func retryReason(err error) string {
	if isTransientNetworkError(err) {
		return "Transient network error, retrying request"
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	switch {
	case containsAnySubstring(msg, "429", "rate limit", "too many requests", "quota exceeded", "resource exhausted"):
		return "Provider rate limit, retrying request"
	case containsAnySubstring(msg, "500", "502", "503", "504", "529", "server error", "service unavailable"):
		return "Provider server error, retrying request"
	default:
		return "Retrying request"
	}
}

func isTransientNetworkError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout() || temporaryNetError(netErr)
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return dnsErr.IsTimeout || dnsErr.IsTemporary
	}

	var opErr *net.OpError
	if errors.As(err, &opErr) {
		if opErr.Timeout() {
			return true
		}
	}

	errText := strings.ToLower(err.Error())
	return containsAnySubstring(errText,
		"timeout",
		"temporary failure",
		"connection reset",
		"connection refused",
		"connection aborted",
		"unexpected eof",
		"eof",
		"broken pipe",
		"reset by peer",
		"server closed idle connection",
		"tls handshake timeout",
		"wsarecv",
		"wsasend",
	)
}

func temporaryNetError(err net.Error) bool {
	type temporary interface {
		Temporary() bool
	}
	if tmp, ok := err.(temporary); ok {
		return tmp.Temporary()
	}
	return false
}

func containsAnySubstring(text string, parts ...string) bool {
	for _, part := range parts {
		if strings.Contains(text, part) {
			return true
		}
	}
	return false
}
