package ui

import (
	"context"
	"fmt"
	"os/user"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"zapret2/tui/internal/config"
	"zapret2/tui/internal/profiles"
	"zapret2/tui/internal/service"
	"zapret2/tui/internal/strategy"
	"zapret2/tui/internal/sys"
)

type stratMode int

const (
	smList stratMode = iota
	smEdit
	smName
)

type strategies struct {
	mode    stratMode
	list    []profiles.Profile
	cursor  int
	current string
	ta      textarea.Model
	name    textinput.Model
	msg     string
	busy    bool
}

func newStrategies() *strategies {
	s := &strategies{}
	s.ta = textarea.New()
	s.ta.ShowLineNumbers = false
	s.ta.CharLimit = 0
	s.name = textinput.New()
	s.name.Placeholder = "profile name"
	s.name.CharLimit = 60
	s.reload()
	return s
}

func (s *strategies) reload() {
	s.list = profiles.Load()
	if s.cursor >= len(s.list) {
		s.cursor = len(s.list) - 1
	}
	if f, err := config.Load(sys.Path("config")); err == nil {
		s.current = f.GetOr("NFQWS2_OPT", "")
	}
}

func (s *strategies) Name() string    { return "Strategies" }
func (s *strategies) Init() tea.Cmd   { return nil }
func (s *strategies) Capturing() bool { return s.mode != smList }
func (s *strategies) Help() string {
	switch s.mode {
	case smEdit:
		return keys("ctrl+s", "validate & save", "esc", "cancel")
	case smName:
		return keys("enter", "save", "esc", "cancel")
	}
	return keys("↑↓", "select", "enter", "apply", "e", "edit active", "n", "save active as…", "d", "delete")
}

// validate runs nfqws2 --dry-run like install_easy.sh:dry_run_nfqws, with
// hostlist placeholders removed and shell word splitting (no globbing).
func validate(opt string) error {
	clean := strings.NewReplacer("<HOSTLIST>", "", "<HOSTLIST_NOAUTO>", "").Replace(opt)
	args := []string{"-c", `set -f; exec "$0" --dry-run --qnum=300 $2 $1`, sys.Path("nfq2", "nfqws2"), clean, ""}
	if sys.IsRoot() {
		// as root nfqws2 drops privileges; use the same user the init script
		// does (init.d/sysv/functions: WS_USER, default tpws)
		u := "tpws"
		if f, err := config.Load(sys.Path("config")); err == nil {
			u = f.GetOr("WS_USER", u)
		}
		if _, err := user.Lookup(u); err != nil {
			u = "nobody"
		}
		args[4] = "--user=" + u
	}
	out, err := sys.Output("sh", args...)
	if err != nil {
		lines := strings.Split(out, "\n")
		return fmt.Errorf("%s", lines[len(lines)-1])
	}
	return nil
}

func (s *strategies) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		s.ta.SetWidth(m.Width - navW - 6)
		s.ta.SetHeight(max(5, m.Height-12))
	case statusMsg:
		if s.mode == smList {
			s.reload()
		}
	case jobMsg:
		if m.id != "strat" {
			return s, nil
		}
		if m.done {
			s.busy = false
			if m.err != nil {
				s.msg = sBad.Render("restart failed: " + m.err.Error())
			} else {
				s.msg = sOK.Render("Applied, zapret2 restarted.")
			}
			return s, refresh
		}
		return s, m.next()
	case tea.KeyMsg:
		return s.key(m)
	}
	return s, nil
}

func (s *strategies) save(opt string) (Screen, tea.Cmd) {
	if !sys.IsRoot() {
		s.msg = sBad.Render("need root")
		return s, nil
	}
	if err := validate(opt); err != nil {
		s.msg = sBad.Render("nfqws2 rejected it: " + err.Error())
		return s, nil
	}
	f, err := config.Load(sys.Path("config"))
	if err != nil {
		s.msg = sBad.Render(err.Error())
		return s, nil
	}
	f.Set("NFQWS2_ENABLE", "1")
	f.Set("NFQWS2_OPT", opt)
	if err := f.Save(); err != nil {
		s.msg = sBad.Render(err.Error())
		return s, nil
	}
	s.current = opt
	s.busy = true
	s.msg = "Saved. Restarting zapret2…"
	return s, startJob(context.Background(), "strat", simple(service.Restart))
}

