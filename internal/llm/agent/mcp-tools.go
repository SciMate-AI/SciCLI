package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/SciMate-AI/scicli/internal/auth"
	"github.com/SciMate-AI/scicli/internal/config"
	"github.com/SciMate-AI/scicli/internal/llm/tools"
	"github.com/SciMate-AI/scicli/internal/logging"
	"github.com/SciMate-AI/scicli/internal/mcpclient"
	"github.com/SciMate-AI/scicli/internal/permission"
	"github.com/SciMate-AI/scicli/internal/version"

	"github.com/mark3labs/mcp-go/mcp"
)

const (
	maxMCPContextChars     = 4000
	maxMCPContextLines     = 120
	maxMCPJSONItems        = 24
	maxMCPJSONStringChars  = 320
	maxMCPJSONNestedDepth  = 6
	maxMCPPreviewLineChars = 240
)

type MCPToolResponseMetadata struct {
	RawContent    string `json:"raw_content,omitempty"`
	OriginalChars int    `json:"original_chars,omitempty"`
	OriginalLines int    `json:"original_lines,omitempty"`
	Compacted     bool   `json:"compacted,omitempty"`
}

type mcpTool struct {
	mcpName     string
	tool        mcp.Tool
	mcpConfig   config.MCPServer
	permissions permission.Service
}

type MCPClient interface {
	Initialize(
		ctx context.Context,
		request mcp.InitializeRequest,
	) (*mcp.InitializeResult, error)
	ListTools(ctx context.Context, request mcp.ListToolsRequest) (*mcp.ListToolsResult, error)
	CallTool(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error)
	Close() error
}

func (b *mcpTool) Info() tools.ToolInfo {
	description := strings.TrimSpace(b.tool.Description)
	if description == "" {
		description = fmt.Sprintf("Remote MCP tool exposed by the %s server.", b.mcpName)
	} else {
		description = fmt.Sprintf("Remote MCP tool from %s: %s", b.mcpName, description)
	}
	if _, ok := b.tool.InputSchema.Properties["access_token"]; ok {
		description += " Requires SciCLI login; access_token is injected automatically."
	}
	return tools.ToolInfo{
		Name:        fmt.Sprintf("%s_%s", b.mcpName, b.tool.Name),
		Description: description,
		Parameters:  b.tool.InputSchema.Properties,
		Required:    b.tool.InputSchema.Required,
	}
}

func runTool(ctx context.Context, c MCPClient, tool mcp.Tool, input string) (tools.ToolResponse, error) {
	defer c.Close()
	if client, ok := c.(mcpclient.Client); ok {
		if err := mcpclient.EnsureStarted(ctx, client); err != nil {
			return tools.NewTextErrorResponse(err.Error()), nil
		}
	}
	initRequest := mcp.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcp.Implementation{
		Name:    "SciCLI",
		Version: version.Version,
	}

	_, err := c.Initialize(ctx, initRequest)
	if err != nil {
		return tools.NewTextErrorResponse(err.Error()), nil
	}

	toolRequest := mcp.CallToolRequest{}
	toolRequest.Params.Name = tool.Name
	var args map[string]any
	if err = json.Unmarshal([]byte(input), &args); err != nil {
		return tools.NewTextErrorResponse(fmt.Sprintf("error parsing parameters: %s", err)), nil
	}
	var authSvc *auth.Service
	needsAccessToken := false
	if _, ok := tool.InputSchema.Properties["access_token"]; ok {
		needsAccessToken = true
		authSvc, authErr := auth.NewService()
		if authErr != nil {
			return tools.NewTextErrorResponse(authErr.Error()), nil
		}
		if !hasUsableAccessToken(args) {
			token, tokenErr := authSvc.RequireAccessToken()
			if tokenErr != nil {
				return tools.NewTextErrorResponse(tokenErr.Error()), nil
			}
			args["access_token"] = token
		}
	}
	toolRequest.Params.Arguments = args
	result, err := c.CallTool(ctx, toolRequest)
	if err != nil {
		if needsAccessToken && authSvc != nil && isAuthError(err) {
			refreshed, refreshErr := authSvc.Refresh()
			if refreshErr == nil {
				args["access_token"] = refreshed.AccessToken
				toolRequest.Params.Arguments = args
				result, err = c.CallTool(ctx, toolRequest)
			}
		}
	}
	if err != nil {
		return tools.NewTextErrorResponse(err.Error()), nil
	}

	parts := make([]string, 0, len(result.Content))
	for _, v := range result.Content {
		switch item := v.(type) {
		case mcp.TextContent:
			parts = append(parts, item.Text)
		case *mcp.TextContent:
			parts = append(parts, item.Text)
		default:
			parts = append(parts, fmt.Sprintf("%v", v))
		}
	}
	output := strings.TrimSpace(strings.Join(parts, "\n\n"))

	return compactMCPToolResponse(tool.Name, output), nil
}

