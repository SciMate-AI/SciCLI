package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/SciMate-AI/scicli/internal/scimate"
	"github.com/SciMate-AI/scicli/internal/state"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(newRunsCmd())
	rootCmd.AddCommand(newLogCmd())
	rootCmd.AddCommand(newArtifactsCmd())
}

func newRunsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "runs",
		Short: "Manage remote CAE runs",
	}
	cmd.AddCommand(newRunsStartCmd())
	cmd.AddCommand(newRunsLastCmd())
	return cmd
}

func newRunsStartCmd() *cobra.Command {
	var server string
	var solverPath string
	var projectID string
	var runID string
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Upload a solver and start a remote run",
		RunE: func(cmd *cobra.Command, args []string) error {
			debug, _ := cmd.Flags().GetBool("debug")
			cwd, _ := cmd.Flags().GetString("cwd")
			if err := loadRuntimeConfig(cwd, debug); err != nil {
				return err
			}
			if filepath.Ext(solverPath) != ".py" {
				return fmt.Errorf("solver must be a .py file")
			}
			if projectID == "" {
				workingDir, err := os.Getwd()
				if err != nil {
					return err
				}
				projectID = scimate.ComputeProjectID(workingDir)
			}
			if runID == "" {
				runID = scimate.GenerateRunID(time.Now())
			}

			client, err := scimate.NewClient(server)
			if err != nil {
				return err
			}
			scriptPath, runResult, err := client.StartRun(context.Background(), solverPath, projectID, runID)
			if err != nil {
				return err
			}

			store, err := state.NewStore()
			if err != nil {
				return err
			}
			current, err := store.Load()
			if err != nil {
				return err
			}
			current.LastRun = &state.RunRef{
				Server:    server,
				ProjectID: projectID,
				RunID:     runID,
				UpdatedAt: time.Now().UTC(),
			}
			if err := store.Save(current); err != nil {
				return err
			}

			if jsonOutput {
				return printJSON(map[string]any{
					"server":          server,
					"project_id":      projectID,
					"run_id":          runID,
					"script_gcs_path": scriptPath,
					"result":          runResult,
				})
			}
			fmt.Printf("Started run %s/%s on %s.\n", projectID, runID, server)
			fmt.Printf("Uploaded solver to %s.\n", scriptPath)
			return nil
		},
	}
	cmd.Flags().StringVar(&server, "server", "cae-agent", "Configured MCP server name")
	cmd.Flags().StringVar(&solverPath, "solver", "", "Path to the solver .py file")
	cmd.Flags().StringVar(&projectID, "project-id", "", "Project ID override")
	cmd.Flags().StringVar(&runID, "run-id", "", "Run ID override")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Print JSON output")
	_ = cmd.MarkFlagRequired("solver")
	return cmd
}

func newRunsLastCmd() *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "last",
		Short: "Show the last recorded remote run",
		RunE: func(cmd *cobra.Command, args []string) error {
			debug, _ := cmd.Flags().GetBool("debug")
			cwd, _ := cmd.Flags().GetString("cwd")
			if err := loadRuntimeConfig(cwd, debug); err != nil {
				return err
			}
			store, err := state.NewStore()
			if err != nil {
				return err
			}
			current, err := store.Load()
			if err != nil {
				return err
			}
			if current.LastRun == nil {
				return fmt.Errorf("no run has been recorded yet")
			}
			if jsonOutput {
				return printJSON(current.LastRun)
			}
			fmt.Printf("%s %s/%s\n", current.LastRun.Server, current.LastRun.ProjectID, current.LastRun.RunID)
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Print JSON output")
	return cmd
}

func newLogCmd() *cobra.Command {
	var server string
	var projectID string
	var runID string
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "log",
		Short: "Fetch the run.log for a CAE run",
	}
	getCmd := &cobra.Command{
		Use:   "get",
		Short: "Get the run log",
		RunE: func(cmd *cobra.Command, args []string) error {
			debug, _ := cmd.Flags().GetBool("debug")
			cwd, _ := cmd.Flags().GetString("cwd")
			if err := loadRuntimeConfig(cwd, debug); err != nil {
				return err
			}
			resolvedServer, resolvedProjectID, resolvedRunID, err := resolveRunRef(server, projectID, runID)
			if err != nil {
				return err
			}
			client, err := scimate.NewClient(resolvedServer)
			if err != nil {
				return err
			}
			logText, err := client.GetRunLog(context.Background(), resolvedProjectID, resolvedRunID)
			if err != nil {
				return err
			}
			if jsonOutput {
				return printJSON(map[string]any{
					"server":     resolvedServer,
					"project_id": resolvedProjectID,
					"run_id":     resolvedRunID,
					"log":        logText,
				})
			}
			fmt.Println(logText)
			return nil
		},
	}
	getCmd.Flags().StringVar(&server, "server", "", "Configured MCP server name")
	getCmd.Flags().StringVar(&projectID, "project-id", "", "Project ID")
	getCmd.Flags().StringVar(&runID, "run-id", "", "Run ID")
	getCmd.Flags().BoolVar(&jsonOutput, "json", false, "Print JSON output")
	cmd.AddCommand(getCmd)
	return cmd
}

