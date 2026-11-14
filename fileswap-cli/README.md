# FileSwap CLI

> A FileSwap node for the terminal: send files between a browser and a Linux server (or between two servers), peer to peer.

Most transfer apps connect phones and laptops. `fileswap` adds **servers**: it is a single static binary that joins the same FileSwap network as the web app, so you can drop a file from your browser straight into a server, or push a log from a server to your laptop, with one command.

The CLI is a FileSwap device like any browser tab: it has a name and a **device code** (UUID), pairs by code, and uses the same signaling server (`fileswap/server`) and the same transfer format ([docs/protocol.md](../docs/protocol.md)). No extra server, no SSH keys, no open inbound ports.

> **Status:** early development. `init`, `id`, `peers`, `allow`, `version` and `help` work today. Pairing (`sync`), sending (`tfr`) and receiving (`listen`) are being built — see [Roadmap](#roadmap).

---

## Install

Download from the [latest release](https://github.com/SethSterling22/SwapFile/releases/latest) (`amd64` = x86_64, `arm64` = aarch64).

**Debian / Ubuntu**

```bash
sudo apt install ./fileswap_<version>_amd64.deb
```

**Arch Linux**

```bash
sudo pacman -U fileswap-<version>-1-x86_64.pkg.tar.zst
```

**Any Linux (plain binary)**

```bash
tar xzf fileswap_<version>_linux_amd64.tar.gz
sudo install -m 755 fileswap /usr/local/bin/
```

Each release also has `SHA256SUMS` to check the downloads: `sha256sum -c SHA256SUMS --ignore-missing`.

---

## Quick start

```bash
# 1. Create this node's identity, pointing at your FileSwap server
fileswap init --name srv-01 --server 192.168.1.10:7341

# 2. Show its code and the pairing link
fileswap id
```

Pair from the web app by typing (or pasting) the code under **"Código del otro dispositivo"**, or open the pairing link printed by `fileswap id` in the browser.

Planned flow once pairing and transfers land:

```bash
fileswap listen --dir ~/FileSwap        # on the server: stay online, receive files
fileswap sync 1f2e3d4c-....             # pair with a device by its code   (also: -sync)
fileswap tfr laptop ./backup.tar.gz     # send a file to a paired device   (also: -tfr)
```

---

## Commands

Run `fileswap --help` for the list, or `fileswap help <command>` / `fileswap <command> --help` for details.

| Command | Alias | What it does |
|---|---|---|
| `init --name <name> [--server <url>] [--force]` | | Create this node's identity. Re-running keeps the code and updates name/server; `--force` makes a new code |
| `id` | | Show name, device code, server and the browser pairing link |
| `peers` | | List paired devices |
| `sync <code>` | `-sync` | Ask a device to pair with this node *(coming)* |
| `tfr <node> <path>` | `-tfr`, `send` | Send a file to a paired device *(coming)* |
| `listen [--dir <path>]` | | Stay online to receive files and pairing requests *(coming)* |
| `allow <code>` | | Let `listen` accept pairing from this code without asking |
| `unpair <node>` | | Remove a paired device on both sides *(coming)* |
| `version` | `-v`, `--version` | Print the version |
| `help [command]` | `-h`, `--help` | Show help |

`<node>` is a device code, a unique prefix of it, or the device's name. `<code>` is a full device code.

`--server` accepts `host:port`, `http(s)://host[:port]` or `ws(s)://host[:port]/ws`; it is stored as the WebSocket URL. Use `https://` / `wss://` when the server is behind TLS.

Messages are in Spanish when the locale is `es_*` (`LANG`, `LC_MESSAGES` or `LC_ALL`), otherwise English — like the web app follows the browser.

### Where things are stored

| Path | Content |
|---|---|
| `$FILESWAP_CONFIG`, else `$XDG_CONFIG_HOME/fileswap`, else `~/.config/fileswap` | `identity.json` — device secret (mode `0600`), name, server, paired devices, allow list |

Override per command with `fileswap --config <dir> <command>`. The secret is what makes the code yours: copying `identity.json` to another machine clones the device.

---

## Build from source

Requires Go 1.24+ — or only Docker, using the `docker-*` targets.

```bash
cd fileswap-cli
make build            # ./dist/fileswap for this machine
make test             # go vet + go test
make dist             # dist/fileswap-linux-amd64 and -arm64
make package          # + .deb and Arch packages in dist/ (needs network for nfpm)

make docker-build     # the same, without installing Go
make docker-test
make docker-package
```

Binaries are static (`CGO_ENABLED=0`), so the same file runs on Arch, Debian or any other Linux of that architecture.

### Releases

Pushing a tag `vX.Y.Z` runs [.github/workflows/cli.yml](../.github/workflows/cli.yml): tests, builds `amd64`/`arm64`, packages `.deb` and Arch, installs them in `debian:stable` and `archlinux:latest` containers as a smoke test, and publishes everything to a GitHub Release.

```bash
git tag -a v0.2.0 -m "FileSwap v0.2.0"
git push origin v0.2.0
```

---

## Layout

```
fileswap-cli/
├── cmd/fileswap/       entry point (version set at build time)
├── internal/
│   ├── cli/            argument parsing, help (ES/EN), commands
│   ├── identity/       secret → device code, identity.json, paired devices
│   └── transfer/       DataChannel frames, safe file names on receive
├── packaging/          nfpm config for .deb and Arch packages
└── Makefile
```

Planned: `internal/signaling` (WebSocket client: register, pairing, presence, reconnect) and `internal/peer` (WebRTC with [pion](https://github.com/pion/webrtc)).

---

## Roadmap

- [x] **F0** — Protocol spec, web client fixes (bidirectional transfers, receipt ack, SHA-256), safe file names
- [ ] **F1** — Signaling client: `sync`, `peers` with online status, pairing requests in `listen`, `unpair`, `id --qr`
- [ ] **F2** — Transfers with pion: `tfr` and receiving in `listen`, streamed to disk, ack + SHA-256 check
- [ ] **F3** — `listen` as a service: systemd unit, reconnect, non-interactive pairing via `allow`
- [ ] **F4** — End-to-end tests in Arch and Debian containers (two nodes pair, transfer, compare hashes)
- [ ] Later — E2E encryption shared with the web app, folders / multiple files, resume, TURN

---

## Security notes

- Received file names are reduced to a plain name (no directories, no leading dots, no control characters) and never overwrite existing files.
- Transfers are encrypted in transit by WebRTC (DTLS), but there is **no end-to-end encryption yet**: do not use FileSwap for patient or medical data (PHI).

## License

[GPL-3.0](../LICENSE)
