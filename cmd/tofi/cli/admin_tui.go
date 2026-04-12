package cli

import (
	"bytes"
	"encoding/json"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

type adminItem struct {
	id   string
	name string
	desc string
}

var adminMenuItems = []adminItem{
	{"users", "Users", "Manage user accounts"},
	{"registration", "Registration", "Enable/disable signup"},
	{"usage", "Usage", "Token usage by model"},
}

type adminMenuModel struct {
	cursor     int
	selected   string
	exitReason tuiExitReason
	ctrlC      ctrlCGuard
}

func newAdminMenuModel() *adminMenuModel {
	return &adminMenuModel{}
}

func (m *adminMenuModel) Init() tea.Cmd { return nil }

func (m *adminMenuModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			if quit, cmd := m.ctrlC.HandleCtrlC(); quit {
				m.exitReason = exitQuit
				return m, tea.Quit
			} else {
				return m, cmd
			}
		case "esc":
			m.exitReason = exitToMenu
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(adminMenuItems)-1 {
				m.cursor++
			}
		case "enter":
			m.selected = adminMenuItems[m.cursor].id
			return m, tea.Quit
		default:
			m.ctrlC.HandleReset()
		}
	case ctrlCResetMsg:
		m.ctrlC.HandleReset()
		return m, nil
	}
	return m, nil
}

func (m *adminMenuModel) View() string {
	var items string
	for i, item := range adminMenuItems {
		if i == m.cursor {
			label := fmt.Sprintf("%-16s %s", item.name, tuiSelectedDesc.Render(item.desc))
			items += tuiSelectedRow.Render("► "+label) + "\n"
		} else {
			label := fmt.Sprintf("%-16s %s", item.name, subtitleStyle.Render(item.desc))
			items += "  " + label + "\n"
		}
	}

	footer := subtitleStyle.Render("↑↓ navigate · enter select · esc back")
	content := items + "\n" + footer

	warn := ""
	if m.ctrlC.IsArmed() {
		warn = "\n" + m.ctrlC.RenderWarning()
	}

	return "\n" + renderTUIBox("Admin", content) + warn + "\n"
}

// ── Registration Settings TUI ──

type regSettings struct {
	AllowSignup          bool `json:"allow_signup"`
	RequireVerifiedEmail bool `json:"require_verified_email"`
	EmailConfigured      bool `json:"email_configured"`
	AllowUserKeys        bool `json:"allow_user_keys"`
}

type regItem struct {
	id   string
	name string
}

var regMenuItems = []regItem{
	{"signup", "Allow Registration"},
	{"verify", "Require Email Verification"},
	{"userkeys", "Allow User API Keys"},
}

type regModel struct {
	cursor     int
	settings   regSettings
	loaded     bool
	err        string
	exitReason tuiExitReason
	ctrlC      ctrlCGuard
	client     *apiClient
}

func newRegModel() *regModel {
	return &regModel{client: newAPIClient()}
}

type regLoadedMsg struct{ s regSettings }
type regErrorMsg struct{ err string }
type regUpdatedMsg struct{ s regSettings }

func (m *regModel) loadSettings() tea.Msg {
	var s regSettings
	if err := m.client.get("/api/v1/admin/settings/registration", &s); err != nil {
		return regErrorMsg{err: err.Error()}
	}
	// Also fetch allow_user_keys from the AI keys endpoint
	var aiKeys struct {
		AllowUserKeys bool `json:"allow_user_keys"`
	}
	if err := m.client.get("/api/v1/user/settings/ai-keys", &aiKeys); err == nil {
		s.AllowUserKeys = aiKeys.AllowUserKeys
	}
	return regLoadedMsg{s: s}
}

func (m *regModel) toggleSetting(field string, val bool) tea.Cmd {
	return func() tea.Msg {
		if field == "allow" {
			// User keys uses a different endpoint
			payload := map[string]bool{"allow": val}
			body, _ := json.Marshal(payload)
			if err := m.client.put("/api/v1/admin/settings/allow-user-keys", bytes.NewReader(body), nil); err != nil {
				return regErrorMsg{err: err.Error()}
			}
		} else {
			payload := map[string]bool{field: val}
			body, _ := json.Marshal(payload)
			if err := m.client.put("/api/v1/admin/settings/registration", bytes.NewReader(body), nil); err != nil {
				return regErrorMsg{err: err.Error()}
			}
		}
		// Reload all settings
		var s regSettings
		if err := m.client.get("/api/v1/admin/settings/registration", &s); err != nil {
			return regErrorMsg{err: err.Error()}
		}
		var aiKeys struct {
			AllowUserKeys bool `json:"allow_user_keys"`
		}
		if err := m.client.get("/api/v1/user/settings/ai-keys", &aiKeys); err == nil {
			s.AllowUserKeys = aiKeys.AllowUserKeys
		}
		return regUpdatedMsg{s: s}
	}
}