func compactMCPToolResponse(toolName, output string) tools.ToolResponse {
	return compactToolResponseForModelContext(toolName, tools.NewTextResponse(output))
}

func summarizeMCPOutput(toolName, output string) string {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return trimmed
	}
	if len(trimmed) <= maxMCPContextChars && lineCount(trimmed) <= maxMCPContextLines {
		return trimmed
	}

	if summarized, ok := summarizeMCPJSON(trimmed); ok {
		return truncateMCPText(
			fmt.Sprintf(
				"[MCP tool output compacted for model context from %s: %s]\n%s",
				toolName,
				mcpSummaryStats(trimmed),
				summarized,
			),
			trimmed,
		)
	}

	preview := truncatePreviewLines(trimmed, maxMCPContextLines/3)
	return truncateMCPText(
		fmt.Sprintf(
			"[MCP tool output compacted for model context from %s: %s]\n%s",
			toolName,
			mcpSummaryStats(trimmed),
			preview,
		),
		trimmed,
	)
}

func summarizeMCPJSON(output string) (string, bool) {
	var value any
	if err := json.Unmarshal([]byte(output), &value); err != nil {
		return "", false
	}
	compact := sanitizeMCPJSONValue("", value, 0)
	data, err := json.MarshalIndent(compact, "", "  ")
	if err != nil {
		return "", false
	}
	return string(data), true
}

func sanitizeMCPJSONValue(path string, value any, depth int) any {
	if depth >= maxMCPJSONNestedDepth {
		return fmt.Sprintf("[omitted nested data at %s]", pathLabel(path))
	}

	switch item := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(item))
		for key := range item {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		out := make(map[string]any, len(item))
		for _, key := range keys {
			nextPath := key
			if path != "" {
				nextPath = path + "." + key
			}
			out[key] = sanitizeMCPJSONValue(nextPath, item[key], depth+1)
		}
		return out
	case []any:
		if len(item) > maxMCPJSONItems {
			items := make([]any, 0, maxMCPJSONItems+1)
			for i := 0; i < maxMCPJSONItems; i++ {
				nextPath := fmt.Sprintf("%s[%d]", pathLabel(path), i)
				items = append(items, sanitizeMCPJSONValue(nextPath, item[i], depth+1))
			}
			items = append(items, fmt.Sprintf("[omitted %d more items]", len(item)-maxMCPJSONItems))
			return items
		}
		items := make([]any, 0, len(item))
		for i, v := range item {
			nextPath := fmt.Sprintf("%s[%d]", pathLabel(path), i)
			items = append(items, sanitizeMCPJSONValue(nextPath, v, depth+1))
		}
		return items
	case string:
		return sanitizeMCPJSONString(path, item)
	default:
		return value
	}
}

func sanitizeMCPJSONString(path, value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return value
	}
	if isBulkyMCPField(path) || len(trimmed) > maxMCPJSONStringChars || lineCount(trimmed) > 12 {
		return fmt.Sprintf("[omitted %d chars at %s]", len(value), pathLabel(path))
	}
	return trimLineLength(trimmed, maxMCPPreviewLineChars)
}

func isBulkyMCPField(path string) bool {
	path = strings.ToLower(path)
	bulkyFields := []string{
		"mol_block",
		"molblock",
		"pdb",
		"sdf",
		"xyz",
		"svg",
		"conformer",
		"coordinate",
		"coords",
		"base64",
		"binary",
		"image",
	}
	for _, field := range bulkyFields {
		if strings.Contains(path, field) {
			return true
		}
	}
	return false
}

func truncateMCPText(summary, original string) string {
	summary = trimTextByLines(summary, maxMCPContextLines)
	if len(summary) > maxMCPContextChars {
		summary = strings.TrimSpace(summary[:maxMCPContextChars])
	}
	if summary == strings.TrimSpace(original) {
		return summary
	}
	return strings.TrimSpace(summary) + fmt.Sprintf(
		"\n\n[full MCP output omitted from model context: %s]",
		mcpSummaryStats(original),
	)
}

func truncatePreviewLines(content string, maxLines int) string {
	if maxLines <= 0 {
		return ""
	}
	lines := strings.Split(content, "\n")
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	for i, line := range lines {
		lines[i] = trimLineLength(line, maxMCPPreviewLineChars)
	}
	return strings.Join(lines, "\n")
}

func trimTextByLines(content string, maxLines int) string {
	lines := strings.Split(content, "\n")
	if len(lines) <= maxLines {
		return strings.TrimSpace(content)
	}
	return strings.TrimSpace(strings.Join(lines[:maxLines], "\n"))
}

