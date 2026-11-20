package blockcheck

import (
	"strings"
	"testing"
)

const sample = `* checking system
- curl_test_https_tls12 ipv4 a.com : nfqws2 --payload=tls_client_hello --lua-desync=multisplit:pos=2
!!!!! AVAILABLE !!!!!
!!!!! curl_test_https_tls12: working strategy found for ipv4 a.com : nfqws2 --payload=tls_client_hello --lua-desync=multisplit:pos=2 !!!!!

* SUMMARY
curl_test_https_tls12 ipv4 a.com : nfqws2 --payload=tls_client_hello --lua-desync=multisplit:pos=2
curl_test_https_tls12 ipv4 a.com : nfqws2 --payload=tls_client_hello --lua-desync=fake:blob=fake_default_tls:tcp_md5
curl_test_https_tls12 ipv4 b.com : nfqws2 --payload=tls_client_hello --lua-desync=fake:blob=fake_default_tls:tcp_md5
curl_test_http3 ipv4 a.com : working without bypass
curl_test_http3 ipv4 b.com : nfqws2 not working

* COMMON
curl_test_https_tls12 ipv4 : nfqws2 --payload=tls_client_hello --lua-desync=fake:blob=fake_default_tls:tcp_md5

* COVERAGE
2/2 : curl_test_https_tls12 ipv4 : nfqws2 --payload=tls_client_hello --lua-desync=fake:blob=fake_default_tls:tcp_md5
`

func TestParseAndRank(t *testing.T) {
	lines := strings.Split(sample, "\n")
	rs := ParseSummary(lines)
	if len(rs) != 5 {
		t.Fatalf("got %d results: %+v", len(rs), rs)
	}
	if rs[4].Working || rs[3].Working || !rs[0].Working {
		t.Fatalf("%+v", rs)
	}
	rk := Rank(rs)
	if len(rk) != 2 {
		t.Fatalf("%+v", rk)
	}
	if len(rk[0].Domains) != 2 || !strings.Contains(rk[0].Strategy, "tcp_md5") || rk[0].Total != 2 {
		t.Fatalf("top: %+v", rk[0])
	}

	var p Progress
	for _, l := range lines {
		p.Feed(l)
	}
	if p.Tried != 1 || p.Worked != 1 {
		t.Fatalf("%+v", p)
	}
}
