package cmd

import (
	"testing"

	"github.com/SciMate-AI/scicli/internal/config"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

func TestResolveTUIRuntimeOptionsUsesConfigDefaults(t *testing.T) {
	command := newTUITestCommand()
	cfg := &config.Config{
		TUI: config.TUIConfig{
			AltScreen: false,
			Mouse:     false,
		},
	}

	noAltScreen, noMouse := resolveTUIRuntimeOptions(command, cfg, false, false, false, false)
	assert.True(t, noAltScreen)
	assert.True(t, noMouse)
}

func TestResolveTUIRuntimeOptionsRespectsConfigDisable(t *testing.T) {
	command := newTUITestCommand()
	cfg := &config.Config{
		TUI: config.TUIConfig{
			AltScreen: true,
			Mouse:     true,
		},
	}

	noAltScreen, noMouse := resolveTUIRuntimeOptions(command, cfg, false, false, false, false)
	assert.False(t, noAltScreen)
	assert.False(t, noMouse)
}

func TestResolveTUIRuntimeOptionsPrefersFlagsOverConfig(t *testing.T) {
	command := newTUITestCommand()
	mustSetFlagChanged(t, command, "no-alt-screen")
	mustSetFlagChanged(t, command, "mouse")

	cfg := &config.Config{
		TUI: config.TUIConfig{
			AltScreen: false,
			Mouse:     false,
		},
	}

	noAltScreen, noMouse := resolveTUIRuntimeOptions(command, cfg, true, true, false, true)
	assert.True(t, noAltScreen)
	assert.False(t, noMouse)
}

func TestResolveTUIRuntimeOptionsForceFlagsWin(t *testing.T) {
	command := newTUITestCommand()
	cfg := &config.Config{
		TUI: config.TUIConfig{
			AltScreen: false,
			Mouse:     false,
		},
	}

	noAltScreen, noMouse := resolveTUIRuntimeOptions(command, cfg, true, true, true, true)
	assert.False(t, noAltScreen)
	assert.False(t, noMouse)
}

func newTUITestCommand() *cobra.Command {
	command := &cobra.Command{Use: "scicli"}
	command.Flags().Bool("no-alt-screen", false, "")
	command.Flags().Bool("no-mouse", false, "")
	command.Flags().Bool("alt-screen", false, "")
	command.Flags().Bool("mouse", false, "")
	return command
}

func mustSetFlagChanged(t *testing.T, command *cobra.Command, name string) {
	t.Helper()
	flag := command.Flags().Lookup(name)
	if flag == nil {
		t.Fatalf("flag %s not found", name)
	}
	flag.Changed = true
}
