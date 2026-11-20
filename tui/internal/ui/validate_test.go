package ui

import (
	"os"
	"testing"

	"zapret2/tui/internal/strategy"
)

// needs a base with binaries: ZAPRET_BASE=<extracted release> go test ./internal/ui -run Validate
func TestValidate(t *testing.T) {
	if os.Getenv("ZAPRET_BASE") == "" {
		t.Skip("set ZAPRET_BASE to an extracted release with install_bin.sh run")
	}
	if err := validate(strategy.Default); err != nil {
		t.Fatalf("default rejected: %v", err)
	}
	if err := validate("--filter-tcp=443 --bogus-option=1"); err == nil {
		t.Fatal("bogus option accepted")
	} else {
		t.Log("bad option error:", err)
	}
}
