// Package install performs the Linux/systemd install that upstream
// install_easy.sh does interactively (install_systemd), using its scripts
// where they are non-interactive (install_bin.sh, ipset/*.sh, uninstall_easy.sh).
package install

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"zapret2/tui/internal/config"
	"zapret2/tui/internal/release"
	"zapret2/tui/internal/service"
	"zapret2/tui/internal/sys"
)

// Options are the wizard answers.
type Options struct {
	ModeFilter string // none, hostlist, autohostlist
	IPv6       bool
	FWType     string // nftables / iptables
	IfaceLAN   string // router only; empty on desktops
	IfaceWAN   string
	Tarball    string // optional local tarball instead of downloading
}

// Step is one named install step.
type Step struct {
	Name string
	Run  func(log func(string), progress func(float64)) error
}

// preserved user files kept across reinstall (paths relative to Base).
var preserved = []string{
	"config",
	"ipset/zapret-hosts-user.txt",
	"ipset/zapret-hosts-user-exclude.txt",
	"ipset/zapret-hosts-user-ipban.txt",
	"ipset/zapret-hosts-auto.txt",
	"init.d/sysv/custom.d",
	"tui",
}

// Plan returns the install steps for opts.
func Plan(opts Options) []Step {
	var tmp, tarball string
	var rel release.Release
	newDir := sys.Base + ".new"
	return []Step{
		{"Download zapret2 release", func(log func(string), progress func(float64)) error {
			if opts.Tarball != "" {
				tarball = opts.Tarball
				log("using local tarball " + tarball)
				return nil
			}
			var err error
			rel, err = release.Latest()
			if err != nil {
				return err
			}
			log("latest release: " + rel.Tag + " from github.com/" + release.Repo)
			tmp, err = os.MkdirTemp("", "zapret2-tui-")
			if err != nil {
				return err
			}
			tarball, err = release.Download(rel, tmp, func(d, t int64) {
				if t > 0 {
					progress(float64(d) / float64(t))
				}
			})
			if err == nil {
				log("archive sha256 verified")
			}
			return err
		}},
		{"Unpack files", func(log func(string), _ func(float64)) error {
			os.RemoveAll(newDir)
			if err := release.Extract(tarball, newDir); err != nil {
				return err
			}
			if tmp != "" {
				os.RemoveAll(tmp)
			}
			if !sys.Exists(filepath.Join(newDir, "init.d", "systemd", "zapret2.service")) {
				return fmt.Errorf("archive is missing init.d/systemd files (embedded release?)")
			}
			log("unpacked to " + newDir)
			if opts.Tarball == "" {
				n, err := release.VerifyBinaries(rel, newDir)
				if err != nil {
					return err
				}
				log(fmt.Sprintf("%d binaries match sha256sum.txt", n))
			}
			return nil
		}},
		{"Stop running zapret2", func(log func(string), _ func(float64)) error {
			if sys.Installed() {
				_ = service.Stop(log)
			}
			return nil
		}},
		{"Move into " + sys.Base, func(log func(string), _ func(float64)) error {
			if sys.Exists(sys.Base) {
				for _, p := range preserved {
					src := filepath.Join(sys.Base, p)
					if sys.Exists(src) {
						if err := copyTree(src, filepath.Join(newDir, p)); err != nil {
							return err
						}
						log("kept " + p)
					}
				}
				old := sys.Base + ".old"
				os.RemoveAll(old)
				if err := os.Rename(sys.Base, old); err != nil {
					return err
				}
				defer os.RemoveAll(old)
			}
			if err := os.Rename(newDir, sys.Base); err != nil {
				return err
			}
			os.MkdirAll(sys.Path("tmp"), 0o755)
			return sys.RunLogged(log, "chown", "-R", "root:root", sys.Base)
		}},
		{"Install binaries for this CPU", func(log func(string), _ func(float64)) error {
			if err := sys.RunLogged(log, "sh", sys.Path("install_bin.sh")); err != nil {
				return fmt.Errorf("no compatible binaries found: %w", err)
			}
			return nil
		}},
		{"Write config", func(log func(string), _ func(float64)) error {
			defaultFiles()
			return WriteConfig(opts, log)
		}},
		{"Install systemd service", func(log func(string), _ func(float64)) error {
			dir := service.SystemdDir()
			for _, u := range []string{"zapret2.service", "zapret2-list-update.service", "zapret2-list-update.timer"} {
				if err := copyFile(sys.Path("init.d", "systemd", u), filepath.Join(dir, u), 0o644); err != nil {
					return err
				}
			}
			if err := sys.RunLogged(log, "systemctl", "daemon-reload"); err != nil {
				return err
			}
			if err := sys.RunLogged(log, "systemctl", "enable", "zapret2"); err != nil {
				return err
			}
			return sys.RunLogged(log, "systemctl", "enable", "--now", "zapret2-list-update.timer")
		}},
		{"Download host/IP lists", func(log func(string), _ func(float64)) error {
			_ = sys.RunLogged(log, sys.Path("ipset", "clear_lists.sh"))
			if err := sys.RunLogged(log, sys.Path("ipset", "get_config.sh")); err != nil {
				log("list update failed (not fatal): " + err.Error())
			}
			return nil
		}},
		{"Start zapret2", func(log func(string), _ func(float64)) error {
			return service.Start(log)
		}},
	}
}

