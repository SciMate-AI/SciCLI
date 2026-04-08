package runtimex

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/SciMate-AI/scicli/internal/config"
)

type ExecutionRequest struct {
	NodeID       string   `json:"node_id,omitempty"`
	RoleID       string   `json:"role_id,omitempty"`
	Workdir      string   `json:"workdir,omitempty"`
	OutputDir    string   `json:"output_dir,omitempty"`
	RuntimeHints []string `json:"runtime_hints,omitempty"`
}

type Plan struct {
	RuntimeID      string   `json:"runtime_id,omitempty"`
	Summary        string   `json:"summary,omitempty"`
	Commands       []string `json:"commands,omitempty"`
	OutputDir      string   `json:"output_dir,omitempty"`
	TimeoutMinutes int      `json:"timeout_minutes,omitempty"`
	SafetyFlags    []string `json:"safety_flags,omitempty"`
	DryRun         bool     `json:"dry_run,omitempty"`
}

type Executor interface {
	BuildPlans(specs []Spec, req ExecutionRequest) []Plan
	BuildPlan(spec Spec, req ExecutionRequest) Plan
}

type executor struct{}

func NewExecutor() Executor {
	return &executor{}
}

func (e *executor) BuildPlans(specs []Spec, req ExecutionRequest) []Plan {
	out := make([]Plan, 0, len(specs))
	for _, spec := range specs {
		out = append(out, e.BuildPlan(spec, req))
	}
	return out
}

func (e *executor) BuildPlan(spec Spec, req ExecutionRequest) Plan {
	workdir := strings.TrimSpace(req.Workdir)
	if workdir == "" {
		workdir = defaultRuntimeWorkdir()
	}
	outputDir := strings.TrimSpace(req.OutputDir)
	if outputDir == "" {
		outputDir = filepath.Join(workdir, ".scicli", "runtime", sanitizeRuntimeID(spec.ID))
	}

	command := synthesizeDockerCommand(spec, workdir, outputDir, req.RuntimeHints)
	return Plan{
		RuntimeID:      spec.ID,
		Summary:        summarizeSpec(spec),
		Commands:       []string{command},
		OutputDir:      outputDir,
		TimeoutMinutes: spec.ResourceLimits.TimeoutMinutes,
		SafetyFlags:    append([]string{}, spec.SafetyFlags...),
		DryRun:         true,
	}
}

func synthesizeDockerCommand(spec Spec, workdir, outputDir string, hints []string) string {
	mountedWorkdir := strings.ReplaceAll(workdir, `\`, `/`)
	mountedOutput := strings.ReplaceAll(outputDir, `\`, `/`)

	innerCommand := "echo Runtime ready"
	switch spec.ID {
	case "docker.openfoam.v1":
		innerCommand = "bash -lc \"foamSystemCheck || true; ls -la /workspace; echo OpenFOAM runtime prepared\""
	case "docker.latexmk.v1":
		innerCommand = "bash -lc \"ls -la /workspace; latexmk -pdf paper.tex || true\""
	case "docker.python-sci.v1":
		innerCommand = "bash -lc \"python --version; if [ -f pyproject.toml ]; then python -m pytest -q || true; fi\""
	case "docker.benchmark-runner.v1":
		innerCommand = "bash -lc \"python -m benchmark_runner --help || true\""
	}
	if len(hints) > 0 {
		innerCommand += " # hints: " + strings.Join(hints, " | ")
	}

	return fmt.Sprintf(
		"docker run --rm -v \"%s:/workspace\" -v \"%s:/outputs\" -w /workspace %s %s",
		mountedWorkdir,
		mountedOutput,
		spec.Image,
		innerCommand,
	)
}

func summarizeSpec(spec Spec) string {
	return strings.TrimSpace(fmt.Sprintf(
		"%s using %s with timeout=%dmin cpu=%d mem=%dGB",
		spec.ID,
		spec.Image,
		spec.ResourceLimits.TimeoutMinutes,
		spec.ResourceLimits.CPU,
		spec.ResourceLimits.MemoryGB,
	))
}

func sanitizeRuntimeID(id string) string {
	id = strings.TrimSpace(strings.ToLower(id))
	id = strings.ReplaceAll(id, ".", "-")
	id = strings.ReplaceAll(id, "/", "-")
	return id
}

func defaultRuntimeWorkdir() string {
	if cfg := config.Get(); cfg != nil {
		if dir := strings.TrimSpace(cfg.WorkingDir); dir != "" {
			return dir
		}
	}
	if dir, err := os.Getwd(); err == nil {
		if dir = strings.TrimSpace(dir); dir != "" {
			return dir
		}
	}
	return "."
}
