package runtimex

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestForNodeRoleFiltersCompatibleRuntimes(t *testing.T) {
	svc := NewService()

	items := svc.ForNodeRole("node-execution", "execution_agent")
	require.NotEmpty(t, items)
	assert.Equal(t, "docker.benchmark-runner.v1", items[0].ID)

	items = svc.ForNodeRole("node-paper-draft", "paper_writer")
	require.Len(t, items, 1)
	assert.Equal(t, "docker.latexmk.v1", items[0].ID)
}
