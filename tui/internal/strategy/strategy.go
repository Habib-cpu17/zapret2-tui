// Package strategy edits NFQWS2_OPT: a list of nfqws2 profiles separated by
// "--new", each starting with filters (--filter-tcp/udp, --filter-l7) and a
// hostlist placeholder, followed by --payload/--lua-desync actions.
package strategy

import (
	"regexp"
	"strings"
)

// Proto identifies which traffic a profile handles.
type Proto string

const (
	HTTP  Proto = "http"
	TLS   Proto = "tls"
	QUIC  Proto = "quic"
	Other Proto = ""
)

func (p Proto) Label() string {
	switch p {
	case HTTP:
		return "HTTP (tcp/80)"
	case TLS:
		return "HTTPS/TLS (tcp/443)"
	case QUIC:
		return "QUIC/HTTP3 (udp/443)"
	}
	return "custom"
}

// Prefix is the filter part blockcheck strategies are wrapped in. Matches
// config.default.
func (p Proto) Prefix() string {
	switch p {
	case HTTP:
		return "--filter-tcp=80 --filter-l7=http <HOSTLIST>"
	case TLS:
		return "--filter-tcp=443 --filter-l7=tls <HOSTLIST>"
	case QUIC:
		return "--filter-udp=443 --filter-l7=quic <HOSTLIST_NOAUTO>"
	}
	return ""
}

// ProtoForTest maps a blockcheck test function to the protocol it covers.
func ProtoForTest(test string) Proto {
	switch test {
	case "curl_test_http":
		return HTTP
	case "curl_test_https_tls12", "curl_test_https_tls13":
		return TLS
	case "curl_test_http3":
		return QUIC
	}
	return Other
}

var l7Re = regexp.MustCompile(`--filter-l7=([a-z0-9,]+)`)

// Profile is one --new separated section.
type Profile struct {
	Text string // whitespace-normalized
}

func (p Profile) Proto() Proto {
	m := l7Re.FindStringSubmatch(p.Text)
	if m == nil {
		return Other
	}
	switch m[1] {
	case "http":
		return HTTP
	case "tls":
		return TLS
	case "quic":
		return QUIC
	}
	return Other
}

// Actions returns the profile without its filter/hostlist tokens.
func (p Profile) Actions() string {
	var out []string
	for _, f := range strings.Fields(p.Text) {
		if strings.HasPrefix(f, "--filter-") || f == "<HOSTLIST>" || f == "<HOSTLIST_NOAUTO>" {
			continue
		}
		out = append(out, f)
	}
	return strings.Join(out, " ")
}

// Split parses NFQWS2_OPT into profiles.
func Split(opt string) []Profile {
	var out []Profile
	var cur []string
	flush := func() {
		if len(cur) > 0 {
			out = append(out, Profile{Text: strings.Join(cur, " ")})
			cur = nil
		}
	}
	for _, f := range strings.Fields(opt) {
		if f == "--new" {
			flush()
			continue
		}
		cur = append(cur, f)
	}
	flush()
	return out
}

// Join renders profiles in config.default's layout: one profile per line.
func Join(ps []Profile) string {
	var lines []string
	for i, p := range ps {
		l := p.Text
		if i < len(ps)-1 {
			l += " --new"
		}
		lines = append(lines, l)
	}
	return "\n" + strings.Join(lines, "\n") + "\n"
}

// Replace sets the actions for proto, replacing an existing profile for that
// proto in place, or appending one. actions is what blockcheck reports after
// "nfqws2 " (e.g. "--payload=tls_client_hello --lua-desync=...").
func Replace(opt string, proto Proto, actions string) string {
	np := Profile{Text: strings.TrimSpace(proto.Prefix() + " " + strings.Join(strings.Fields(actions), " "))}
	ps := Split(opt)
	for i, p := range ps {
		if p.Proto() == proto {
			ps[i] = np
			return Join(ps)
		}
	}
	// keep config.default order: http, tls, quic, then others
	order := map[Proto]int{HTTP: 0, TLS: 1, QUIC: 2, Other: 3}
	pos := len(ps)
	for i, p := range ps {
		if order[p.Proto()] > order[proto] {
			pos = i
			break
		}
	}
	ps = append(ps[:pos], append([]Profile{np}, ps[pos:]...)...)
	return Join(ps)
}

// Remove drops the profile for proto.
func Remove(opt string, proto Proto) string {
	var out []Profile
	for _, p := range Split(opt) {
		if p.Proto() != proto {
			out = append(out, p)
		}
	}
	return Join(out)
}

// Default is config.default's NFQWS2_OPT.
const Default = `
--filter-tcp=80 --filter-l7=http <HOSTLIST> --payload=http_req --lua-desync=fake:blob=fake_default_http:tcp_md5 --lua-desync=multisplit:pos=method+2 --new
--filter-tcp=443 --filter-l7=tls <HOSTLIST> --payload=tls_client_hello --lua-desync=fake:blob=fake_default_tls:tcp_md5:tcp_seq=-10000 --lua-desync=multidisorder:pos=1,midsld --new
--filter-udp=443 --filter-l7=quic <HOSTLIST_NOAUTO> --payload=quic_initial --lua-desync=fake:blob=fake_default_quic:repeats=6
`

// Summary is a short human description: the chain of desync functions,
// e.g. "fake → multidisorder".
func (p Profile) Summary() string {
	var names []string
	for _, f := range strings.Fields(p.Text) {
		if v, ok := strings.CutPrefix(f, "--lua-desync="); ok {
			n, _, _ := strings.Cut(v, ":")
			if len(names) == 0 || names[len(names)-1] != n {
				names = append(names, n)
			}
		}
	}
	if len(names) == 0 {
		return "(no desync)"
	}
	return strings.Join(names, " → ")
}
