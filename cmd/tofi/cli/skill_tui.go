package cli

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"tofi-core/internal/models"
	"tofi-core/internal/skills"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

// --- Steps ---

type skillStep int

const (
	skillStepList        skillStep = iota // installed + system + actions
	skillStepDetail                       // single skill detail + operations
	skillStepInstall                      // input source URL
	skillStepInstallPick                  // checkbox select discovered skills
	skillStepImport                       // input local path
	skillStepImportPick                   // checkbox select discovered skills
	skillStepCreate                       // input skill name
)

// --- Messages ---

type skillListLoadedMsg struct {
	user   []skills.SkillInfo
	system []skills.SkillInfo
}

type skillDiscoveredMsg struct {
	skills  []*models.SkillFile
	source  string // display source for header
	srcDir  string // for import: where files came from
	cleanup func() // cleanup function from PreviewInstall
}

type skillActionDoneMsg struct{ msg string }
type skillActionErrMsg struct{ err string }

// --- List item types ---

type skillListItem struct {
	kind string // "skill", "system", "divider", "action"
	// For skill items
	info skills.SkillInfo
	// For action items
	actionID   string
	actionName string
}

// --- Model ---

type skillModel struct {
	step       skillStep
	cursor     int
	items      []skillListItem // unified list for skillStepList
	exitReason tuiExitReason
	ctrlC      ctrlCGuard

	// Detail
	selectedSkill skills.SkillInfo
	detailActions []string
	detailCursor  int

	// Install/Import input
	textInput textinput.Model

	// Install/Import pick
	discovered     []*models.SkillFile
	picked         []bool
	pickCursor     int
	pickSource     string // display name
	pickSrcDir     string // for import
	previewCleanup func() // cleanup function from PreviewInstall

	// Result
	resultMsg string

	store *skills.LocalStore
}

func newSkillModel() *skillModel {
	ti := textinput.New()
	ti.CharLimit = 256
	ti.Width = 40

	store := skills.NewLocalStore(homeDir)

	return &skillModel{
		step:      skillStepList,
		textInput: ti,
		store:     store,
	}
}

func (m *skillModel) Init() tea.Cmd {
	return m.loadSkills()
}

func (m *skillModel) loadSkills() tea.Cmd {
	return func() tea.Msg {
		userID := resolveUserID()
		userSkills, err := m.store.ListUserSkills(userID)
		if err != nil {
			log.Printf("[skills] warning: failed to list user skills: %v", err)
		}

		// Load system skills from DB via API would be complex;
		// for now list system skills from embedded FS
		var systemSkills []skills.SkillInfo
		sysDir := filepath.Join(homeDir, ".tofi", "skills")
		// System skills are in the binary, not on disk. We'll show them from DB later.
		// For now, show a placeholder.
		_ = sysDir

		return skillListLoadedMsg{user: userSkills, system: systemSkills}
	}
}

func (m *skillModel) buildListItems(user, system []skills.SkillInfo) {
	m.items = nil

	if len(user) > 0 {
		for _, s := range user {
			m.items = append(m.items, skillListItem{kind: "skill", info: s})
		}
	}

	if len(system) > 0 {
		m.items = append(m.items, skillListItem{kind: "divider"})
		for _, s := range system {
			m.items = append(m.items, skillListItem{kind: "system", info: s})
		}
	}

	// Actions at the bottom
	m.items = append(m.items, skillListItem{kind: "divider"})
	m.items = append(m.items, skillListItem{kind: "action", actionID: "install", actionName: "Install from GitHub"})
	m.items = append(m.items, skillListItem{kind: "action", actionID: "import", actionName: "Import from local"})
	m.items = append(m.items, skillListItem{kind: "action", actionID: "create", actionName: "Create new skill"})
}

// isSelectable returns true if the cursor can land on this item.
func (item *skillListItem) isSelectable() bool {
	return item.kind == "skill" || item.kind == "action"
}

