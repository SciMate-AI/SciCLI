package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/opencode-ai/opencode/internal/auth"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(newAuthCmd())
}

func newAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage SciCLI authentication",
	}
	cmd.AddCommand(newAuthRegisterCmd())
	cmd.AddCommand(newAuthLoginCmd())
	cmd.AddCommand(newAuthLogoutCmd())
	cmd.AddCommand(newAuthStatusCmd())
	return cmd
}

func newAuthRegisterCmd() *cobra.Command {
	var email string
	var password string
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "register",
		Short: "Register a Supabase account",
		RunE: func(cmd *cobra.Command, args []string) error {
			debug, _ := cmd.Flags().GetBool("debug")
			cwd, _ := cmd.Flags().GetString("cwd")
			if err := loadRuntimeConfig(cwd, debug); err != nil {
				return err
			}
			svc, err := auth.NewService()
			if err != nil {
				return err
			}
			session, needsConfirmation, err := svc.Register(email, password)
			if err != nil {
				return err
			}
			if jsonOutput {
				return printJSON(map[string]any{
					"registered":               true,
					"needs_email_confirmation": needsConfirmation,
					"email":                    email,
					"session_stored":           session != nil,
				})
			}
			if needsConfirmation {
				fmt.Printf("Registered %s. Email confirmation is required before login.\n", strings.TrimSpace(email))
				return nil
			}
			fmt.Printf("Registered and signed in as %s.\n", strings.TrimSpace(email))
			return nil
		},
	}
	cmd.Flags().StringVar(&email, "email", "", "Email address")
	cmd.Flags().StringVar(&password, "password", "", "Password")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Print JSON output")
	_ = cmd.MarkFlagRequired("email")
	_ = cmd.MarkFlagRequired("password")
	return cmd
}

func newAuthLoginCmd() *cobra.Command {
	var email string
	var password string
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Login with Supabase email and password",
		RunE: func(cmd *cobra.Command, args []string) error {
			debug, _ := cmd.Flags().GetBool("debug")
			cwd, _ := cmd.Flags().GetString("cwd")
			if err := loadRuntimeConfig(cwd, debug); err != nil {
				return err
			}
			svc, err := auth.NewService()
			if err != nil {
				return err
			}
			session, err := svc.Login(email, password)
			if err != nil {
				return err
			}
			if jsonOutput {
				return printJSON(map[string]any{
					"logged_in":   true,
					"email":       session.Email,
					"expires_at":  session.ExpiresAt,
					"token_type":  session.TokenType,
					"has_refresh": session.RefreshToken != "",
				})
			}
			fmt.Printf("Logged in as %s.\n", strings.TrimSpace(session.Email))
			return nil
		},
	}
	cmd.Flags().StringVar(&email, "email", "", "Email address")
	cmd.Flags().StringVar(&password, "password", "", "Password")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Print JSON output")
	_ = cmd.MarkFlagRequired("email")
	_ = cmd.MarkFlagRequired("password")
	return cmd
}

func newAuthLogoutCmd() *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Clear the local auth session",
		RunE: func(cmd *cobra.Command, args []string) error {
			debug, _ := cmd.Flags().GetBool("debug")
			cwd, _ := cmd.Flags().GetString("cwd")
			if err := loadRuntimeConfig(cwd, debug); err != nil {
				return err
			}
			svc, err := auth.NewService()
			if err != nil {
				return err
			}
			if err := svc.Logout(); err != nil {
				return err
			}
			if jsonOutput {
				return printJSON(map[string]any{"logged_out": true})
			}
			fmt.Println("Logged out.")
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Print JSON output")
	return cmd
}

func newAuthStatusCmd() *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show current auth status",
		RunE: func(cmd *cobra.Command, args []string) error {
			debug, _ := cmd.Flags().GetBool("debug")
			cwd, _ := cmd.Flags().GetString("cwd")
			if err := loadRuntimeConfig(cwd, debug); err != nil {
				return err
			}
			svc, err := auth.NewService()
			if err != nil {
				return err
			}
			session, err := svc.Status()
			if err != nil {
				return err
			}
			if jsonOutput {
				if session == nil {
					return printJSON(map[string]any{"logged_in": false})
				}
				return printJSON(map[string]any{
					"logged_in":   true,
					"email":       session.Email,
					"expires_at":  session.ExpiresAt,
					"has_refresh": session.RefreshToken != "",
				})
			}
			if session == nil {
				fmt.Println("Not logged in.")
				return nil
			}
			expiry := "unknown"
			if !session.ExpiresAt.IsZero() {
				expiry = session.ExpiresAt.Format(time.RFC3339)
			}
			fmt.Printf("Logged in as %s. Expires: %s\n", session.Email, expiry)
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Print JSON output")
	return cmd
}
