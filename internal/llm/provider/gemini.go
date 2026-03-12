package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/SciMate-AI/scicli/internal/config"
	"github.com/SciMate-AI/scicli/internal/llm/tools"
	"github.com/SciMate-AI/scicli/internal/logging"
	"github.com/SciMate-AI/scicli/internal/message"
	"github.com/google/uuid"
	"google.golang.org/genai"
)

const geminiStreamMaxChunkSize = 8 * 1024 * 1024

type geminiOptions struct {
	disableCache bool
}

type GeminiOption func(*geminiOptions)

type geminiClient struct {
	providerOptions providerClientOptions
	options         geminiOptions
	client          *genai.Client
}

type GeminiClient ProviderClient

func newGeminiClient(opts providerClientOptions) GeminiClient {
	geminiOpts := geminiOptions{}
	for _, o := range opts.geminiOptions {
		o(&geminiOpts)
	}

	client, err := genai.NewClient(context.Background(), &genai.ClientConfig{APIKey: opts.apiKey, Backend: genai.BackendGeminiAPI})
	if err != nil {
		logging.Error("Failed to create Gemini client", "error", err)
		return nil
	}

	return &geminiClient{
		providerOptions: opts,
		options:         geminiOpts,
		client:          client,
	}
}

func (g *geminiClient) convertMessages(messages []message.Message) []map[string]any {
	history := make([]map[string]any, 0, len(messages))

	for _, msg := range messages {
		switch msg.Role {
		case message.User:
			parts := make([]map[string]any, 0, len(msg.BinaryContent())+1)
			parts = append(parts, map[string]any{"text": msg.Content().String()})
			for _, binaryContent := range msg.BinaryContent() {
				parts = append(parts, map[string]any{
					"inlineData": map[string]any{
						"mimeType": binaryContent.MIMEType,
						"data":     binaryContent.String(g.providerOptions.model.Provider),
					},
				})
			}
			history = append(history, map[string]any{
				"role":  "user",
				"parts": parts,
			})
		case message.Assistant:
			parts := g.assistantParts(msg)
			if len(parts) == 0 {
				continue
			}
			history = append(history, map[string]any{
				"role":  "model",
				"parts": parts,
			})
		case message.Tool:
			for _, result := range msg.ToolResults() {
				response := map[string]any{"result": result.Content}
				if parsed, err := parseJsonToMap(result.Content); err == nil {
					response = parsed
				}

				toolName := result.Name
				for _, parent := range messages {
					if parent.Role != message.Assistant {
						continue
					}
					for _, call := range parent.ToolCalls() {
						if call.ID == result.ToolCallID {
							toolName = call.Name
							break
						}
					}
					if toolName != "" {
						break
					}
				}

				history = append(history, map[string]any{
					"role": "function",
					"parts": []map[string]any{
						{
							"functionResponse": map[string]any{
								"name":     toolName,
								"response": response,
							},
						},
					},
				})
			}
		}
	}

	return history
}

func (g *geminiClient) assistantParts(msg message.Message) []map[string]any {
	if raw := msg.GeminiRawContent(); raw != nil && len(raw.Parts) > 0 {
		return cloneRawParts(raw.Parts)
	}

	parts := make([]map[string]any, 0, len(msg.ToolCalls())+1)
	if content := msg.Content().String(); content != "" {
		parts = append(parts, map[string]any{"text": content})
	}

	for _, call := range msg.ToolCalls() {
		args, _ := parseJsonToMap(call.Input)
		parts = append(parts, map[string]any{
			"functionCall": map[string]any{
				"name": call.Name,
				"args": args,
			},
		})
	}

	return parts
}

func (g *geminiClient) convertTools(tools []tools.BaseTool) []*genai.Tool {
	geminiTool := &genai.Tool{}
	geminiTool.FunctionDeclarations = make([]*genai.FunctionDeclaration, 0, len(tools))

	for _, tool := range tools {
		info := tool.Info()
		declaration := &genai.FunctionDeclaration{
			Name:        info.Name,
			Description: info.Description,
			Parameters: &genai.Schema{
				Type:       genai.TypeObject,
				Properties: convertSchemaProperties(info.Parameters),
				Required:   info.Required,
			},
		}

		geminiTool.FunctionDeclarations = append(geminiTool.FunctionDeclarations, declaration)
	}

	return []*genai.Tool{geminiTool}
}