// nextSelectable moves cursor to the next selectable item.
func (m *skillModel) nextSelectable() {
	for i := m.cursor + 1; i < len(m.items); i++ {
		if m.items[i].isSelectable() {
			m.cursor = i
			return
		}
	}
}

// prevSelectable moves cursor to the previous selectable item.
func (m *skillModel) prevSelectable() {
	for i := m.cursor - 1; i >= 0; i-- {
		if m.items[i].isSelectable() {
			m.cursor = i
			return
		}
	}
}

// firstSelectable sets cursor to first selectable item.
func (m *skillModel) firstSelectable() {
	for i, item := range m.items {
		if item.isSelectable() {
			m.cursor = i
			return
		}
	}
}

func (m *skillModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			if quit, cmd := m.ctrlC.HandleCtrlC(); quit {
				m.exitReason = exitQuit
				return m, tea.Quit
			} else {
				return m, cmd
			}
		}
		m.ctrlC.HandleReset()
	case ctrlCResetMsg:
		m.ctrlC.HandleReset()
		return m, nil
	case skillListLoadedMsg:
		m.buildListItems(msg.user, msg.system)
		m.firstSelectable()
		// resultMsg cleared on next user key press in updateList
		return m, nil
	case skillDiscoveredMsg:
		m.discovered = msg.skills
		m.picked = make([]bool, len(msg.skills))
		for i := range m.picked {
			m.picked[i] = true // default all selected
		}
		m.pickCursor = 0
		m.pickSource = msg.source
		m.pickSrcDir = msg.srcDir
		m.previewCleanup = msg.cleanup
		if m.step == skillStepInstall {
			m.step = skillStepInstallPick
		} else {
			m.step = skillStepImportPick
		}
		return m, nil
	case skillActionDoneMsg:
		m.cleanupPreview()
		m.resultMsg = msg.msg
		m.step = skillStepList
		return m, m.loadSkills()
	case skillActionErrMsg:
		m.cleanupPreview()
		m.resultMsg = msg.err
		m.step = skillStepList
		return m, m.loadSkills()
	}

	switch m.step {
	case skillStepList:
		return m.updateList(msg)
	case skillStepDetail:
		return m.updateDetail(msg)
	case skillStepInstall, skillStepImport:
		return m.updateInput(msg)
	case skillStepInstallPick, skillStepImportPick:
		return m.updatePick(msg)
	case skillStepCreate:
		return m.updateCreate(msg)
	}

	return m, nil
}

// --- List step ---

func (m *skillModel) updateList(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		m.resultMsg = "" // clear result on any key press
		switch km.String() {
		case "esc":
			m.exitReason = exitToMenu
			return m, tea.Quit
		case "up", "k":
			m.prevSelectable()
		case "down", "j":
			m.nextSelectable()
		case "enter":
			if m.cursor < len(m.items) {
				item := m.items[m.cursor]
				switch item.kind {
				case "skill":
					m.selectedSkill = item.info
					m.detailCursor = 0
					m.detailActions = []string{"Uninstall", "View SKILL.md"}
					if item.info.Scope == "user" {
						m.detailActions = []string{"Uninstall", "View SKILL.md", "Edit SKILL.md"}
					}
					m.step = skillStepDetail
				case "action":
					switch item.actionID {
					case "install":
						m.textInput.SetValue("")
						m.textInput.Placeholder = "owner/repo or GitHub URL"
						m.textInput.Focus()
						m.step = skillStepInstall
					case "import":
						m.textInput.SetValue("")
						m.textInput.Placeholder = "/path/to/skills/folder"
						m.textInput.Focus()
						m.step = skillStepImport
					case "create":
						m.textInput.SetValue("")
						m.textInput.Placeholder = "my-skill-name"
						m.textInput.Focus()
						m.step = skillStepCreate
					}
				}
			}
		}
	}
	return m, nil
}

// --- Detail step ---

