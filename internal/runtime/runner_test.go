package runtimex

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubCommandRunner struct {
	stdout   string
	stderr   string
	exitCode int
	err      error
}

func (s stubCommandRunner) Run(context.Context, string, int) (string, string, int, error) {
	return s.stdout, s.stderr, s.exitCode, s.err
}

func TestRunnerRunPlanDryRunWhenExecuteDisabled(t *testing.T) {
	runner := newRunnerWithCommandRunner(nil, stubCommandRunner{})

	result := runner.RunPlan(context.Background(), Plan{
		RuntimeID: "docker.python-sci.v1",
		Commands:  []string{"docker run --rm python:3.12-slim python --version"},
	}, RunRequest{})

	assert.True(t, result.DryRun)
	assert.False(t, result.Executed)
	assert.False(t, result.Succeeded)
	assert.Equal(t, "Runtime plan prepared but not executed", result.Summary)
}

func TestRunnerRunPlanExecutesCommand(t *testing.T) {
	runner := newRunnerWithCommandRunner(nil, stubCommandRunner{
		stdout:   "ok",
		exitCode: 0,
	})

	result := runner.RunPlan(context.Background(), Plan{
		RuntimeID:      "docker.python-sci.v1",
		Commands:       []string{"docker run --rm python:3.12-slim python --version"},
		TimeoutMinutes: 5,
	}, RunRequest{Execute: true})

	require.True(t, result.Executed)
	assert.False(t, result.DryRun)
	assert.True(t, result.Succeeded)
	assert.Equal(t, "Runtime execution completed", result.Summary)
	assert.Equal(t, "ok", result.Stdout)
}
