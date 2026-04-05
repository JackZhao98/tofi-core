package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// sendVerificationEmail sends a verification code via Resend API.
func sendVerificationEmail(resendKey, toEmail, code string) error {
	if resendKey == "" {
		return fmt.Errorf("TOFI_RESEND_API_KEY not configured")
	}

	body := map[string]interface{}{
		"from":    "Tofi <noreply@tofi.sentiosurge.com>",
		"to":      []string{toEmail},
		"subject": "Verify your Tofi account",
		"html": fmt.Sprintf(`
			<div style="font-family: -apple-system, sans-serif; max-width: 400px; margin: 0 auto; padding: 40px 20px;">
				<h2 style="color: #ff7b72; margin-bottom: 8px;">/tofi</h2>
				<p style="color: #8b949e; margin-bottom: 24px;">Your verification code:</p>
				<div style="background: #161b22; border: 1px solid #30363d; border-radius: 8px; padding: 20px; text-align: center; margin-bottom: 24px;">
					<span style="font-family: monospace; font-size: 32px; letter-spacing: 8px; color: #f0f6fc;">%s</span>
				</div>
				<p style="color: #8b949e; font-size: 14px;">This code expires in 15 minutes.</p>
				<p style="color: #8b949e; font-size: 14px;">If you didn't create a Tofi account, ignore this email.</p>
			</div>
		`, code),
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal email body: %w", err)
	}

	req, err := http.NewRequest("POST", "https://api.resend.com/emails", bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+resendKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("send email: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("resend API error %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}
