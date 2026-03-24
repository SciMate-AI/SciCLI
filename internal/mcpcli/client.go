package mcpcli

import (
	"context"
	"fmt"
	"strings"

	"github.com/SciMate-AI/scicli/internal/config"
	"github.com/SciMate-AI/scicli/internal/mcpclient"
	"github.com/mark3labs/mcp-go/mcp"
)

type Client struct {
	serverName string
	server     config.MCPServer
}

func NewClient(serverName string) (*Client, error) {
	serverName = strings.TrimSpace(serverName)
	resolvedServerName, err := ResolveServerName(serverName)
	if err != nil {
		return nil, err
	}

	cfg := config.Get()
	if cfg == nil {
		return nil, fmt.Errorf("config not loaded")
	}

	return &Client{
		serverName: resolvedServerName,
		server:     cfg.MCPServers[resolvedServerName],
	}, nil
}

func ResolveServerName(serverName string) (string, error) {
	serverName = strings.TrimSpace(serverName)
	cfg := config.Get()
	if cfg == nil {
		return "", fmt.Errorf("config not loaded")
	}

	if serverName != "" {
		if _, ok := cfg.MCPServers[serverName]; !ok {
			return "", fmt.Errorf("mcp server %q is not configured", serverName)
		}
		return serverName, nil
	}

	switch len(cfg.MCPServers) {
	case 0:
		return "", fmt.Errorf("no mcp servers are configured")
	case 1:
		for name := range cfg.MCPServers {
			return name, nil
		}
	}

	return "", fmt.Errorf("multiple mcp servers are configured; pass --server")
}

func (c *Client) ListTools(ctx context.Context) ([]mcp.Tool, error) {
	client, err := mcpclient.New(c.server)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	if err := mcpclient.EnsureStarted(ctx, client); err != nil {
		return nil, err
	}
	if _, err := client.Initialize(ctx, mcpclient.DefaultInitializeRequest()); err != nil {
		return nil, err
	}

	result, err := client.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		return nil, err
	}
	return result.Tools, nil
}

func (c *Client) CallTool(ctx context.Context, toolName string, args map[string]any) (*mcp.CallToolResult, error) {
	client, err := mcpclient.New(c.server)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	if err := mcpclient.EnsureStarted(ctx, client); err != nil {
		return nil, err
	}
	if _, err := client.Initialize(ctx, mcpclient.DefaultInitializeRequest()); err != nil {
		return nil, err
	}

	req := mcp.CallToolRequest{}
	req.Params.Name = toolName
	req.Params.Arguments = args
	return client.CallTool(ctx, req)
}

func ExtractToolText(result *mcp.CallToolResult) string {
	if result == nil {
		return ""
	}

	for _, item := range result.Content {
		switch content := item.(type) {
		case mcp.TextContent:
			return content.Text
		case *mcp.TextContent:
			return content.Text
		}
	}
	return ""
}
