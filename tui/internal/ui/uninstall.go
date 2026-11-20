package ui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"zapret2/tui/internal/install"
	"zapret2/tui/internal/sys"
)

type uninstallScreen struct {
	armed bool
	busy  bool
	done  bool
	err   error
	log   logBuf
}

func newUninstall() *uninstallScreen { return &uninstallScreen{} }

func (u *uninstallScreen) Name() string    { return "Uninstall" }
func (u *uninstallScreen) Init() tea.Cmd   { return nil }
func (u *uninstallScreen) Capturing() bool { return u.busy }
func (u *uninstallScreen) Help() string {
	if u.armed {
		return keys("y", "yes, remove everything", "n", "cancel")
	}
	return keys("enter", "uninstall")
}

func (u *uninstallScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch m := msg.(type) {
	case jobMsg:
		if m.id != "uninstall" {
			return u, nil
		}
		if m.line != "" {
			u.log.add(m.line)
		}
		if m.done {
			u.busy, u.done, u.err = false, true, m.err
			return u, refresh
		}
		return u, m.next()
	case tea.KeyMsg:
		if u.busy {
			return u, nil
		}
		switch m.String() {
		case "enter":
			if sys.IsRoot() && sys.Exists(sys.Base) {
				u.armed, u.done = true, false
			}
		case "n":
			u.armed = false
		case "y":
			if u.armed {
				u.armed, u.busy = false, true
				u.log = logBuf{}
				return u, startJob(context.Background(), "uninstall", simple(install.Uninstall))
			}
		}
	}
	return u, nil
}

func (u *uninstallScreen) View(w, h int) string {
	var b strings.Builder
	b.WriteString(sTitle.Render("Uninstall") + "\n\n")
	switch {
	case u.busy:
		b.WriteString("Removing zapret2…\n\n")
	case u.done && u.err == nil:
		b.WriteString(sOK.Render("zapret2 removed.") + " Firewall rules, service and " + sys.Base + " are gone.\n\n")
	case u.done:
		b.WriteString(sBad.Render("Uninstall had errors: "+u.err.Error()) + "\n\n")
	case !sys.Exists(sys.Base):
		b.WriteString(sDim.Render("zapret2 is not installed.") + "\n")
	case !sys.IsRoot():
		b.WriteString(needRoot() + "\n")
	case u.armed:
		b.WriteString(sBad.Render("This stops zapret2, removes its service, firewall rules and deletes "+sys.Base) + "\n")
		b.WriteString(sBad.Render("including your config, lists and saved strategies.") + "\n\n")
		b.WriteString("Are you sure? " + keys("y", "yes", "n", "no") + "\n")
	default:
		b.WriteString("Stops zapret2, removes the systemd service and timer, firewall rules,\nipsets, and deletes " + sys.Base + ".\n\nPress " + sKey.Render("enter") + " to continue.\n")
	}
	n := h - strings.Count(b.String(), "\n") - 1
	if n > 0 {
		b.WriteString(sDim.Render(u.log.tail(n, w-2)))
	}
	return b.String()
}
