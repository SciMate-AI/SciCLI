package cmd

import (
	"testing"

	"github.com/SciMate-AI/scicli/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMcpPresetConfigZotero(t *testing.T) {
	server, note, err := mcpPresetConfig("zotero", "")
	require.NoError(t, err)
	assert.Equal(t, config.MCPStdio, server.Type)
	assert.Equal(t, "uvx", server.Command)
	assert.Equal(t, []string{"zotero-mcp"}, server.Args)
	assert.Contains(t, note, "uvx")
}
