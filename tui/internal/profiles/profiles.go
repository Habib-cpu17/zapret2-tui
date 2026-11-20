// Package profiles stores named NFQWS2_OPT strategies the user saved.
package profiles

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"zapret2/tui/internal/strategy"
	"zapret2/tui/internal/sys"
)

type Profile struct {
	Name    string    `json:"name"`
	Opt     string    `json:"opt"`
	Note    string    `json:"note,omitempty"`
	Created time.Time `json:"created"`
	BuiltIn bool      `json:"-"`
}

func file() string { return sys.Path("tui", "profiles.json") }

// BuiltIns are always offered.
var BuiltIns = []Profile{
	{Name: "zapret2 default", Opt: strategy.Default, Note: "config.default: fake+split for HTTP, fake+disorder for TLS, fake for QUIC", BuiltIn: true},
}

// Load returns built-ins followed by saved profiles.
func Load() []Profile {
	var saved []Profile
	if b, err := os.ReadFile(file()); err == nil {
		_ = json.Unmarshal(b, &saved)
	}
	return append(append([]Profile{}, BuiltIns...), saved...)
}

func save(ps []Profile) error {
	var saved []Profile
	for _, p := range ps {
		if !p.BuiltIn {
			saved = append(saved, p)
		}
	}
	b, _ := json.MarshalIndent(saved, "", "  ")
	os.MkdirAll(filepath.Dir(file()), 0o755)
	return os.WriteFile(file(), b, 0o644)
}

// Add saves p, replacing any saved profile with the same name.
func Add(p Profile) error {
	ps := Load()
	if p.Created.IsZero() {
		p.Created = time.Now()
	}
	for i := range ps {
		if !ps[i].BuiltIn && ps[i].Name == p.Name {
			ps[i] = p
			return save(ps)
		}
	}
	return save(append(ps, p))
}

func Delete(name string) error {
	var out []Profile
	for _, p := range Load() {
		if p.BuiltIn || p.Name != name {
			out = append(out, p)
		}
	}
	return save(out)
}
