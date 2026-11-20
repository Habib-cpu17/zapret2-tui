package release

import (
	"os"
	"path/filepath"
	"testing"
)

// Network test: ZAPRET_NET_TEST=1 go test ./internal/release -run Live -v
func TestLiveDownloadExtract(t *testing.T) {
	if os.Getenv("ZAPRET_NET_TEST") != "1" {
		t.Skip("set ZAPRET_NET_TEST=1")
	}
	rel, err := Latest()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if keep := os.Getenv("ZAPRET_TEST_OUT"); keep != "" {
		dir = keep
	}
	tb, err := Download(rel, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "base")
	if err := Extract(tb, dest); err != nil {
		t.Fatal(err)
	}
	n, err := VerifyBinaries(rel, dest)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d binaries verified", n)
	for _, f := range []string{"install_bin.sh", "blockcheck2.sh", "init.d/systemd/zapret2.service", "config.default"} {
		if _, err := os.Stat(filepath.Join(dest, f)); err != nil {
			t.Error(err)
		}
	}
	t.Logf("%s ok -> %s", rel.Tag, dest)
}
