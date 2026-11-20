// Package ui is the Bubble Tea front end.
package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"zapret2/tui/internal/config"
	"zapret2/tui/internal/service"
	"zapret2/tui/internal/sys"
)

// Screen is one sidebar entry.
type Screen interface {
	Name() string
	Init() tea.Cmd
	Update(tea.Msg) (Screen, tea.Cmd)
	View(w, h int) string
	Help() string
	// Capturing is true while the screen owns all keys (text input, running
	// job that esc would cancel, ...), so the app won't steal esc/q/tab.
	Capturing() bool
}

// statusMsg carries a fresh service snapshot to everyone.
type statusMsg struct {
	st   service.Status
	mode string
	fw   string
}

type tickMsg struct{}

// refreshStatus asks the app to poll status immediately.
type refreshStatus struct{}

func refresh() tea.Msg { return refreshStatus{} }

// gotoMsg switches screens.
type gotoMsg struct{ name string }

func pollStatus() tea.Msg {
	m := statusMsg{st: service.Get(), fw: sys.FWType()}
	if f, err := config.Load(sys.Path("config")); err == nil {
		m.mode = f.GetOr("MODE_FILTER", "none")
		if v, ok := f.Get("FWTYPE"); ok {
			m.fw = v
		}
	}
	return m
}

type App struct {
	screens  []Screen
	cur      int
	focusNav bool
	w, h     int
	status   statusMsg
	polled   bool
}

func New(start string) *App {
	a := &App{
		screens: []Screen{
			newDashboard(),
			newInstall(),
			newScan(),
			newStrategies(),
			newLists(),
			newSettings(),
			newLogs(),
			newUninstall(),
		},
		focusNav: true,
	}
	if start == "" && !sys.Installed() {
		start = "Install"
	}
	for i, s := range a.screens {
		if strings.EqualFold(s.Name(), start) {
			a.cur = i
			a.focusNav = false
		}
	}
	return a
}

func (a *App) Init() tea.Cmd {
	cmds := []tea.Cmd{pollStatus, tick(), tea.SetWindowTitle("zapret2")}
	for _, s := range a.screens {
		cmds = append(cmds, s.Init())
	}
	return tea.Batch(cmds...)
}

func tick() tea.Cmd { return tea.Tick(3*time.Second, func(time.Time) tea.Msg { return tickMsg{} }) }

func (a *App) broadcast(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd
	for i, s := range a.screens {
		ns, c := s.Update(msg)
		a.screens[i] = ns
		cmds = append(cmds, c)
	}
	return tea.Batch(cmds...)
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		a.w, a.h = m.Width, m.Height
		return a, a.broadcast(m)
	case tickMsg:
		return a, tea.Batch(pollStatus, tick())
	case refreshStatus:
		return a, pollStatus
	case statusMsg:
		a.status, a.polled = m, true
		return a, a.broadcast(m)
	case gotoMsg:
		for i, s := range a.screens {
			if s.Name() == m.name {
				a.cur, a.focusNav = i, false
			}
		}
		return a, nil
	case tea.KeyMsg:
		return a.key(m)
	}
	return a, a.broadcast(msg)
}

func (a *App) key(m tea.KeyMsg) (tea.Model, tea.Cmd) {
	scr := a.screens[a.cur]
	if m.String() == "ctrl+c" {
		return a, tea.Quit
	}
	if a.focusNav {
		switch m.String() {
		case "q":
			return a, tea.Quit
		case "up", "k":
			if a.cur > 0 {
				a.cur--
			}
		case "down", "j":
			if a.cur < len(a.screens)-1 {
				a.cur++
			}
		case "enter", "right", "l", "tab":
			a.focusNav = false
		default:
			// dashboard hotkeys (s/x/r/t) also work from the menu
			if scr.Name() == "Dashboard" {
				ns, c := scr.Update(m)
				a.screens[a.cur] = ns
				return a, c
			}
		}
		return a, nil
	}
	if !scr.Capturing() {
		switch m.String() {
		case "esc", "tab", "left", "h":
			a.focusNav = true
			return a, nil
		case "q":
			return a, tea.Quit
		}
	}
	ns, c := scr.Update(m)
	a.screens[a.cur] = ns
	return a, c
}

const navW = 16

func (a *App) View() string {
	if a.w == 0 {
		return ""
	}
	header := a.header()
	scr := a.screens[a.cur]
	help := scr.Help()
	if a.focusNav {
		help = keys("↑↓", "choose", "enter", "open", "q", "quit")
	} else if !scr.Capturing() {
		help = help + sDim.Render("  ·  ") + keys("esc", "menu")
	}
	footer := lipgloss.NewStyle().Foreground(cDim).Width(a.w).Render(truncate(help, a.w))

	bodyH := a.h - lipgloss.Height(header) - lipgloss.Height(footer) - 1
	if bodyH < 5 {
		bodyH = 5
	}
	contentW := a.w - navW - 3

	var nav strings.Builder
	for i, s := range a.screens {
		name := s.Name()
		line := "  " + name
		if i == a.cur {
			if a.focusNav {
				line = sSel.Render("> " + name)
			} else {
				line = sAccent.Render("> " + name)
			}
		}
		nav.WriteString(lipgloss.NewStyle().Width(navW).Render(line) + "\n")
	}
	navBox := lipgloss.NewStyle().Width(navW).Height(bodyH).
		Border(lipgloss.NormalBorder(), false, true, false, false).BorderForeground(cLine).
		Render(nav.String())

	content := scr.View(contentW, bodyH)
	content = lipgloss.NewStyle().Width(contentW).Height(bodyH).MaxHeight(bodyH).MaxWidth(contentW).PaddingLeft(1).Render(content)

	body := lipgloss.JoinHorizontal(lipgloss.Top, navBox, content)
	sep := lipgloss.NewStyle().Foreground(cLine).Render(strings.Repeat("─", a.w))
	return lipgloss.JoinVertical(lipgloss.Left, header, sep, body, footer)
}

func (a *App) header() string {
	title := sTitle.Render("zapret2")
	if !a.polled {
		return title + sDim.Render("  checking status…")
	}
	st := a.status.st
	var state string
	switch {
	case !st.Installed:
		state = sDim.Render("○ NOT INSTALLED")
	case st.Running():
		state = sOK.Render("● RUNNING")
	case st.Active == "failed":
		state = sBad.Render("● FAILED")
	default:
		state = sBad.Render("○ STOPPED")
	}
	parts := []string{state}
	if len(st.PIDs) > 0 {
		parts = append(parts, fmt.Sprintf("nfqws2 pid %d", st.PIDs[0]))
	}
	if a.status.fw != "" {
		parts = append(parts, a.status.fw)
	}
	if st.Installed {
		parts = append(parts, "filter: "+a.status.mode)
	}
	if !sys.IsRoot() {
		parts = append(parts, sBad.Render("not root — read-only"))
	}
	return truncate(title+"  "+strings.Join(parts, sDim.Render(" · ")), a.w)
}

// needRoot renders a standard message for screens that change the system.
func needRoot() string {
	return sBad.Render("This needs root.") + "\n\n" + "Restart with:  " + sAccent.Render("sudo zapret2-tui")
}

func needInstall() string {
	return sDim.Render("zapret2 is not installed yet.") + "\n\nOpen " + sAccent.Render("Install") + " in the menu to set it up."
}