func (m *skillModel) updateDetail(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "esc":
			m.step = skillStepList
		case "up", "k":
			if m.detailCursor > 0 {
				m.detailCursor--
			}
		case "down", "j":
			if m.detailCursor < len(m.detailActions)-1 {
				m.detailCursor++
			}
		case "enter":
			action := m.detailActions[m.detailCursor]
			switch action {
			case "Uninstall":
				return m, m.doUninstall()
			case "View SKILL.md":
				// Read and display — for now just show path
				m.resultMsg = "SKILL.md at: " + m.selectedSkill.Dir
				m.step = skillStepList
			case "Edit SKILL.md":
				m.resultMsg = "Edit: " + filepath.Join(m.selectedSkill.Dir, "SKILL.md")
				m.step = skillStepList
			}
		}
	}
	return m, nil
}

func (m *skillModel) doUninstall() tea.Cmd {
	return func() tea.Msg {
		userID := resolveUserID()
		if err := m.store.DeactivateSkill(userID, m.selectedSkill.Name); err != nil {
			return skillActionErrMsg{err: fmt.Sprintf("Failed to remove %s: %v", m.selectedSkill.Name, err)}
		}
		return skillActionDoneMsg{msg: fmt.Sprintf("Removed %s", m.selectedSkill.Name)}
	}
}

// --- Input step (Install source / Import path / Create name) ---

func (m *skillModel) updateInput(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "esc":
			m.step = skillStepList
			return m, nil
		case "enter":
			value := strings.TrimSpace(m.textInput.Value())
			if value == "" {
				return m, nil
			}
			if m.step == skillStepInstall {
				return m, m.discoverFromSource(value)
			}
			return m, m.discoverFromLocal(value)
		}
	}
	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m *skillModel) updateCreate(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "esc":
			m.step = skillStepList
			return m, nil
		case "enter":
			name := strings.TrimSpace(m.textInput.Value())
			if name == "" {
				return m, nil
			}
			return m, m.doCreate(name)
		}
	}
	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m *skillModel) discoverFromSource(source string) tea.Cmd {
	store := m.store
	return func() tea.Msg {
		installer := skills.NewSkillInstaller(store)
		result, cleanup, err := installer.PreviewInstall(source)
		if err != nil {
			return skillActionErrMsg{err: fmt.Sprintf("Failed: %v", err)}
		}
		return skillDiscoveredMsg{skills: result.Skills, source: source, cleanup: cleanup}
	}
}

func (m *skillModel) discoverFromLocal(path string) tea.Cmd {
	return func() tea.Msg {
		if strings.HasPrefix(path, "~") {
			home, _ := os.UserHomeDir()
			path = filepath.Join(home, path[1:])
		}
		discovered, err := skills.DiscoverSkills(path)
		if err != nil || len(discovered) == 0 {
			return skillActionErrMsg{err: fmt.Sprintf("No skills found in %s", path)}
		}
		return skillDiscoveredMsg{skills: discovered, source: filepath.Base(path), srcDir: path}
	}
}

// --- Pick step (checkbox select) ---

func (m *skillModel) cleanupPreview() {
	if m.previewCleanup != nil {
		m.previewCleanup()
		m.previewCleanup = nil
	}
}

func (m *skillModel) updatePick(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "esc":
			m.cleanupPreview()
			m.step = skillStepList
		case "up", "k":
			if m.pickCursor > 0 {
				m.pickCursor--
			}
		case "down", "j":
			if m.pickCursor < len(m.discovered)-1 {
				m.pickCursor++
			}
		case " ":
			m.picked[m.pickCursor] = !m.picked[m.pickCursor]
		case "enter":
			if m.step == skillStepInstallPick {
				return m, m.doInstallPicked()
			}
			return m, m.doImportPicked()
		}
	}
	return m, nil
}