func (g *geminiClient) finishReason(reason genai.FinishReason) message.FinishReason {
	switch {
	case reason == genai.FinishReasonStop:
		return message.FinishReasonEndTurn
	case reason == genai.FinishReasonMaxTokens:
		return message.FinishReasonMaxTokens
	default:
		return message.FinishReasonUnknown
	}
}

func (g *geminiClient) send(ctx context.Context, messages []message.Message, tools []tools.BaseTool) (*ProviderResponse, error) {
	geminiMessages := g.convertMessages(messages)

	cfg := config.Get()
	if cfg != nil && cfg.Debug {
		jsonData, _ := json.Marshal(geminiMessages)
		logging.Debug("Prepared messages", "messages", string(jsonData))
	}

	body := g.buildRequestBody(geminiMessages, tools)

	attempts := 0
	for {
		attempts++

		resp, err := g.doRequest(ctx, "generateContent", body)
		if err != nil {
			retry, after, retryErr := g.shouldRetry(attempts, err)
			if retryErr != nil {
				return nil, retryErr
			}
			if retry {
				logging.WarnPersist(fmt.Sprintf("Retrying due to rate limit... attempt %d of %d", attempts, maxRetries), logging.PersistTimeArg, time.Millisecond*time.Duration(after+100))
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(time.Duration(after) * time.Millisecond):
					continue
				}
			}
			return nil, retryErr
		}

		rawParts := rawPartsFromResponse(resp)
		return g.providerResponseFromRaw(resp, extractVisibleContent(rawParts), extractToolCalls(rawParts), rawParts), nil
	}
}

func (g *geminiClient) stream(ctx context.Context, messages []message.Message, tools []tools.BaseTool) <-chan ProviderEvent {
	geminiMessages := g.convertMessages(messages)

	cfg := config.Get()
	if cfg != nil && cfg.Debug {
		jsonData, _ := json.Marshal(geminiMessages)
		logging.Debug("Prepared messages", "messages", string(jsonData))
	}

	body := g.buildRequestBody(geminiMessages, tools)
	eventChan := make(chan ProviderEvent)

	go func() {
		defer close(eventChan)

		attempts := 0
		for {
			attempts++

			eventChan <- ProviderEvent{Type: EventContentStart}

			resp, err := g.doStreamRequest(ctx, body)
			if err != nil {
				retry, after, retryErr := g.shouldRetry(attempts, err)
				if retryErr != nil {
					eventChan <- ProviderEvent{Type: EventError, Error: retryErr}
					return
				}
				if retry {
					logging.WarnPersist(fmt.Sprintf("Retrying due to rate limit... attempt %d of %d", attempts, maxRetries), logging.PersistTimeArg, time.Millisecond*time.Duration(after+100))
					select {
					case <-ctx.Done():
						if ctx.Err() != nil {
							eventChan <- ProviderEvent{Type: EventError, Error: ctx.Err()}
						}
						return
					case <-time.After(time.Duration(after) * time.Millisecond):
						continue
					}
				}
				eventChan <- ProviderEvent{Type: EventError, Error: retryErr}
				return
			}

			scanner := bufio.NewScanner(resp.Body)
			scanner.Buffer(make([]byte, 0, 64*1024), geminiStreamMaxChunkSize)

			currentContent := ""
			toolCalls := []message.ToolCall{}
			aggregatedRawParts := make([]map[string]any, 0)
			var finalResp map[string]any
			hadChunks := false

			for scanner.Scan() {
				line := scanner.Bytes()
				if len(line) == 0 {
					continue
				}

				prefix, data, found := bytes.Cut(line, []byte(":"))
				if !found || string(prefix) != "data" {
					_ = resp.Body.Close()
					eventChan <- ProviderEvent{Type: EventError, Error: fmt.Errorf("invalid stream chunk: %s", string(line))}
					return
				}

				chunk := make(map[string]any)
				if err := json.Unmarshal(data, &chunk); err != nil {
					_ = resp.Body.Close()
					eventChan <- ProviderEvent{Type: EventError, Error: fmt.Errorf("failed to decode Gemini stream chunk: %w", err)}
					return
				}

				hadChunks = true
				finalResp = chunk

				for _, part := range rawPartsFromResponse(chunk) {
					aggregatedRawParts = mergeRawPart(aggregatedRawParts, part)

					switch {
					case isThoughtPart(part):
						if text := stringValue(part["text"]); text != "" {
							eventChan <- ProviderEvent{Type: EventThinkingDelta, Content: text}
						}
					case stringValue(part["text"]) != "":
						text := stringValue(part["text"])
						eventChan <- ProviderEvent{Type: EventContentDelta, Content: text}
						currentContent += text
					case functionCallMap(part) != nil:
						call := rawToolCallToMessage(functionCallMap(part))
						if !containsToolCall(toolCalls, call) {
							toolCalls = append(toolCalls, call)
						}
					}
				}
			}

			streamErr := scanner.Err()
			closeErr := resp.Body.Close()
			if streamErr != nil {
				err = streamErr
				if closeErr != nil {
					err = fmt.Errorf("%w: %v", err, closeErr)
				}
			} else {
				err = closeErr
			}

			if err != nil {
				retry, after, retryErr := g.shouldRetry(attempts, err)
				if retry && !hadChunks {
					logging.WarnPersist(fmt.Sprintf("Retrying due to rate limit... attempt %d of %d", attempts, maxRetries), logging.PersistTimeArg, time.Millisecond*time.Duration(after+100))
					select {
					case <-ctx.Done():
						if ctx.Err() != nil {
							eventChan <- ProviderEvent{Type: EventError, Error: ctx.Err()}
						}
						return
					case <-time.After(time.Duration(after) * time.Millisecond):
						continue
					}
				}
				if retryErr != nil {
					eventChan <- ProviderEvent{Type: EventError, Error: retryErr}
				} else {
					eventChan <- ProviderEvent{Type: EventError, Error: err}
				}
				return
			}

			eventChan <- ProviderEvent{Type: EventContentStop}

			if finalResp == nil {
				eventChan <- ProviderEvent{Type: EventError, Error: io.EOF}
				return
			}

			eventChan <- ProviderEvent{
				Type: EventComplete,
				Response: g.providerResponseFromRaw(
					finalResp,
					currentContent,
					toolCalls,
					aggregatedRawParts,
				),
			}
			return
		}
	}()

	return eventChan
}

