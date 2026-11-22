# FileSwap CLI

> A FileSwap node for the terminal: send files between a browser and a Linux server (or between two servers), peer to peer.

Most transfer apps connect phones and laptops. `fileswap` adds **servers**: it is a single static binary that joins the same FileSwap network as the web app, so you can drop a file from your browser straight into a server, or push a log from a server to your laptop, with one command.

The CLI is a FileSwap device like any browser tab: it has a name and a **device code** (UUID), pairs by code, and uses the same signaling server (`fileswap/server`) and the same transfer format ([docs/protocol.md](../docs/protocol.md)). No extra server, no SSH keys, no open inbound ports.

> **Status:** early development, but the whole loop works: pair with browsers and other nodes, receive files in the background with `start` into a swap folder, send with `tfr` — every transfer checked end to end with SHA-256. See [Roadmap](#roadmap).

**No open ports.** `fileswap` only makes outgoing connections: a WebSocket to the FileSwap server (the same one the browser reaches) and, for transfers, WebRTC straight to the other device. Nothing on the node needs to be reachable from the network.

---

## Install

**One command** — Debian/Ubuntu, Arch, or any other Linux (amd64 or arm64):

```bash
curl -fsSL https://raw.githubusercontent.com/SethSterling22/SwapFile/main/fileswap-cli/install.sh | sh
```

It picks the right package (`.deb` with apt, the Arch package with pacman, or the plain binary in `/usr/local/bin` elsewhere), checks it against the release's `SHA256SUMS`, installs it with `sudo`, **deletes the download**, and restarts the `fileswap` service if it is running. Run the same command again to **update**. A specific version: `… | sh -s -- --version v0.8.0-alpha.1`. To read the script first: `curl -fsSL …/install.sh -o install.sh && less install.sh && sh install.sh`.

**Manually** — download into `/tmp`, install, delete (replace the version with the one you want from [Releases](https://github.com/SethSterling22/SwapFile/releases)):

```bash
# Debian / Ubuntu
cd /tmp && wget https://github.com/SethSterling22/SwapFile/releases/download/v0.8.0-alpha.1/fileswap_0.8.0.alpha.1_amd64.deb && sudo apt install ./fileswap_0.8.0.alpha.1_amd64.deb && rm fileswap_0.8.0.alpha.1_amd64.deb

# Arch Linux
cd /tmp && curl -LO https://github.com/SethSterling22/SwapFile/releases/download/v0.8.0-alpha.1/fileswap-0.8.0alpha.1-1-x86_64.pkg.tar.zst && sudo pacman -U fileswap-0.8.0alpha.1-1-x86_64.pkg.tar.zst && rm fileswap-0.8.0alpha.1-1-x86_64.pkg.tar.zst

# Any Linux (plain binary)
cd /tmp && curl -LO https://github.com/SethSterling22/SwapFile/releases/download/v0.8.0-alpha.1/fileswap_0.8.0-alpha.1_linux_amd64.tar.gz && tar xzf fileswap_0.8.0-alpha.1_linux_amd64.tar.gz && sudo install -m 755 fileswap_0.8.0-alpha.1_linux_amd64/fileswap /usr/local/bin/ && rm -r fileswap_0.8.0-alpha.1_linux_amd64*
```

Download into `/tmp` rather than your home folder: apt reads local packages as its `_apt` user, which cannot enter `~` (and otherwise prints a harmless "couldn't be accessed by user '_apt'" notice).

Every release keeps its versioned files and, from v0.8.1 on, also has **version-less copies** — `fileswap_amd64.deb`, `fileswap-x86_64.pkg.tar.zst`, `fileswap_linux_amd64.tar.gz` (and the `arm64`/`aarch64` ones) — so the newest *stable* release can always be downloaded from the same address:

```bash
cd /tmp && wget https://github.com/SethSterling22/SwapFile/releases/latest/download/fileswap_amd64.deb && sudo apt install ./fileswap_amd64.deb && rm fileswap_amd64.deb
```

(GitHub's `releases/latest` skips pre-releases such as `v0.8.1-alpha.1`; the install script does not.) Each release also has `SHA256SUMS` and `install.sh`.

**Uninstall:** `sudo apt remove fileswap`, `sudo pacman -R fileswap`, or `sudo rm /usr/local/bin/fileswap` — plus `fileswap service remove` first if you installed the service. Your identity (`~/.config/fileswap`), received files (`~/FileSwap`) and log (`~/.local/state/fileswap`) are left in place.

---

## Quick start

```bash
# 1. Create this node's identity, pointing at your FileSwap server
fileswap init --name srv-01 --server 192.168.1.10:7341

# 2. Go online in the background (the terminal stays free)
fileswap start
```

`start` prints the node's status — name, the **device code** in a box, server, swap folder, log file and a table of paired devices — and returns. Check it later with `fileswap status`, take it offline with `fileswap stop`.

**Pair from the node** (works while it runs in the background): with FileSwap open in the browser, copy the browser's code and run

```bash
fileswap sync 1f2e3d4c-....     # also: -sync — accept the request in the browser
```

**Pair from the browser:** type (or paste) the node's code in the web app under **"Código del otro dispositivo"**, or open the link from `fileswap id`. In the background only allowed codes are accepted, so either `fileswap allow <browser-code>` first, or run in the foreground once and answer the question:

```bash
fileswap start -f               # same as `fileswap listen`: events on screen, asks "Pair with pc (…)? [y/N]"
```

**Receive files:** while the node is online, files sent from a paired browser or node land in the **swap folder** — `~/FileSwap` (`/home/$USER/FileSwap`) unless you choose another:

```bash
fileswap init --dir /srv/inbox  # save it as this node's swap folder
fileswap start --dir /tmp/drop  # or just for this run
```

**Send files:**

```bash
fileswap tfr laptop ./backup.tar.gz    # also -tfr / send; progress bar, Ctrl+C cancels on both sides
fileswap tfr laptop ./Fotos ./a.pdf    # several files or folders → one ZIP made here while sending
```

A folder arrives as `Fotos.zip`; several paths as `fileswap-<date>-<time>.zip`. The ZIP is built on this machine as it is sent (nothing is staged on disk, the server never sees it); photos, videos and other compressed formats are stored as they are, the rest is deflated.

**Watch what happens** in the background: `tail -f ~/.local/state/fileswap/listen.log`.

**Pair a phone by QR:** `fileswap id --qr` draws the pairing link as a QR code in the terminal; scan it with the phone (the node must be online).

**Clipboard:** a place to leave texts (links, commands, notes) and files for your other devices — they see it while this node is online and take what they need. Items never replace each other and each one lasts 24 h (500 MB at most).

```bash
fileswap clip add -t "ssh seth@aery -p 2222"         # a text
fileswap clip add -t "Plano de la oficina" plano.pdf  # a text with a file
echo "https://…" | fileswap clip add                  # from a pipe
fileswap clip ls                                      # mine
fileswap clip ls pc                                   # pc's clipboard (the last one seen if it is offline)
fileswap clip get pc 3b66                             # prints the text, saves the file in ~/FileSwap
fileswap clip rm 3b66                                 # or: fileswap clip rm all
```

The clipboard is the folder `~/FileSwap/Clipboard`: files copied there by hand are shared too.

**Run it as a service** (starts at login, restarts if it crashes):

```bash
fileswap service install               # writes ~/.config/systemd/user/fileswap.service and starts it
sudo loginctl enable-linger $USER      # optional: also start at boot, without logging in
journalctl --user -u fileswap -f       # its log
fileswap service remove                # undo
```

The `.deb` and Arch packages also ship the unit as `/usr/lib/systemd/user/fileswap.service`, so `systemctl --user enable --now fileswap` works too. `fileswap status` and `fileswap stop` work the same with the service (`stop` keeps it stopped until the next login or `systemctl --user start fileswap`).

---

## Commands

Run `fileswap --help` for the list, or `fileswap help <command>` / `fileswap <command> --help` for details.

| Command | Alias | What it does |
|---|---|---|
| `init --name <name> [--server <url>] [--dir <path>] [--force]` | | Create this node's identity. Re-running keeps the code and updates name/server/swap folder; `--force` makes a new code |
| `id [--qr]` | | Show name, device code, server, swap folder and the browser pairing link; `--qr` also draws it as a QR code |
| `sync <code>` | `-sync` | Ask a device to pair with this node and wait (up to 2 min) for it to accept |
| `peers` | | List paired devices and which are online |
| `unpair <node>` | | Remove a paired device on both sides (only here if the server is unreachable) |
| `allow <code>` | | Accept pairing from this code without asking (also in the background) |
| `start [-f] [--dir <path>]` | | Go online **in the background** to receive files (into the swap folder) and pairing requests, and return. `-f` stays in the foreground with the events on screen |
| `listen [--dir <path>]` | | Same as `start -f` |
| `status` | | Name, online state, device code, server, swap folder, uptime and the paired-devices table. Exit code 3 when it is not running |
| `stop` | | Go offline (background or foreground) |
| `service install\|remove` | | Run as a systemd user service: starts at login (at boot with lingering), restarts on crashes |
| `clip add [-t <text>] [<file>]` | | Add an item to this node's clipboard (text from stdin with `-t -` or a pipe) |
| `clip ls [<node>]` | | This node's clipboard, or another device's (last one seen while it is offline) |
| `clip get <node> <id> [--dir <path>]` | | Take an item: print its text, save its file in the swap folder |
| `clip rm <id>\|all` | | Remove items from this node's clipboard |
| `tfr <node> <path>...` | `-tfr`, `send` | Send files or folders to a paired, online device (several → one ZIP) and wait for its SHA-256-checked confirmation |
| `version` | `-v`, `--version` | Print the version |
| `help [command]` | `-h`, `--help` | Show help |

`<node>` is a device code, a unique prefix of it, or the device's name. `<code>` is a full device code.

`--server` accepts `host:port`, `http(s)://host[:port]` or `ws(s)://host[:port]/ws`; it is stored as the WebSocket URL. Use `https://` / `wss://` when the server is behind TLS.

Messages are in Spanish when the locale is `es_*` (`LANG`, `LC_MESSAGES` or `LC_ALL`), otherwise English — like the web app follows the browser.

### How the running node and the other commands share the connection

The server allows **one connection per identity**: a second one with the same secret takes over and the first is disconnected. So while the node runs (`start`), `sync`, `peers`, `unpair`, `tfr`, `status` and `stop` don't connect themselves — they ask it through a local Unix socket (`$XDG_RUNTIME_DIR/fileswap/<code>.sock`, in a directory only your user can open). When it is not running, `sync`, `peers`, `unpair` and `tfr` connect just for the command and disconnect.

Pairing requests reaching the node:

| Requester | Foreground (`start -f` / `listen`) | Background (`start`) |
|---|---|---|
| In the allow list, or already paired | Accepted | Accepted |
| Anyone else | Asked on the terminal (2 min, default no) | Rejected — `fileswap allow <code>` and retry |

### Transfers

- One WebRTC connection per transfer, straight between the two devices (DTLS-encrypted in transit). STUN (`stun.l.google.com`) helps cross NATs; there is no TURN relay yet.
- Received files are written to a hidden `.fileswap-*.part` file and renamed only after the size and SHA-256 match, so a cut transfer never leaves a truncated file. Names are reduced to a plain file name and never overwrite: `report.pdf`, `report (1).pdf`, `backup (1).tar.gz`.
- The receiver checks free disk space as soon as the file is announced and refuses with `NO_SPACE`.
- Failures end quickly and on both sides: cancel (Ctrl+C / the web app's Cancel), connection lost (10 s), no progress (30 s, or 5 s once the other device is reported offline), no confirmation (60 s). Up to 4 incoming transfers at once.
- Several files or folders travel as one ZIP built while sending (ZIP64 past 4 GB). Received ZIPs are saved as they are, never extracted.

### Where things are stored

| Path | Content |
|---|---|
| `$FILESWAP_CONFIG`, else `$XDG_CONFIG_HOME/fileswap`, else `~/.config/fileswap` | `identity.json` — device secret (mode `0600`), name, server, paired devices, allow list |
| `~/FileSwap` (or `init --dir` / `start --dir`), mode `0700` | Received files |
| `~/FileSwap/Clipboard` (+ `.clipboard.json`) | This node's clipboard: attachments and the texts/times of the items |
| `~/.local/state/fileswap/clipcache/<code>.json` | The last clipboard seen of each device, for offline viewing |
| `$XDG_STATE_HOME/fileswap/listen.log`, else `~/.local/state/fileswap/listen.log` | Log of the node running in the background |
| `$XDG_RUNTIME_DIR/fileswap/<code>.sock`, else `/tmp/fileswap-<uid>/` | Control socket of the running node |

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
│   ├── node/           a running device: pairing, presence, unpair, transfers, control socket
│   ├── peer/           pion WebRTC connection per transfer: offer/answer, ICE, abort
│   └── transfer/       DataChannel framing: streaming sender/receiver, ack, SHA-256, safe names
├── packaging/          nfpm config for .deb and Arch packages
└── Makefile
```

`make test` runs the `node` integration tests against the real signaling server: it starts `fileswap/server` with `node` (after `npm ci` there), or uses `FILESWAP_TEST_SERVER=ws://host:port/ws`. Without either they are skipped. With `make docker-test`, start a server on the host and set `FILESWAP_TEST_SERVER`.

---

## Roadmap

- [x] **F0** — Protocol spec, web client fixes (bidirectional transfers, receipt ack, SHA-256), safe file names
- [x] **F1** — Signaling client: `sync`, `peers` with online status, pairing requests in `listen`, `unpair`, `listen -d` / `status` / `stop`
- [x] **F2** — Transfers with [pion](https://github.com/pion/webrtc): `tfr` and receiving in `listen` into the swap folder, streamed to disk, ack + SHA-256, cancel/abort, stall and connection-loss handling
- [x] `start` / `status` / `stop` with readable output (boxed device code, peers table)
- [x] **F3** — Run as a service: systemd user unit in the packages, `fileswap service install|remove`
- [x] `id --qr` (QR code in the terminal)
- [ ] **F4** — End-to-end tests in Arch and Debian containers (two nodes pair, transfer, compare hashes)
- [x] Several files and folders as one ZIP built while sending (CLI and web app)
- [x] Clipboard per node (texts and files, 24 h, 500 MB), browsable by paired devices, offline preview
- [ ] Later — E2E encryption shared with the web app, resume, TURN

---

## Security notes

- Received file names are reduced to a plain name (no directories, no leading dots, no control characters) and never overwrite existing files.
- Transfers are encrypted in transit by WebRTC (DTLS), but there is **no end-to-end encryption yet**: do not use FileSwap for patient or medical data (PHI).

## License

[GPL-3.0](../LICENSE)
