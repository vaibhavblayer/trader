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
	rootCmd.AddCommand(newAutoLoginCmd(app))
	rootCmd.AddCommand(newLogoutCmd(app))
	rootCmd.AddCommand(newAuthStatusCmd(app))
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

func newAutoLoginCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "autologin",
		Short: "Auto-login using TOTP (no browser required)",
		Long: `Automatically login to Zerodha using stored password and TOTP secret.

This requires password and totp_secret to be configured in credentials.toml:

[zerodha]
api_key = "your_api_key"
api_secret = "your_api_secret"
user_id = "your_user_id"
password = "your_kite_password"
totp_secret = "your_totp_secret"

To get your TOTP secret:
1. Go to Zerodha Console > My Profile > Password & Security
2. Enable TOTP if not already enabled
3. When setting up, copy the secret key (not the QR code)
4. Add it to credentials.toml`,
		Example: `  trader autologin`,
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			if app.Broker == nil {
				output.Error("Broker not configured. Check credentials.toml")
				return fmt.Errorf("broker not configured")
			}

			// Check if already authenticated
			if app.Broker.IsAuthenticated() {
				output.Success("✓ Already logged in!")
				return nil
			}

			// Get credentials
			password := app.Config.Credentials.Zerodha.Password
			totpSecret := app.Config.Credentials.Zerodha.TOTPSecret

			if password == "" || totpSecret == "" {
				output.Error("Auto-login requires password and totp_secret in credentials.toml")
				output.Println()
				output.Info("Add these to ~/.config/zerodha-trader/credentials.toml:")
				output.Println()
				output.Dim("[zerodha]")
				output.Dim("password = \"your_kite_password\"")
				output.Dim("totp_secret = \"your_totp_secret\"")
				output.Println()
				output.Info("To get TOTP secret: Zerodha Console > My Profile > Password & Security > TOTP")
				return fmt.Errorf("credentials not configured for auto-login")
			}

			output.Info("Performing auto-login...")

			zb, ok := app.Broker.(*broker.ZerodhaBroker)
			if !ok {
				output.Error("Auto-login only works with Zerodha broker")
				return fmt.Errorf("invalid broker type")
			}

			if err := zb.AutoLogin(ctx, password, totpSecret); err != nil {
				output.Error("Auto-login failed: %v", err)
				output.Println()
				output.Info("Try manual login: trader login")
				return err
			}

			output.Success("✓ Auto-login successful!")
			output.Println()
			output.Info("Session will expire at 6:00 AM tomorrow.")
			output.Dim("Run 'trader autologin' again tomorrow to re-authenticate.")

			return nil
		},
	}
}

func newAuthStatusCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "auth-status",
		Short: "Check authentication status",
		Long:  "Display current authentication status and session expiry.",
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)

			if app.Broker == nil {
				output.Error("Broker not configured")
				return nil
			}

			if !app.Broker.IsAuthenticated() {
				output.Warning("Not authenticated")
				output.Println()
				output.Info("Run 'trader login' or 'trader autologin' to authenticate")
				return nil
			}

			output.Success("✓ Authenticated")
			output.Println()

			// Try to get user profile
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			balance, err := app.Broker.GetBalance(ctx)
			if err != nil {
				output.Warning("Session may be expired: %v", err)
				output.Info("Run 'trader login' or 'trader autologin' to re-authenticate")
				return nil
			}

			output.Printf("  User ID:    %s\n", app.Config.Credentials.Zerodha.UserID)
			output.Printf("  Balance:    %s\n", FormatIndianCurrency(balance.AvailableCash))
			output.Println()

			// Session expiry info
			now := time.Now()
			loc, _ := time.LoadLocation("Asia/Kolkata")
			expiry := time.Date(now.Year(), now.Month(), now.Day()+1, 6, 0, 0, 0, loc)
			if now.Hour() < 6 {
				expiry = time.Date(now.Year(), now.Month(), now.Day(), 6, 0, 0, 0, loc)
			}
			remaining := expiry.Sub(now)

			output.Printf("  Session expires: %s (%s remaining)\n", 
				expiry.Format("02 Jan 15:04"), 
				formatDuration(remaining))

			// Check if auto-login is configured
			if app.Config.Credentials.Zerodha.Password != "" && app.Config.Credentials.Zerodha.TOTPSecret != "" {
				output.Println()
				output.Success("✓ Auto-login configured")
				output.Dim("Run 'trader autologin' to re-authenticate when session expires")
			}

			return nil
		},
	}
}

func formatDuration(d time.Duration) string {
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}