func newArtifactsCmd() *cobra.Command {
	var server string
	var projectID string
	var runID string
	var relativePath string
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "artifacts",
		Short: "Inspect run artifacts",
	}
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List artifacts for a run",
		RunE: func(cmd *cobra.Command, args []string) error {
			debug, _ := cmd.Flags().GetBool("debug")
			cwd, _ := cmd.Flags().GetString("cwd")
			if err := loadRuntimeConfig(cwd, debug); err != nil {
				return err
			}
			resolvedServer, resolvedProjectID, resolvedRunID, err := resolveRunRef(server, projectID, runID)
			if err != nil {
				return err
			}
			client, err := scimate.NewClient(resolvedServer)
			if err != nil {
				return err
			}
			artifacts, err := client.ListRunArtifacts(context.Background(), resolvedProjectID, resolvedRunID)
			if err != nil {
				return err
			}
			if jsonOutput {
				return printJSON(artifacts)
			}
			for _, item := range artifacts.Items {
				fmt.Println(item.Name)
			}
			return nil
		},
	}
	getCmd := &cobra.Command{
		Use:   "get",
		Short: "Read a text artifact by relative path",
		RunE: func(cmd *cobra.Command, args []string) error {
			debug, _ := cmd.Flags().GetBool("debug")
			cwd, _ := cmd.Flags().GetString("cwd")
			if err := loadRuntimeConfig(cwd, debug); err != nil {
				return err
			}
			resolvedServer, resolvedProjectID, resolvedRunID, err := resolveRunRef(server, projectID, runID)
			if err != nil {
				return err
			}
			client, err := scimate.NewClient(resolvedServer)
			if err != nil {
				return err
			}
			text, err := client.ReadRunArtifactText(context.Background(), resolvedProjectID, resolvedRunID, relativePath)
			if err != nil {
				return err
			}
			if jsonOutput {
				return printJSON(map[string]any{
					"server":        resolvedServer,
					"project_id":    resolvedProjectID,
					"run_id":        resolvedRunID,
					"relative_path": relativePath,
					"content":       text,
				})
			}
			fmt.Println(text)
			return nil
		},
	}
	listCmd.Flags().StringVar(&server, "server", "", "Configured MCP server name")
	listCmd.Flags().StringVar(&projectID, "project-id", "", "Project ID")
	listCmd.Flags().StringVar(&runID, "run-id", "", "Run ID")
	listCmd.Flags().BoolVar(&jsonOutput, "json", false, "Print JSON output")
	getCmd.Flags().StringVar(&server, "server", "", "Configured MCP server name")
	getCmd.Flags().StringVar(&projectID, "project-id", "", "Project ID")
	getCmd.Flags().StringVar(&runID, "run-id", "", "Run ID")
	getCmd.Flags().StringVar(&relativePath, "path", "", "Artifact relative path")
	getCmd.Flags().BoolVar(&jsonOutput, "json", false, "Print JSON output")
	_ = getCmd.MarkFlagRequired("path")
	cmd.AddCommand(listCmd)
	cmd.AddCommand(getCmd)
	return cmd
}

func resolveRunRef(server string, projectID string, runID string) (string, string, string, error) {
	if projectID != "" && runID != "" {
		if server == "" {
			server = "cae-agent"
		}
		return server, projectID, runID, nil
	}

	store, err := state.NewStore()
	if err != nil {
		return "", "", "", err
	}
	current, err := store.Load()
	if err != nil {
		return "", "", "", err
	}
	if current.LastRun == nil {
		return "", "", "", fmt.Errorf("no run reference available; pass --project-id and --run-id or start a run first")
	}
	resolvedServer := current.LastRun.Server
	if server != "" {
		resolvedServer = server
	}
	resolvedProjectID := current.LastRun.ProjectID
	if projectID != "" {
		resolvedProjectID = projectID
	}
	resolvedRunID := current.LastRun.RunID
	if runID != "" {
		resolvedRunID = runID
	}
	return resolvedServer, resolvedProjectID, resolvedRunID, nil
}
