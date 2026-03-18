package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/SciMate-AI/scicli/internal/app"
	"github.com/SciMate-AI/scicli/internal/config"
	"github.com/SciMate-AI/scicli/internal/db"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(newUltraworkCmd())
}

func newUltraworkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ultrawork <prompt>",
		Short: "Run a prompt in ultrawork mode",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			debug, _ := cmd.Flags().GetBool("debug")
			cwd, _ := cmd.Flags().GetString("cwd")
			outputFormat, _ := cmd.Flags().GetString("output-format")
			quiet, _ := cmd.Flags().GetBool("quiet")

			runtimeWorkMode = string(config.WorkModeUltrawork)
			if err := loadRuntimeConfig(cwd, debug); err != nil {
				return err
			}

			conn, err := db.Connect()
			if err != nil {
				return err
			}

			application, err := app.New(context.Background(), conn)
			if err != nil {
				return fmt.Errorf("create app: %w", err)
			}
			defer application.Shutdown()

			return application.RunNonInteractive(context.Background(), strings.Join(args, " "), outputFormat, quiet)
		},
	}
}
