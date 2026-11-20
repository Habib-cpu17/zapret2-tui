// zapret2-tui: terminal UI and easy installer for zapret2.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"

	"zapret2/tui/internal/blockcheck"
	"zapret2/tui/internal/config"
	"zapret2/tui/internal/profiles"
	"zapret2/tui/internal/service"
	"zapret2/tui/internal/strategy"
	"zapret2/tui/internal/sys"
	"zapret2/tui/internal/ui"
)

var version = "dev"

const usage = `zapret2-tui — easy installer and terminal UI for zapret2

usage:
  zapret2-tui [--screen NAME]   open the UI (re-runs itself with sudo if needed)
  zapret2-tui status            print service status
  zapret2-tui start|stop|restart
  zapret2-tui scan [flags]      run blockcheck and print ranked strategies
      --domains "a.com b.com" --level quick|standard|force --ipv 4|6|46
      --http --no-tls12 --no-tls13 --no-quic --repeats N
  zapret2-tui apply NAME        apply a saved strategy profile and restart
  zapret2-tui profiles          list saved strategy profiles
  zapret2-tui version

env: ZAPRET_BASE (default /opt/zapret2), ZAPRET_REPO (default bol-van/zapret2),
     ZAPRET_TUI_NOSUDO=1 to open the UI read-only without sudo
`

func main() {
	args := os.Args[1:]
	cmd := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}
	switch cmd {
	case "", "ui":
		fs := flag.NewFlagSet("ui", flag.ExitOnError)
		screen := fs.String("screen", "", "screen to open (Dashboard, Install, …)")
		fs.Usage = func() { fmt.Print(usage) }
		fs.Parse(args)
		elevate()
		runUI(*screen)
	case "status":
		printStatus()
	case "start", "stop", "restart":
		elevate()
		f := map[string]func(func(string)) error{"start": service.Start, "stop": service.Stop, "restart": service.Restart}[cmd]
		die(f(func(s string) { fmt.Println(s) }))
	case "scan":
		elevate()
		runScan(args)
	case "apply":
		elevate()
		if len(args) != 1 {
			die(fmt.Errorf("usage: zapret2-tui apply NAME"))
		}
		applyProfile(args[0])
	case "profiles":
		for _, p := range profiles.Load() {
			fmt.Printf("%-28s %s\n", p.Name, p.Note)
		}
	case "version", "--version", "-v":
		fmt.Println("zapret2-tui", version)
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
}

func die(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// elevate re-execs through sudo when not root, keeping ZAPRET_* env.
func elevate() {
	if sys.IsRoot() || os.Getenv("ZAPRET_TUI_NOSUDO") == "1" {
		return
	}
	sudo := sys.Which("sudo")
	if sudo == "" {
		fmt.Fprintln(os.Stderr, "note: not root and sudo not found; running read-only")
		return
	}
	self, err := os.Executable()
	if err != nil {
		return
	}
	argv := []string{"sudo"}
	var env []string
	for _, k := range []string{"ZAPRET_BASE", "ZAPRET_REPO", "TERM", "COLORTERM"} {
		if v := os.Getenv(k); v != "" {
			env = append(env, k+"="+v)
		}
	}
	if len(env) > 0 {
		argv = append(argv, append([]string{"env"}, env...)...)
	}
	argv = append(argv, append([]string{self}, os.Args[1:]...)...)
	err = syscall.Exec(sudo, argv, os.Environ())
	fmt.Fprintln(os.Stderr, "sudo failed:", err)
}

func runUI(screen string) {
	p := tea.NewProgram(ui.New(screen), tea.WithAltScreen())
	_, err := p.Run()
	ui.Cleanup()
	die(err)
}

func printStatus() {
	st := service.Get()
	fmt.Println("installed:", st.Installed, "("+sys.Base+")")
	if !st.Installed {
		return
	}
	fmt.Println("service:  ", st.Active, "/ autostart", st.Enabled)
	fmt.Println("nfqws2:   ", st.PIDs)
	fmt.Println("firewall: ", sys.FWType(), "rules:", st.FWRules)
	if f, err := config.Load(sys.Path("config")); err == nil {
		fmt.Println("filter:   ", f.GetOr("MODE_FILTER", "none"))
		for _, p := range strategy.Split(f.GetOr("NFQWS2_OPT", "")) {
			fmt.Printf("  %-22s %s\n", p.Proto().Label(), p.Actions())
		}
	}
}

func runScan(args []string) {
	o := blockcheck.DefaultOptions()
	fs := flag.NewFlagSet("scan", flag.ExitOnError)
	domains := fs.String("domains", strings.Join(o.Domains, " "), "space separated blocked domains")
	fs.StringVar(&o.ScanLevel, "level", o.ScanLevel, "quick|standard|force")
	fs.StringVar(&o.IPVs, "ipv", o.IPVs, "4|6|46")
	fs.IntVar(&o.Repeats, "repeats", o.Repeats, "repeats per test")
	fs.BoolVar(&o.HTTP, "http", false, "test plain HTTP")
	noT12 := fs.Bool("no-tls12", false, "skip TLS 1.2")
	noT13 := fs.Bool("no-tls13", false, "skip TLS 1.3")
	noQ := fs.Bool("no-quic", false, "skip QUIC")
	fs.Parse(args)
	o.Domains = strings.Fields(*domains)
	o.TLS12, o.TLS13, o.QUIC = !*noT12, !*noT13, !*noQ
	if !sys.Installed() {
		die(fmt.Errorf("zapret2 is not installed at %s", sys.Base))
	}
	wasUp := service.Get().Running()
	if wasUp {
		die(service.Stop(func(s string) { fmt.Println(s) }))
		defer service.Start(func(s string) { fmt.Println(s) })
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	lines := make(chan string, 256)
	errc := make(chan error, 1)
	go func() { errc <- blockcheck.Run(ctx, o, lines) }()
	var all []string
	for l := range lines {
		all = append(all, l)
		fmt.Println(l)
	}
	if err := <-errc; err != nil {
		fmt.Fprintln(os.Stderr, "blockcheck:", err)
	}
	fmt.Println("\n=== ranked strategies ===")
	for _, r := range blockcheck.Rank(blockcheck.ParseSummary(all)) {
		fmt.Printf("%d/%d %-5s %s  %s\n", len(r.Domains), r.Total, r.Proto, r.IPV, r.Strategy)
	}
}

func applyProfile(name string) {
	for _, p := range profiles.Load() {
		if p.Name == name {
			f, err := config.Load(sys.Path("config"))
			die(err)
			f.Set("NFQWS2_ENABLE", "1")
			f.Set("NFQWS2_OPT", p.Opt)
			die(f.Save())
			die(service.Restart(func(s string) { fmt.Println(s) }))
			fmt.Println("applied", name)
			return
		}
	}
	die(fmt.Errorf("no profile %q (see: zapret2-tui profiles)", name))
}
