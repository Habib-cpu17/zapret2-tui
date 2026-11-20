package ui

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"zapret2/tui/internal/config"
	"zapret2/tui/internal/lists"
	"zapret2/tui/internal/strategy"
	"zapret2/tui/internal/sys"
)

type siteResult struct {
	domain string
	ok     bool
	detail string
}

type testDoneMsg struct{ results []siteResult }

type dashboard struct {
	st      statusMsg
	log     logBuf
	busy    string
	testing bool
	results []siteResult
	profs   []strategy.Profile
}

func newDashboard() *dashboard { return &dashboard{} }

func (d *dashboard) Name() string    { return "Dashboard" }
func (d *dashboard) Init() tea.Cmd   { return nil }
func (d *dashboard) Capturing() bool { return false }
func (d *dashboard) Help() string {
	return keys("s", "start", "x", "stop", "r", "restart", "a", "autostart", "t", "test sites")
}

// testDomains: user's bypass list (minus the placeholder) or a default set.
func testDomains() []string {
	var out []string
	for _, d := range lists.Read("zapret-hosts-user.txt") {
		if d != "nonexistent.domain" && !strings.ContainsAny(d, "/:") {
			out = append(out, d)
		}
		if len(out) == 6 {
			break
		}
	}
	if len(out) == 0 {
		out = []string{"youtube.com", "discord.com", "x.com", "telegram.org"}
	}
	return out
}

func siteTest() tea.Msg {
	ds := testDomains()
	res := make([]siteResult, len(ds))
	cl := &http.Client{
		Timeout: 6 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{},
			Proxy:           nil,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	var wg sync.WaitGroup
	for i, d := range ds {
		wg.Add(1)
		go func(i int, d string) {
			defer wg.Done()
			start := time.Now()
			resp, err := cl.Get("https://" + d + "/")
			if err != nil {
				msg := err.Error()
				switch {
				case strings.Contains(msg, "reset"):
					msg = "connection reset (likely DPI)"
				case strings.Contains(msg, "Timeout") || strings.Contains(msg, "deadline"):
					msg = "timeout"
				case strings.Contains(msg, "no such host"):
					msg = "DNS failed"
				}
				res[i] = siteResult{d, false, msg}
				return
			}
			resp.Body.Close()
			res[i] = siteResult{d, true, fmt.Sprintf("HTTP %d · %dms", resp.StatusCode, time.Since(start).Milliseconds())}
		}(i, d)
	}
	wg.Wait()
	return testDoneMsg{res}
}

func (d *dashboard) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch m := msg.(type) {
	case statusMsg:
		d.st = m
		if f, err := config.Load(sys.Path("config")); err == nil {
			d.profs = strategy.Split(f.GetOr("NFQWS2_OPT", ""))
		}
	case testDoneMsg:
		d.testing, d.results = false, m.results
	case jobMsg:
		if m.id != "dash" {
			return d, nil
		}
		if m.line != "" {
			d.log.add(m.line)
		}
		if m.done {
			if m.err != nil {
				d.log.add(sBad.Render("error: " + m.err.Error()))
			} else {
				d.log.add(sOK.Render(d.busy + ": done"))
			}
			d.busy = ""
			return d, refresh
		}
		return d, m.next()
	case tea.KeyMsg:
		if d.busy != "" {
			return d, nil
		}
		run := func(name string, f func(func(string)) error) (Screen, tea.Cmd) {
			if !sys.IsRoot() {
				d.log.add(sBad.Render("need root: restart with sudo"))
				return d, nil
			}
			if !d.st.st.Installed {
				d.log.add(sBad.Render("zapret2 is not installed"))
				return d, nil
			}
			d.busy = name
			d.log.add(sAccent.Render("» " + name))
			return d, startJob(context.Background(), "dash", simple(f))
		}
		switch m.String() {
		case "s":
			return run("start", svcStart)
		case "x":
			return run("stop", svcStop)
		case "r":
			return run("restart", svcRestart)
		case "a":
			on := d.st.st.Enabled != "enabled"
			return run(fmt.Sprintf("autostart %v", on), func(l func(string)) error { return svcEnable(l, on) })
		case "t":
			if !d.testing {
				d.testing = true
				return d, siteTest
			}
		}
	}
	return d, nil
}

func (d *dashboard) View(w, h int) string {
	st := d.st.st
	var b strings.Builder
	b.WriteString(sTitle.Render("Dashboard") + "\n\n")
	if !st.Installed {
		b.WriteString(needInstall() + "\n")
		return b.String()
	}
	svc := sBad.Render("stopped")
	if st.Running() {
		svc = sOK.Render("running")
	} else if st.Active == "failed" {
		svc = sBad.Render("failed — see Logs")
	}
	if d.busy != "" {
		svc += sDim.Render("  (" + d.busy + "…)")
	}
	b.WriteString(row("Service", svc) + "\n")
	auto := st.Enabled
	if auto == "enabled" {
		auto = sOK.Render("on") + sDim.Render(" (starts at boot)")
	} else {
		auto = sDim.Render("off")
	}
	b.WriteString(row("Autostart", auto) + "\n")
	pids := sDim.Render("none")
	if len(st.PIDs) > 0 {
		pids = fmt.Sprint(st.PIDs)
	}
	b.WriteString(row("nfqws2", pids) + "\n")
	if sys.IsRoot() {
		b.WriteString(row("Firewall", ok(st.FWRules, d.st.fw+" rules active", "no rules")) + "\n")
	} else {
		b.WriteString(row("Firewall", d.st.fw+sDim.Render("  (rules visible as root only)")) + "\n")
	}
	b.WriteString(row("Filter mode", d.st.mode+sDim.Render("  "+modeHelp(d.st.mode))) + "\n")

	b.WriteString("\n" + sBold.Render("Active strategy") + "\n")
	if len(d.profs) == 0 {
		b.WriteString(sDim.Render("  none — run Find strategy") + "\n")
	}
	for _, p := range d.profs {
		b.WriteString("  " + sAccent.Render(fmt.Sprintf("%-22s", p.Proto().Label())) + p.Summary() + "\n")
	}

	b.WriteString("\n" + sBold.Render("Site test") + sDim.Render("  [t] checks HTTPS reachability through zapret2") + "\n")
	switch {
	case d.testing:
		b.WriteString(sDim.Render("  testing…") + "\n")
	case d.results == nil:
		b.WriteString(sDim.Render("  not run yet") + "\n")
	default:
		for _, r := range d.results {
			b.WriteString(fmt.Sprintf("  %-22s %s\n", r.domain, ok(r.ok, r.detail, r.detail)))
		}
	}
	used := strings.Count(b.String(), "\n")
	if n := h - used - 2; n > 0 && len(d.log.lines) > 0 {
		b.WriteString("\n" + sDim.Render(d.log.tail(n, w-2)))
	}
	return b.String()
}

func modeHelp(m string) string {
	switch m {
	case "none":
		return "all traffic on ports 80/443"
	case "hostlist":
		return "only domains in your bypass list"
	case "autohostlist":
		return "bypass list + domains learned automatically"
	case "ipset":
		return "only IPs in ipset"
	}
	return ""
}
