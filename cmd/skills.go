package cmd

import (
	"context"
	"fmt"

	"github.com/SciMate-AI/scicli/internal/config"
	"github.com/SciMate-AI/scicli/internal/skills"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(newSkillsCmd())
}

func newSkillsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skills",
		Short: "Inspect and manage Agent Skills",
	}
	cmd.AddCommand(newSkillsListCmd())
	cmd.AddCommand(newSkillsRecommendCmd())
	cmd.AddCommand(newSkillsInstallCmd())
	cmd.AddCommand(newSkillsUninstallCmd())
	cmd.AddCommand(newSkillsEnableCmd())
	cmd.AddCommand(newSkillsDisableCmd())
	return cmd
}

func newSkillsListCmd() *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List discovered skills",
		RunE: func(cmd *cobra.Command, args []string) error {
			debug, _ := cmd.Flags().GetBool("debug")
			cwd, _ := cmd.Flags().GetString("cwd")
			if err := loadRuntimeConfig(cwd, debug); err != nil {
				return err
			}

			skillSvc := skills.NewService()
			items, err := skillSvc.List(context.Background())
			if err != nil {
				return err
			}
			if jsonOutput {
				return printJSON(items)
			}
			if len(items) == 0 {
				fmt.Println("No skills found.")
				return nil
			}
			for _, skill := range items {
				fmt.Printf("%s\t%s\t%s\n", skill.ID, skill.Scope, skill.Path)
				if skill.Description != "" {
					fmt.Printf("  %s\n", skill.Description)
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Print JSON output")
	return cmd
}

func newSkillsEnableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "enable <skill-id>",
		Short: "Enable a disabled skill",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			debug, _ := cmd.Flags().GetBool("debug")
			cwd, _ := cmd.Flags().GetString("cwd")
			if err := loadRuntimeConfig(cwd, debug); err != nil {
				return err
			}
			if err := config.SetSkillDisabled(args[0], false); err != nil {
				return err
			}
			fmt.Printf("Enabled skill %q.\n", args[0])
			return nil
		},
	}
}

func newSkillsRecommendCmd() *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "recommend <query>",
		Short: "Recommend relevant skills for a task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			debug, _ := cmd.Flags().GetBool("debug")
			cwd, _ := cmd.Flags().GetString("cwd")
			if err := loadRuntimeConfig(cwd, debug); err != nil {
				return err
			}

			skillSvc := skills.NewService()
			items, err := skillSvc.Recommend(context.Background(), args[0], 10)
			if err != nil {
				return err
			}
			if jsonOutput {
				return printJSON(items)
			}
			if len(items) == 0 {
				fmt.Println("No matching skills found.")
				return nil
			}
			for _, skill := range items {
				fmt.Printf("%s\t%s\n", skill.ID, skill.Description)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Print JSON output")
	return cmd
}

func newSkillsInstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install <source>",
		Short: "Install a skill from a local directory or GitHub tree URL",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			debug, _ := cmd.Flags().GetBool("debug")
			cwd, _ := cmd.Flags().GetString("cwd")
			if err := loadRuntimeConfig(cwd, debug); err != nil {
				return err
			}

			skillSvc := skills.NewService()
			skill, err := skillSvc.Install(context.Background(), args[0])
			if err != nil {
				return err
			}
			fmt.Printf("Installed skill %q at %s.\n", skill.ID, skill.Path)
			return nil
		},
	}
}

func newSkillsUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall <skill-id>",
		Short: "Remove a previously installed skill",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			debug, _ := cmd.Flags().GetBool("debug")
			cwd, _ := cmd.Flags().GetString("cwd")
			if err := loadRuntimeConfig(cwd, debug); err != nil {
				return err
			}

			skillSvc := skills.NewService()
			if err := skillSvc.Uninstall(context.Background(), args[0]); err != nil {
				return err
			}
			fmt.Printf("Uninstalled skill %q.\n", args[0])
			return nil
		},
	}
}

func newSkillsDisableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "disable <skill-id>",
		Short: "Disable a skill",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			debug, _ := cmd.Flags().GetBool("debug")
			cwd, _ := cmd.Flags().GetString("cwd")
			if err := loadRuntimeConfig(cwd, debug); err != nil {
				return err
			}
			if err := config.SetSkillDisabled(args[0], true); err != nil {
				return err
			}
			fmt.Printf("Disabled skill %q.\n", args[0])
			return nil
		},
	}
}
