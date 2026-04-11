package runtimex

import (
	"slices"
	"strings"
)

type ResourceLimits struct {
	CPU            int `json:"cpu,omitempty"`
	MemoryGB       int `json:"memory_gb,omitempty"`
	TimeoutMinutes int `json:"timeout_minutes,omitempty"`
}

type Spec struct {
	ID               string            `json:"runtime_id"`
	RuntimeClass     string            `json:"runtime_class,omitempty"`
	Image            string            `json:"image,omitempty"`
	EntrypointPolicy string            `json:"entrypoint_policy,omitempty"`
	Mounts           []string          `json:"mounts,omitempty"`
	EnvTemplate      map[string]string `json:"env_template,omitempty"`
	ResourceLimits   ResourceLimits    `json:"resource_limits,omitempty"`
	AllowedRoles     []string          `json:"allowed_roles,omitempty"`
	AllowedNodes     []string          `json:"allowed_nodes,omitempty"`
	ArtifactsEmitted []string          `json:"artifacts_emitted,omitempty"`
	SafetyFlags      []string          `json:"safety_flags,omitempty"`
}

type Service interface {
	List() []Spec
	ForNodeRole(nodeID string, roleID string) []Spec
	Get(id string) (Spec, bool)
}

type service struct {
	specs map[string]Spec
}

func NewService() Service {
	items := []Spec{
		{
			// Pre-built image provided by the project; includes OpenFOAM 2412,
			// Python 3.12, matplotlib, numpy and ParaFoam.
			// Source: https://hub.docker.com/r/fzj1214/openfoam-env
			ID:               "docker.openfoam.v1",
			RuntimeClass:     "docker",
			Image:            "fzj1214/openfoam-env:latest",
			EntrypointPolicy: "restricted",
			ResourceLimits:   ResourceLimits{CPU: 8, MemoryGB: 16, TimeoutMinutes: 60},
			AllowedRoles:     []string{"execution_agent"},
			AllowedNodes:     []string{"node-execution"},
			ArtifactsEmitted: []string{"docker_log", "result_bundle"},
			SafetyFlags:      []string{"network_restricted", "readonly_inputs"},
		},
		{
			// Built from docker/Dockerfile.latex; extends texlive with the full set of
			// packages required by paper_writer (algorithm2e, natbib, booktabs …) and
			// the scicli-latex-build wrapper that emits a JSON compilation report.
			ID:               "docker.latexmk.v1",
			RuntimeClass:     "docker",
			Image:            "ghcr.io/scimate-ai/latex-scicli:latest",
			EntrypointPolicy: "restricted",
			ResourceLimits:   ResourceLimits{CPU: 4, MemoryGB: 8, TimeoutMinutes: 20},
			AllowedRoles:     []string{"execution_agent", "paper_writer"},
			AllowedNodes:     []string{"node-execution", "node-paper-draft"},
			ArtifactsEmitted: []string{"latex_pdf", "docker_log"},
			SafetyFlags:      []string{"network_restricted"},
		},
		{
			ID:               "docker.python-sci.v1",
			RuntimeClass:     "docker",
			Image:            "python:3.12-slim",
			EntrypointPolicy: "restricted",
			ResourceLimits:   ResourceLimits{CPU: 8, MemoryGB: 12, TimeoutMinutes: 45},
			AllowedRoles:     []string{"code_agent", "execution_agent"},
			AllowedNodes:     []string{"node-implementation", "node-execution"},
			ArtifactsEmitted: []string{"result_bundle", "docker_log"},
			SafetyFlags:      []string{"network_restricted"},
		},
		{
			ID:               "docker.benchmark-runner.v1",
			RuntimeClass:     "docker",
			Image:            "ghcr.io/scimate-ai/benchmark-runner:latest",
			EntrypointPolicy: "restricted",
			ResourceLimits:   ResourceLimits{CPU: 8, MemoryGB: 16, TimeoutMinutes: 90},
			AllowedRoles:     []string{"execution_agent"},
			AllowedNodes:     []string{"node-execution"},
			ArtifactsEmitted: []string{"result_bundle", "docker_log"},
			SafetyFlags:      []string{"network_restricted", "readonly_inputs"},
		},
		{
			// Built from docker/Dockerfile.paperbanana.
			// PaperBanana calls an external LLM API so network access is required.
			// API keys are injected at runtime from the scicli config / host environment
			// via EnvTemplate: keys with an empty value are resolved from config then env.
			ID:               "docker.paperbanana.v1",
			RuntimeClass:     "docker",
			Image:            "ghcr.io/scimate-ai/paperbanana:latest",
			EntrypointPolicy: "open",
			ResourceLimits:   ResourceLimits{CPU: 4, MemoryGB: 8, TimeoutMinutes: 30},
			AllowedRoles:     []string{"figure_agent"},
			AllowedNodes:     []string{"node-analysis-figures"},
			ArtifactsEmitted: []string{"figure"},
			// No network_restricted: PaperBanana must reach the LLM API endpoint.
			// OPENAI_API_KEY is forwarded from the host environment.
			EnvTemplate: map[string]string{
				"OPENAI_API_KEY":       "",
				"OPENAI_BASE_URL":      "",
				"ANTHROPIC_API_KEY":    "",
				"PAPERBANANA_MODEL":    "gpt-4o",
			},
		},
	}

	index := make(map[string]Spec, len(items))
	for _, item := range items {
		index[item.ID] = normalizeSpec(item)
	}
	return &service{specs: index}
}

func (s *service) List() []Spec {
	out := make([]Spec, 0, len(s.specs))
	for _, item := range s.specs {
		out = append(out, item)
	}
	slices.SortFunc(out, func(a, b Spec) int {
		return strings.Compare(a.ID, b.ID)
	})
	return out
}

func (s *service) ForNodeRole(nodeID string, roleID string) []Spec {
	nodeID = strings.TrimSpace(nodeID)
	roleID = strings.TrimSpace(roleID)
	out := make([]Spec, 0)
	for _, item := range s.List() {
		if !contains(item.AllowedNodes, nodeID) {
			continue
		}
		if !contains(item.AllowedRoles, roleID) {
			continue
		}
		out = append(out, item)
	}
	return out
}

func (s *service) Get(id string) (Spec, bool) {
	item, ok := s.specs[strings.TrimSpace(id)]
	return item, ok
}

func normalizeSpec(item Spec) Spec {
	item.ID = strings.TrimSpace(item.ID)
	item.RuntimeClass = strings.TrimSpace(item.RuntimeClass)
	item.Image = strings.TrimSpace(item.Image)
	item.EntrypointPolicy = strings.TrimSpace(item.EntrypointPolicy)
	if item.EnvTemplate == nil {
		item.EnvTemplate = map[string]string{}
	}
	item.Mounts = normalizeStrings(item.Mounts)
	item.AllowedRoles = normalizeStrings(item.AllowedRoles)
	item.AllowedNodes = normalizeStrings(item.AllowedNodes)
	item.ArtifactsEmitted = normalizeStrings(item.ArtifactsEmitted)
	item.SafetyFlags = normalizeStrings(item.SafetyFlags)
	return item
}

func normalizeStrings(items []string) []string {
	out := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func contains(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