func (g *geminiClient) buildRequestBody(contents []map[string]any, tools []tools.BaseTool) map[string]any {
	body := map[string]any{
		"contents": contents,
	}

	generationConfig := map[string]any{}
	if g.providerOptions.maxTokens > 0 {
		generationConfig["maxOutputTokens"] = g.providerOptions.maxTokens
	}
	if len(generationConfig) > 0 {
		body["generationConfig"] = generationConfig
	}

	if g.providerOptions.systemMessage != "" {
		body["systemInstruction"] = map[string]any{
			"parts": []map[string]any{
				{"text": g.providerOptions.systemMessage},
			},
		}
	}

	if len(tools) > 0 {
		body["tools"] = g.convertTools(tools)
	}

	return body
}

func (g *geminiClient) doRequest(ctx context.Context, action string, body map[string]any) (map[string]any, error) {
	req, err := g.newRequest(ctx, action, body)
	if err != nil {
		return nil, err
	}

	resp, err := g.client.ClientConfig().HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("doRequest: error sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, readGeminiAPIError(resp)
	}

	output := make(map[string]any)
	if err := json.NewDecoder(resp.Body).Decode(&output); err != nil {
		return nil, fmt.Errorf("failed to decode Gemini response: %w", err)
	}

	return output, nil
}

func (g *geminiClient) doStreamRequest(ctx context.Context, body map[string]any) (*http.Response, error) {
	req, err := g.newRequest(ctx, "streamGenerateContent?alt=sse", body)
	if err != nil {
		return nil, err
	}

	resp, err := g.client.ClientConfig().HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("doRequest: error sending request: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		return nil, readGeminiAPIError(resp)
	}

	return resp, nil
}

