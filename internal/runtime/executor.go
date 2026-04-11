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
		// Use sh (not bash) to source the OF environment — the OF bashrc uses
		// bash-specific constructs that segfault on ARM64 when sourced inside bash -c.
		// sh -c works reliably on all platforms.
		innerCommand = "sh -c \". /usr/lib/openfoam/openfoam2412/etc/bashrc 2>/dev/null; foamVersion && simpleFoam -help 2>&1 | head -1 && echo OpenFOAM runtime ready\""
	case "docker.latexmk.v1":
		// scicli-latex-build compiles paper.tex and emits a JSON compilation report.
		innerCommand = "bash -lc \"ls -la /workspace; scicli-latex-build paper.tex || latexmk -pdf paper.tex || true\""
	case "docker.python-sci.v1":
		innerCommand = "bash -lc \"python --version; if [ -f pyproject.toml ]; then python -m pytest -q || true; fi\""
	case "docker.benchmark-runner.v1":
		innerCommand = "bash -lc \"python -m benchmark_runner --help || true\""
	case "docker.paperbanana.v1":
		// PaperBanana must be called with method_text and caption at generation time;
		// this command verifies the runtime is healthy.
		innerCommand = "bash -lc \"python /app/PaperBanana/main.py --help || true; echo PaperBanana runtime ready\""
	}
	if len(hints) > 0 {
		innerCommand += " # hints: " + strings.Join(hints, " | ")
	}

	// Build --env flags for keys listed in EnvTemplate.
	// An empty-string value means "forward from host environment".
	// A non-empty value is used as a literal override.
	envFlags := buildEnvFlags(spec.EnvTemplate)

	return fmt.Sprintf(
		"docker run --rm%s -v \"%s:/workspace\" -v \"%s:/outputs\" -w /workspace %s %s",
		envFlags,
		mountedWorkdir,
		mountedOutput,
		spec.Image,
		innerCommand,
	)
}

// buildEnvFlags converts an EnvTemplate map into a space-prefixed string of
// --env flags suitable for insertion into a docker run command.
//
// Resolution order for each key (stops at first non-empty hit):
//  1. A non-empty literal value in the EnvTemplate map itself.
//  2. The host environment variable of the same name.
//  3. The scicli config (covers the case where the user set the API key in
//     ~/.config/scicli/config.json rather than as a shell variable).
//
// Keys that resolve to an empty string are omitted so the container does not
// receive an empty variable that might override a default baked into the image.
func buildEnvFlags(envTemplate map[string]string) string {
	if len(envTemplate) == 0 {
		return ""
	}
	keys := make([]string, 0, len(envTemplate))
	for k := range envTemplate {
		keys = append(keys, k)
	}
	// Sort for deterministic output.
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	var sb strings.Builder
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		v := resolveEnvVar(k, strings.TrimSpace(envTemplate[k]))
		if v == "" {
			continue // omit unresolved keys
		}
		fmt.Fprintf(&sb, " --env %s=%s", k, v)
	}
	return sb.String()
}

// resolveEnvVar resolves the effective value for an environment variable key
// using the three-step precedence described in buildEnvFlags.
func resolveEnvVar(key, literalValue string) string {
	if literalValue != "" {
		return literalValue
	}
	if v := os.Getenv(key); v != "" {
		return v
	}
	return resolveEnvVarFromConfig(key)
}

// resolveEnvVarFromConfig maps well-known environment variable names to the
// corresponding scicli config fields so that API keys configured in
// ~/.config/scicli/config.json are automatically forwarded to Docker runtimes
// that need them (e.g. PaperBanana needs an OpenAI or Anthropic key).
func resolveEnvVarFromConfig(key string) string {
	cfg := config.Get()
	if cfg == nil {
		return ""
	}
	switch key {
	case "OPENAI_API_KEY":
		if p, ok := cfg.Providers["openai"]; ok {
			return strings.TrimSpace(p.APIKey)
		}
	case "ANTHROPIC_API_KEY":
		if p, ok := cfg.Providers["anthropic"]; ok {
			return strings.TrimSpace(p.APIKey)
		}
	case "OPENAI_BASE_URL":
		if p, ok := cfg.Providers["openai"]; ok {
			return strings.TrimSpace(p.BaseURL)
		}
	case "GEMINI_API_KEY":
		if p, ok := cfg.Providers["gemini"]; ok {
			return strings.TrimSpace(p.APIKey)
		}
	}
	return ""
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