func (m *regModel) Init() tea.Cmd {
	return m.loadSettings
}

func (m *regModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case regLoadedMsg:
		m.settings = msg.s
		m.loaded = true
		return m, nil
	case regUpdatedMsg:
		m.settings = msg.s
		return m, nil
	case regErrorMsg:
		m.err = msg.err
		m.loaded = true
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			if quit, quitCmd := m.ctrlC.HandleCtrlC(); quit {
				m.exitReason = exitQuit
				return m, tea.Quit
			} else {
				return m, quitCmd
			}
		case "esc":
			m.exitReason = exitToMenu
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(regMenuItems)-1 {
				m.cursor++
			}
		case "enter", " ":
			if !m.loaded {
				return m, nil
			}
			switch regMenuItems[m.cursor].id {
			case "signup":
				return m, m.toggleSetting("allow_signup", !m.settings.AllowSignup)
			case "verify":
				if m.settings.EmailConfigured {
					return m, m.toggleSetting("require_verified_email", !m.settings.RequireVerifiedEmail)
				}
			case "userkeys":
				return m, m.toggleSetting("allow", !m.settings.AllowUserKeys)
			}
		default:
			m.ctrlC.HandleReset()
		}
	case ctrlCResetMsg:
		m.ctrlC.HandleReset()
		return m, nil
	}
	return m, nil
}

func (m *regModel) View() string {
	if !m.loaded {
		return "\n" + renderTUIBox("Registration", subtitleStyle.Render("Loading...")) + "\n"
	}
	if m.err != "" {
		return "\n" + renderTUIBox("Registration", errorStyle.Render("  ✗ "+m.err)) + "\n"
	}

	var items string
	for i, item := range regMenuItems {
		var toggle string
		switch item.id {
		case "signup":
			if m.settings.AllowSignup {
				toggle = successStyle.Render("ON")
			} else {
				toggle = subtitleStyle.Render("OFF")
			}
		case "verify":
			if !m.settings.EmailConfigured {
				toggle = subtitleStyle.Render("OFF") + subtitleStyle.Render(" (no email provider)")
			} else if m.settings.RequireVerifiedEmail {
				toggle = successStyle.Render("ON")
			} else {
				toggle = subtitleStyle.Render("OFF")
			}
		case "userkeys":
			if m.settings.AllowUserKeys {
				toggle = successStyle.Render("ON") + subtitleStyle.Render(" (BYOK)")
			} else {
				toggle = subtitleStyle.Render("OFF") + subtitleStyle.Render(" (SaaS mode)")
			}
		}

		label := fmt.Sprintf("%-28s %s", item.name, toggle)
		if i == m.cursor {
			items += tuiSelectedRow.Render("► " + label) + "\n"
		} else {
			items += "  " + label + "\n"
		}
	}

	footer := subtitleStyle.Render("↑↓ navigate · enter toggle · esc back")
	content := items + "\n" + footer

	warn := ""
	if m.ctrlC.IsArmed() {
		warn = "\n" + m.ctrlC.RenderWarning()
	}

	return "\n" + renderTUIBox("Registration", content) + warn + "\n"
}

func runRegistrationSection(_ *cobra.Command) error {
	c := newAPIClient()
	if err := c.ensureRunning(); err != nil {
		return err
	}

	p := tea.NewProgram(newRegModel())
	_, err := p.Run()
	return err
}

// runAdminSection runs the admin menu loop.
func runAdminSection(cmd *cobra.Command) (tuiExitReason, error) {
	for {
		model := newAdminMenuModel()
		p := tea.NewProgram(model)
		if _, err := p.Run(); err != nil {
			return exitQuit, err
		}

		if model.exitReason == exitQuit {
			return exitQuit, nil
		}
		if model.selected == "" {
			return exitToMenu, nil
		}

		clearScreen()
		var subErr error
		switch model.selected {
		case "users":
			subErr = runUsersSection(cmd)
		case "registration":
			subErr = runRegistrationSection(cmd)
		case "usage":
			subErr = runUsageSection(cmd)
		}
		if subErr != nil {
			fmt.Println(errorStyle.Render("  ✗ " + subErr.Error()))
		}
		clearScreen()
	}
}
