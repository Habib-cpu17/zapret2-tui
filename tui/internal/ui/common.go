package ui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// ---- styles: plain workbench look, amber accent, no gradients ----

var (
	cAccent = lipgloss.AdaptiveColor{Light: "#9A6200", Dark: "#E0A84E"}
	cOK     = lipgloss.AdaptiveColor{Light: "#2E7D32", Dark: "#86B866"}
	cBad    = lipgloss.AdaptiveColor{Light: "#C62828", Dark: "#E06C60"}
	cDim    = lipgloss.AdaptiveColor{Light: "#707070", Dark: "#8A8A8A"}
	cLine   = lipgloss.AdaptiveColor{Light: "#C8C8C8", Dark: "#3E3E3E"}
	cSelBg  = lipgloss.AdaptiveColor{Light: "#EFE3CC", Dark: "#3A3226"}

	sAccent = lipgloss.NewStyle().Foreground(cAccent)
	sBold   = lipgloss.NewStyle().Bold(true)
	sOK     = lipgloss.NewStyle().Foreground(cOK)
	sBad    = lipgloss.NewStyle().Foreground(cBad)
	sDim    = lipgloss.NewStyle().Foreground(cDim)
	sTitle  = lipgloss.NewStyle().Bold(true).Foreground(cAccent)
	sSel    = lipgloss.NewStyle().Background(cSelBg).Bold(true)
	sKey    = lipgloss.NewStyle().Foreground(cAccent).Bold(true)
	sLabel  = lipgloss.NewStyle().Foreground(cDim).Width(14)
	sBox    = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(cLine).Padding(0, 1)
)

func ok(b bool, yes, no string) string {
	if b {
		return sOK.Render("✓ " + yes)
	}
	return sBad.Render("✗ " + no)
}

func row(label, value string) string { return sLabel.Render(label) + value }

// keys renders "[k] label" hints.
func keys(pairs ...string) string {
	var out []string
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, sKey.Render(pairs[i])+" "+sDim.Render(pairs[i+1]))
	}
	return strings.Join(out, sDim.Render("  ·  "))
}

func truncate(s string, w int) string {
	if w <= 1 {
		return s
	}
	return ansi.Truncate(s, w, "…")
}

// ---- background jobs: run fn in a goroutine, stream log lines as msgs ----

type jobMsg struct {
	id       string
	ch       <-chan jobMsg
	line     string
	progress float64
	hasProg  bool
	done     bool
	err      error
}

type jobFn func(ctx context.Context, log func(string), progress func(float64)) error

func startJob(ctx context.Context, id string, fn jobFn) tea.Cmd {
	ch := make(chan jobMsg, 256)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				ch <- jobMsg{id: id, done: true, err: fmt.Errorf("panic: %v", r)}
			}
			close(ch)
		}()
		err := fn(ctx,
			func(l string) { ch <- jobMsg{id: id, line: l} },
			func(p float64) { ch <- jobMsg{id: id, progress: p, hasProg: true} })
		ch <- jobMsg{id: id, done: true, err: err}
	}()
	return waitJob(ch)
}

func waitJob(ch <-chan jobMsg) tea.Cmd {
	return func() tea.Msg {
		m, okk := <-ch
		if !okk {
			return nil
		}
		m.ch = ch
		return m
	}
}

// next returns the command to keep receiving from a job.
func (m jobMsg) next() tea.Cmd {
	if m.done {
		return nil
	}
	return waitJob(m.ch)
}

// simple wraps a func without ctx/progress.
func simple(f func(log func(string)) error) jobFn {
	return func(_ context.Context, log func(string), _ func(float64)) error { return f(log) }
}

// ---- choice: a vertical single-select with descriptions ----

type option struct{ value, label, desc string }

type choice struct {
	title  string
	opts   []option
	cursor int
}

func (c *choice) up() {
	if c.cursor > 0 {
		c.cursor--
	}
}
func (c *choice) down() {
	if c.cursor < len(c.opts)-1 {
		c.cursor++
	}
}
func (c *choice) value() string { return c.opts[c.cursor].value }
func (c *choice) set(v string) {
	for i, o := range c.opts {
		if o.value == v {
			c.cursor = i
		}
	}
}

func (c choice) view(focused bool) string {
	var b strings.Builder
	if c.title != "" {
		b.WriteString(sBold.Render(c.title) + "\n")
	}
	for i, o := range c.opts {
		mark := "( )"
		if i == c.cursor {
			mark = sAccent.Render("(•)")
		}
		line := mark + " " + o.label
		if i == c.cursor && focused {
			line = sSel.Render(line)
		}
		b.WriteString(line)
		if o.desc != "" && i == c.cursor {
			b.WriteString("  " + sDim.Render(o.desc))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// logBuf keeps the tail of a log.
type logBuf struct {
	lines []string
	max   int
}

func (l *logBuf) add(s string) {
	if l.max == 0 {
		l.max = 3000
	}
	l.lines = append(l.lines, s)
	if len(l.lines) > l.max {
		l.lines = l.lines[len(l.lines)-l.max:]
	}
}

func (l logBuf) tail(n, width int) string {
	start := len(l.lines) - n
	if start < 0 {
		start = 0
	}
	var out []string
	for _, s := range l.lines[start:] {
		out = append(out, truncate(s, width))
	}
	return strings.Join(out, "\n")
}

// wrapIndent wraps space-separated words to width, indenting continuation lines.
func wrapIndent(s string, width, indent int) string {
	if width < 20 {
		width = 20
	}
	var lines []string
	cur := ""
	var words []string
	for _, w := range strings.Fields(s) {
		for len([]rune(w)) > width { // hard-break very long options
			words = append(words, string([]rune(w)[:width]))
			w = string([]rune(w)[width:])
		}
		words = append(words, w)
	}
	for _, w := range words {
		if cur != "" && len([]rune(cur))+1+len([]rune(w)) > width {
			lines = append(lines, cur)
			cur = w
			continue
		}
		if cur == "" {
			cur = w
		} else {
			cur += " " + w
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return strings.Join(lines, "\n"+strings.Repeat(" ", indent))
}

// cleanups run when the program exits (see Cleanup).
var cleanups []func()

func onExit(f func()) { cleanups = append(cleanups, f) }

// Cleanup stops background work: called by main after the UI exits.
func Cleanup() {
	for i := len(cleanups) - 1; i >= 0; i-- {
		cleanups[i]()
	}
	cleanups = nil
}
