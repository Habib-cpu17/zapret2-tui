package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"zapret2/tui/internal/blockcheck"
	"zapret2/tui/internal/config"
	"zapret2/tui/internal/lists"
	"zapret2/tui/internal/profiles"
	"zapret2/tui/internal/service"
	"zapret2/tui/internal/strategy"
	"zapret2/tui/internal/sys"
)

type scanStage int

const (
	scForm scanStage = iota
	scRun
	scResults
)

var (
	ipvOpts   = []string{"4", "6", "46"}
	levelOpts = []string{"quick", "standard", "force"}
	levelHelp = map[string]string{
		"quick":    "fastest; stops at first working variant",
		"standard": "explores what works on your DPI (slower)",
		"force":    "tries everything; can take a long time",
	}
	repeatOpts = []int{1, 2, 3, 5}
)

const (
	fDomains = iota
	fHTTP
	fTLS12
	fTLS13
	fQUIC
	fIPV
	fLevel
	fRepeats
	fStart
	nFields
)

type scan struct {
	h3       bool // curl supports HTTP/3
	stage    scanStage
	field    int
	dom      textinput.Model
	o        blockcheck.Options
	ipvI     int
	levelI   int
	repI     int
	cancel   context.CancelFunc
	finished *bool
	wasUp    bool
	all      []string
	log      logBuf
	prog     blockcheck.Progress
	started  time.Time
	err      error

	ranked  []blockcheck.Ranked
	cursor  int
	chosen  map[strategy.Proto]int
	showLog bool
	msg     string
}

func newScan() *scan {
	s := &scan{o: blockcheck.DefaultOptions(), chosen: map[strategy.Proto]int{}, h3: sys.CurlHTTP3()}
	s.dom = textinput.New()
	s.dom.Placeholder = "youtube.com discord.com"
	s.dom.SetValue(strings.Join(s.o.Domains, " "))
	s.dom.CharLimit = 512
	s.dom.Width = 50
	s.dom.Prompt = ""
	s.dom.Focus()
	return s
}

func (s *scan) Name() string  { return "Find strategy" }
func (s *scan) Init() tea.Cmd { return nil }
func (s *scan) Capturing() bool {
	return (s.stage == scForm && s.field == fDomains) || s.stage == scRun
}

func (s *scan) Help() string {
	switch s.stage {
	case scForm:
		if s.field == fDomains {
			return keys("type", "domains", "↓/enter", "next field", "esc", "menu")
		}
		return keys("↑↓", "field", "space/←→", "change", "enter", "start scan")
	case scRun:
		return keys("esc", "cancel scan")
	}
	return keys("↑↓", "move", "space", "pick for its protocol", "a", "apply picked", "v", "log", "n", "new scan")
}

func (s *scan) syncOpts() {
	s.o.Domains = strings.Fields(strings.ReplaceAll(s.dom.Value(), ",", " "))
	s.o.IPVs = ipvOpts[s.ipvI]
	s.o.ScanLevel = levelOpts[s.levelI]
	s.o.Repeats = repeatOpts[s.repI]
}

func (s *scan) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch m := msg.(type) {
	case jobMsg:
		return s.onJob(m)
	case tea.KeyMsg:
		switch s.stage {
		case scForm:
			return s.formKey(m)
		case scRun:
			if m.String() == "esc" && s.cancel != nil {
				s.cancel()
				s.log.add(sBad.Render("cancelling…"))
			}
			return s, nil
		case scResults:
			return s.resultKey(m)
		}
	}
	return s, nil
}

