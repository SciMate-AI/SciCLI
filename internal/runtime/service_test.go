package runtimex

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestForNodeRoleFiltersCompatibleRuntimes(t *testing.T) {
	svc := NewService()

	// node-execution / execution_agent: benchmark-runner, openfoam, python-sci
	items := svc.ForNodeRole("node-execution", "execution_agent")
	require.NotEmpty(t, items)
	ids := make([]string, len(items))
	for i, it := range items {
		ids[i] = it.ID
	}
	assert.Contains(t, ids, "docker.benchmark-runner.v1")
	assert.Contains(t, ids, "docker.openfoam.v1")
	assert.Contains(t, ids, "docker.python-sci.v1")

	// node-paper-draft / paper_writer: only latexmk
	items = svc.ForNodeRole("node-paper-draft", "paper_writer")
	require.Len(t, items, 1)
	assert.Equal(t, "docker.latexmk.v1", items[0].ID)

	// node-analysis-figures / figure_agent: only paperbanana
	items = svc.ForNodeRole("node-analysis-figures", "figure_agent")
	require.Len(t, items, 1)
	assert.Equal(t, "docker.paperbanana.v1", items[0].ID)
}

func TestAllRegisteredSpecsHaveRequiredFields(t *testing.T) {
	svc := NewService()
	for _, spec := range svc.List() {
		assert.NotEmpty(t, spec.ID, "spec missing ID")
		assert.NotEmpty(t, spec.Image, "spec %s missing Image", spec.ID)
		assert.NotEmpty(t, spec.AllowedRoles, "spec %s missing AllowedRoles", spec.ID)
		assert.NotEmpty(t, spec.AllowedNodes, "spec %s missing AllowedNodes", spec.ID)
		assert.Greater(t, spec.ResourceLimits.TimeoutMinutes, 0,
			"spec %s has zero TimeoutMinutes", spec.ID)
	}
}

func TestPaperBananaSpecHasEnvTemplate(t *testing.T) {
	svc := NewService()
	spec, ok := svc.Get("docker.paperbanana.v1")
	require.True(t, ok)
	assert.Contains(t, spec.EnvTemplate, "OPENAI_API_KEY")
	assert.Contains(t, spec.EnvTemplate, "ANTHROPIC_API_KEY")
	// PaperBanana needs network access to reach LLM APIs
	assert.NotContains(t, spec.SafetyFlags, "network_restricted",
		"paperbanana must NOT be network_restricted")
}

func TestOpenFOAMSpecUsesCustomImage(t *testing.T) {
	svc := NewService()
	spec, ok := svc.Get("docker.openfoam.v1")
	require.True(t, ok)
	assert.Contains(t, spec.Image, "openfoam",
		"openfoam spec should reference an openfoam image")
}

func TestLatexSpecUsesCustomImage(t *testing.T) {
	svc := NewService()
	spec, ok := svc.Get("docker.latexmk.v1")
	require.True(t, ok)
	assert.Contains(t, spec.Image, "ghcr.io/scimate-ai/latex-scicli",
		"latexmk should use the custom scicli image, not bare texlive")
}

