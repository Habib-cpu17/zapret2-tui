// Package config reads and edits zapret2's shell-style config file
// (/opt/zapret2/config) while preserving comments, ordering and any lines it
// does not touch, so the file stays hand-editable and upstream-compatible.
package config

import (
	"os"
	"regexp"
	"strings"
)

var assignRe = regexp.MustCompile(`^(\s*)(#?)([A-Za-z_][A-Za-z0-9_]*)=(.*)$`)

// block is one or more physical lines. Assignment blocks carry the key.
type block struct {
	lines  []string
	key    string
	active bool // false = commented-out assignment ("#KEY=...")
}

type File struct {
	blocks []block
	path   string
}

func Load(path string) (*File, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	f := Parse(string(b))
	f.path = path
	return f, nil
}

// Parse splits text into blocks. A value opening a quote that is not closed on
// the same line (e.g. NFQWS2_OPT="\n...\n") spans until the closing quote.
func Parse(text string) *File {
	f := &File{}
	lines := strings.Split(text, "\n")
	// strings.Split on text ending in "\n" yields a trailing "" we keep so that
	// String() round-trips exactly.
	for i := 0; i < len(lines); i++ {
		m := assignRe.FindStringSubmatch(lines[i])
		if m == nil {
			f.blocks = append(f.blocks, block{lines: []string{lines[i]}})
			continue
		}
		b := block{lines: []string{lines[i]}, key: m[3], active: m[2] == ""}
		if q := openQuote(m[4]); q != 0 {
			for i+1 < len(lines) {
				i++
				b.lines = append(b.lines, lines[i])
				if closesQuote(lines[i], q) {
					break
				}
			}
		}
		f.blocks = append(f.blocks, b)
	}
	return f
}

// openQuote returns the quote char if v opens a quote it doesn't close.
func openQuote(v string) byte {
	if v == "" || (v[0] != '"' && v[0] != '\'') {
		return 0
	}
	q := v[0]
	if closesQuote(v[1:], q) {
		return 0
	}
	return q
}

func closesQuote(s string, q byte) bool {
	for i := 0; i < len(s); i++ {
		if q == '"' && s[i] == '\\' {
			i++
			continue
		}
		if s[i] == q {
			return true
		}
	}
	return false
}

func (f *File) String() string {
	var all []string
	for _, b := range f.blocks {
		all = append(all, b.lines...)
	}
	return strings.Join(all, "\n")
}

func (f *File) Save() error { return f.SaveAs(f.path) }

func (f *File) SaveAs(path string) error {
	tmp := path + ".tui-tmp"
	if err := os.WriteFile(tmp, []byte(f.String()), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (f *File) find(key string, active bool) int {
	idx := -1
	for i, b := range f.blocks {
		if b.key == key && b.active == active {
			idx = i // last assignment wins in shell
		}
	}
	return idx
}

// Get returns the unquoted value of an active assignment.
func (f *File) Get(key string) (string, bool) {
	i := f.find(key, true)
	if i < 0 {
		return "", false
	}
	raw := strings.Join(f.blocks[i].lines, "\n")
	raw = raw[strings.Index(raw, "=")+1:]
	return unquote(raw), true
}

// GetOr returns Get or def.
func (f *File) GetOr(key, def string) string {
	if v, ok := f.Get(key); ok {
		return v
	}
	return def
}

func unquote(v string) string {
	if len(v) >= 2 && v[0] == '\'' && v[len(v)-1] == '\'' {
		return v[1 : len(v)-1]
	}
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		v = v[1 : len(v)-1]
		var sb strings.Builder
		for i := 0; i < len(v); i++ {
			if v[i] == '\\' && i+1 < len(v) && strings.IndexByte("\"\\$`", v[i+1]) >= 0 {
				i++
			}
			sb.WriteByte(v[i])
		}
		return sb.String()
	}
	return v
}

// Quote renders v for a shell assignment. Plain words stay bare.
func Quote(v string) string {
	if v != "" && !strings.ContainsAny(v, " \t\n\"'\\$`;&|<>()*?[]#~!{}") {
		return v
	}
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`, "`", "\\`")
	return `"` + r.Replace(v) + `"`
}

// Set assigns key. It replaces an active assignment in place, otherwise
// uncomments the "#KEY=" template line, otherwise appends.
func (f *File) Set(key, value string) {
	line := key + "=" + Quote(value)
	nb := block{lines: strings.Split(line, "\n"), key: key, active: true}
	if i := f.find(key, true); i >= 0 {
		indent := assignRe.FindStringSubmatch(f.blocks[i].lines[0])[1]
		nb.lines[0] = indent + nb.lines[0]
		f.blocks[i] = nb
		return
	}
	if i := f.find(key, false); i >= 0 {
		f.blocks[i] = nb
		return
	}
	// append before trailing empty line if present
	n := len(f.blocks)
	if n > 0 && len(f.blocks[n-1].lines) == 1 && f.blocks[n-1].lines[0] == "" && f.blocks[n-1].key == "" {
		f.blocks = append(f.blocks[:n-1], nb, f.blocks[n-1])
		return
	}
	f.blocks = append(f.blocks, nb)
}

// Unset comments out every active assignment of key.
func (f *File) Unset(key string) {
	for i, b := range f.blocks {
		if b.key == key && b.active {
			b.lines = append([]string(nil), b.lines...)
			b.lines[0] = "#" + strings.TrimLeft(b.lines[0], " \t")
			b.active = false
			f.blocks[i] = b
		}
	}
}

// SetOrUnset sets key when value is non-empty, otherwise comments it out.
func (f *File) SetOrUnset(key, value string) {
	if value == "" {
		f.Unset(key)
	} else {
		f.Set(key, value)
	}
}
