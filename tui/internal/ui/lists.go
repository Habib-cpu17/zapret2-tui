package ui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"zapret2/tui/internal/config"
	"zapret2/tui/internal/lists"
	"zapret2/tui/internal/sys"
)

type listsScreen struct {
	fileI   int
	entries []string
	cursor  int
	adding  bool
	in      textinput.Model
	src     choice
	picking bool
	msg     string
	busy    bool
	log     logBuf
}

func newLists() *listsScreen {
	s := &listsScreen{}
	s.in = textinput.New()
	s.in.Placeholder = "example.com (or paste a URL)"
	s.in.CharLimit = 253
	s.in.Prompt = ""
	s.src.title = "Where should extra lists come from? (GETLIST)"
	for _, src := range lists.Sources {
		s.src.opts = append(s.src.opts, option{src.Script, src.Title, src.Script})
	}
	s.load()
	return s
}

func (s *listsScreen) file() lists.File { return lists.Files[s.fileI] }

func (s *listsScreen) load() {
	s.entries = lists.Read(s.file().Key)
	if s.cursor >= len(s.entries) {
		s.cursor = max(0, len(s.entries)-1)
	}
}

func (s *listsScreen) Name() string    { return "Lists" }
func (s *listsScreen) Init() tea.Cmd   { return nil }
func (s *listsScreen) Capturing() bool { return s.adding || s.picking }
func (s *listsScreen) Help() string {
	if s.adding {
		return keys("enter", "add", "esc", "cancel")
	}
	if s.picking {
		return keys("↑↓", "choose", "enter", "save", "esc", "cancel")
	}
	return keys("[ ]", "switch list", "↑↓", "move", "a", "add", "d", "delete", "g", "list source", "u", "update now")
}

func (s *listsScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch m := msg.(type) {
	case statusMsg:
		if !s.adding {
			s.load()
		}
	case jobMsg:
		if m.id != "lists" {
			return s, nil
		}
		if m.line != "" {
			s.log.add(m.line)
		}
		if m.done {
			s.busy = false
			if m.err != nil {
				s.msg = sBad.Render("update failed: " + m.err.Error())
			} else {
				s.msg = sOK.Render("lists updated")
			}
			s.load()
			return s, nil
		}
		return s, m.next()
	case tea.KeyMsg:
		return s.key(m)
	}
	return s, nil
}

func (s *listsScreen) key(m tea.KeyMsg) (Screen, tea.Cmd) {
	k := m.String()
	if s.adding {
		switch k {
		case "esc":
			s.adding = false
		case "enter":
			v := strings.TrimSpace(s.in.Value())
			if v != "" {
				for _, e := range strings.Fields(strings.ReplaceAll(v, ",", " ")) {
					if err := lists.Add(s.file().Key, e); err != nil {
						s.msg = sBad.Render(err.Error())
					}
				}
				s.load()
				s.msg = sOK.Render("added. nfqws2 picks up list changes automatically.")
			}
			s.in.SetValue("")
			s.adding = false
		default:
			var c tea.Cmd
			s.in, c = s.in.Update(m)
			return s, c
		}
		return s, nil
	}
	if s.picking {
		switch k {
		case "up", "k":
			s.src.up()
		case "down", "j":
			s.src.down()
		case "esc":
			s.picking = false
		case "enter":
			s.picking = false
			f, err := config.Load(sys.Path("config"))
			if err == nil {
				f.SetOrUnset("GETLIST", s.src.value())
				err = f.Save()
			}
			if err != nil {
				s.msg = sBad.Render(err.Error())
			} else {
				s.msg = sOK.Render("list source saved. Press u to download now.")
			}
		}
		return s, nil
	}
	if s.busy {
		return s, nil
	}
	writable := sys.IsRoot() && sys.Installed()
	switch k {
	case "]", "L":
		s.fileI = (s.fileI + 1) % len(lists.Files)
		s.cursor, s.msg = 0, ""
		s.load()
	case "[", "H":
		s.fileI = (s.fileI - 1 + len(lists.Files)) % len(lists.Files)
		s.cursor, s.msg = 0, ""
		s.load()
	case "up", "k":
		if s.cursor > 0 {
			s.cursor--
		}
	case "down", "j":
		if s.cursor < len(s.entries)-1 {
			s.cursor++
		}
	case "a":
		if !writable {
			s.msg = sBad.Render("need root and an installed zapret2")
			return s, nil
		}
		s.adding = true
		return s, s.in.Focus()
	case "d", "delete":
		if writable && len(s.entries) > 0 {
			_ = lists.Remove(s.file().Key, s.entries[s.cursor])
			s.load()
		}
	case "g":
		if !writable {
			s.msg = sBad.Render("need root and an installed zapret2")
			return s, nil
		}
		if f, err := config.Load(sys.Path("config")); err == nil {
			s.src.set(f.GetOr("GETLIST", ""))
		}
		s.picking = true
	case "u":
		if writable {
			s.busy = true
			s.log = logBuf{}
			s.msg = "updating lists…"
			return s, startJob(context.Background(), "lists", simple(lists.Update))
		}
	}
	return s, nil
}

func (s *listsScreen) View(w, h int) string {
	var b strings.Builder
	b.WriteString(sTitle.Render("Lists") + "\n\n")
	if !sys.Installed() {
		return b.String() + needInstall()
	}
	var tabs []string
	for i, f := range lists.Files {
		t := fmt.Sprintf("%s (%d)", f.Title, lists.Count(f.Key))
		if i == s.fileI {
			t = sSel.Render(" " + t + " ")
		} else {
			t = sDim.Render(" " + t + " ")
		}
		tabs = append(tabs, t)
	}
	b.WriteString(strings.Join(tabs, " ") + "\n")
	b.WriteString(sDim.Render(truncate(s.file().Help, w-2)) + "\n\n")

	if s.picking {
		b.WriteString(s.src.view(true))
		return b.String()
	}
	if s.adding {
		b.WriteString("Add: " + s.in.View() + "\n\n")
	}
	rows := h - strings.Count(b.String(), "\n") - 4
	if len(s.entries) == 0 {
		b.WriteString(sDim.Render("  (empty)") + "\n")
	}
	start := 0
	if s.cursor >= rows {
		start = s.cursor - rows + 1
	}
	for i := start; i < len(s.entries) && i-start < rows; i++ {
		l := "  " + s.entries[i]
		if i == s.cursor {
			l = sSel.Render("> " + s.entries[i])
		}
		b.WriteString(l + "\n")
	}
	if gen := lists.Generated(); len(gen) > 0 {
		b.WriteString(sDim.Render("\ndownloaded: "+strings.Join(gen, ", ")) + "\n")
	}
	if s.msg != "" {
		b.WriteString("\n" + s.msg + "\n")
	}
	if s.busy {
		b.WriteString(sDim.Render(s.log.tail(3, w-2)) + "\n")
	}
	return b.String()
}
