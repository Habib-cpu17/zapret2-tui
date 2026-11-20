// Package lists manages zapret2's host/IP list files and list sources.
package lists

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"zapret2/tui/internal/sys"
)

// File is an editable user list.
type File struct {
	Key   string
	Title string
	Help  string
}

var Files = []File{
	{"zapret-hosts-user.txt", "Bypass list", "Domains zapret2 applies tricks to (when filter mode is hostlist). Subdomains are included."},
	{"zapret-hosts-user-exclude.txt", "Exclude list", "Domains never touched by zapret2, even in autohostlist mode."},
	{"zapret-hosts-user-ipban.txt", "IP-ban list", "IPs/subnets blocked by IP rather than DPI."},
	{"zapret-hosts-auto.txt", "Auto list", "Domains zapret2 learned automatically (autohostlist mode)."},
}

func path(name string) string { return sys.Path("ipset", name) }

// Read returns non-comment, non-empty entries.
func Read(name string) []string {
	f, err := os.Open(path(name))
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if l != "" && !strings.HasPrefix(l, "#") {
			out = append(out, l)
		}
	}
	return out
}

// Write replaces the list with entries (deduped, sorted).
func Write(name string, entries []string) error {
	seen := map[string]bool{}
	var out []string
	for _, e := range entries {
		e = strings.ToLower(strings.TrimSpace(e))
		// accept pasted URLs ("https://x.com/path" -> "x.com"); keep CIDRs intact
		if i := strings.Index(e, "://"); i >= 0 {
			e = strings.SplitN(e[i+3:], "/", 2)[0]
		}
		e = strings.TrimSuffix(e, ".")
		if e != "" && !seen[e] {
			seen[e] = true
			out = append(out, e)
		}
	}
	sort.Strings(out)
	data := strings.Join(out, "\n")
	if data != "" {
		data += "\n"
	}
	return os.WriteFile(path(name), []byte(data), 0o644)
}

func Add(name, entry string) error { return Write(name, append(Read(name), entry)) }
func Remove(name, entry string) error {
	var keep []string
	for _, e := range Read(name) {
		if e != entry {
			keep = append(keep, e)
		}
	}
	return Write(name, keep)
}

// Source is a GETLIST option (ipset/get_*.sh).
type Source struct {
	Script string
	Title  string
}

// Sources lists what's offered for GETLIST. Most upstream sources are
// Russia-specific registries; get_user.sh is the general-purpose one.
var Sources = []Source{
	{"", "None — only the lists you edit here (recommended)"},
	{"get_user.sh", "Resolve your bypass list to IPs (ipset filter mode)"},
	{"get_ipban.sh", "Your IP-ban list only"},
	{"get_refilter_domains.sh", "re:filter domains (Russia)"},
	{"get_antizapret_domains.sh", "antizapret domains (Russia)"},
	{"get_reestr_resolvable_domains.sh", "RKN registry domains (Russia)"},
	{"get_antifilter_allyouneed.sh", "antifilter allyouneed IPs (Russia)"},
}

// Count returns line count of a list, including the downloaded/gzipped
// generated lists that upstream puts beside the user files.
func Count(name string) int { return len(Read(name)) }

// Generated returns generated list files (zapret-hosts.txt[.gz], zapret-ip*.txt[.gz]).
func Generated() []string {
	m, _ := filepath.Glob(sys.Path("ipset", "zapret-*.txt*"))
	var out []string
	for _, p := range m {
		b := filepath.Base(p)
		if strings.HasPrefix(b, "zapret-hosts-user") || b == "zapret-hosts-auto.txt" || strings.HasSuffix(b, ".default") {
			continue
		}
		out = append(out, b)
	}
	return out
}

// Update re-downloads lists using GETLIST from config (ipset/get_config.sh).
func Update(log func(string)) error {
	_ = sys.RunLogged(log, sys.Path("ipset", "clear_lists.sh"))
	return sys.RunLogged(log, sys.Path("ipset", "get_config.sh"))
}
