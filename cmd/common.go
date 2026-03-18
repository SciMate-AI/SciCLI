package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/SciMate-AI/scicli/internal/config"
	"github.com/SciMate-AI/scicli/internal/logging"
	"github.com/SciMate-AI/scicli/internal/skills"
)

func loadRuntimeConfig(cmdCwd string, debug bool) error {
	if cmdCwd != "" {
		if err := os.Chdir(cmdCwd); err != nil {
			return fmt.Errorf("failed to change directory: %w", err)
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current working directory: %w", err)
	}
	_, err = config.Load(cwd, debug)
	if err != nil {
		return err
	}
	if err := config.ApplyRuntimeOverrides(runtimeWorkMode, runtimeAutoApprove, runtimeAllowPrefixes); err != nil {
		return err
	}
	if err := skills.EnsureBundledExtensions(); err != nil {
		logging.Warn("failed to sync bundled skills", "error", err)
	}
	return nil
}

func printJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
