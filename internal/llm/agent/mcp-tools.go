package agent

import (
	"context"
	"encoding/json"
	"fmt"
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
		if _, exists := args["access_token"]; !exists {
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

	output := ""
	for _, v := range result.Content {
		switch item := v.(type) {
		case mcp.TextContent:
			output = item.Text
		case *mcp.TextContent:
			output = item.Text
		default:
			output = fmt.Sprintf("%v", v)
		}
	}

	return tools.NewTextResponse(output), nil
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
