package config

import (
	"runtime"
	"testing"
)

func TestDefaultShellConfig(t *testing.T) {
	t.Setenv("SCICLI_SHELL_PATH", "")
	t.Setenv("SHELL", "")

	cfg := DefaultShellConfig()
	if runtime.GOOS == "windows" {
		if cfg.Path != "powershell.exe" {
			t.Fatalf("expected powershell.exe on windows, got %q", cfg.Path)
		}
		if len(cfg.Args) == 0 || cfg.Args[0] != "-NoLogo" {
			t.Fatalf("expected PowerShell args on windows, got %v", cfg.Args)
		}
		return
	}

	if cfg.Path != "/bin/bash" {
		t.Fatalf("expected /bin/bash on non-windows, got %q", cfg.Path)
	}
	if len(cfg.Args) == 0 || cfg.Args[0] != "-l" {
		t.Fatalf("expected login shell args on non-windows, got %v", cfg.Args)
	}
}