func (m *skillModel) doInstallPicked() tea.Cmd {
	var names []string
	for i, s := range m.discovered {
		if m.picked[i] {
			names = append(names, s.Manifest.Name)
		}
	}
	if len(names) == 0 {
		return func() tea.Msg { return skillActionErrMsg{err: "No skills selected"} }
	}

	source := m.pickSource
	store := m.store
	return func() tea.Msg {
		installer := skills.NewSkillInstaller(store)
		userID := resolveUserID()

		result, err := installer.InstallForUser(source, userID, names)
		if err != nil {
			return skillActionErrMsg{err: fmt.Sprintf("Install failed: %v", err)}
		}
		var installed []string
		for _, s := range result.Skills {
			installed = append(installed, s.Manifest.Name)
		}
		return skillActionDoneMsg{msg: fmt.Sprintf("Installed: %s", strings.Join(installed, ", "))}
	}
}

func (m *skillModel) doImportPicked() tea.Cmd {
	var selected []*models.SkillFile
	for i, s := range m.discovered {
		if m.picked[i] {
			selected = append(selected, s)
		}
	}
	if len(selected) == 0 {
		return func() tea.Msg { return skillActionErrMsg{err: "No skills selected"} }
	}

	srcDir := m.pickSrcDir
	store := m.store
	return func() tea.Msg {
		userID := resolveUserID()
		var imported, failed []string
		for _, skill := range selected {
			// Try to find the skill's directory
			skillDir := filepath.Join(srcDir, skill.Manifest.Name)
			if _, err := os.Stat(filepath.Join(skillDir, "SKILL.md")); os.IsNotExist(err) {
				skillDir = srcDir // skill is at root
			}
			if err := store.SaveUserSkill(userID, skill.Manifest.Name, skillDir); err != nil {
				failed = append(failed, fmt.Sprintf("%s: %v", skill.Manifest.Name, err))
				continue
			}
			imported = append(imported, skill.Manifest.Name)
		}
		if len(imported) == 0 {
			return skillActionErrMsg{err: fmt.Sprintf("Failed to import: %s", strings.Join(failed, "; "))}
		}
		msg := fmt.Sprintf("Imported: %s", strings.Join(imported, ", "))
		if len(failed) > 0 {
			msg += fmt.Sprintf(" (failed: %s)", strings.Join(failed, "; "))
		}
		return skillActionDoneMsg{msg: msg}
	}
}

func (m *skillModel) doCreate(name string) tea.Cmd {
	store := m.store
	return func() tea.Msg {
		userID := resolveUserID()

		template := fmt.Sprintf("---\nname: %s\ndescription: TODO\nversion: \"1.0\"\n---\n\n# %s\n\nInstructions for the AI agent go here.\n", name, name)

		userDir := store.UserSkillDir(userID)
		if err := os.MkdirAll(userDir, 0755); err != nil {
			return skillActionErrMsg{err: err.Error()}
		}

		skillDir := filepath.Join(userDir, name)
		if err := os.MkdirAll(skillDir, 0755); err != nil {
			return skillActionErrMsg{err: err.Error()}
		}

		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(template), 0644); err != nil {
			return skillActionErrMsg{err: err.Error()}
		}

		return skillActionDoneMsg{msg: fmt.Sprintf("Created skill %q", name)}
	}
}

// --- View ---

func (m *skillModel) View() string {
	switch m.step {
	case skillStepList:
		return m.viewList()
	case skillStepDetail:
		return m.viewDetail()
	case skillStepInstall:
		return m.viewInput("Install Skill", "Enter a GitHub repo (owner/repo) or URL")
	case skillStepImport:
		return m.viewInput("Import Local Skill", "Enter a local directory path containing SKILL.md files")
	case skillStepCreate:
		return m.viewInput("Create New Skill", "Enter a name for the new skill")
	case skillStepInstallPick, skillStepImportPick:
		return m.viewPick()
	}
	return ""
}