func (g *geminiClient) newRequest(ctx context.Context, action string, body map[string]any) (*http.Request, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to encode Gemini request: %w", err)
	}

	url, err := g.apiURL(action)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	cfg := g.client.ClientConfig()
	if cfg.APIKey != "" {
		req.Header.Set("x-goog-api-key", cfg.APIKey)
	}
	for key, values := range cfg.HTTPOptions.Headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}

	return req, nil
}

func (g *geminiClient) apiURL(action string) (string, error) {
	cfg := g.client.ClientConfig()
	baseURL := strings.TrimRight(cfg.HTTPOptions.BaseURL, "/")
	apiVersion := strings.Trim(cfg.HTTPOptions.APIVersion, "/")
	if baseURL == "" || apiVersion == "" {
		return "", fmt.Errorf("gemini client config missing base URL or API version")
	}

	return fmt.Sprintf("%s/%s/%s:%s", baseURL, apiVersion, geminiModelPath(cfg, g.providerOptions.model.APIModel), action), nil
}

func geminiModelPath(cfg genai.ClientConfig, model string) string {
	if cfg.Backend == genai.BackendVertexAI {
		switch {
		case strings.HasPrefix(model, "projects/"):
			return model
		case strings.HasPrefix(model, "locations/"):
			return fmt.Sprintf("projects/%s/%s", cfg.Project, model)
		case strings.HasPrefix(model, "publishers/"):
			return fmt.Sprintf("projects/%s/locations/%s/%s", cfg.Project, cfg.Location, model)
		case strings.HasPrefix(model, "models/"):
			return fmt.Sprintf("projects/%s/locations/%s/publishers/google/%s", cfg.Project, cfg.Location, model)
		case strings.Contains(model, "/"):
			parts := strings.SplitN(model, "/", 2)
			return fmt.Sprintf("projects/%s/locations/%s/publishers/%s/models/%s", cfg.Project, cfg.Location, parts[0], parts[1])
		default:
			return fmt.Sprintf("projects/%s/locations/%s/publishers/google/models/%s", cfg.Project, cfg.Location, model)
		}
	}

	if strings.HasPrefix(model, "models/") || strings.HasPrefix(model, "tunedModels/") {
		return model
	}
	return "models/" + model
}

func (g *geminiClient) providerResponseFromRaw(raw map[string]any, content string, toolCalls []message.ToolCall, rawParts []map[string]any) *ProviderResponse {
	finishReason := message.FinishReasonEndTurn
	if candidate := firstCandidate(raw); candidate != nil {
		if rawFinishReason := stringValue(candidate["finishReason"]); rawFinishReason != "" {
			finishReason = g.finishReason(genai.FinishReason(rawFinishReason))
		}
	}
	if len(toolCalls) > 0 {
		finishReason = message.FinishReasonToolUse
	}

	response := &ProviderResponse{
		Content:      content,
		ToolCalls:    toolCalls,
		Usage:        usageFromRaw(raw),
		FinishReason: finishReason,
	}
	if len(rawParts) > 0 {
		response.GeminiRawContent = &message.GeminiRawContent{Parts: cloneRawParts(rawParts)}
	}
	return response
}

func (g *geminiClient) shouldRetry(attempts int, err error) (bool, int64, error) {
	if attempts > maxRetries {
		return false, 0, fmt.Errorf("maximum retry attempts reached for rate limit: %d retries", maxRetries)
	}

	if errors.Is(err, io.EOF) {
		return false, 0, err
	}

	errMsg := err.Error()
	isRateLimit := false
	if contains(errMsg, "rate limit", "quota exceeded", "too many requests", "resource exhausted", "429") {
		isRateLimit = true
	}

	if !isRateLimit {
		return false, 0, err
	}

	backoffMs := 2000 * (1 << (attempts - 1))
	jitterMs := int(float64(backoffMs) * 0.2)
	retryMs := backoffMs + jitterMs

	return true, int64(retryMs), nil
}

func WithGeminiDisableCache() GeminiOption {
	return func(options *geminiOptions) {
		options.disableCache = true
	}
}

