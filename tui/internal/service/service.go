// Package service controls the zapret2 service via systemd (or the sysv
// init script when systemd units are not installed).
package service

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"zapret2/tui/internal/sys"
)

const Unit = "zapret2"

// SystemdDir mirrors common/installer.sh.
func SystemdDir() string {
	if sys.Exists("/lib/systemd") {
		return "/lib/systemd/system"
	}
	return "/usr/lib/systemd/system"
}

func UnitInstalled() bool {
	return sys.HasSystemd() && sys.Exists(filepath.Join(SystemdDir(), Unit+".service"))
}

type Status struct {
	Installed bool
	Active    string // active / inactive / failed / unknown
	Enabled   string // enabled / disabled / -
	PIDs      []int  // running nfqws2 processes
	FWRules   bool   // zapret2 firewall table/chains present
}

func (s Status) Running() bool { return s.Active == "active" || len(s.PIDs) > 0 }

func Get() Status {
	s := Status{Installed: sys.Installed(), Active: "unknown", Enabled: "-"}
	if UnitInstalled() {
		s.Active, _ = sys.Output("systemctl", "is-active", Unit)
		s.Enabled, _ = sys.Output("systemctl", "is-enabled", Unit)
	}
	s.PIDs = Pids("nfqws2")
	if s.Active == "unknown" {
		if len(s.PIDs) > 0 {
			s.Active = "active"
		} else {
			s.Active = "inactive"
		}
	}
	if sys.Has("nft") {
		if _, err := sys.Output("nft", "list", "table", "inet", "zapret2"); err == nil {
			s.FWRules = true
		}
	}
	if !s.FWRules && sys.Has("iptables") {
		out, _ := sys.Output("iptables", "-t", "mangle", "-S")
		s.FWRules = strings.Contains(out, "NFQUEUE")
	}
	return s
}

// Pids scans /proc for processes whose executable basename is name.
func Pids(name string) []int {
	ents, _ := os.ReadDir("/proc")
	var out []int
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		b, err := os.ReadFile("/proc/" + e.Name() + "/comm")
		if err == nil && strings.TrimSpace(string(b)) == name {
			out = append(out, pid)
		}
	}
	return out
}

func ctl(log func(string), action string) error {
	if UnitInstalled() {
		return sys.RunLogged(log, "systemctl", action, Unit)
	}
	return sys.RunLogged(log, sys.Path("init.d", "sysv", "zapret2"), action)
}

func Start(log func(string)) error   { return ctl(log, "start") }
func Stop(log func(string)) error    { return ctl(log, "stop") }
func Restart(log func(string)) error { return ctl(log, "restart") }

func SetEnabled(log func(string), on bool) error {
	if !UnitInstalled() {
		return nil
	}
	if on {
		return sys.RunLogged(log, "systemctl", "enable", Unit)
	}
	return sys.RunLogged(log, "systemctl", "disable", Unit)
}