func trimLineLength(line string, maxChars int) string {
	if maxChars <= 0 || len(line) <= maxChars {
		return line
	}
	return strings.TrimSpace(line[:maxChars]) + " ..."
}

func mcpSummaryStats(content string) string {
	return fmt.Sprintf("%d chars, %d lines", len(content), lineCount(content))
}

func lineCount(content string) int {
	if content == "" {
		return 0
	}
	return strings.Count(content, "\n") + 1
}

func pathLabel(path string) string {
	if strings.TrimSpace(path) == "" {
		return "root"
	}
	return path
}

func isAuthError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unauthorized") ||
		strings.Contains(msg, "access_token") ||
		strings.Contains(msg, "access token") ||
		strings.Contains(msg, "jwt")
}

func hasUsableAccessToken(args map[string]any) bool {
	if args == nil {
		return false
	}
	raw, exists := args["access_token"]
	if !exists {
		return false
	}
	token, ok := raw.(string)
	return ok && strings.TrimSpace(token) != ""
}

func (b *mcpTool) Run(ctx context.Context, params tools.ToolCall) (tools.ToolResponse, error) {
	sessionID, messageID := tools.GetContextValues(ctx)
	if sessionID == "" || messageID == "" {
		return tools.ToolResponse{}, fmt.Errorf("session ID and message ID are required for creating a new file")
	}
	permissionDescription := fmt.Sprintf("execute %s with the following parameters: %s", b.Info().Name, params.Input)
	p := b.permissions.Request(
		permission.CreatePermissionRequest{
			SessionID:   sessionID,
			Path:        config.WorkingDirectory(),
			ToolName:    b.Info().Name,
			Action:      "execute",
			Description: permissionDescription,
			Params:      params.Input,
		},
	)
	if !p {
		return tools.NewTextErrorResponse("permission denied"), nil
	}

	switch b.mcpConfig.Type {
	case config.MCPStdio:
		c, err := mcpclient.New(b.mcpConfig)
		if err != nil {
			return tools.NewTextErrorResponse(err.Error()), nil
		}
		return runTool(ctx, c, b.tool, params.Input)
	case config.MCPSse:
		c, err := mcpclient.New(b.mcpConfig)
		if err != nil {
			return tools.NewTextErrorResponse(err.Error()), nil
		}
		return runTool(ctx, c, b.tool, params.Input)
	case config.MCPStreamableHTTP:
		c, err := mcpclient.New(b.mcpConfig)
		if err != nil {
			return tools.NewTextErrorResponse(err.Error()), nil
		}
		return runTool(ctx, c, b.tool, params.Input)
	}

	return tools.NewTextErrorResponse("invalid mcp type"), nil
}

func NewMcpTool(name string, tool mcp.Tool, permissions permission.Service, mcpConfig config.MCPServer) tools.BaseTool {
	return &mcpTool{
		mcpName:     name,
		tool:        tool,
		mcpConfig:   mcpConfig,
		permissions: permissions,
	}
}

var mcpTools []tools.BaseTool

func getTools(ctx context.Context, name string, m config.MCPServer, permissions permission.Service, c MCPClient) []tools.BaseTool {
	var stdioTools []tools.BaseTool
	if client, ok := c.(mcpclient.Client); ok {
		if err := mcpclient.EnsureStarted(ctx, client); err != nil {
			logging.Error("error starting mcp client", "error", err)
			return stdioTools
		}
	}
	initRequest := mcp.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcp.Implementation{
		Name:    "SciCLI",
		Version: version.Version,
	}

	_, err := c.Initialize(ctx, initRequest)
	if err != nil {
		logging.Error("error initializing mcp client", "error", err)
		return stdioTools
	}
	toolsRequest := mcp.ListToolsRequest{}
	tools, err := c.ListTools(ctx, toolsRequest)
	if err != nil {
		logging.Error("error listing tools", "error", err)
		return stdioTools
	}
	for _, t := range tools.Tools {
		stdioTools = append(stdioTools, NewMcpTool(name, t, permissions, m))
	}
	defer c.Close()
	return stdioTools
}

func GetMcpTools(ctx context.Context, permissions permission.Service) []tools.BaseTool {
	if len(mcpTools) > 0 {
		return mcpTools
	}
	for name, m := range config.Get().MCPServers {
		switch m.Type {
		case config.MCPStdio, config.MCPSse, config.MCPStreamableHTTP:
			c, err := mcpclient.New(m)
			if err != nil {
				logging.Error("error creating mcp client", "error", err)
				continue
			}
			mcpTools = append(mcpTools, getTools(ctx, name, m, permissions, c)...)
		}
	}

	return mcpTools
}
