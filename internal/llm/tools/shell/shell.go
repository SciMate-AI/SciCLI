package shell

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/SciMate-AI/scicli/internal/config"
)

type PersistentShell struct {
	cmd          *exec.Cmd
	stdin        *os.File
	isAlive      bool
	cwd          string
	kind         shellKind
	mu           sync.Mutex
	commandQueue chan *commandExecution
}

type shellKind int

const (
	shellKindPOSIX shellKind = iota
	shellKindPowerShell
)

type commandExecution struct {
	command    string
	timeout    time.Duration
	resultChan chan commandResult
	ctx        context.Context
}

type commandResult struct {
	stdout      string
	stderr      string
	exitCode    int
	interrupted bool
	err         error
}

var (
	shellInstance     *PersistentShell
	shellInstanceOnce sync.Once
)

func GetPersistentShell(workingDir string) *PersistentShell {
	shellInstanceOnce.Do(func() {
		shellInstance = newPersistentShell(workingDir)
	})

	if shellInstance == nil {
		shellInstance = newPersistentShell(workingDir)
	} else if !shellInstance.isAlive {
		shellInstance = newPersistentShell(shellInstance.cwd)
	}

	return shellInstance
}

func newPersistentShell(cwd string) *PersistentShell {
	// Get shell configuration from config
	cfg := config.Get()

	// Default to environment variable if config is not set or nil
	var shellPath string
	var shellArgs []string

	if cfg != nil {
		shellPath = cfg.Shell.Path
		shellArgs = cfg.Shell.Args
	}

	if shellPath == "" {
		defaultShell := config.DefaultShellConfig()
		shellPath = defaultShell.Path
		if len(shellArgs) == 0 {
			shellArgs = defaultShell.Args
		}
	}

	// Default shell args
	if len(shellArgs) == 0 {
		shellArgs = config.DefaultShellConfig().Args
	}

	cmd := exec.Command(shellPath, shellArgs...)
	cmd.Dir = cwd

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil
	}

	cmd.Env = append(os.Environ(), "GIT_EDITOR=true")

	err = cmd.Start()
	if err != nil {
		return nil
	}

	shell := &PersistentShell{
		cmd:          cmd,
		stdin:        stdinPipe.(*os.File),
		isAlive:      true,
		cwd:          cwd,
		kind:         detectShellKind(shellPath),
		commandQueue: make(chan *commandExecution, 10),
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Fprintf(os.Stderr, "Panic in shell command processor: %v\n", r)
				shell.isAlive = false
				close(shell.commandQueue)
			}
		}()
		shell.processCommands()
	}()

	go func() {
		err := cmd.Wait()
		if err != nil {
			// Log the error if needed
		}
		shell.isAlive = false
		close(shell.commandQueue)
	}()

	return shell
}

func (s *PersistentShell) processCommands() {
	for cmd := range s.commandQueue {
		result := s.execCommand(cmd.command, cmd.timeout, cmd.ctx)
		cmd.resultChan <- result
	}
}

func (s *PersistentShell) execCommand(command string, timeout time.Duration, ctx context.Context) commandResult {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.isAlive {
		return commandResult{
			stderr:   "Shell is not alive",
			exitCode: 1,
			err:      errors.New("shell is not alive"),
		}
	}

	stdoutFile, err := createTempPath("scicli-stdout-*")
	if err != nil {
		return commandResult{stderr: fmt.Sprintf("Failed to create stdout file: %v", err), exitCode: 1, err: err}
	}
	stderrFile, err := createTempPath("scicli-stderr-*")
	if err != nil {
		return commandResult{stderr: fmt.Sprintf("Failed to create stderr file: %v", err), exitCode: 1, err: err}
	}
	statusFile, err := createTempPath("scicli-status-*")
	if err != nil {
		return commandResult{stderr: fmt.Sprintf("Failed to create status file: %v", err), exitCode: 1, err: err}
	}
	cwdFile, err := createTempPath("scicli-cwd-*")
	if err != nil {
		return commandResult{stderr: fmt.Sprintf("Failed to create cwd file: %v", err), exitCode: 1, err: err}
	}

	defer func() {
		os.Remove(stdoutFile)
		os.Remove(stderrFile)
		os.Remove(statusFile)
		os.Remove(cwdFile)
	}()

	fullCommand, cleanup, err := s.buildCommandInvocation(command, stdoutFile, stderrFile, statusFile, cwdFile)
	if err != nil {
		return commandResult{
			stderr:   fmt.Sprintf("Failed to prepare command execution: %v", err),
			exitCode: 1,
			err:      err,
		}
	}
	defer cleanup()

	_, err = s.stdin.Write([]byte(fullCommand + "\n"))
	if err != nil {
		return commandResult{
			stderr:   fmt.Sprintf("Failed to write command to shell: %v", err),
			exitCode: 1,
			err:      err,
		}
	}

	interrupted := false

	startTime := time.Now()

	done := make(chan bool)
	go func() {
		for {
			select {
			case <-ctx.Done():
				s.killChildren()
				interrupted = true
				done <- true
				return

			case <-time.After(10 * time.Millisecond):
				if fileExists(statusFile) && fileSize(statusFile) > 0 {
					done <- true
					return
				}

				if timeout > 0 {
					elapsed := time.Since(startTime)
					if elapsed > timeout {
						s.killChildren()
						interrupted = true
						done <- true
						return
					}
				}
			}
		}
	}()

	<-done

	stdout := readFileOrEmpty(stdoutFile)
	stderr := readFileOrEmpty(stderrFile)
	exitCodeStr := readFileOrEmpty(statusFile)
	newCwd := readFileOrEmpty(cwdFile)

	exitCode := 0
	if exitCodeStr != "" {
		fmt.Sscanf(exitCodeStr, "%d", &exitCode)
	} else if interrupted {
		exitCode = 143
		stderr += "\nCommand execution timed out or was interrupted"
	}

	if newCwd != "" {
		s.cwd = strings.TrimSpace(newCwd)
	}

	return commandResult{
		stdout:      stdout,
		stderr:      stderr,
		exitCode:    exitCode,
		interrupted: interrupted,
	}
}

