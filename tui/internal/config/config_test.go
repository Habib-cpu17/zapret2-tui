package config

import (
	"os"
	"strings"
	"testing"
)

func loadDefault(t *testing.T) string {
	b, err := os.ReadFile("../../../config.default")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestRoundTrip(t *testing.T) {
	src := loadDefault(t)
	if got := Parse(src).String(); got != src {
		t.Fatal("round trip changed the file")
	}
}

func TestGetMultiline(t *testing.T) {
	f := Parse(loadDefault(t))
	opt, ok := f.Get("NFQWS2_OPT")
	if !ok || !strings.Contains(opt, "--filter-l7=quic") || !strings.HasPrefix(opt, "\n--filter-tcp=80") {
		t.Fatalf("bad NFQWS2_OPT: %q", opt)
	}
	if v, _ := f.Get("MODE_FILTER"); v != "none" {
		t.Fatalf("MODE_FILTER=%q", v)
	}
	if _, ok := f.Get("FWTYPE"); ok {
		t.Fatal("commented FWTYPE should not be active")
	}
}

func TestSetReplacesAndPreserves(t *testing.T) {
	src := loadDefault(t)
	f := Parse(src)
	newOpt := "\n--filter-tcp=443 --filter-l7=tls <HOSTLIST> --payload=tls_client_hello --lua-desync=fake:blob=x:ip_ttl=5\n"
	f.Set("NFQWS2_OPT", newOpt)
	f.Set("FWTYPE", "nftables")      // uncomment template line
	f.Set("MODE_FILTER", "hostlist") // replace active
	f.Set("GETLIST", "")             // empty -> active empty
	f.Unset("DISABLE_IPV6")          // comment out
	f.Set("BRAND_NEW", "a b")        // appended

	g := Parse(f.String())
	if v, _ := g.Get("NFQWS2_OPT"); v != newOpt {
		t.Fatalf("NFQWS2_OPT=%q", v)
	}
	if v, _ := g.Get("FWTYPE"); v != "nftables" {
		t.Fatalf("FWTYPE=%q", v)
	}
	if v, _ := g.Get("MODE_FILTER"); v != "hostlist" {
		t.Fatalf("MODE_FILTER=%q", v)
	}
	if _, ok := g.Get("DISABLE_IPV6"); ok {
		t.Fatal("DISABLE_IPV6 still active")
	}
	if v, _ := g.Get("BRAND_NEW"); v != "a b" {
		t.Fatalf("BRAND_NEW=%q", v)
	}
	// untouched comments survive
	if !strings.Contains(f.String(), "# mark bit used by nfqws to prevent loop") {
		t.Fatal("comment lost")
	}
	// FWTYPE replaced its template in place (no duplicate)
	if strings.Count(f.String(), "FWTYPE=") != 1 {
		t.Fatal("FWTYPE duplicated")
	}
}

func TestQuoteRoundTrip(t *testing.T) {
	for _, v := range []string{"plain", "has space", `q"uote`, "dollar $X", "back`tick", `bs\x`, "--out-range=<s1"} {
		f := Parse("A=1\n")
		f.Set("A", v)
		if got, _ := Parse(f.String()).Get("A"); got != v {
			t.Errorf("%q -> %q (%s)", v, got, f.String())
		}
	}
}
