package ui

import (
	"context"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"zapret2/tui/internal/config"
	"zapret2/tui/internal/service"
	"zapret2/tui/internal/sys"
)

type setField struct {
	key, label, help string
	opts             []string // cycle values; nil = text input
	labels           []string // optional display names for opts
	ti               textinput.Model
	cur              int
}

func (f *setField) value() string {
	if f.opts == nil {
		return strings.TrimSpace(f.ti.Value())
	}
	return f.opts[f.cur]
}

func (f *setField) display() string {
	if f.labels != nil {
		return f.labels[f.cur]
	}
	return f.value()
}

func (f *setField) set(v string) {
	if f.opts == nil {
		f.ti.SetValue(v)
		return
	}
	for i, o := range f.opts {
		if o == v {
			f.cur = i
		}
	}
}

// extra is an upstream custom.d example that can be switched on.
type extra struct{ file, label string }

var extras = []extra{
	{"50-discord-media", "Discord voice & video"},
	{"50-stun4all", "STUN for all (voice calls: Telegram, WhatsApp, …)"},
	{"50-quic4all", "QUIC for all sites, not only listed ones"},
	{"50-wg4all", "WireGuard handshakes (if your VPN is blocked)"},
	{"50-dht4all", "DHT (torrent peer discovery)"},
}

func extraSrc(e extra) string { return sys.Path("init.d", "custom.d.examples.linux", e.file) }
func extraDst(e extra) string { return sys.Path("init.d", "sysv", "custom.d", e.file) }

type settings struct {
	fields  []*setField
	cursor  int
	editing bool
	dirty   bool
	msg     string
	busy    bool
}

func newSettings() *settings {
	text := func(ph string) textinput.Model {
		t := textinput.New()
		t.Placeholder = ph
		t.CharLimit = 120
		t.Width = 30
		t.Prompt = ""
		return t
	}
	s := &settings{fields: []*setField{
		{key: "MODE_FILTER", label: "Filter mode", opts: []string{"none", "hostlist", "autohostlist", "ipset"}, help: "which traffic gets bypass tricks"},
		{key: "DISABLE_IPV6", label: "IPv6", opts: []string{"1", "0"}, labels: []string{"off", "on"}, help: "also handle IPv6 traffic"},
		{key: "FWTYPE", label: "Firewall", opts: []string{"nftables", "iptables"}, help: "backend for traffic redirection"},
		{key: "NFQWS2_PORTS_TCP", label: "TCP ports", ti: text("80,443"), help: "ports sent to nfqws2"},
		{key: "NFQWS2_PORTS_UDP", label: "UDP ports", ti: text("443"), help: "443 = QUIC"},
		{key: "FLOWOFFLOAD", label: "Flow offload", opts: []string{"donttouch", "none", "software", "hardware"}, help: "routers only; leave donttouch"},
		{key: "IFACE_LAN", label: "LAN iface", ti: text("empty = not a router"), help: "router mode only"},
		{key: "IFACE_WAN", label: "WAN iface", ti: text("empty = auto"), help: "router mode only"},
	}}
	s.load()
	return s
}

func (s *settings) load() {
	f, err := config.Load(sys.Path("config"))
	if err != nil {
		return
	}
	for _, fl := range s.fields {
		def := ""
		if fl.opts != nil {
			def = fl.opts[0]
		}
		if fl.key == "FWTYPE" {
			def = sys.FWType()
		}
		fl.set(f.GetOr(fl.key, def))
	}
	s.dirty = false
}

func (s *settings) Name() string    { return "Settings" }
func (s *settings) Init() tea.Cmd   { return nil }
func (s *settings) Capturing() bool { return s.editing }
func (s *settings) Help() string {
	if s.editing {
		return keys("enter", "done", "esc", "done")
	}
	return keys("↑↓", "field", "space/←→", "change", "enter", "edit text", "s", "save & restart", "u", "undo")
}

func (s *settings) nRows() int { return len(s.fields) + len(extras) }

