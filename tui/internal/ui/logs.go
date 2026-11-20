package ui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"zapret2/tui/internal/sys"
)

// logsScreen follows the zapret2 unit's journal.
type logsScreen struct {
	log     logBuf
	cancel  context.CancelFunc
	running bool
	scroll  int // lines from bottom
}

func newLogs() *logsScreen { return &logsScreen{} }

func (l *logsScreen) Name() string    { return "Logs" }
func (l *logsScreen) Capturing() bool { return false }
func (l *logsScreen) Help() string {
	return keys("↑↓/pgup/pgdn", "scroll", "end", "follow", "c", "clear")
}

func (l *logsScreen) Init() tea.Cmd {
	if !sys.Has("journalctl") {
		l.log.add("journalctl not found")
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	l.cancel, l.running = cancel, true
	onExit(cancel)
	return startJob(ctx, "logs", func(ctx context.Context, log func(string), _ func(float64)) error {
		lines := make(chan string, 256)
		errc := make(chan error, 1)
		go func() {
			errc <- sys.Stream(ctx, nil, lines, "journalctl", "-u", "zapret2", "-u", "zapret2-list-update", "-n", "300", "-f", "-o", "short-iso", "--no-pager")
		}()
		for x := range lines {
			log(x)
		}
		return <-errc
	})
}

func (l *logsScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch m := msg.(type) {
	case jobMsg:
		if m.id != "logs" {
			return l, nil
		}
		if m.line != "" {
			l.log.add(m.line)
			if l.scroll > 0 {
				l.scroll++
			}
		}
		if m.done {
			l.running = false
			if m.err != nil {
				l.log.add(sBad.Render("journal stopped: " + m.err.Error()))
			}
		}
		return l, m.next()
	case tea.KeyMsg:
		switch m.String() {
		case "up", "k":
			l.scroll++
		case "down", "j":
			if l.scroll > 0 {
				l.scroll--
			}
		case "pgup":
			l.scroll += 15
		case "pgdown":
			l.scroll = max(0, l.scroll-15)
		case "end", "G":
			l.scroll = 0
		case "c":
			l.log, l.scroll = logBuf{}, 0
		}
		if l.scroll > len(l.log.lines) {
			l.scroll = len(l.log.lines)
		}
	}
	return l, nil
}

func (l *logsScreen) View(w, h int) string {
	var b strings.Builder
	b.WriteString(sTitle.Render("Logs") + sDim.Render("  journalctl -u zapret2 -f") + "\n\n")
	if !sys.IsRoot() {
		b.WriteString(sDim.Render("(not root: you may only see part of the journal)") + "\n")
	}
	n := h - strings.Count(b.String(), "\n") - 1
	end := len(l.log.lines) - l.scroll
	start := max(0, end-n)
	for _, s := range l.log.lines[start:end] {
		b.WriteString(truncate(s, w-2) + "\n")
	}
	if l.scroll > 0 {
		b.WriteString(sAccent.Render("— paused, press end to follow —"))
	}
	return b.String()
}
