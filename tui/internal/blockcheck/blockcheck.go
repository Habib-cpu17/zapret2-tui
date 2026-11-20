// Package blockcheck runs upstream blockcheck2.sh non-interactively (BATCH=1)
// and parses its output into ranked strategies.
package blockcheck

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"zapret2/tui/internal/strategy"
	"zapret2/tui/internal/sys"
)

type Options struct {
	Domains   []string
	IPVs      string // "4", "6", "46"
	HTTP      bool
	TLS12     bool
	TLS13     bool
	QUIC      bool
	ScanLevel string // veryquick, quick, standard, force
	Repeats   int
	SkipIPBlk bool
}

func DefaultOptions() Options {
	return Options{Domains: []string{"youtube.com"}, IPVs: "4", TLS12: true, TLS13: true, QUIC: true, ScanLevel: "quick", Repeats: 1, SkipIPBlk: true}
}

// Env renders the env vars blockcheck2.sh reads in BATCH mode.
func (o Options) Env() []string {
	b := func(v bool) string {
		if v {
			return "1"
		}
		return "0"
	}
	rep := o.Repeats
	if rep < 1 {
		rep = 1
	}
	env := []string{
		"BATCH=1",
		"DOMAINS=" + strings.Join(o.Domains, " "),
		"IPVS=" + o.IPVs,
		"ENABLE_HTTP=" + b(o.HTTP),
		"ENABLE_HTTPS_TLS12=" + b(o.TLS12),
		"ENABLE_HTTPS_TLS13=" + b(o.TLS13),
		"ENABLE_HTTP3=" + b(o.QUIC),
		"SCANLEVEL=" + o.ScanLevel,
		fmt.Sprintf("REPEATS=%d", rep),
		"PARALLEL=0",
	}
	if o.SkipIPBlk {
		env = append(env, "SKIP_IPBLOCK=1")
	}
	return env
}

// Run streams blockcheck output to lines. The zapret2 service must be stopped
// first: blockcheck refuses to run alongside an active nfqws2.
func Run(ctx context.Context, o Options, lines chan<- string) error {
	return sys.Stream(ctx, o.Env(), lines, "sh", sys.Path("blockcheck2.sh"))
}

// Result is one line from blockcheck's "* SUMMARY" section.
type Result struct {
	Test     string // curl_test_https_tls12 ...
	IPV      string // ipv4 / ipv6
	Domain   string
	Value    string // "nfqws2 <args>" or a status text
	Working  bool   // Value is a working strategy
	Strategy string // args after "nfqws2 " when Working
}

// Ranked is a distinct working strategy with how many domains it covered.
type Ranked struct {
	Test     string
	Proto    strategy.Proto
	IPV      string
	Strategy string
	Domains  []string
	Total    int
	Order    int // first appearance; blockcheck emits better strategies first
}

var summaryRe = regexp.MustCompile(`^(curl_test_\S+) (ipv[46]) (\S+) : (.*)$`)

// ParseSummary extracts results after the "* SUMMARY" marker.
func ParseSummary(lines []string) []Result {
	var out []Result
	in := false
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "* SUMMARY" {
			in, out = true, nil // last summary wins
			continue
		}
		if !in {
			continue
		}
		if strings.HasPrefix(l, "* ") { // * COMMON / * COVERAGE
			in = false
			continue
		}
		m := summaryRe.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		r := Result{Test: m[1], IPV: m[2], Domain: m[3], Value: m[4]}
		for _, d := range []string{"nfqws2 ", "dvtws2 "} {
			if strings.HasPrefix(r.Value, d) && !strings.HasSuffix(r.Value, "not working") {
				r.Working = true
				r.Strategy = strings.TrimSpace(strings.TrimPrefix(r.Value, d))
			}
		}
		out = append(out, r)
	}
	return out
}

// Rank groups working results by (test, ipv, strategy) and sorts by coverage.
func Rank(rs []Result) []Ranked {
	domains := map[string]bool{}
	for _, r := range rs {
		domains[r.Domain] = true
	}
	idx := map[string]int{}
	var out []Ranked
	for _, r := range rs {
		if !r.Working {
			continue
		}
		k := r.Test + "|" + r.IPV + "|" + r.Strategy
		i, ok := idx[k]
		if !ok {
			i = len(out)
			idx[k] = i
			out = append(out, Ranked{Test: r.Test, Proto: strategy.ProtoForTest(r.Test), IPV: r.IPV, Strategy: r.Strategy, Order: i})
		}
		out[i].Domains = append(out[i].Domains, r.Domain)
	}
	for i := range out {
		out[i].Total = len(domains)
	}
	sort.SliceStable(out, func(a, b int) bool {
		if len(out[a].Domains) != len(out[b].Domains) {
			return len(out[a].Domains) > len(out[b].Domains)
		}
		return out[a].Order < out[b].Order
	})
	return out
}

// Progress tracks live output for the UI.
type Progress struct {
	Tried   int
	Worked  int
	Current string // "curl_test_https_tls12 ipv4 youtube.com"
	Phase   string
}

var tryRe = regexp.MustCompile(`^- (curl_test_\S+ ipv[46] \S+) : `)

func (p *Progress) Feed(l string) {
	switch {
	case tryRe.MatchString(l):
		p.Tried++
		p.Current = tryRe.FindStringSubmatch(l)[1]
	case strings.HasPrefix(l, "!!!!! ") && strings.Contains(l, "working strategy found"):
		p.Worked++
	case strings.HasPrefix(l, "* "):
		p.Phase = strings.TrimPrefix(l, "* ")
	}
}