func (s *settings) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch m := msg.(type) {
	case jobMsg:
		if m.id != "settings" {
			return s, nil
		}
		if m.done {
			s.busy = false
			if m.err != nil {
				s.msg = sBad.Render("restart failed: " + m.err.Error() + " — see Logs")
			} else {
				s.msg = sOK.Render("saved, zapret2 restarted")
			}
			return s, refresh
		}
		return s, m.next()
	case tea.KeyMsg:
		return s.key(m)
	}
	return s, nil
}

func (s *settings) key(m tea.KeyMsg) (Screen, tea.Cmd) {
	k := m.String()
	if s.editing {
		f := s.fields[s.cursor]
		if k == "enter" || k == "esc" {
			s.editing = false
			f.ti.Blur()
			return s, nil
		}
		var c tea.Cmd
		f.ti, c = f.ti.Update(m)
		s.dirty = true
		return s, c
	}
	if s.busy {
		return s, nil
	}
	writable := sys.IsRoot() && sys.Installed()
	switch k {
	case "up", "k":
		if s.cursor > 0 {
			s.cursor--
		}
	case "down", "j":
		if s.cursor < s.nRows()-1 {
			s.cursor++
		}
	case "u":
		s.load()
		s.msg = "reverted"
	case " ", "right", "l", "left", "h", "enter":
		if !writable {
			s.msg = sBad.Render("need root and an installed zapret2")
			return s, nil
		}
		if s.cursor >= len(s.fields) {
			e := extras[s.cursor-len(s.fields)]
			var err error
			if sys.Exists(extraDst(e)) {
				err = os.Remove(extraDst(e))
			} else {
				var b []byte
				if b, err = os.ReadFile(extraSrc(e)); err == nil {
					err = os.WriteFile(extraDst(e), b, 0o644)
				}
			}
			if err != nil {
				s.msg = sBad.Render(err.Error())
			}
			s.dirty = true
			return s, nil
		}
		f := s.fields[s.cursor]
		if f.opts == nil {
			s.editing = true
			return s, f.ti.Focus()
		}
		d := 1
		if k == "left" || k == "h" {
			d = -1
		}
		f.cur = (f.cur + d + len(f.opts)) % len(f.opts)
		s.dirty = true
	case "s":
		if !writable {
			return s, nil
		}
		cf, err := config.Load(sys.Path("config"))
		if err != nil {
			s.msg = sBad.Render(err.Error())
			return s, nil
		}
		for _, f := range s.fields {
			cf.SetOrUnset(f.key, f.value())
		}
		if err := cf.Save(); err != nil {
			s.msg = sBad.Render(err.Error())
			return s, nil
		}
		s.dirty, s.busy = false, true
		s.msg = "saved, restarting zapret2…"
		return s, startJob(context.Background(), "settings", simple(service.Restart))
	}
	return s, nil
}

func (s *settings) View(w, h int) string {
	var b strings.Builder
	b.WriteString(sTitle.Render("Settings") + sDim.Render(truncate("  edits "+sys.Path("config"), w-12)) + "\n\n")
	if !sys.Installed() {
		return b.String() + needInstall()
	}
	for i, f := range s.fields {
		var v string
		if f.opts == nil {
			v = f.ti.View()
		} else {
			v = "‹ " + f.display() + " ›"
		}
		l := sLabel.Render(f.label) + v
		if i == s.cursor {
			l = sSel.Render(sLabel.Render(f.label)+stripANSI(v)) + "  " + sDim.Render(f.help)
			if f.opts == nil {
				l = sSel.Render(sLabel.Render(f.label)) + v + "  " + sDim.Render(f.help)
			}
		}
		b.WriteString(l + "\n")
	}
	b.WriteString("\n" + sBold.Render("Extras") + sDim.Render("  extra traffic types (upstream custom.d scripts)") + "\n")
	for i, e := range extras {
		l := check(sys.Exists(extraDst(e))) + " " + e.label
		if len(s.fields)+i == s.cursor {
			l = sSel.Render(stripANSI(l))
		}
		b.WriteString(l + "\n")
	}
	b.WriteString("\n")
	if s.dirty {
		b.WriteString(sAccent.Render("unsaved changes — press s to save and restart") + "\n")
	}
	if s.msg != "" {
		b.WriteString(s.msg + "\n")
	}
	return b.String()
}