// WriteConfig creates config from config.default if missing and applies opts.
func WriteConfig(opts Options, log func(string)) error {
	path := sys.Path("config")
	if !sys.Exists(path) {
		if err := copyFile(sys.Path("config.default"), path, 0o644); err != nil {
			return err
		}
	}
	f, err := config.Load(path)
	if err != nil {
		return err
	}
	f.Set("NFQWS2_ENABLE", "1")
	f.Set("MODE_FILTER", opts.ModeFilter)
	f.Set("FWTYPE", opts.FWType)
	if opts.IPv6 {
		f.Set("DISABLE_IPV6", "0")
	} else {
		f.Set("DISABLE_IPV6", "1")
	}
	f.SetOrUnset("IFACE_LAN", opts.IfaceLAN)
	f.SetOrUnset("IFACE_WAN", opts.IfaceWAN)
	// hostlist modes read ipset/zapret-hosts-user.txt directly (nfqws2
	// re-reads it on change); GETLIST is only needed for downloaded lists.
	log(fmt.Sprintf("MODE_FILTER=%s FWTYPE=%s IPv6=%v", opts.ModeFilter, opts.FWType, opts.IPv6))
	return f.Save()
}

// defaultFiles mirrors install_easy.sh:default_files.
func defaultFiles() {
	ip := sys.Path("ipset")
	os.MkdirAll(ip, 0o755)
	if p := filepath.Join(ip, "zapret-hosts-user-exclude.txt"); !sys.Exists(p) {
		copyFile(filepath.Join(ip, "zapret-hosts-user-exclude.txt.default"), p, 0o644)
	}
	if p := filepath.Join(ip, "zapret-hosts-user.txt"); !sys.Exists(p) {
		os.WriteFile(p, []byte("nonexistent.domain\n"), 0o644)
	}
	if p := filepath.Join(ip, "zapret-hosts-user-ipban.txt"); !sys.Exists(p) {
		os.WriteFile(p, nil, 0o644)
	}
	os.MkdirAll(sys.Path("init.d", "sysv", "custom.d"), 0o755)
}

// Uninstall runs upstream uninstall_easy.sh (stops service, removes units,
// timer, firewall table, ipsets) and then deletes Base.
func Uninstall(log func(string)) error {
	if sys.Exists(sys.Path("uninstall_easy.sh")) {
		if err := sys.RunLogged(log, "sh", sys.Path("uninstall_easy.sh")); err != nil {
			log("uninstall script: " + err.Error())
		}
	}
	for _, u := range []string{"zapret2.service", "zapret2-list-update.service", "zapret2-list-update.timer"} {
		os.Remove(filepath.Join(service.SystemdDir(), u))
	}
	if sys.HasSystemd() {
		_ = sys.RunLogged(log, "systemctl", "daemon-reload")
	}
	if err := os.RemoveAll(sys.Base); err != nil {
		return err
	}
	log("removed " + sys.Base)
	return nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	os.MkdirAll(filepath.Dir(dst), 0o755)
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(p, src)
		target := dst + rel
		switch {
		case info.IsDir():
			return os.MkdirAll(target, 0o755)
		case info.Mode()&os.ModeSymlink != 0:
			l, err := os.Readlink(p)
			if err != nil {
				return err
			}
			os.Remove(target)
			return os.Symlink(l, target)
		default:
			return copyFile(p, target, info.Mode().Perm())
		}
	})
}
