package orchestrator

import (
	"testing"

	"github.com/SciMate-AI/scicli/internal/scientistbench"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBootstrapCaseSeedsInitialNode(t *testing.T) {
	svc := NewService()

	item, err := svc.BootstrapCase(scientistbench.Case{
		ID:    "case-1",
		Title: "Bootstrap me",
	})
	require.NoError(t, err)
	assert.Equal(t, "node-case-intake", item.GraphState.ActiveNode)
	assert.Equal(t, "chief_scientist", item.GraphState.ActiveRole)
	assert.Equal(t, "planning", item.GraphState.CurrentStage)
	assert.Contains(t, item.GraphState.PendingNodes, "node-research-plan")
}

func TestApplySignalAdvancesToNextNode(t *testing.T) {
	svc := NewService()

	item, err := svc.BootstrapCase(scientistbench.Case{ID: "case-2"})
	require.NoError(t, err)

	item, err = svc.ApplySignal(item, "case_initialized")
	require.NoError(t, err)
	assert.Equal(t, "node-research-plan", item.GraphState.ActiveNode)
	assert.Equal(t, "research_agent", item.GraphState.ActiveRole)
	assert.Contains(t, item.GraphState.CompletedNodes, "node-case-intake")
	assert.Equal(t, scientistbench.StatusRunning, item.Status)
}

func TestApplySignalTerminatesAggregateNode(t *testing.T) {
	svc := NewService()

	item := scientistbench.Case{
		ID:     "case-3",
		Status: scientistbench.StatusRunning,
		GraphState: scientistbench.GraphState{
			CurrentStage: "aggregation",
			ActiveNode:   "node-aggregate",
			ActiveRole:   "chief_scientist",
		},
	}

	item, err := svc.ApplySignal(item, string(scientistbench.SignalCaseResolved))
	require.NoError(t, err)
	assert.Equal(t, scientistbench.StatusResolved, item.Status)
	assert.Equal(t, scientistbench.SignalCaseResolved, item.Termination.Signal)
	assert.True(t, item.Termination.Resolved)
}

func TestWorkerProfileForRole(t *testing.T) {
	svc := NewService()

	profile, ok := svc.WorkerProfileForRole("research_agent")
	require.True(t, ok)
	assert.Equal(t, WorkerToolProfileResearch, profile.ToolProfile)
	assert.Contains(t, profile.PromptPreamble, "arxiv")

	profile, ok = svc.WorkerProfileForRole("idea_hater")
	require.True(t, ok)
	assert.Equal(t, WorkerToolProfileDeliberation, profile.ToolProfile)
	assert.Contains(t, profile.PromptPreamble, "reject")

	profile, ok = svc.WorkerProfileForRole("code_agent")
	require.True(t, ok)
	assert.Equal(t, WorkerToolProfileCode, profile.ToolProfile)

	profile, ok = svc.WorkerProfileForRole("execution_agent")
	require.True(t, ok)
	assert.Equal(t, WorkerToolProfileExecution, profile.ToolProfile)

	profile, ok = svc.WorkerProfileForRole("paper_writer")
	require.True(t, ok)
	assert.Equal(t, WorkerToolProfileCode, profile.ToolProfile)
	assert.Contains(t, profile.PromptPreamble, "LaTeX")

	profile, ok = svc.WorkerProfileForRole("domain_expert_reviewer")
	require.True(t, ok)
	assert.Equal(t, WorkerToolProfileDeliberation, profile.ToolProfile)

	profile, ok = svc.WorkerProfileForRole("judge_agent")
	require.True(t, ok)
	assert.Equal(t, WorkerToolProfileDeliberation, profile.ToolProfile)
}
