// Package cli provides the command-line interface for the trading application.
package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"zerodha-trader/internal/broker"
)

// addAuthCommands adds authentication commands.
// Requirements: 1.1, 1.3
func addAuthCommands(rootCmd *cobra.Command, app *App) {
	rootCmd.AddCommand(newLoginCmd(app))
	rootCmd.AddCommand(newLogoutCmd(app))
}

func newLoginCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Login to Zerodha Kite Connect",
		Long: `Initiate OAuth flow with Zerodha Kite Connect.

This will open a browser window for authentication. After successful login,
you'll need to paste the request_token from the redirect URL.`,
		Example: `  trader login
  trader login --token=<request_token>  # Complete login with token`,
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()

			// Check if broker is configured
			if app.Broker == nil {
				output.Error("Broker not configured. Please check your credentials.toml")
				return fmt.Errorf("broker not configured")
			}

			// Check if token is provided directly
			token, _ := cmd.Flags().GetString("token")
			if token != "" {
				return completeLogin(ctx, app, output, token)
			}

			// Try to login (will fail with URL if not authenticated)
			err := app.Broker.Login(ctx)
			if err == nil {
				// Already authenticated
				output.Success("✓ Already logged in!")
				return nil
			}

			// Extract login URL from error message
			errMsg := err.Error()
			if !strings.Contains(errMsg, "please visit") {
				output.Error("Login failed: %v", err)
				return err
			}

			// Extract URL
			urlStart := strings.Index(errMsg, "https://")
			urlEnd := strings.Index(errMsg, " and complete")
			if urlStart == -1 || urlEnd == -1 {
				output.Error("Could not extract login URL")
				return err
			}
			loginURL := errMsg[urlStart:urlEnd]

			output.Info("Opening Zerodha login page...")
			output.Println()
			output.Bold("Login URL:")
			output.Println(loginURL)
			output.Println()

			// Try to open browser
			openBrowser, _ := cmd.Flags().GetBool("browser")
			if openBrowser {
				if err := openURL(loginURL); err != nil {
					output.Warning("Could not open browser automatically")
				}
			}

			output.Info("After logging in, you'll be redirected to a URL like:")
			output.Dim("  https://your-redirect-url.com/?request_token=XXXXXX&status=success")
			output.Println()
			output.Bold("Paste the request_token value here:")

			// Read token from stdin
			reader := bufio.NewReader(os.Stdin)
			fmt.Print("> ")
			inputToken, _ := reader.ReadString('\n')
			inputToken = strings.TrimSpace(inputToken)

			if inputToken == "" {
				output.Error("No token provided")
				return fmt.Errorf("no token provided")
			}

			return completeLogin(ctx, app, output, inputToken)
		},
	}

	cmd.Flags().Bool("browser", true, "Open browser for OAuth")
	cmd.Flags().String("token", "", "Request token from redirect URL")

	return cmd
}

func completeLogin(ctx context.Context, app *App, output *Output, token string) error {
	output.Info("Completing login with token...")

	// Get the Zerodha broker to call CompleteLogin
	zb, ok := app.Broker.(*broker.ZerodhaBroker)
	if !ok {
		output.Error("Broker is not Zerodha broker")
		return fmt.Errorf("invalid broker type")
	}

	if err := zb.CompleteLogin(ctx, token); err != nil {
		output.Error("Login failed: %v", err)
		return err
	}

	if output.IsJSON() {
		return output.JSON(map[string]interface{}{
			"success":   true,
			"message":   "Login successful",
			"timestamp": time.Now().Format(time.RFC3339),
		})
	}

	output.Success("✓ Login successful!")
	output.Println()
	output.Info("Session tokens have been stored securely.")
	output.Dim("Tokens will be automatically refreshed when needed.")
	output.Println()
	output.Bold("Next steps:")
	output.Println("  • Run 'trader balance' to check your account")
	output.Println("  • Run 'trader quote RELIANCE' to get a quote")
	output.Println("  • Run 'trader positions' to view positions")

	return nil
}

func openURL(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return fmt.Errorf("unsupported platform")
	}
	return cmd.Start()
}

func newLogoutCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Logout from Zerodha Kite Connect",
		Long: `Invalidate the current session and clear stored credentials.

This will log you out from Zerodha and remove stored session tokens.
You will need to login again to use trading features.`,
		Example: `  trader logout`,
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			// Check if broker is configured
			if app.Broker == nil {
				output.Warning("No active session found.")
				return nil
			}

			// Check if authenticated
			if !app.Broker.IsAuthenticated() {
				output.Warning("Not currently logged in.")
				return nil
			}

			output.Info("Logging out...")

			// Perform logout
			if err := app.Broker.Logout(ctx); err != nil {
				output.Error("Logout failed: %v", err)
				return err
			}

			if output.IsJSON() {
				return output.JSON(map[string]interface{}{
					"success":   true,
					"message":   "Logout successful",
					"timestamp": time.Now().Format(time.RFC3339),
				})
			}

			output.Success("✓ Logged out successfully!")
			output.Dim("Session tokens have been cleared.")

			return nil
		},
	}
}