func (m *skillModel) viewList() string {
	var b strings.Builder

	for i, item := range m.items {
		switch item.kind {
		case "skill":
			scope := item.info.Scope
			if scope == "global" {
				scope = "Global"
			} else {
				scope = "User"
			}
			meta := fmt.Sprintf("%s · v%s", scope, item.info.Version)
			if i == m.cursor {
				label := fmt.Sprintf("► %-20s %s", item.info.Name, tuiSelectedDesc.Render(meta))
				b.WriteString(tuiSelectedRow.Render(label))
			} else {
				label := fmt.Sprintf("  %-20s %s", item.info.Name, subtitleStyle.Render(meta))
				b.WriteString(label)
			}
			b.WriteString("\n")

		case "system":
			label := fmt.Sprintf("%-20s %s", item.info.Name, subtitleStyle.Render("Built-in"))
			b.WriteString("  " + subtitleStyle.Render(label) + "\n")

		case "divider":
			b.WriteString(subtitleStyle.Render("  ────────────────────────────────────") + "\n")

		case "action":
			if i == m.cursor {
				b.WriteString(tuiSelectedRow.Render("► " + item.actionName) + "\n")
			} else {
				b.WriteString("  " + item.actionName + "\n")
			}
		}
	}

	footer := subtitleStyle.Render("↑↓ navigate · enter select · esc back")
	content := b.String() + "\n" + footer

	warn := ""
	if m.ctrlC.IsArmed() {
		warn = "\n" + m.ctrlC.RenderWarning()
	}

	result := ""
	if m.resultMsg != "" {
		result = "\n" + successStyle.Render("  "+m.resultMsg)
	}

	return "\n" + renderTUIBox("Skills", content) + warn + result + "\n"
}

func (m *skillModel) viewDetail() string {
	s := m.selectedSkill
	scope := s.Scope
	if scope == "global" {
		scope = "Global"
	} else {
		scope = "User"
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Scope:    %s\n", scope))
	b.WriteString(fmt.Sprintf("Version:  %s\n", s.Version))
	if s.Desc != "" {
		b.WriteString(fmt.Sprintf("\n%s\n", s.Desc))
	}
	b.WriteString("\n")

	for i, action := range m.detailActions {
		if i == m.detailCursor {
			b.WriteString(tuiSelectedRow.Render("► "+action) + "\n")
		} else {
			b.WriteString("  " + action + "\n")
		}
	}

	footer := subtitleStyle.Render("↑↓ navigate · enter select · esc back")
	content := b.String() + "\n" + footer

	return "\n" + renderTUIBox(s.Name, content) + "\n"
}

func (m *skillModel) viewInput(title, hint string) string {
	var b strings.Builder
	b.WriteString(m.textInput.View() + "\n\n")
	b.WriteString(subtitleStyle.Render(hint) + "\n\n")
	b.WriteString(subtitleStyle.Render("esc back · enter confirm"))

	return "\n" + renderTUIBox(title, b.String()) + "\n"
}

func (m *skillModel) viewPick() string {
	action := "install"
	if m.step == skillStepImportPick {
		action = "import"
	}
	title := fmt.Sprintf("Found %d skills in %s", len(m.discovered), m.pickSource)

	var b strings.Builder
	b.WriteString(title + "\n\n")

	for i, skill := range m.discovered {
		check := "[ ]"
		if m.picked[i] {
			check = "[x]"
		}
		name := skill.Manifest.Name
		desc := skill.Manifest.Description
		if len(desc) > 40 {
			desc = desc[:37] + "..."
		}
		if i == m.pickCursor {
			line := fmt.Sprintf("%s %-18s %s", check, name, tuiSelectedDesc.Render(desc))
			b.WriteString(tuiSelectedRow.Render(line) + "\n")
		} else {
			line := fmt.Sprintf("%s %-18s %s", check, name, subtitleStyle.Render(desc))
			b.WriteString("  " + line + "\n")
		}
	}

	b.WriteString("\n" + subtitleStyle.Render(fmt.Sprintf("↑↓ navigate · space toggle · enter %s · esc cancel", action)))

	return "\n" + renderTUIBox("Select Skills", b.String()) + "\n"
}

// --- Run ---

func runSkillSection(cmd *cobra.Command) (tuiExitReason, error) {
	model := newSkillModel()
	p := tea.NewProgram(model)
	if _, err := p.Run(); err != nil {
		return exitQuit, err
	}
	return model.exitReason, nil
}