func (s *strategies) key(m tea.KeyMsg) (Screen, tea.Cmd) {
	k := m.String()
	switch s.mode {
	case smEdit:
		switch k {
		case "esc":
			s.mode = smList
			s.ta.Blur()
			return s, nil
		case "ctrl+s":
			opt := "\n" + strings.TrimSpace(s.ta.Value()) + "\n"
			ns, c := s.save(opt)
			if !strings.Contains(s.msg, "rejected") && !strings.Contains(s.msg, "need root") {
				s.mode = smList
				s.ta.Blur()
			}
			return ns, c
		}
		var c tea.Cmd
		s.ta, c = s.ta.Update(m)
		return s, c
	case smName:
		switch k {
		case "esc":
			s.mode = smList
			return s, nil
		case "enter":
			n := strings.TrimSpace(s.name.Value())
			if n != "" {
				if err := profiles.Add(profiles.Profile{Name: n, Opt: s.current}); err != nil {
					s.msg = sBad.Render(err.Error())
				} else {
					s.msg = sOK.Render("saved " + n)
				}
				s.reload()
			}
			s.mode = smList
			return s, nil
		}
		var c tea.Cmd
		s.name, c = s.name.Update(m)
		return s, c
	}
	if s.busy {
		return s, nil
	}
	switch k {
	case "up", "k":
		if s.cursor > 0 {
			s.cursor--
		}
	case "down", "j":
		if s.cursor < len(s.list)-1 {
			s.cursor++
		}
	case "enter":
		if len(s.list) > 0 {
			return s.save(s.list[s.cursor].Opt)
		}
	case "e":
		if !sys.Installed() {
			return s, nil
		}
		s.mode = smEdit
		s.ta.SetValue(strings.TrimSpace(s.current))
		s.msg = ""
		return s, s.ta.Focus()
	case "n":
		s.mode = smName
		s.name.SetValue("")
		return s, s.name.Focus()
	case "d":
		if len(s.list) > 0 && !s.list[s.cursor].BuiltIn {
			_ = profiles.Delete(s.list[s.cursor].Name)
			s.reload()
		}
	}
	return s, nil
}

func (s *strategies) View(w, h int) string {
	var b strings.Builder
	b.WriteString(sTitle.Render("Strategies") + "\n\n")
	if !sys.Installed() {
		return b.String() + needInstall()
	}
	if s.mode == smEdit {
		b.WriteString(sDim.Render("NFQWS2_OPT — one profile per line, end each but the last with --new.\n<HOSTLIST> is replaced by your lists depending on filter mode.") + "\n\n")
		b.WriteString(s.ta.View() + "\n")
		if s.msg != "" {
			b.WriteString(s.msg + "\n")
		}
		return b.String()
	}
	b.WriteString(sBold.Render("Active") + "\n")
	for _, p := range strategy.Split(s.current) {
		b.WriteString("  " + sAccent.Render(p.Proto().Label()) + "\n    " + wrapIndent(p.Actions(), w-6, 4) + "\n")
	}
	b.WriteString("\n" + sBold.Render("Saved") + "\n")
	for i, p := range s.list {
		tag := ""
		if p.BuiltIn {
			tag = sDim.Render(" (built-in)")
		}
		if strings.Join(strings.Fields(p.Opt), " ") == strings.Join(strings.Fields(s.current), " ") {
			tag += sOK.Render(" active")
		}
		line := "  " + p.Name + tag
		if i == s.cursor {
			line = sSel.Render("> "+p.Name) + tag
		}
		b.WriteString(line + "\n")
	}
	if len(s.list) > 0 {
		p := s.list[s.cursor]
		b.WriteString("\n")
		if p.Note != "" {
			b.WriteString(sDim.Render(truncate(p.Note, w-2)) + "\n")
		}
		for _, pr := range strategy.Split(p.Opt) {
			b.WriteString(sDim.Render("  "+fmt.Sprintf("%-22s", pr.Proto().Label())+pr.Summary()) + "\n")
		}
	}
	if s.mode == smName {
		b.WriteString("\nName: " + s.name.View() + "\n")
	}
	if s.msg != "" {
		b.WriteString("\n" + s.msg + "\n")
	}
	return b.String()
}
