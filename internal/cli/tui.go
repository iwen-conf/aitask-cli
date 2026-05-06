package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
)

// RunTUI launches the interactive menu when `aitask` is invoked with no
// subcommand on a TTY. It mutates global config / project state via the same
// helpers the regular subcommands use, so the CLI and TUI stay in lockstep.
func RunTUI(env *CommandEnv) error {
	m, err := initialTUIModel(env)
	if err != nil {
		return err
	}
	prog := tea.NewProgram(m, tea.WithAltScreen())
	_, err = prog.Run()
	return err
}

type tuiScreen int

const (
	screenMenu tuiScreen = iota
	screenServerInput
	screenProjectInput
	screenChangeProject
	screenBusy
	screenResult
)

type tuiModel struct {
	env     *CommandEnv
	screen  tuiScreen
	cursor  int
	input   textinput.Model
	busyMsg string
	result  string
	err     error

	// snapshot data refreshed on every menu render
	serverURL  string
	projectID  string
	projectDir string
}

var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("99"))
	labelStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	valueStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("87"))
	cursorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
	hintStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Italic(true)
	okStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
)

var menuItems = []string{
	"Test connection",
	"Set backend URL",
	"Initialize project here",
	"Change project_id",
	"Quit",
}

func initialTUIModel(env *CommandEnv) (tuiModel, error) {
	ti := textinput.New()
	ti.CharLimit = 256
	ti.Width = 60
	m := tuiModel{env: env, input: ti, screen: screenMenu}
	m.refreshSnapshot()
	return m, nil
}

// refreshSnapshot reloads server URL + bound project from disk so the menu
// always shows current state after a write.
func (m *tuiModel) refreshSnapshot() {
	m.serverURL = m.env.opts.serverURL
	if m.serverURL == "" {
		m.serverURL = defaultServerURL
	}
	m.projectID = ""
	m.projectDir = ""
	if cwd, err := os.Getwd(); err == nil {
		if cfg, err := LoadProjectConfig(cwd); err == nil {
			m.projectID = cfg.ProjectID
			m.projectDir = cfg.RootDir
		}
	}
}

func (m tuiModel) Init() tea.Cmd { return nil }

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Ctrl+C / Esc always quit (or back-out of a sub screen)
		if msg.Type == tea.KeyCtrlC {
			return m, tea.Quit
		}
		switch m.screen {
		case screenMenu:
			return m.updateMenu(msg)
		case screenServerInput, screenProjectInput, screenChangeProject:
			return m.updateInput(msg)
		case screenResult:
			if msg.Type == tea.KeyEnter || msg.Type == tea.KeyEsc || msg.String() == "q" {
				m.screen = screenMenu
				m.refreshSnapshot()
			}
		}
	case connectionResultMsg:
		m.screen = screenResult
		if msg.err != nil {
			m.err = msg.err
			m.result = ""
		} else {
			m.err = nil
			m.result = msg.summary
		}
	case actionResultMsg:
		m.screen = screenResult
		m.err = msg.err
		m.result = msg.summary
	}
	return m, nil
}

func (m tuiModel) updateMenu(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(menuItems)-1 {
			m.cursor++
		}
	case "q", "esc":
		return m, tea.Quit
	case "enter":
		return m.activateMenuItem()
	}
	return m, nil
}

func (m tuiModel) activateMenuItem() (tea.Model, tea.Cmd) {
	switch m.cursor {
	case 0: // Test connection
		m.screen = screenBusy
		m.busyMsg = "Calling whoami on " + m.serverURL + " ..."
		return m, testConnectionCmd(m.env)
	case 1: // Set backend URL
		m.input.Reset()
		m.input.Placeholder = defaultServerURL
		m.input.SetValue(m.serverURL)
		m.input.Focus()
		m.screen = screenServerInput
	case 2: // Initialize project
		m.input.Reset()
		m.input.Placeholder = "project_id (e.g. proj_xxx)"
		m.input.Focus()
		m.screen = screenProjectInput
	case 3: // Change project_id
		if m.projectID == "" {
			m.screen = screenResult
			m.err = errors.New("no project bound in current directory; use 'Initialize project here' first")
			return m, nil
		}
		m.input.Reset()
		m.input.Placeholder = "new project_id"
		m.input.SetValue(m.projectID)
		m.input.Focus()
		m.screen = screenChangeProject
	case 4: // Quit
		return m, tea.Quit
	}
	return m, nil
}

func (m tuiModel) updateInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.screen = screenMenu
		return m, nil
	case tea.KeyEnter:
		value := strings.TrimSpace(m.input.Value())
		switch m.screen {
		case screenServerInput:
			return m.commitServerURL(value)
		case screenProjectInput:
			return m.commitInitProject(value)
		case screenChangeProject:
			return m.commitChangeProject(value)
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m tuiModel) commitServerURL(value string) (tea.Model, tea.Cmd) {
	if value == "" {
		m.screen = screenResult
		m.err = errors.New("server URL cannot be empty")
		return m, nil
	}
	normalized := normalizeServerURL(value)
	cfg, _ := LoadGlobalConfig()
	cfg.ServerURL = normalized
	if err := SaveGlobalConfig(cfg); err != nil {
		m.screen = screenResult
		m.err = err
		return m, nil
	}
	m.env.opts.serverURL = normalized
	m.screen = screenResult
	m.result = "Saved server URL: " + normalized
	return m, nil
}