func rawPartsFromResponse(response map[string]any) []map[string]any {
	candidate := firstCandidate(response)
	if candidate == nil {
		return nil
	}
	content, ok := candidate["content"].(map[string]any)
	if !ok {
		return nil
	}
	rawParts, ok := content["parts"].([]any)
	if !ok {
		return nil
	}

	parts := make([]map[string]any, 0, len(rawParts))
	for _, rawPart := range rawParts {
		part, ok := rawPart.(map[string]any)
		if !ok {
			continue
		}
		parts = append(parts, cloneRawPart(part))
	}
	return parts
}

func firstCandidate(response map[string]any) map[string]any {
	candidates, ok := response["candidates"].([]any)
	if !ok || len(candidates) == 0 {
		return nil
	}
	candidate, ok := candidates[0].(map[string]any)
	if !ok {
		return nil
	}
	return candidate
}

func extractVisibleContent(parts []map[string]any) string {
	var builder strings.Builder
	for _, part := range parts {
		if isThoughtPart(part) {
			continue
		}
		if text := stringValue(part["text"]); text != "" {
			builder.WriteString(text)
		}
	}
	return builder.String()
}

func extractToolCalls(parts []map[string]any) []message.ToolCall {
	toolCalls := make([]message.ToolCall, 0)
	for _, part := range parts {
		call := rawToolCallToMessage(functionCallMap(part))
		if call.Name == "" {
			continue
		}
		toolCalls = append(toolCalls, call)
	}
	return toolCalls
}

func rawToolCallToMessage(functionCall map[string]any) message.ToolCall {
	if functionCall == nil {
		return message.ToolCall{}
	}

	args := "{}"
	if rawArgs, ok := functionCall["args"]; ok {
		if encoded, err := json.Marshal(rawArgs); err == nil {
			args = string(encoded)
		}
	}

	return message.ToolCall{
		ID:       "call_" + uuid.New().String(),
		Name:     stringValue(functionCall["name"]),
		Input:    args,
		Type:     "function",
		Finished: true,
	}
}

func functionCallMap(part map[string]any) map[string]any {
	functionCall, _ := part["functionCall"].(map[string]any)
	return functionCall
}

func mergeRawPart(parts []map[string]any, part map[string]any) []map[string]any {
	cloned := cloneRawPart(part)
	if len(parts) == 0 {
		return append(parts, cloned)
	}

	if functionCall := functionCallMap(cloned); functionCall != nil {
		for i, existing := range parts {
			existingCall := functionCallMap(existing)
			if existingCall == nil {
				continue
			}
			if stringValue(existingCall["name"]) == stringValue(functionCall["name"]) &&
				normalizedJSON(existingCall["args"]) == normalizedJSON(functionCall["args"]) {
				parts[i] = mergeTopLevelMap(existing, cloned)
				return parts
			}
		}
		return append(parts, cloned)
	}

	text := stringValue(cloned["text"])
	if text == "" {
		return append(parts, cloned)
	}

	signature := stringValue(cloned["thoughtSignature"])
	isThought := isThoughtPart(cloned)

	for i := len(parts) - 1; i >= 0; i-- {
		existing := parts[i]
		if functionCallMap(existing) != nil {
			break
		}
		if isThoughtPart(existing) != isThought {
			continue
		}
		if signature != "" && stringValue(existing["thoughtSignature"]) != signature {
			continue
		}
		existingText := stringValue(existing["text"])
		if existingText == "" {
			continue
		}
		mergedText := existingText + text
		existing["text"] = mergedText
		parts[i] = mergeTopLevelMap(existing, cloned)
		parts[i]["text"] = mergedText
		return parts
	}

	return append(parts, cloned)
}

func containsToolCall(toolCalls []message.ToolCall, candidate message.ToolCall) bool {
	for _, existing := range toolCalls {
		if existing.Name == candidate.Name && existing.Input == candidate.Input {
			return true
		}
	}
	return false
}

func isThoughtPart(part map[string]any) bool {
	value, _ := part["thought"].(bool)
	return value
}

func usageFromRaw(response map[string]any) TokenUsage {
	usage, ok := response["usageMetadata"].(map[string]any)
	if !ok {
		return TokenUsage{}
	}

	return TokenUsage{
		InputTokens:         int64Value(usage["promptTokenCount"]),
		OutputTokens:        int64Value(usage["candidatesTokenCount"]),
		CacheCreationTokens: 0,
		CacheReadTokens:     int64Value(usage["cachedContentTokenCount"]),
	}
}

