# FileSwap CLI

> A FileSwap node for the terminal: send files between a browser and a Linux server (or between two servers), peer to peer.

Most transfer apps connect phones and laptops. `fileswap` adds **servers**: it is a single static binary that joins the same FileSwap network as the web app, so you can drop a file from your browser straight into a server, or push a log from a server to your laptop, with one command.

The CLI is a FileSwap device like any browser tab: it has a name and a **device code** (UUID), pairs by code, and uses the same signaling server (`fileswap/server`) and the same transfer format ([docs/protocol.md](../docs/protocol.md)). No extra server, no SSH keys, no open inbound ports.

> **Status:** early development. Identity, pairing with browsers and other nodes, presence and `listen` (foreground or background) work. Sending and receiving files (`tfr`) is next — see [Roadmap](#roadmap).

**No open ports.** `fileswap` only makes outgoing connections: a WebSocket to the FileSwap server (the same one the browser reaches) and, for transfers, WebRTC straight to the other device. Nothing on the node needs to be reachable from the network.

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

**Pair from the browser:** put the node online, then type (or paste) its code in the web app under **"Código del otro dispositivo"**, or open the pairing link from `fileswap id`.

```bash
fileswap listen                 # asks on the terminal: "Pair with Laptop (…)? [y/N]"
```

**Pair from the node:** with FileSwap open in the browser, copy its code and run:

```bash
fileswap sync 1f2e3d4c-....     # also: -sync — accept the request in the browser
```

**Keep a server online** in the background, accepting only devices you allowed:

```bash
fileswap allow 1f2e3d4c-....    # this code is accepted without asking
fileswap listen -d              # background; log in ~/.local/state/fileswap/listen.log
fileswap status                 # online? which devices are online?
fileswap stop                   # go offline
```

Coming next: `fileswap tfr laptop ./backup.tar.gz` (also `-tfr`) to send, and receiving into a folder while `listen` runs.

---

## Commands

Run `fileswap --help` for the list, or `fileswap help <command>` / `fileswap <command> --help` for details.

| Command | Alias | What it does |
|---|---|---|
| `init --name <name> [--server <url>] [--force]` | | Create this node's identity. Re-running keeps the code and updates name/server; `--force` makes a new code |
| `id` | | Show name, device code, server and the browser pairing link |
| `sync <code>` | `-sync` | Ask a device to pair with this node and wait (up to 2 min) for it to accept |
| `peers` | | List paired devices and which are online |
| `unpair <node>` | | Remove a paired device on both sides (only here if the server is unreachable) |
| `allow <code>` | | Let `listen` accept pairing from this code without asking |
| `listen [-d]` | | Stay online for pairing requests (and, soon, files). `-d` runs it in the background |
| `status` | | Show whether `listen` runs, its connection and peers. Exit code 3 when it is not running |
| `stop` | | Take a running `listen` offline |
| `tfr <node> <path>` | `-tfr`, `send` | Send a file to a paired device *(coming)* |
| `version` | `-v`, `--version` | Print the version |
| `help [command]` | `-h`, `--help` | Show help |

`<node>` is a device code, a unique prefix of it, or the device's name. `<code>` is a full device code.

`--server` accepts `host:port`, `http(s)://host[:port]` or `ws(s)://host[:port]/ws`; it is stored as the WebSocket URL. Use `https://` / `wss://` when the server is behind TLS.

Messages are in Spanish when the locale is `es_*` (`LANG`, `LC_MESSAGES` or `LC_ALL`), otherwise English — like the web app follows the browser.

### How `listen` and the other commands share the connection

The server allows **one connection per identity**: a second one with the same secret takes over and the first is disconnected. So while `listen` runs, `sync`, `peers`, `unpair`, `status` and `stop` don't connect themselves — they ask the running `listen` through a local Unix socket (`$XDG_RUNTIME_DIR/fileswap/<code>.sock`, in a directory only your user can open). Without a `listen`, `sync`, `peers` and `unpair` connect just for the command and disconnect.

Pairing requests reaching `listen`:

| Requester | Foreground (`listen`) | Background (`listen -d`) |
|---|---|---|
| In the allow list, or already paired | Accepted | Accepted |
| Anyone else | Asked on the terminal (2 min, default no) | Rejected — `fileswap allow <code>` and retry |

### Where things are stored

| Path | Content |
|---|---|
| `$FILESWAP_CONFIG`, else `$XDG_CONFIG_HOME/fileswap`, else `~/.config/fileswap` | `identity.json` — device secret (mode `0600`), name, server, paired devices, allow list |
| `$XDG_STATE_HOME/fileswap/listen.log`, else `~/.local/state/fileswap/listen.log` | Log of `listen -d` |
| `$XDG_RUNTIME_DIR/fileswap/<code>.sock`, else `/tmp/fileswap-<uid>/` | Control socket of a running `listen` |

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
│   ├── identity/       secret → device code, identity.json (locked updates), paired devices
│   ├── signaling/      WebSocket client: register, keep-alive, reconnect
│   ├── node/           a running device: pairing, presence, unpair, control socket
│   └── transfer/       DataChannel frames, safe file names on receive
├── packaging/          nfpm config for .deb and Arch packages
└── Makefile
```

Planned: `internal/peer` (WebRTC with [pion](https://github.com/pion/webrtc)).

`make test` runs the `node` integration tests against the real signaling server: it starts `fileswap/server` with `node` (after `npm ci` there), or uses `FILESWAP_TEST_SERVER=ws://host:port/ws`. Without either they are skipped. With `make docker-test`, start a server on the host and set `FILESWAP_TEST_SERVER`.

---

## Roadmap

- [x] **F0** — Protocol spec, web client fixes (bidirectional transfers, receipt ack, SHA-256), safe file names
- [x] **F1** — Signaling client: `sync`, `peers` with online status, pairing requests in `listen`, `unpair`, `listen -d` / `status` / `stop`
- [ ] `id --qr` (QR code in the terminal)
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
