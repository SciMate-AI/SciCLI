package shell

import (
	"os"
	"strings"
	"testing"
)

func TestDetectShellKind(t *testing.T) {
	tests := []struct {
		path string
		want shellKind
	}{
		{path: "powershell.exe", want: shellKindPowerShell},
		{path: "C:\\Windows\\System32\\WindowsPowerShell\\v1.0\\powershell.exe", want: shellKindPowerShell},
		{path: "pwsh", want: shellKindPowerShell},
		{path: "/bin/bash", want: shellKindPOSIX},
		{path: "bash", want: shellKindPOSIX},
	}

	for _, tt := range tests {
		if got := detectShellKind(tt.path); got != tt.want {
			t.Fatalf("detectShellKind(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestBuildPowerShellInvocation(t *testing.T) {
	invocation, cleanup, err := buildPowerShellInvocation(
		"Get-Location",
		"C:\\tmp\\stdout.txt",
		"C:\\tmp\\stderr.txt",
		"C:\\tmp\\status.txt",
		"C:\\tmp\\cwd.txt",
	)
	if err != nil {
		t.Fatalf("buildPowerShellInvocation returned error: %v", err)
	}
	defer cleanup()

	if !strings.HasPrefix(invocation, ". '") {
		t.Fatalf("unexpected invocation format: %q", invocation)
	}

	scriptPath := strings.TrimPrefix(invocation, ". '")
	scriptPath = strings.TrimSuffix(scriptPath, "'")
	data, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("failed to read generated script: %v", err)
	}
	content := string(data)
	for _, expected := range []string{
		"Invoke-Expression 'Get-Location'",
		"Set-Content -Path 'C:\\tmp\\cwd.txt'",
		"Set-Content -Path 'C:\\tmp\\status.txt'",
	} {
		if !strings.Contains(content, expected) {
			t.Fatalf("generated script missing %q:\n%s", expected, content)
		}
	}
}
