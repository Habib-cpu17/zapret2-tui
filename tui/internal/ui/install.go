package ui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"

	"zapret2/tui/internal/install"
	"zapret2/tui/internal/sys"
)

type instStage int

const (
	stPre instStage = iota
	stAsk
	stConfirm
	stRun
	stDone
)

type installScreen struct {
	stage   instStage
	missing []string
	fw      string
	q       []*choice // questions
	qi      int
	steps   []install.Step
	stepIdx int
	stepErr error
	bar     progress.Model
	prog    float64
	log     logBuf
	pkgBusy bool
}

func newInstall() *installScreen {
	s := &installScreen{bar: progress.New(progress.WithSolidFill("#E0A84E"), progress.WithoutPercentage())}
	s.reset()
	return s
}

func (s *installScreen) reset() {
	s.stage, s.qi, s.stepIdx, s.stepErr, s.prog = stPre, 0, 0, nil, 0
	s.fw = sys.FWType()
	s.missing = sys.Missing(s.fw)
	ifaces := []option{{"", "This is a desktop/server (not a router)", "zapret2 handles this machine's own traffic"}}
	for _, i := range sys.Interfaces() {
		ifaces = append(ifaces, option{i, i, ""})
	}
	wan := append([]option{{"", "Auto (interface with default route)", ""}}, ifaces[1:]...)
	s.q = []*choice{
		{title: "Which traffic should zapret2 touch?", opts: []option{
			{"autohostlist", "Auto-learn blocked sites", "starts with your bypass list, adds sites that fail. Recommended"},
			{"hostlist", "Only sites in my bypass list", "most conservative; edit the list in Lists"},
			{"none", "Everything on ports 80/443", "simplest; can slow down or break some sites"},
		}},
		{title: "Handle IPv6 too?", opts: []option{
			{"0", "No, IPv4 only", "default"},
			{"1", "Yes", "only if your ISP gives you IPv6"},
		}},
		{title: "Is this machine a router for other devices? Pick its LAN interface:", opts: ifaces},
		{title: "WAN interface (internet side):", opts: wan},
	}
}

func (s *installScreen) Name() string  { return "Install" }
func (s *installScreen) Init() tea.Cmd { return nil }
func (s *installScreen) Capturing() bool {
	return s.stage == stRun || s.stage == stAsk || s.stage == stConfirm
}

func (s *installScreen) Help() string {
	switch s.stage {
	case stPre:
		h := []string{"enter", "continue"}
		if len(s.missing) > 0 {
			h = []string{"p", "install packages", "enter", "continue"}
		}
		return keys(h...)
	case stAsk:
		return keys("↑↓", "choose", "enter", "next", "esc", "back")
	case stConfirm:
		return keys("enter", "install", "esc", "back")
	case stRun:
		return sDim.Render("installing…")
	}
	return keys("enter", "start over")
}

func (s *installScreen) opts() install.Options {
	o := install.Options{ModeFilter: s.q[0].value(), IPv6: s.q[1].value() == "1", FWType: s.fw, IfaceLAN: s.q[2].value()}
	if o.IfaceLAN != "" {
		o.IfaceWAN = s.q[3].value()
	}
	return o
}

func (s *installScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		s.bar.Width = min(50, m.Width-navW-20)
	case jobMsg:
		switch m.id {
		case "pkg":
			if m.line != "" {
				s.log.add(m.line)
			}
			if m.done {
				s.pkgBusy = false
				if m.err != nil {
					s.log.add(sBad.Render(m.err.Error()))
				}
				s.fw = sys.FWType()
				s.missing = sys.Missing(s.fw)
			}
			return s, m.next()
		case "install":
			if m.line != "" {
				s.log.add("  " + m.line)
			}
			if m.hasProg {
				s.prog = m.progress
			}
			if m.done {
				if m.err != nil {
					s.stepErr = m.err
					s.log.add(sBad.Render("✗ " + m.err.Error()))
					s.stage = stDone
					return s, refresh
				}
				s.stepIdx++
				return s, s.runStep()
			}
			return s, m.next()
		}
	case tea.KeyMsg:
		return s.key(m)
	}
	return s, nil
}

func (s *installScreen) runStep() tea.Cmd {
	if s.stepIdx >= len(s.steps) {
		s.stage = stDone
		s.log.add(sOK.Render("✓ zapret2 installed and running"))
		return refresh
	}
	st := s.steps[s.stepIdx]
	s.prog = 0
	s.log.add(sAccent.Render(fmt.Sprintf("[%d/%d] %s", s.stepIdx+1, len(s.steps), st.Name)))
	return startJob(context.Background(), "install", func(_ context.Context, log func(string), p func(float64)) error {
		return st.Run(log, p)
	})
}

