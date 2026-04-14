package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var adminCmd = &cobra.Command{
	Use:   "admin",
	Short: "Admin-only operations (system API keys, user keys toggle)",
	Long: `Manage system-wide API keys and admin settings.

System keys are used as fallback when users don't have their own keys configured.
Priority order: user key → system key → environment variable.`,
}

func init() {
	rootCmd.AddCommand(adminCmd)

	adminCmd.AddCommand(adminListKeysCmd)
	adminCmd.AddCommand(adminSetKeyCmd)
	adminCmd.AddCommand(adminDeleteKeyCmd)
	adminCmd.AddCommand(adminToggleUserKeysCmd)
}

// --- tofi admin list-keys ---

var adminListKeysCmd = &cobra.Command{
	Use:   "list-keys",
	Short: "List all API keys (system, user, env)",
	RunE:  runAdminListKeys,
}

type scopedKey struct {
	Provider  string `json:"provider"`
	MaskedKey string `json:"masked_key"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

type allKeysResponse struct {
	System []scopedKey `json:"system"`
	User   []scopedKey `json:"user"`
	Env    []scopedKey `json:"env"`
}

func runAdminListKeys(cmd *cobra.Command, args []string) error {
	client := newAPIClient()
	if err := client.ensureRunning(); err != nil {
		return err
	}

	var resp allKeysResponse
	if err := client.get("/api/v1/settings/ai-keys", &resp); err != nil {
		fmt.Println()
		fmt.Println(errorStyle.Render("  ✗ ") + err.Error())
		fmt.Println()
		return err
	}

	fmt.Println()
	printKeySection("System Keys", resp.System)
	printKeySection("User Keys (yours)", resp.User)
	printKeySection("Environment", resp.Env)
	fmt.Println()
	return nil
}

func printKeySection(title string, keys []scopedKey) {
	fmt.Println("  " + titleStyle.Render(title))
	if len(keys) == 0 {
		fmt.Println("    " + subtitleStyle.Render("(none)"))
		fmt.Println()
		return
	}
	for _, k := range keys {
		fmt.Println("    " +
			accentStyle.Render(k.Provider) + " " +
			subtitleStyle.Render(k.MaskedKey))
	}
	fmt.Println()
}

// --- tofi admin set-key <provider> <key> ---

var adminSetKeyCmd = &cobra.Command{
	Use:   "set-key <provider> <key>",
	Short: "Set a system-wide API key (admin only)",
	Long: `Set a system-level API key that serves as fallback for all users.

Providers: openai, anthropic, gemini, deepseek, groq, openrouter

Examples:
  tofi admin set-key openai sk-...
  tofi admin set-key openrouter sk-or-...`,
	Args: cobra.ExactArgs(2),
	RunE: runAdminSetKey,
}

func runAdminSetKey(cmd *cobra.Command, args []string) error {
	provider := strings.ToLower(args[0])
	apiKey := args[1]

	client := newAPIClient()
	if err := client.ensureRunning(); err != nil {
		return err
	}

	body := map[string]string{
		"provider": provider,
		"api_key":  apiKey,
		"scope":    "system",
	}
	jsonBody, _ := json.Marshal(body)

	if err := client.post("/api/v1/settings/ai-keys", bytes.NewReader(jsonBody), nil); err != nil {
		fmt.Println()
		fmt.Println(errorStyle.Render("  ✗ ") + err.Error())
		fmt.Println()
		return err
	}

	fmt.Println()
	fmt.Println(successStyle.Render("  ✓ system ") + accentStyle.Render(provider) + successStyle.Render(" key saved"))
	fmt.Println()
	return nil
}

// --- tofi admin delete-key <provider> ---

var adminDeleteKeyCmd = &cobra.Command{
	Use:   "delete-key <provider>",
	Short: "Delete a system-wide API key (admin only)",
	Args:  cobra.ExactArgs(1),
	RunE:  runAdminDeleteKey,
}

func runAdminDeleteKey(cmd *cobra.Command, args []string) error {
	provider := strings.ToLower(args[0])

	client := newAPIClient()
	if err := client.ensureRunning(); err != nil {
		return err
	}

	if err := client.delete(fmt.Sprintf("/api/v1/settings/ai-keys/%s?scope=system", provider)); err != nil {
		fmt.Println()
		fmt.Println(errorStyle.Render("  ✗ ") + err.Error())
		fmt.Println()
		return err
	}

	fmt.Println()
	fmt.Println(successStyle.Render("  ✓ system ") + accentStyle.Render(provider) + successStyle.Render(" key deleted"))
	fmt.Println()
	return nil
}

// --- tofi admin toggle-user-keys [on|off] ---

var adminToggleUserKeysCmd = &cobra.Command{
	Use:   "toggle-user-keys [on|off]",
	Short: "Enable or disable per-user API keys (admin only)",
	Long: `Control whether users can bring their own LLM API keys.

When disabled, only system keys are used — equivalent to SaaS mode.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runAdminToggleUserKeys,
}

func runAdminToggleUserKeys(cmd *cobra.Command, args []string) error {
	client := newAPIClient()
	if err := client.ensureRunning(); err != nil {
		return err
	}

	if len(args) == 0 {
		// Show current state
		var resp allKeysResponse
		if err := client.get("/api/v1/settings/ai-keys", &resp); err != nil {
			return err
		}
		fmt.Println()
		fmt.Println(subtitleStyle.Render("  Use 'tofi admin toggle-user-keys on' or 'off' to change."))
		fmt.Println()
		return nil
	}

	allow := strings.ToLower(args[0]) == "on"
	body := map[string]bool{"allow": allow}
	jsonBody, _ := json.Marshal(body)

	if err := client.put("/api/v1/admin/settings/allow-user-keys", bytes.NewReader(jsonBody), nil); err != nil {
		fmt.Println()
		fmt.Println(errorStyle.Render("  ✗ ") + err.Error())
		fmt.Println()
		return err
	}

	fmt.Println()
	if allow {
		fmt.Println(successStyle.Render("  ✓ User API keys: enabled"))
	} else {
		fmt.Println(successStyle.Render("  ✓ User API keys: disabled (SaaS mode)"))
	}
	fmt.Println()
	return nil
}