func cloneRawParts(parts []map[string]any) []map[string]any {
	cloned := make([]map[string]any, 0, len(parts))
	for _, part := range parts {
		cloned = append(cloned, cloneRawPart(part))
	}
	return cloned
}

func cloneRawPart(part map[string]any) map[string]any {
	if part == nil {
		return nil
	}

	data, err := json.Marshal(part)
	if err != nil {
		return map[string]any{}
	}

	cloned := make(map[string]any)
	if err := json.Unmarshal(data, &cloned); err != nil {
		return map[string]any{}
	}
	return cloned
}

func mergeTopLevelMap(dst, src map[string]any) map[string]any {
	merged := cloneRawPart(dst)
	for key, value := range src {
		if existingMap, ok := merged[key].(map[string]any); ok {
			if valueMap, ok := value.(map[string]any); ok {
				for nestedKey, nestedValue := range valueMap {
					existingMap[nestedKey] = nestedValue
				}
				merged[key] = existingMap
				continue
			}
		}
		merged[key] = value
	}
	return merged
}

func normalizedJSON(value any) string {
	if value == nil {
		return ""
	}
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(data)
}

func stringValue(value any) string {
	s, _ := value.(string)
	return s
}

func int64Value(value any) int64 {
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case float32:
		return int64(typed)
	case int:
		return int64(typed)
	case int32:
		return int64(typed)
	case int64:
		return typed
	case json.Number:
		v, err := typed.Int64()
		if err == nil {
			return v
		}
	}
	return 0
}

func readGeminiAPIError(resp *http.Response) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("gemini API request failed with status %d", resp.StatusCode)
	}

	var decoded struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &decoded); err == nil && decoded.Error.Message != "" {
		return fmt.Errorf("gemini API request failed with status %d: %s", resp.StatusCode, decoded.Error.Message)
	}

	messageText := strings.TrimSpace(string(body))
	if messageText == "" {
		messageText = resp.Status
	}
	return fmt.Errorf("gemini API request failed with status %d: %s", resp.StatusCode, messageText)
}

func parseJsonToMap(jsonStr string) (map[string]any, error) {
	var result map[string]any
	err := json.Unmarshal([]byte(jsonStr), &result)
	return result, err
}

func convertSchemaProperties(parameters map[string]interface{}) map[string]*genai.Schema {
	properties := make(map[string]*genai.Schema)

	for name, param := range parameters {
		properties[name] = convertToSchema(param)
	}

	return properties
}

func convertToSchema(param interface{}) *genai.Schema {
	schema := &genai.Schema{Type: genai.TypeString}

	paramMap, ok := param.(map[string]interface{})
	if !ok {
		return schema
	}

	if desc, ok := paramMap["description"].(string); ok {
		schema.Description = desc
	}

	typeVal, hasType := paramMap["type"]
	if !hasType {
		return schema
	}

	typeStr, ok := typeVal.(string)
	if !ok {
		return schema
	}

	schema.Type = mapJSONTypeToGenAI(typeStr)

	switch typeStr {
	case "array":
		schema.Items = processArrayItems(paramMap)
	case "object":
		if props, ok := paramMap["properties"].(map[string]interface{}); ok {
			schema.Properties = convertSchemaProperties(props)
		}
	}

	return schema
}

func processArrayItems(paramMap map[string]interface{}) *genai.Schema {
	items, ok := paramMap["items"].(map[string]interface{})
	if !ok {
		return nil
	}

	return convertToSchema(items)
}

func mapJSONTypeToGenAI(jsonType string) genai.Type {
	switch jsonType {
	case "string":
		return genai.TypeString
	case "number":
		return genai.TypeNumber
	case "integer":
		return genai.TypeInteger
	case "boolean":
		return genai.TypeBoolean
	case "array":
		return genai.TypeArray
	case "object":
		return genai.TypeObject
	default:
		return genai.TypeString
	}
}

func contains(s string, substrs ...string) bool {
	for _, substr := range substrs {
		if strings.Contains(strings.ToLower(s), strings.ToLower(substr)) {
			return true
		}
	}
	return false
}
