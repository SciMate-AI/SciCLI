package scimate

import (
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/SciMate-AI/scicli/internal/auth"
	"github.com/SciMate-AI/scicli/internal/config"
	"github.com/SciMate-AI/scicli/internal/mcpclient"
)

type Client struct {
	serverName string
	server     config.MCPServer
	auth       *auth.Service
}

type ArtifactList struct {
	Prefix string `json:"prefix"`
	Items  []struct {
		Name    string `json:"name"`
		GCSPath string `json:"gcs_path"`
		Size    string `json:"size,omitempty"`
		Updated string `json:"updated,omitempty"`
	} `json:"items"`
}

func NewClient(serverName string) (*Client, error) {
	serverName = strings.TrimSpace(serverName)
	if serverName == "" {
		serverName = "cae-agent"
	}
	cfg := config.Get()
	if cfg == nil {
		return nil, fmt.Errorf("config not loaded")
	}
	server, ok := cfg.MCPServers[serverName]
	if !ok {
		return nil, fmt.Errorf("mcp server %q is not configured", serverName)
	}
	authSvc, err := auth.NewService()
	if err != nil {
		return nil, err
	}
	return &Client{
		serverName: serverName,
		server:     server,
		auth:       authSvc,
	}, nil
}

func (c *Client) ListTools(ctx context.Context) ([]mcp.Tool, error) {
	client, err := mcpclient.New(c.server)
	if err != nil {
		return nil, err
	}
	defer client.Close()

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
	if _, err := client.Initialize(ctx, mcpclient.DefaultInitializeRequest()); err != nil {
		return nil, err
	}

	toolsResult, err := client.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		return nil, err
	}
	var chosen *mcp.Tool
	for idx := range toolsResult.Tools {
		if toolsResult.Tools[idx].Name == toolName {
			chosen = &toolsResult.Tools[idx]
			break
		}
	}
	if chosen == nil {
		return nil, fmt.Errorf("tool %q was not found on server %q", toolName, c.serverName)
	}

	preparedArgs, err := c.prepareArgs(args, chosen)
	if err != nil {
		return nil, err
	}
	req := mcp.CallToolRequest{}
	req.Params.Name = toolName
	req.Params.Arguments = preparedArgs
	result, err := client.CallTool(ctx, req)
	if err != nil && toolWantsAccessToken(chosen) && isAuthError(err) {
		refreshed, refreshErr := c.auth.Refresh()
		if refreshErr == nil {
			preparedArgs["access_token"] = refreshed.AccessToken
			req.Params.Arguments = preparedArgs
			return client.CallTool(ctx, req)
		}
	}
	return result, err
}

func (c *Client) StartRun(ctx context.Context, solverPath string, projectID string, runID string) (string, any, error) {
	content, err := os.ReadFile(solverPath)
	if err != nil {
		return "", nil, fmt.Errorf("failed to read solver: %w", err)
	}

	saveResult, err := c.CallTool(ctx, "save_run_solver_code", map[string]any{
		"project_id":   projectID,
		"run_id":       runID,
		"filename":     filepath.Base(solverPath),
		"code_content": string(content),
	})
	if err != nil {
		return "", nil, err
	}
	scriptPath, err := ExtractScriptGCSPath(saveResult)
	if err != nil {
		return "", nil, err
	}

	runResult, err := c.CallTool(ctx, "run_simulation_job", map[string]any{
		"project_id":      projectID,
		"run_id":          runID,
		"script_gcs_path": scriptPath,
	})
	if err != nil {
		return "", nil, err
	}
	return scriptPath, runResult, nil
}

func (c *Client) GetRunLog(ctx context.Context, projectID string, runID string) (string, error) {
	result, err := c.CallTool(ctx, "get_run_log", map[string]any{
		"project_id": projectID,
		"run_id":     runID,
	})
	if err != nil {
		return "", err
	}
	text := ExtractToolText(result)
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("run log is empty")
	}
	return text, nil
}

func (c *Client) ListRunArtifacts(ctx context.Context, projectID string, runID string) (*ArtifactList, error) {
	result, err := c.CallTool(ctx, "list_run_artifacts", map[string]any{
		"project_id": projectID,
		"run_id":     runID,
	})
	if err != nil {
		return nil, err
	}
	text := ExtractToolText(result)
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("artifact list is empty")
	}
	var parsed ArtifactList
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return nil, fmt.Errorf("failed to decode artifact list: %w", err)
	}
	return &parsed, nil
}

func (c *Client) ReadRunArtifactText(ctx context.Context, projectID string, runID string, relativePath string) (string, error) {
	result, err := c.CallTool(ctx, "read_run_artifact_text", map[string]any{
		"project_id":    projectID,
		"run_id":        runID,
		"relative_path": relativePath,
	})
	if err != nil {
		return "", err
	}
	text := ExtractToolText(result)
	if text == "" {
		return "", fmt.Errorf("artifact content is empty")
	}
	return text, nil
}

func (c *Client) prepareArgs(args map[string]any, tool *mcp.Tool) (map[string]any, error) {
	if args == nil {
		args = map[string]any{}
	}
	if toolWantsAccessToken(tool) {
		if _, exists := args["access_token"]; !exists {
			token, err := c.auth.RequireAccessToken()
			if err != nil {
				return nil, err
			}
			args["access_token"] = token
		}
	}
	return args, nil
}

func toolWantsAccessToken(tool *mcp.Tool) bool {
	if tool == nil {
		return false
	}
	_, exists := tool.InputSchema.Properties["access_token"]
	return exists
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

func ExtractScriptGCSPath(result *mcp.CallToolResult) (string, error) {
	text := ExtractToolText(result)
	if strings.HasPrefix(strings.TrimSpace(text), "gs://") {
		return strings.TrimSpace(text), nil
	}

	var payload struct {
		ScriptGCSPath string `json:"script_gcs_path"`
	}
	if err := json.Unmarshal([]byte(text), &payload); err == nil && strings.TrimSpace(payload.ScriptGCSPath) != "" {
		return strings.TrimSpace(payload.ScriptGCSPath), nil
	}
	return "", fmt.Errorf("save_run_solver_code returned no gs:// path")
}

func ComputeProjectID(workingDir string) string {
	base := filepath.Base(workingDir)
	base = sanitizeName(base)
	hash := sha1.Sum([]byte(workingDir))
	return fmt.Sprintf("%s-%s", base, hex.EncodeToString(hash[:])[:8])
}

func GenerateRunID(now time.Time) string {
	ts := now.UTC().Format("2006-01-02T15-04-05Z")
	buf := make([]byte, 2)
	if _, err := rand.Read(buf); err != nil {
		return ts
	}
	return fmt.Sprintf("%s-%x", ts, buf)
}

func sanitizeName(value string) string {
	if strings.TrimSpace(value) == "" {
		return "project"
	}
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
			continue
		}
		b.WriteRune('_')
	}
	return b.String()
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
