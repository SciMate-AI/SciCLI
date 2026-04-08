package runtimex

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecutorBuildPlansProducesDryRunCommands(t *testing.T) {
	registry := NewService()
	exec := NewExecutor()

	specs := registry.ForNodeRole("node-execution", "execution_agent")
	plans := exec.BuildPlans(specs, ExecutionRequest{
		NodeID:       "node-execution",
		RoleID:       "execution_agent",
		Workdir:      "D:/javascript/cae-agent-2026/scicli",
		RuntimeHints: []string{"use smoke test first"},
	})

	require.NotEmpty(t, plans)
	assert.True(t, plans[0].DryRun)
	assert.Contains(t, plans[0].Commands[0], "docker run --rm")
	assert.Contains(t, plans[0].Commands[0], "use smoke test first")
}

func TestExecutorBuildPlanFallsBackWhenWorkdirMissing(t *testing.T) {
	exec := NewExecutor()

	plan := exec.BuildPlan(Spec{
		ID:             "docker.python-sci.v1",
		Image:          "python:3.12-slim",
		ResourceLimits: ResourceLimits{TimeoutMinutes: 5},
	}, ExecutionRequest{})

	require.NotEmpty(t, plan.OutputDir)
	assert.NotEmpty(t, plan.Commands)
}