func (s *scan) formKey(m tea.KeyMsg) (Screen, tea.Cmd) {
	k := m.String()
	if s.field == fDomains {
		switch k {
		case "down", "enter", "tab":
			s.field++
			s.dom.Blur()
			return s, nil
		case "esc":
			s.dom.Blur()
			s.field = fStart
			return s, nil // App sees Capturing()=false next key
		}
		var c tea.Cmd
		s.dom, c = s.dom.Update(m)
		return s, c
	}
	toggle := func(dir int) {
		switch s.field {
		case fHTTP:
			s.o.HTTP = !s.o.HTTP
		case fTLS12:
			s.o.TLS12 = !s.o.TLS12
		case fTLS13:
			s.o.TLS13 = !s.o.TLS13
		case fQUIC:
			s.o.QUIC = !s.o.QUIC
		case fIPV:
			s.ipvI = (s.ipvI + dir + len(ipvOpts)) % len(ipvOpts)
		case fLevel:
			s.levelI = (s.levelI + dir + len(levelOpts)) % len(levelOpts)
		case fRepeats:
			s.repI = (s.repI + dir + len(repeatOpts)) % len(repeatOpts)
		}
	}
	switch k {
	case "up", "k":
		if s.field > 0 {
			s.field--
		}
		if s.field == fDomains {
			return s, s.dom.Focus()
		}
	case "down", "j":
		if s.field < nFields-1 {
			s.field++
		}
	case " ", "right", "l":
		toggle(1)
	case "left", "h":
		toggle(-1)
	case "enter":
		if s.field != fStart {
			s.field = fStart
			return s, nil
		}
		return s.start()
	}
	return s, nil
}

