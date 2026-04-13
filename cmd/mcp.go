package cmd

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/SciMate-AI/scicli/internal/config"
	"github.com/SciMate-AI/scicli/internal/mcpcli"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(newMcpCmd())
}

func newMcpCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Inspect and call configured MCP tools",
	}
	cmd.AddCommand(newMcpInstallCmd())
	cmd.AddCommand(newMcpListToolsCmd())
	cmd.AddCommand(newMcpCallCmd())
	return cmd
}

func newMcpInstallCmd() *cobra.Command {
	var name string
	var force bool
	cmd := &cobra.Command{
		Use:   "install <preset>",
		Short: "Install a built-in MCP server preset into config",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			debug, _ := cmd.Flags().GetBool("debug")
			cwd, _ := cmd.Flags().GetString("cwd")
			if err := loadRuntimeConfig(cwd, debug); err != nil {
				return err
			}

			presetName := args[0]
			serverName := name
			server, note, err := mcpPresetConfig(presetName, serverName)
			if err != nil {
				return err
			}
			if serverName == "" {
				serverName = presetName
			}

			if err := config.SetMCPServer(serverName, server, force); err != nil {
				return err
			}

			fmt.Printf("Installed MCP preset %q as server %q in %s.\n", presetName, serverName, config.ConfigFilePath())
			if note != "" {
				fmt.Println(note)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Override the configured MCP server name")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite an existing MCP server with the same name")
	return cmd
}

func mcpPresetConfig(presetName, serverName string) (config.MCPServer, string, error) {
	switch presetName {
	case "zotero":
		if serverName == "" {
			serverName = "zotero"
		}
		return config.MCPServer{
				Type:    config.MCPStdio,
				Command: "uvx",
				Args:    []string{"zotero-mcp"},
			},
			"The preset writes a stdio server entry only. Runtime still requires `uvx` to be available so `zotero-mcp` can be launched.",
			nil
	default:
		return config.MCPServer{}, "", fmt.Errorf("unknown MCP preset %q", presetName)
	}
}

func newMcpListToolsCmd() *cobra.Command {
	var server string
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "list-tools",
		Short: "List tools exposed by a configured MCP server",
		RunE: func(cmd *cobra.Command, args []string) error {
			debug, _ := cmd.Flags().GetBool("debug")
			cwd, _ := cmd.Flags().GetString("cwd")
			if err := loadRuntimeConfig(cwd, debug); err != nil {
				return err
			}
			client, err := mcpcli.NewClient(server)
			if err != nil {
				return err
			}
			tools, err := client.ListTools(context.Background())
			if err != nil {
				return err
			}
			if jsonOutput {
				return printJSON(tools)
			}
			for _, tool := range tools {
				fmt.Println(tool.Name)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&server, "server", "", "Configured MCP server name")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Print JSON output")
	return cmd
}

func newMcpCallCmd() *cobra.Command {
	var server string
	var toolName string
	var rawArgs string
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "call",
		Short: "Call a configured MCP tool",
		RunE: func(cmd *cobra.Command, args []string) error {
			debug, _ := cmd.Flags().GetBool("debug")
			cwd, _ := cmd.Flags().GetString("cwd")
			if err := loadRuntimeConfig(cwd, debug); err != nil {
				return err
			}
			var payload map[string]any
			if rawArgs != "" {
				if err := json.Unmarshal([]byte(rawArgs), &payload); err != nil {
					return fmt.Errorf("failed to parse --args JSON: %w", err)
				}
			}
			client, err := mcpcli.NewClient(server)
			if err != nil {
				return err
			}
			result, err := client.CallTool(context.Background(), toolName, payload)
			if err != nil {
				return err
			}
			if jsonOutput {
				return printJSON(result)
			}
			fmt.Println(mcpcli.ExtractToolText(result))
			return nil
		},
	}
	cmd.Flags().StringVar(&server, "server", "", "Configured MCP server name")
	cmd.Flags().StringVar(&toolName, "tool", "", "Tool name")
	cmd.Flags().StringVar(&rawArgs, "args", "{}", "Tool arguments as JSON")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Print JSON output")
	_ = cmd.MarkFlagRequired("tool")
	return cmd
}
