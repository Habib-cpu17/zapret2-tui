// Package sys holds host detection and process helpers shared by the rest of the TUI.
package sys

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

// Base is where zapret2 is installed. Upstream scripts and the systemd unit
// hardcode /opt/zapret2, so only override it (ZAPRET_BASE) for testing.
var Base = envOr("ZAPRET_BASE", "/opt/zapret2")

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// Path joins elements onto Base.
func Path(elem ...string) string { return filepath.Join(append([]string{Base}, elem...)...) }

func IsRoot() bool { return os.Geteuid() == 0 }

func Exists(p string) bool { _, err := os.Stat(p); return err == nil }

// Has reports whether cmd is on PATH (also checking sbin dirs, which are often
// missing from a normal user's PATH).
func Has(cmd string) bool { return Which(cmd) != "" }

func Which(cmd string) string {
	if p, err := exec.LookPath(cmd); err == nil {
		return p
	}
	for _, d := range []string{"/usr/sbin", "/sbin", "/usr/local/sbin"} {
		p := filepath.Join(d, cmd)
		if st, err := os.Stat(p); err == nil && st.Mode()&0o111 != 0 {
			return p
		}
	}
	return ""
}

// HasSystemd mirrors upstream's check: systemd is PID 1.
func HasSystemd() bool { return Exists("/run/systemd/system") }

// FWType mirrors common/fwtype.sh:linux_fwtype (nftables if nft exists, else iptables).
func FWType() string {
	if Has("nft") {
		return "nftables"
	}
	if Has("iptables") && Has("ip6tables") {
		return "iptables"
	}
	return "unsupported"
}

// Installed reports whether zapret2 looks installed at Base.
func Installed() bool {
	return Exists(Path("init.d", "sysv", "zapret2")) && Exists(Path("config"))
}

// Interfaces returns non-loopback interface names.
func Interfaces() []string {
	ifs, _ := net.Interfaces()
	var out []string
	for _, i := range ifs {
		if i.Flags&net.FlagLoopback == 0 {
			out = append(out, i.Name)
		}
	}
	sort.Strings(out)
	return out
}

// PkgManager returns an install command prefix for the host, or "".
func PkgManager() []string {
	switch {
	case Has("dnf"):
		return []string{"dnf", "install", "-y"}
	case Has("apt-get"):
		return []string{"apt-get", "install", "-y"}
	case Has("pacman"):
		return []string{"pacman", "-S", "--noconfirm", "--needed"}
	case Has("zypper"):
		return []string{"zypper", "--non-interactive", "install"}
	case Has("apk"):
		return []string{"apk", "add"}
	}
	return nil
}

// Missing returns packages needed by zapret2 that are absent, mirroring
// common/installer.sh:check_prerequisites_linux.
func Missing(fw string) []string {
	req := [][2]string{{"curl", "curl"}}
	switch fw {
	case "iptables":
		req = append(req, [2]string{"iptables", "iptables"}, [2]string{"ip6tables", "iptables"}, [2]string{"ipset", "ipset"})
	default:
		req = append(req, [2]string{"nft", "nftables"})
	}
	seen := map[string]bool{}
	var out []string
	for _, r := range req {
		if !Has(r[0]) && !seen[r[1]] {
			seen[r[1]] = true
			out = append(out, r[1])
		}
	}
	// blockcheck2.sh needs nslookup or host, and hexdump (common/base.sh:random)
	if !Has("nslookup") && !Has("host") {
		out = append(out, pkgFor("dns"))
	}
	if !Has("hexdump") {
		out = append(out, pkgFor("hexdump"))
	}
	return out
}

// pkgFor names the package providing a tool on this distro.
func pkgFor(tool string) string {
	pm := ""
	if p := PkgManager(); p != nil {
		pm = p[0]
	}
	names := map[string]map[string]string{
		"dns":     {"apt-get": "dnsutils", "pacman": "bind", "apk": "bind-tools", "": "bind-utils"},
		"hexdump": {"apt-get": "bsdextrautils", "apk": "util-linux-misc", "": "util-linux"},
	}[tool]
	if n, ok := names[pm]; ok {
		return n
	}
	return names[""]
}

// Output runs a command and returns trimmed combined output.
func Output(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Stdin = nil
	b, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(b)), err
}

// Stream runs a command (stdin = /dev/null so upstream "press enter" prompts
// return immediately), sending each output line to lines. It runs in its own
// process group so ctx cancellation kills the whole tree (blockcheck spawns
// nfqws2/curl children).
func Stream(ctx context.Context, env []string, lines chan<- string, name string, args ...string) error {
	defer close(lines)
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), env...)
	// own process group (so cancel can signal the whole tree) and die with us
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGTERM}
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			// SIGINT first: blockcheck traps it and cleans its firewall rules.
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGINT)
		case <-done:
		}
	}()
	scanDone := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			lines <- sc.Text()
		}
		_, _ = io.Copy(io.Discard, pr)
		close(scanDone)
	}()
	err := cmd.Wait()
	close(done)
	pw.Close()
	<-scanDone
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

// RunLogged runs a command, sending each output line to log as it appears.
func RunLogged(log func(string), name string, args ...string) error {
	cmd := exec.Command(name, args...)
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(name), err)
	}
	done := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			if l := strings.TrimRight(sc.Text(), "\r"); l != "" {
				log(l)
			}
		}
		_, _ = io.Copy(io.Discard, pr)
		close(done)
	}()
	err := cmd.Wait()
	pw.Close()
	<-done
	if err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(name), err)
	}
	return nil
}

// CurlHTTP3 reports whether curl can do HTTP/3 (blockcheck skips QUIC tests otherwise).
func CurlHTTP3() bool {
	out, err := Output("curl", "-V")
	return err == nil && strings.Contains(out, "HTTP3")
}