func (m tuiModel) commitInitProject(value string) (tea.Model, tea.Cmd) {
	if value == "" {
		m.screen = screenResult
		m.err = errors.New("project_id cannot be empty")
		return m, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		m.screen = screenResult
		m.err = err
		return m, nil
	}
	values := ProjectDocValues{ProjectID: value, RoomEnabled: true}
	created, err := InitProjectFiles(cwd, values)
	if err != nil {
		m.screen = screenResult
		m.err = err
		return m, nil
	}
	if err := BindProject(cwd, values); err != nil {
		m.screen = screenResult
		m.err = err
		return m, nil
	}
	aiDir := filepath.Join(cwd, AITaskDirName)
	summary := fmt.Sprintf("Initialized project %s in %s", value, aiDir)
	if len(created) > 0 {
		summary += fmt.Sprintf("\nCreated %d files", len(created))
	} else {
		summary += "\nAll required files already existed"
	}
	m.screen = screenResult
	m.result = summary
	return m, nil
}

func (m tuiModel) commitChangeProject(value string) (tea.Model, tea.Cmd) {
	if value == "" {
		m.screen = screenResult
		m.err = errors.New("project_id cannot be empty")
		return m, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		m.screen = screenResult
		m.err = err
		return m, nil
	}
	if _, err := UseProject(cwd, value); err != nil {
		m.screen = screenResult
		m.err = err
		return m, nil
	}
	m.screen = screenResult
	m.result = "Switched active project to: " + value
	return m, nil
}

func (m tuiModel) View() string {
	switch m.screen {
	case screenMenu:
		return m.viewMenu()
	case screenServerInput:
		return m.viewInput("Set backend URL", "Enter the AITask backend base URL.")
	case screenProjectInput:
		return m.viewInput("Initialize project here", "Enter the project_id to initialize in the current directory.")
	case screenChangeProject:
		return m.viewInput("Change project_id", "Enter the new project_id (must already be bound or will be added).")
	case screenBusy:
		return titleStyle.Render("Working...") + "\n\n" + m.busyMsg + "\n"
	case screenResult:
		return m.viewResult()
	}
	return ""
}

func (m tuiModel) viewMenu() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("AITask CLI"))
	b.WriteString("\n")
	b.WriteString(strings.Repeat("─", 50))
	b.WriteString("\n")
	b.WriteString(labelStyle.Render("Server:  "))
	b.WriteString(valueStyle.Render(m.serverURL))
	b.WriteString("\n")
	b.WriteString(labelStyle.Render("Project: "))
	if m.projectID != "" {
		b.WriteString(valueStyle.Render(m.projectID))
		b.WriteString(labelStyle.Render(fmt.Sprintf("  (%s)", m.projectDir)))
	} else {
		b.WriteString(labelStyle.Render("not bound (run from a project dir)"))
	}
	b.WriteString("\n\n")

	for i, item := range menuItems {
		if i == m.cursor {
			b.WriteString(cursorStyle.Render("▸ " + item))
		} else {
			b.WriteString("  " + item)
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(hintStyle.Render("↑/↓ move · enter select · q quit"))
	b.WriteString("\n")
	return b.String()
}

func (m tuiModel) viewInput(title, hint string) string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(title))
	b.WriteString("\n\n")
	b.WriteString(labelStyle.Render(hint))
	b.WriteString("\n\n")
	b.WriteString(m.input.View())
	b.WriteString("\n\n")
	b.WriteString(hintStyle.Render("enter confirm · esc cancel"))
	b.WriteString("\n")
	return b.String()
}

func (m tuiModel) viewResult() string {
	var b strings.Builder
	if m.err != nil {
		b.WriteString(errStyle.Render("✗ Error"))
		b.WriteString("\n\n")
		b.WriteString(m.err.Error())
	} else {
		b.WriteString(okStyle.Render("✓ Done"))
		b.WriteString("\n\n")
		b.WriteString(m.result)
	}
	b.WriteString("\n\n")
	b.WriteString(hintStyle.Render("enter / esc to return"))
	b.WriteString("\n")
	return b.String()
}

// connectionResultMsg / actionResultMsg keep async work off the Update goroutine.
type connectionResultMsg struct {
	summary string
	err     error
}

type actionResultMsg struct {
	summary string
	err     error
}

func testConnectionCmd(env *CommandEnv) tea.Cmd {
	return func() tea.Msg {
		client, _, err := env.clientWithToken(false)
		if err != nil {
			return connectionResultMsg{err: fmt.Errorf("load token: %w", err)}
		}
		ctx, cancel := context.WithTimeout(context.Background(), env.opts.timeout)
		defer cancel()
		res, err := client.WhoAmI(ctx)
		if err != nil {
			return connectionResultMsg{err: err}
		}
		id := res.GetIdentity()
		summary := fmt.Sprintf(
			"Connected to %s\n\nAgent: %s\nType:  %s\nRole:  %s",
			env.opts.serverURL, id.GetAgentId(), id.GetAgentType(), id.GetRole(),
		)
		return connectionResultMsg{summary: summary}
	}
}
