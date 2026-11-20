# zapret2-tui

A terminal UI and one-line installer for zapret2. It does what `install_easy.sh`,
`blockcheck2.sh` and hand-editing `/opt/zapret2/config` do, from menus.

Supported: Linux with systemd and nftables or iptables (Fedora, Ubuntu, Debian, Arch, …).
OpenWrt, BSD, macOS and Windows still use the upstream scripts.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/Habib-cpu17/zapret2-tui/master/install.sh | sh
```

This puts `zapret2-tui` in `/usr/local/bin` and opens it. On first run the
**Install** screen checks your system, installs missing packages (curl, nftables, DNS tools, hexdump),
downloads the latest upstream zapret2 release, checks it against the SHA-256 that
GitHub publishes and the binaries against `sha256sum.txt`, then sets up and starts
the `zapret2` systemd service.

## Screens

| Screen | What it does |
|---|---|
| Dashboard | Status, start/stop/restart (`s` `x` `r`), autostart (`a`), site reachability test (`t`) |
| Install | Setup wizard; also updates an existing install, keeping config, lists and custom scripts |
| Find strategy | Runs `blockcheck2.sh` in batch mode against sites blocked for you, ranks what worked, applies your pick to `NFQWS2_OPT` and restarts |
| Strategies | Saved strategy profiles (every applied scan is saved), raw `NFQWS2_OPT` editor checked with `nfqws2 --dry-run` |
| Lists | Edit bypass / exclude / IP-ban lists, choose the `GETLIST` source, update lists |
| Settings | Filter mode, IPv6, firewall type, ports, router interfaces, extras (Discord voice, STUN, QUIC for all, WireGuard, DHT) |
| Logs | Follows `journalctl -u zapret2` |
| Uninstall | Runs `uninstall_easy.sh` and removes `/opt/zapret2` |

Keys: arrows or `j`/`k` to move, `enter` to open, `esc` to go back to the menu, `q` to quit.

## Commands

```
zapret2-tui                 open the UI (asks for sudo)
zapret2-tui status          print status and active strategy
zapret2-tui start|stop|restart
zapret2-tui scan --domains "youtube.com discord.com" --level quick
zapret2-tui profiles        list saved strategies
zapret2-tui apply NAME      apply a saved strategy
```

## How it works

The TUI doesn't change any upstream files. It runs them:

- `install_bin.sh` picks the binaries for your CPU
- `blockcheck2.sh` runs with `BATCH=1` and settings passed as environment variables; the TUI reads its `* SUMMARY` section
- the config is the same shell file upstream uses; the TUI edits only the values you change and leaves comments and everything else as they are
- the service is the upstream `init.d/systemd/zapret2.service`
- extras are the upstream `init.d/custom.d.examples.linux` scripts, copied into `custom.d`

Saved strategies live in `/opt/zapret2/tui/profiles.json`.

## Build

```sh
cd tui
go test ./...
go build -o zapret2-tui ./cmd/zapret2-tui
```

Releases: push a tag like `tui-v0.1.0`; `.github/workflows/tui-release.yml` builds
static binaries for amd64, arm64 and armv7 and attaches them with `install.sh`.

Environment overrides: `ZAPRET_BASE` (install dir, for testing), `ZAPRET_REPO`
(where to download zapret2 releases from), `ZAPRET_TUI_NOSUDO=1` (open read-only).
