package cmd

import (
	"context"
	"encoding/json"
	"fmt"

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
	cmd.AddCommand(newMcpListToolsCmd())
	cmd.AddCommand(newMcpCallCmd())
	return cmd
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