func (s *scan) start() (Screen, tea.Cmd) {
	s.syncOpts()
	s.msg = ""
	switch {
	case !sys.IsRoot():
		s.msg = "Scanning needs root — restart with sudo."
		return s, nil
	case !sys.Installed():
		s.msg = "Install zapret2 first."
		return s, nil
	case len(s.o.Domains) == 0:
		s.msg = "Enter at least one domain that is blocked for you."
		return s, nil
	case !s.o.HTTP && !s.o.TLS12 && !s.o.TLS13 && !s.o.QUIC:
		s.msg = "Pick at least one protocol."
		return s, nil
	}
	if miss := sys.Missing(sys.FWType()); len(miss) > 0 {
		s.msg = "Missing packages: " + strings.Join(miss, " ") + " — open Install and press p."
		return s, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.stage = scRun
	s.all, s.log, s.prog, s.err = nil, logBuf{}, blockcheck.Progress{}, nil
	s.started = time.Now()
	s.wasUp = service.Get().Running()
	o := s.o
	wasUp := s.wasUp
	jobDone := make(chan struct{})
	finished := false
	s.finished = &finished
	// if the user quits mid-scan: stop blockcheck (it removes its own firewall
	// rules on SIGINT), wait for it, and bring zapret2 back up.
	onExit(func() {
		if finished {
			return
		}
		cancel()
		<-jobDone
		if wasUp {
			fmt.Println("restarting zapret2…")
			_ = service.Start(func(string) {})
		}
	})
	return s, startJob(ctx, "scan", func(ctx context.Context, log func(string), _ func(float64)) error {
		defer close(jobDone)
		if wasUp {
			log("* stopping zapret2 while testing (blockcheck needs it off)")
			if err := service.Stop(log); err != nil {
				return err
			}
		}
		lines := make(chan string, 256)
		errc := make(chan error, 1)
		go func() { errc <- blockcheck.Run(ctx, o, lines) }()
		for l := range lines {
			log(l)
		}
		return <-errc
	})
}

func (s *scan) onJob(m jobMsg) (Screen, tea.Cmd) {
	switch m.id {
	case "scan":
		if m.line != "" {
			s.all = append(s.all, m.line)
			s.log.add(m.line)
			s.prog.Feed(m.line)
		}
		if !m.done {
			return s, m.next()
		}
		s.err = m.err
		if s.finished != nil {
			*s.finished = true // normal path restarts zapret2 below
		}
		s.ranked = topPerProto(blockcheck.Rank(blockcheck.ParseSummary(s.all)), 10)
		s.cursor, s.chosen = 0, map[strategy.Proto]int{}
		// preselect the best strategy per protocol
		for i, r := range s.ranked {
			if _, ok := s.chosen[r.Proto]; !ok && r.Proto != strategy.Other {
				s.chosen[r.Proto] = i
			}
		}
		s.stage = scResults
		s.showLog = len(s.ranked) == 0
		var cmd tea.Cmd = refresh
		if s.wasUp {
			s.wasUp = false
			cmd = tea.Batch(refresh, startJob(context.Background(), "scan-restart", simple(service.Start)))
		}
		return s, cmd
	case "scan-restart", "apply":
		if m.line != "" {
			s.log.add(m.line)
		}
		if m.done {
			if m.err != nil {
				s.msg = sBad.Render("error: " + m.err.Error())
			} else if m.id == "apply" {
				s.msg = sOK.Render(strings.Replace(s.msg, "Restarting zapret2…", "zapret2 restarted — check the Dashboard site test.", 1))
			}
			return s, refresh
		}
		return s, m.next()
	}
	return s, nil
}

func (s *scan) resultKey(m tea.KeyMsg) (Screen, tea.Cmd) {
	switch m.String() {
	case "up", "k":
		if s.cursor > 0 {
			s.cursor--
		}
	case "down", "j":
		if s.cursor < len(s.ranked)-1 {
			s.cursor++
		}
	case " ", "enter":
		if len(s.ranked) > 0 {
			r := s.ranked[s.cursor]
			if s.chosen[r.Proto] == s.cursor {
				if _, ok := s.chosen[r.Proto]; ok {
					delete(s.chosen, r.Proto)
					break
				}
			}
			s.chosen[r.Proto] = s.cursor
		}
	case "v":
		s.showLog = !s.showLog
	case "n":
		s.stage, s.field, s.msg = scForm, fStart, ""
	case "a":
		return s.apply()
	}
	return s, nil
}

func (s *scan) apply() (Screen, tea.Cmd) {
	if len(s.chosen) == 0 {
		s.msg = "Pick at least one strategy with space."
		return s, nil
	}
	path := sys.Path("config")
	f, err := config.Load(path)
	if err != nil {
		s.msg = sBad.Render(err.Error())
		return s, nil
	}
	opt := f.GetOr("NFQWS2_OPT", strategy.Default)
	for proto, i := range s.chosen {
		opt = strategy.Replace(opt, proto, s.ranked[i].Strategy)
	}
	f.Set("NFQWS2_ENABLE", "1")
	f.Set("NFQWS2_OPT", opt)
	if err := f.Save(); err != nil {
		s.msg = sBad.Render(err.Error())
		return s, nil
	}
	added := ""
	if m := f.GetOr("MODE_FILTER", "none"); m == "hostlist" || m == "autohostlist" {
		for _, d := range s.o.Domains {
			_ = lists.Add("zapret-hosts-user.txt", d)
		}
		added = " Added " + strings.Join(s.o.Domains, " ") + " to your bypass list."
	}
	_ = profiles.Add(profiles.Profile{
		Name: "scan " + time.Now().Format("2006-01-02 15:04"),
		Opt:  opt,
		Note: "found for " + strings.Join(s.o.Domains, " "),
	})
	s.msg = "Saved to config and to Strategies." + added + " Restarting zapret2…"
	return s, startJob(context.Background(), "apply", simple(service.Restart))
}

func check(b bool) string {
	if b {
		return sAccent.Render("[x]")
	}
	return "[ ]"
}

func (s *scan) View(w, h int) string {
	var b strings.Builder
	b.WriteString(sTitle.Render("Find strategy") + sDim.Render("  runs blockcheck2 against sites blocked for you") + "\n\n")
	switch s.stage {
	case scForm:
		line := func(i int, label, val, help string) {
			l := sLabel.Render(label) + val
			if s.field == i {
				l = sSel.Render(sLabel.Render(label)+val) + "  " + sDim.Render(help)
			}
			b.WriteString(l + "\n")
		}
		b.WriteString(sLabel.Render("Blocked sites") + s.dom.View() + "\n")
		if s.field == fDomains {
			b.WriteString(sLabel.Render("") + sDim.Render("space-separated; use sites that do NOT open for you now") + "\n")
		}
		b.WriteString("\n")
		line(fHTTP, "HTTP", check(s.o.HTTP), "plain http on port 80 — rarely needed")
		line(fTLS12, "HTTPS TLS1.2", check(s.o.TLS12), "")
		line(fTLS13, "HTTPS TLS1.3", check(s.o.TLS13), "most modern sites")
		if s.h3 {
			line(fQUIC, "QUIC/HTTP3", check(s.o.QUIC), "YouTube & Google use it heavily")
		} else {
			line(fQUIC, "QUIC/HTTP3", check(s.o.QUIC)+sDim.Render("  your curl has no HTTP/3, QUIC will be skipped"), "")
		}
		line(fIPV, "IP version", "‹ "+ipvOpts[s.ipvI]+" ›", "46 = both")
		line(fLevel, "Scan depth", "‹ "+levelOpts[s.levelI]+" ›", levelHelp[levelOpts[s.levelI]])
		line(fRepeats, "Repeats", fmt.Sprintf("‹ %d ›", repeatOpts[s.repI]), "retry each test; more = fewer false positives")
		b.WriteString("\n")
		start := "[ Start scan ]"
		if s.field == fStart {
			start = sSel.Render(start)
		}
		b.WriteString(start + "\n")
		b.WriteString("\n" + sDim.Render("zapret2 is stopped during the scan and restarted afterwards.\nA quick scan of one site takes about 1–5 minutes.") + "\n")
		if s.msg != "" {
			b.WriteString("\n" + sBad.Render(s.msg) + "\n")
		}
	case scRun:
		el := time.Since(s.started).Round(time.Second)
		b.WriteString(fmt.Sprintf("%s  elapsed %s  ·  tried %d  ·  %s\n",
			sAccent.Render("scanning"), el, s.prog.Tried, sOK.Render(fmt.Sprintf("%d working", s.prog.Worked))))
		b.WriteString(sDim.Render(truncate("now: "+s.prog.Current, w-2)) + "\n\n")
		n := h - 5
		if n > 0 {
			b.WriteString(s.log.tail(n, w-2))
		}
	case scResults:
		s.viewResults(&b, w, h)
	}
	return b.String()
}

func (s *scan) viewResults(b *strings.Builder, w, h int) {
	switch {
	case s.err != nil && len(s.ranked) == 0:
		b.WriteString(sBad.Render("Scan ended: "+s.err.Error()) + "\n")
	case len(s.ranked) == 0:
		b.WriteString(sBad.Render("No working strategy found.") + " Try another scan depth, more repeats, or check the log.\n")
	default:
		b.WriteString("Best strategies per protocol. Picked ones (●) are applied — one per protocol.\n\n")
	}
	if s.msg != "" {
		b.WriteString(s.msg + "\n\n")
	}
	if s.showLog {
		n := h - strings.Count(b.String(), "\n") - 1
		b.WriteString(s.log.tail(n, w-2))
		return
	}
	var detail string
	if len(s.ranked) > 0 {
		r := s.ranked[s.cursor]
		detail = "\n" + sBold.Render(r.Proto.Label()) + sDim.Render("  works for: "+strings.Join(r.Domains, " ")) + "\n" +
			wrapIndent(r.Strategy, w-4, 0) + "\n"
	}
	maxRows := h - strings.Count(b.String(), "\n") - strings.Count(detail, "\n") - 2
	start := 0
	if s.cursor >= maxRows {
		start = s.cursor - maxRows + 1
	}
	for i := start; i < len(s.ranked) && i-start < maxRows; i++ {
		r := s.ranked[i]
		mark := "○"
		if c, ok := s.chosen[r.Proto]; ok && c == i {
			mark = sOK.Render("●")
		}
		cov := fmt.Sprintf("%d/%d", len(r.Domains), r.Total)
		line := fmt.Sprintf("%s %-5s %-5s %-4s ", mark, cov, string(r.Proto), strings.TrimPrefix(r.IPV, "ip"))
		line += truncate(r.Strategy, w-lenVisible(line)-3)
		if i == s.cursor {
			line = sSel.Render(line)
		}
		b.WriteString(line + "\n")
	}
	b.WriteString(detail)
}

func lenVisible(s string) int { return len([]rune(stripANSI(s))) }

func stripANSI(s string) string {
	var out strings.Builder
	esc := false
	for _, r := range s {
		switch {
		case r == 0x1b:
			esc = true
		case esc && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'):
			esc = false
		case !esc:
			out.WriteRune(r)
		}
	}
	return out.String()
}

// topPerProto groups ranked results by protocol (http, tls, quic) keeping the
// best n of each, so one protocol's many hits don't hide the others.
func topPerProto(rs []blockcheck.Ranked, n int) []blockcheck.Ranked {
	var out []blockcheck.Ranked
	for _, p := range []strategy.Proto{strategy.HTTP, strategy.TLS, strategy.QUIC, strategy.Other} {
		c := 0
		for _, r := range rs {
			if r.Proto == p && c < n {
				out = append(out, r)
				c++
			}
		}
	}
	return out
}
