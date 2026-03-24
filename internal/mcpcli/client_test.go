package mcpcli

import (
	"testing"

	"github.com/SciMate-AI/scicli/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveServerName(t *testing.T) {
	tmpDir := t.TempDir()

	_, err := config.Load(tmpDir, false)
	require.NoError(t, err)

	cfg := config.Get()
	require.NotNil(t, cfg)

	cfg.MCPServers = map[string]config.MCPServer{}
	_, err = ResolveServerName("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no mcp servers are configured")

	cfg.MCPServers = map[string]config.MCPServer{
		"lab": {Type: config.MCPStdio, Command: "lab-mcp"},
	}
	name, err := ResolveServerName("")
	require.NoError(t, err)
	assert.Equal(t, "lab", name)

	name, err = ResolveServerName("lab")
	require.NoError(t, err)
	assert.Equal(t, "lab", name)

	cfg.MCPServers = map[string]config.MCPServer{
		"lab":    {Type: config.MCPStdio, Command: "lab-mcp"},
		"origin": {Type: config.MCPSse, URL: "https://example.test/sse"},
	}
	_, err = ResolveServerName("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "multiple mcp servers are configured")

	_, err = ResolveServerName("missing")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `mcp server "missing" is not configured`)
}