func (s *installScreen) key(m tea.KeyMsg) (Screen, tea.Cmd) {
	k := m.String()
	switch s.stage {
	case stPre:
		if !sys.IsRoot() || !sys.HasSystemd() {
			return s, nil
		}
		if k == "p" && len(s.missing) > 0 && !s.pkgBusy {
			pm := sys.PkgManager()
			if pm == nil {
				s.log.add(sBad.Render("no supported package manager found; install manually: " + strings.Join(s.missing, " ")))
				return s, nil
			}
			args := append(pm[1:], s.missing...)
			s.pkgBusy = true
			s.log.add(sAccent.Render("» " + pm[0] + " " + strings.Join(args, " ")))
			return s, startJob(context.Background(), "pkg", simple(func(l func(string)) error { return sys.RunLogged(l, pm[0], args...) }))
		}
		if k == "enter" && len(s.missing) == 0 && s.fw != "unsupported" {
			s.stage = stAsk
			s.log = logBuf{}
		}
	case stAsk:
		q := s.q[s.qi]
		switch k {
		case "up", "k":
			q.up()
		case "down", "j":
			q.down()
		case "esc":
			if s.qi == 0 {
				s.stage = stPre
			} else {
				s.qi--
			}
		case "enter":
			// skip WAN question on desktops
			if s.qi == 2 && q.value() == "" {
				s.stage = stConfirm
			} else if s.qi < len(s.q)-1 {
				s.qi++
			} else {
				s.stage = stConfirm
			}
		}
	case stConfirm:
		switch k {
		case "esc":
			s.stage = stAsk
		case "enter":
			s.stage = stRun
			s.log = logBuf{}
			s.steps = install.Plan(s.opts())
			s.stepIdx = 0
			return s, s.runStep()
		}
	case stDone:
		if k == "enter" {
			s.reset()
			s.log = logBuf{}
			if s.stepErr == nil && sys.Installed() {
				return s, func() tea.Msg { return gotoMsg{"Dashboard"} }
			}
		}
	}
	return s, nil
}

func (s *installScreen) View(w, h int) string {
	var b strings.Builder
	b.WriteString(sTitle.Render("Install zapret2") + "\n\n")
	switch s.stage {
	case stPre:
		if sys.Installed() {
			b.WriteString(sDim.Render("zapret2 is already installed. Continuing will update it to the latest release\nand keep your config, lists and custom scripts.") + "\n\n")
		}
		b.WriteString(sBold.Render("System check") + "\n")
		b.WriteString(row("root", ok(sys.IsRoot(), "yes", "no — run with sudo")) + "\n")
		b.WriteString(row("systemd", ok(sys.HasSystemd(), "yes", "not found (only systemd is supported)")) + "\n")
		b.WriteString(row("firewall", ok(s.fw != "unsupported", s.fw, "need nftables or iptables")) + "\n")
		b.WriteString(row("packages", ok(len(s.missing) == 0, "all required tools present", "missing: "+strings.Join(s.missing, " "))) + "\n")
		b.WriteString("\n")
		switch {
		case !sys.IsRoot():
			b.WriteString(needRoot() + "\n")
		case s.pkgBusy:
			b.WriteString(sDim.Render("installing packages…") + "\n")
		case len(s.missing) > 0:
			b.WriteString("Press " + sKey.Render("p") + " to install the missing packages.\n")
		case sys.HasSystemd():
			b.WriteString("Press " + sKey.Render("enter") + " to continue.\n")
		}
	case stAsk:
		b.WriteString(sDim.Render(fmt.Sprintf("Question %d", s.qi+1)) + "\n")
		b.WriteString(s.q[s.qi].view(true))
	case stConfirm:
		o := s.opts()
		b.WriteString(sBold.Render("Ready to install") + "\n\n")
		b.WriteString(row("Location", sys.Base) + "\n")
		b.WriteString(row("Filter mode", o.ModeFilter) + "\n")
		b.WriteString(row("IPv6", map[bool]string{true: "yes", false: "no"}[o.IPv6]) + "\n")
		b.WriteString(row("Firewall", o.FWType) + "\n")
		if o.IfaceLAN != "" {
			wan := o.IfaceWAN
			if wan == "" {
				wan = "auto"
			}
			b.WriteString(row("Router", "LAN "+o.IfaceLAN+", WAN "+wan) + "\n")
		}
		b.WriteString("\n" + sDim.Render("Downloads the latest zapret2 release from GitHub, verifies its checksum,\ninstalls a systemd service and starts it with the default strategy.\nRun Find strategy afterwards to pick one that works on your network.") + "\n\n")
		b.WriteString("Press " + sKey.Render("enter") + " to install.\n")
	case stRun, stDone:
		if s.stage == stRun && s.stepIdx < len(s.steps) {
			b.WriteString(fmt.Sprintf("Step %d of %d  ", s.stepIdx+1, len(s.steps)) + s.bar.ViewAs(float64(s.stepIdx)/float64(len(s.steps))+s.prog/float64(len(s.steps))) + "\n\n")
		}
		if s.stage == stDone {
			if s.stepErr != nil {
				b.WriteString(sBad.Render("Install failed.") + " Fix the problem above and press enter to retry.\n\n")
			} else {
				b.WriteString(sOK.Render("Done.") + " Press enter to open the Dashboard. Next: " + sAccent.Render("Find strategy") + ".\n\n")
			}
		}
	}
	used := strings.Count(b.String(), "\n")
	if n := h - used - 1; n > 0 {
		b.WriteString(s.log.tail(n, w-2))
	}
	return b.String()
}
