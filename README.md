# zapret2-tui

A terminal UI and one-line installer for [zapret2](https://github.com/bol-van/zapret2).

> **This is not the official zapret2.** zapret2 is written and maintained by
> [bol-van](https://github.com/bol-van/zapret2). This repository is a fork that adds
> a menu-driven front end on top of it. The zapret2 code is unchanged. Report bugs in
> zapret2 itself upstream, and bugs in the TUI here.

zapret2 is a powerful DPI bypass tool, but setting it up means running an
interactive installer, running `blockcheck2.sh` to find a strategy that works on
your network, and hand-editing `/opt/zapret2/config`. zapret2-tui does all of that
from menus.

```
zapret2  ● RUNNING · nfqws2 pid 1767 · nftables · filter: autohostlist
────────────────────────────────────────────────────────────────────────────────
  Dashboard     │ Dashboard
  Install       │
  Find strategy │ Service       running
  Strategies    │ Autostart     on (starts at boot)
  Lists         │ Firewall      ✓ nftables rules active
  Settings      │ Filter mode   autohostlist  bypass list + domains learned automatically
  Logs          │
  Uninstall     │ Active strategy
                │   HTTP (tcp/80)         fake → multisplit
                │   HTTPS/TLS (tcp/443)   fake → multidisorder
                │   QUIC/HTTP3 (udp/443)  fake
                │
                │ Site test  [t] checks HTTPS reachability through zapret2
                │   youtube.com            ✓ HTTP 301 · 322ms
                │   discord.com            ✓ HTTP 200 · 343ms
s start  ·  x stop  ·  r restart  ·  a autostart  ·  t test sites  ·  esc menu
```

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/Habib-cpu17/zapret2-tui/master/install.sh | sh
```

This downloads the `zapret2-tui` binary for your CPU (amd64, arm64 or armv7), checks
its SHA-256, puts it in `/usr/local/bin` and opens it. The **Install** screen then:

1. checks your system and installs missing packages (curl, nftables, DNS tools, hexdump)
2. asks three questions: which traffic to handle, IPv6, and whether this machine is a router
3. downloads the latest **official** zapret2 release from bol-van/zapret2 and verifies it
4. sets up and starts the `zapret2` systemd service

If zapret2 is already installed, the same screen updates it and keeps your config,
lists and custom scripts.

Later, run it with `sudo zapret2-tui`.

**Requirements:** Linux with systemd and nftables or iptables (Fedora, Ubuntu,
Debian, Arch, …). For OpenWrt, BSD, macOS or Windows, use
[upstream zapret2](https://github.com/bol-van/zapret2) directly.

## Usage

1. **Find strategy.** Enter a few sites that are blocked for you and start a scan.
   zapret2 is stopped during the scan and started again afterwards. You get the best
   working strategies for each protocol. Pick one per protocol and press `a` to apply it.
2. **Dashboard.** Press `t` to check that the sites open now.
3. **Lists.** Add more sites to the bypass list. In *autohostlist* mode zapret2 also
   adds sites it detects as blocked.

| Screen | What it does |
|---|---|
| Dashboard | Status, start/stop/restart, autostart, site reachability test |
| Install | Setup wizard and updates |
| Find strategy | Runs blockcheck2, ranks the results and applies your pick |
| Strategies | Saved strategies; raw `NFQWS2_OPT` editor checked with `nfqws2 --dry-run` |
| Lists | Bypass / exclude / IP-ban lists, list sources, update |
| Settings | Filter mode, IPv6, firewall, ports, router interfaces, extras (Discord voice, STUN calls, QUIC, WireGuard, DHT) |
| Logs | Live `journalctl -u zapret2` |
| Uninstall | Removes the service, firewall rules and `/opt/zapret2` |

Keys: arrows or `j`/`k` to move, `enter` to open, `esc` to go back to the menu, `q` to quit.

### Command line

```
zapret2-tui status
zapret2-tui start | stop | restart
zapret2-tui scan --domains "youtube.com discord.com" --level quick
zapret2-tui profiles
zapret2-tui apply NAME
```

## How it works

zapret2-tui doesn't change zapret2. It runs zapret2's own scripts:
`install_bin.sh`, `blockcheck2.sh` (in batch mode), `ipset/*.sh`, `uninstall_easy.sh`,
and the stock systemd unit. It edits only the config values you change and keeps
comments and everything else as they are. If you stop using the TUI, your install is a
normal zapret2 install.

More detail: [docs/tui.md](docs/tui.md). The zapret2 manual: [docs/manual.en.md](docs/manual.en.md).

## Build from source

```sh
cd tui
go test ./...
go build -o zapret2-tui ./cmd/zapret2-tui
```

## License

zapret2 is © bol-van, MIT licensed ([docs/LICENSE.txt](docs/LICENSE.txt)).
The TUI in `tui/` is released under the same license.
