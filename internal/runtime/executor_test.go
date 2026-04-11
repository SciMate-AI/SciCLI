package runtimex

import (
	"os"
	"strings"
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

// ── buildEnvFlags ──────────────────────────────────────────────────────────

func TestBuildEnvFlagsEmpty(t *testing.T) {
	assert.Equal(t, "", buildEnvFlags(nil))
	assert.Equal(t, "", buildEnvFlags(map[string]string{}))
}

func TestBuildEnvFlagsLiteralValue(t *testing.T) {
	flags := buildEnvFlags(map[string]string{"FOO": "bar"})
	assert.Equal(t, " --env FOO=bar", flags)
}

func TestBuildEnvFlagsResolvesFromHostEnv(t *testing.T) {
	t.Setenv("SCICLI_TEST_KEY", "from-host")
	flags := buildEnvFlags(map[string]string{"SCICLI_TEST_KEY": ""})
	assert.Contains(t, flags, "--env SCICLI_TEST_KEY=from-host")
}

func TestBuildEnvFlagsOmitsUnresolvableKeys(t *testing.T) {
	// Ensure the key is definitely not set in the environment.
	os.Unsetenv("SCICLI_DEFINITELY_ABSENT_KEY_XYZ")
	flags := buildEnvFlags(map[string]string{"SCICLI_DEFINITELY_ABSENT_KEY_XYZ": ""})
	// Key cannot be resolved → should be omitted entirely.
	assert.Empty(t, flags)
}

func TestBuildEnvFlagsSortedDeterministically(t *testing.T) {
	t.Setenv("AAA", "1")
	t.Setenv("BBB", "2")
	flags := buildEnvFlags(map[string]string{"BBB": "", "AAA": ""})
	idxAAA := strings.Index(flags, "AAA")
	idxBBB := strings.Index(flags, "BBB")
	assert.True(t, idxAAA < idxBBB, "flags should be in sorted key order")
}

// ── Runtime-specific command synthesis ────────────────────────────────────

func TestPaperBananaCommandContainsReadinessCheck(t *testing.T) {
	exec := NewExecutor()
	svc := NewService()
	spec, ok := svc.Get("docker.paperbanana.v1")
	require.True(t, ok)

	plan := exec.BuildPlan(spec, ExecutionRequest{Workdir: "/tmp/workspace"})
	require.Len(t, plan.Commands, 1)
	cmd := plan.Commands[0]
	assert.Contains(t, cmd, "docker run --rm")
	assert.Contains(t, cmd, "ghcr.io/scimate-ai/paperbanana")
	assert.Contains(t, cmd, "/app/PaperBanana/main.py")
}

func TestPaperBananaCommandInjectsAPIKeyFromEnv(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test-openai")
	exec := NewExecutor()
	svc := NewService()
	spec, ok := svc.Get("docker.paperbanana.v1")
	require.True(t, ok)

	plan := exec.BuildPlan(spec, ExecutionRequest{Workdir: "/tmp/workspace"})
	require.Len(t, plan.Commands, 1)
	assert.Contains(t, plan.Commands[0], "--env OPENAI_API_KEY=sk-test-openai",
		"OPENAI_API_KEY set in host env should be forwarded to the container")
}

func TestOpenFOAMCommandContainsReadinessCheck(t *testing.T) {
	exec := NewExecutor()
	svc := NewService()
	spec, ok := svc.Get("docker.openfoam.v1")
	require.True(t, ok)

	plan := exec.BuildPlan(spec, ExecutionRequest{Workdir: "/tmp/workspace"})
	require.Len(t, plan.Commands, 1)
	cmd := plan.Commands[0]
	assert.Contains(t, cmd, "docker run --rm")
	assert.Contains(t, cmd, "fzj1214/openfoam-env")
	assert.Contains(t, cmd, "foamVersion")
}

func TestLatexCommandBuildsWithLatexmk(t *testing.T) {
	exec := NewExecutor()
	svc := NewService()
	spec, ok := svc.Get("docker.latexmk.v1")
	require.True(t, ok)

	plan := exec.BuildPlan(spec, ExecutionRequest{Workdir: "/tmp/workspace"})
	require.Len(t, plan.Commands, 1)
	cmd := plan.Commands[0]
	assert.Contains(t, cmd, "docker run --rm")
	assert.Contains(t, cmd, "ghcr.io/scimate-ai/latex-scicli")
	assert.Contains(t, cmd, "latexmk")
}

func TestLatexSpecHasNoEnvTemplate(t *testing.T) {
	// LaTeX compilation is offline; no API keys should be injected.
	svc := NewService()
	spec, ok := svc.Get("docker.latexmk.v1")
	require.True(t, ok)
	assert.Empty(t, spec.EnvTemplate,
		"latexmk is network_restricted and should not carry an EnvTemplate")
}

