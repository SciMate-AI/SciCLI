package runtimex

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/SciMate-AI/scicli/internal/config"
	"github.com/SciMate-AI/scicli/internal/permission"
)

type RunRequest struct {
	SessionID string `json:"session_id,omitempty"`
	Execute   bool   `json:"execute,omitempty"`
}

type RunResult struct {
	RuntimeID        string   `json:"runtime_id,omitempty"`
	Commands         []string `json:"commands,omitempty"`
	OutputDir        string   `json:"output_dir,omitempty"`
	DryRun           bool     `json:"dry_run,omitempty"`
	Executed         bool     `json:"executed,omitempty"`
	Succeeded        bool     `json:"succeeded,omitempty"`
	Blocked          bool     `json:"blocked,omitempty"`
	ExitCode         int      `json:"exit_code,omitempty"`
	Stdout           string   `json:"stdout,omitempty"`
	Stderr           string   `json:"stderr,omitempty"`
	Summary          string   `json:"summary,omitempty"`
	CompletedCommand string   `json:"completed_command,omitempty"`
}

type Runner interface {
	RunPlan(ctx context.Context, plan Plan, req RunRequest) RunResult
}

type CommandRunner interface {
	Run(context.Context, string, int) (string, string, int, error)
}

type runner struct {
	permissions permission.Service
	commands    CommandRunner
}

func NewRunner(permissions permission.Service) Runner {
	return &runner{
		permissions: permissions,
		commands:    shellCommandRunner{},
	}
}

func newRunnerWithCommandRunner(permissions permission.Service, commands CommandRunner) Runner {
	if commands == nil {
		commands = shellCommandRunner{}
	}
	return &runner{
		permissions: permissions,
		commands:    commands,
	}
}

func (r *runner) RunPlan(ctx context.Context, plan Plan, req RunRequest) RunResult {
	result := RunResult{
		RuntimeID: plan.RuntimeID,
		Commands:  append([]string{}, plan.Commands...),
		OutputDir: strings.TrimSpace(plan.OutputDir),
		DryRun:    true,
		Summary:   "Runtime plan prepared but not executed",
	}
	if !req.Execute {
		return result
	}
	if len(plan.Commands) == 0 {
		result.Summary = "Runtime plan has no commands"
		return result
	}

	command := strings.TrimSpace(plan.Commands[0])
	if command == "" {
		result.Summary = "Runtime plan command is empty"
		return result
	}

	if r.permissions != nil {
		allowed := r.permissions.Request(permission.CreatePermissionRequest{
			SessionID:   strings.TrimSpace(req.SessionID),
			ToolName:    "bash",
			Description: "Execute scientist bench runtime plan",
			Action:      "execute",
			Command:     command,
			Path:        safeWorkingDirectory(result.OutputDir),
			Params: map[string]any{
				"command": command,
			},
		})
		if !allowed {
			result.Blocked = true
			result.Summary = "Runtime execution blocked by permission policy"
			return result
		}
	}

	stdout, stderr, exitCode, err := r.commands.Run(ctx, command, max(1, plan.TimeoutMinutes))
	result.DryRun = false
	result.Executed = true
	result.ExitCode = exitCode
	result.Stdout = strings.TrimSpace(stdout)
	result.Stderr = strings.TrimSpace(stderr)
	result.CompletedCommand = command
	if err != nil {
		result.Summary = strings.TrimSpace(err.Error())
	}
	if exitCode == 0 && err == nil {
		result.Succeeded = true
		result.Summary = "Runtime execution completed"
		return result
	}
	if result.Summary == "" {
		result.Summary = fmt.Sprintf("Runtime execution failed with exit code %d", exitCode)
	}
	return result
}

type shellCommandRunner struct{}

func (shellCommandRunner) Run(ctx context.Context, command string, timeoutMinutes int) (string, string, int, error) {
	if timeoutMinutes <= 0 {
		timeoutMinutes = 10
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMinutes)*time.Minute)
	defer cancel()

	shellPath, shellArgs := shellInvocation(command)
	cmd := exec.CommandContext(timeoutCtx, shellPath, shellArgs...) //nolint:gosec
	output, err := cmd.CombinedOutput()
	stdout := strings.TrimSpace(string(output))
	stderr := ""
	exitCode := 0
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}
	if err == nil {
		return stdout, stderr, exitCode, nil
	}
	if stdout == "" {
		stderr = strings.TrimSpace(err.Error())
	} else {
		stderr = stdout
	}
	if exitCode == 0 {
		exitCode = 1
	}
	return stdout, stderr, exitCode, err
}

func shellInvocation(command string) (string, []string) {
	cfg := config.Get()
	shellPath := ""
	shellArgs := []string{}
	if cfg != nil {
		shellPath = strings.TrimSpace(cfg.Shell.Path)
		shellArgs = append(shellArgs, cfg.Shell.Args...)
	}
	if shellPath == "" {
		defaultShell := config.DefaultShellConfig()
		shellPath = defaultShell.Path
		shellArgs = append([]string{}, defaultShell.Args...)
	}
	if runtime.GOOS == "windows" {
		return shellPath, append(shellArgs, "-Command", command)
	}
	return shellPath, append(shellArgs, "-c", command)
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func safeWorkingDirectory(values ...string) string {
	if dir := firstNonEmptyString(values...); dir != "" {
		return dir
	}
	if cfg := config.Get(); cfg != nil {
		if dir := strings.TrimSpace(cfg.WorkingDir); dir != "" {
			return dir
		}
	}
	return "."
}
