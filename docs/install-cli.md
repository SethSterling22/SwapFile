# Installing the FileSwap CLI

The `fileswap` command turns a Linux machine — a server, a laptop, a Raspberry Pi — into a FileSwap device: browsers and other nodes can send it files, take items from its clipboard, and it can send to them. It only makes outgoing connections; no port has to be opened.

Works on Debian/Ubuntu, Arch and any other Linux, on `amd64` (x86_64) and `arm64` (aarch64). You need a FileSwap server to connect to (the web app's `docker compose`, see the [main README](../README.md#deployment)).

## 1. Install

```bash
curl -fsSL https://raw.githubusercontent.com/SethSterling22/SwapFile/main/fileswap-cli/install.sh | sh
```

It installs the newest release with your package manager (`.deb` with apt, the Arch package with pacman, or the plain binary in `/usr/local/bin` elsewhere), checks the download against the release's `SHA256SUMS`, deletes it afterwards, and removes older FileSwap binaries left in your `PATH`. It asks for your `sudo` password.

Check it: `fileswap --version`.

## 2. Set it up (once)

```bash
fileswap init --name office-server --server 192.168.1.10:7341
```

- `--name` is how other devices see this one (it can be changed later).
- `--server` is your FileSwap server: `host:port`, or `https://fileswap.example.com` behind TLS.
- Received files go to `~/FileSwap`; choose another folder with `--dir /path`.

## 3. Go online

As a service — starts at every login, restarts if it crashes:

```bash
fileswap service install
sudo loginctl enable-linger $USER    # optional: also start at boot, without logging in
```

Or just for now, in the background: `fileswap start`. Check it any time with `fileswap status`; stop it with `fileswap stop`.

## 4. Pair with your devices

- **From a browser:** open the FileSwap web app and type this node's code (shown by `fileswap status`) under "Código del otro dispositivo". Or, on a phone, scan `fileswap id --qr`.
- **From this node:** copy the browser's code and run `fileswap sync <code>`, then accept in the browser.

While running in the background, a new device is accepted only if you allowed it first (`fileswap allow <code>`) or if you pair with `fileswap sync`.

## 5. Use it

```bash
fileswap tfr laptop ./backup.tar.gz         # send a file
fileswap tfr laptop ./Photos ./notes.pdf    # several files or folders → one ZIP
fileswap clip add -t "ssh me@server" ./key.pub    # leave a text and a file in the clipboard
fileswap clip ls laptop                     # what the laptop has in its clipboard
```

Files sent to this node land in `~/FileSwap`. `fileswap --help` lists every command.

## Update, other versions, uninstall

- **Update:** run the install command again (the service is restarted for you).
- **A specific version:** `curl -fsSL …/install.sh | sh -s -- --version v0.9.0`.
- **Without the script:** see the manual commands in the [CLI README](../fileswap-cli/README.md#install), or download from [Releases](https://github.com/SethSterling22/SwapFile/releases) (the latest stable is always at `…/releases/latest/download/fileswap_amd64.deb`).
- **Uninstall:** `fileswap service remove` (if you installed it), then `sudo apt remove fileswap`, `sudo pacman -R fileswap`, or `sudo rm /usr/local/bin/fileswap`. Your identity (`~/.config/fileswap`) and files (`~/FileSwap`) are kept.

> FileSwap is not end-to-end encrypted yet (transfers are encrypted in transit by WebRTC): don't use it for patient data, passwords or keys.