func (s *PersistentShell) killChildren() {
	if s.cmd == nil || s.cmd.Process == nil {
		return
	}

	if s.kind == shellKindPowerShell || runtime.GOOS == "windows" {
		_ = exec.Command("taskkill", "/T", "/F", "/PID", fmt.Sprintf("%d", s.cmd.Process.Pid)).Run()
		s.isAlive = false
		return
	}

	pgrepCmd := exec.Command("pgrep", "-P", fmt.Sprintf("%d", s.cmd.Process.Pid))
	output, err := pgrepCmd.Output()
	if err != nil {
		return
	}

	for pidStr := range strings.SplitSeq(string(output), "\n") {
		if pidStr = strings.TrimSpace(pidStr); pidStr != "" {
			var pid int
			fmt.Sscanf(pidStr, "%d", &pid)
			if pid > 0 {
				proc, err := os.FindProcess(pid)
				if err == nil {
					proc.Signal(syscall.SIGTERM)
				}
			}
		}
	}
}

func (s *PersistentShell) Exec(ctx context.Context, command string, timeoutMs int) (string, string, int, bool, error) {
	if !s.isAlive {
		return "", "Shell is not alive", 1, false, errors.New("shell is not alive")
	}

	timeout := time.Duration(timeoutMs) * time.Millisecond

	resultChan := make(chan commandResult)
	s.commandQueue <- &commandExecution{
		command:    command,
		timeout:    timeout,
		resultChan: resultChan,
		ctx:        ctx,
	}

	result := <-resultChan
	return result.stdout, result.stderr, result.exitCode, result.interrupted, result.err
}

func (s *PersistentShell) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.isAlive {
		return
	}

	s.stdin.Write([]byte("exit\n"))

	s.cmd.Process.Kill()
	s.isAlive = false
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func powerShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func (s *PersistentShell) buildCommandInvocation(command, stdoutFile, stderrFile, statusFile, cwdFile string) (string, func(), error) {
	switch s.kind {
	case shellKindPowerShell:
		return buildPowerShellInvocation(command, stdoutFile, stderrFile, statusFile, cwdFile)
	default:
		return buildPOSIXInvocation(command, stdoutFile, stderrFile, statusFile, cwdFile), func() {}, nil
	}
}

func buildPOSIXInvocation(command, stdoutFile, stderrFile, statusFile, cwdFile string) string {
	return fmt.Sprintf(`
eval %s < /dev/null > %s 2> %s
EXEC_EXIT_CODE=$?
pwd > %s
echo $EXEC_EXIT_CODE > %s
`,
		shellQuote(command),
		shellQuote(stdoutFile),
		shellQuote(stderrFile),
		shellQuote(cwdFile),
		shellQuote(statusFile),
	)
}

func buildPowerShellInvocation(command, stdoutFile, stderrFile, statusFile, cwdFile string) (string, func(), error) {
	scriptFile, err := createTempPath("scicli-shell-*.ps1")
	if err != nil {
		return "", func() {}, err
	}

	script := fmt.Sprintf(`$global:LASTEXITCODE = 0
$execExitCode = 0
try {
    Invoke-Expression %s 1> %s 2> %s
    if ($null -ne $LASTEXITCODE) {
        $execExitCode = [int]$LASTEXITCODE
    }
} catch {
    $_ | Out-File -FilePath %s -Append -Encoding utf8
    $execExitCode = 1
}
(Get-Location).Path | Set-Content -Path %s -Encoding utf8
$execExitCode | Set-Content -Path %s -Encoding utf8
`,
		powerShellQuote(command),
		powerShellQuote(stdoutFile),
		powerShellQuote(stderrFile),
		powerShellQuote(stderrFile),
		powerShellQuote(cwdFile),
		powerShellQuote(statusFile),
	)
	if err := os.WriteFile(scriptFile, []byte(script), 0o600); err != nil {
		_ = os.Remove(scriptFile)
		return "", func() {}, err
	}

	invocation := ". " + powerShellQuote(scriptFile)
	cleanup := func() {
		_ = os.Remove(scriptFile)
	}
	return invocation, cleanup, nil
}

func createTempPath(pattern string) (string, error) {
	file, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", err
	}
	path := file.Name()
	if closeErr := file.Close(); closeErr != nil {
		_ = os.Remove(path)
		return "", closeErr
	}
	return path, nil
}

func detectShellKind(shellPath string) shellKind {
	base := strings.ToLower(filepath.Base(strings.TrimSpace(shellPath)))
	switch base {
	case "powershell.exe", "powershell", "pwsh.exe", "pwsh":
		return shellKindPowerShell
	default:
		return shellKindPOSIX
	}
}

func readFileOrEmpty(path string) string {
	content, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(content)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}
