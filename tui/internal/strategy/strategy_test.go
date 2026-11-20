package strategy

import (
	"strings"
	"testing"
)

func TestSplitDefault(t *testing.T) {
	ps := Split(Default)
	if len(ps) != 3 || ps[0].Proto() != HTTP || ps[1].Proto() != TLS || ps[2].Proto() != QUIC {
		t.Fatalf("%+v", ps)
	}
	if a := ps[2].Actions(); a != "--payload=quic_initial --lua-desync=fake:blob=fake_default_quic:repeats=6" {
		t.Fatal(a)
	}
	if Join(ps) != Default {
		t.Fatalf("join mismatch:\n%s", Join(ps))
	}
}

func TestReplace(t *testing.T) {
	out := Replace(Default, TLS, "--payload=tls_client_hello  --lua-desync=multisplit:pos=2")
	ps := Split(out)
	if len(ps) != 3 || ps[1].Text != "--filter-tcp=443 --filter-l7=tls <HOSTLIST> --payload=tls_client_hello --lua-desync=multisplit:pos=2" {
		t.Fatal(out)
	}
	// insert http into tls-only opt goes first
	only := Remove(Remove(Default, HTTP), QUIC)
	out = Replace(only, HTTP, "--payload=http_req --lua-desync=fake")
	if ps := Split(out); ps[0].Proto() != HTTP || ps[1].Proto() != TLS {
		t.Fatal(out)
	}
	if strings.Count(out, "--new") != 1 {
		t.Fatal(out)
	}
}
