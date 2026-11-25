# FileSwap

> Peer-to-peer anonymous file transfer between devices — no accounts, no cloud storage, no file content ever touches the server.

FileSwap connects two browser sessions directly using WebRTC DataChannels. The server's only job is to broker the initial handshake (WebRTC signaling). Once the connection is established, the server is out of the picture entirely.

---

## How It Works

1. **Device A** opens the app and enters a device name. It gets a unique **device code** (a UUID) — the name is only a label and can repeat; the code is what identifies the device.
2. On **"Vincular nuevo dispositivo"**, Device A shows its code as a QR, as text, and as a shareable link.
3. **Device B** pairs by any of these:
   - scanning the QR or opening the link, or
   - typing (or pasting) Device A's code into **"Código del otro dispositivo"** — handy for two computers.
4. Device A sees a pairing request with B's name **and code**, and accepts it. Both devices are now linked.
5. The pairing is saved to `localStorage` — next time both devices come online, they recognize each other automatically.
6. When a paired device shows a **green indicator** (live session), click it to send a file.
7. The file travels directly from browser to browser. The receiver gets a download button.

If a paired device is offline (grey indicator), sending is blocked — there is nowhere for the file to go.

Each device also has a **clipboard**: texts (links, commands, notes) and files that its paired devices can see, copy and download while it is online — items last 24 h, up to 500 MB, and the last list seen stays visible (greyed out) while the device is offline.

There is also a **command-line node** for Linux servers — see [FileSwap CLI](#fileswap-cli-for-servers) below. Install it with:

```bash
curl -fsSL https://raw.githubusercontent.com/SethSterling22/SwapFile/main/fileswap-cli/install.sh | sh
```

and follow the [CLI install guide](docs/install-cli.md).

The interface is available in **Spanish and English**. It follows the browser's language (any `es-*` → Spanish, otherwise English) until the user picks one with the **ES/EN** button in the header; the choice is saved per browser. Light/dark mode works the same way with the ☾/☀ button.

---

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                        FileSwap Server                          │
│                                                                 │
│  HTTP  ──▶  Serves client/index.html (SPA)                      │
│  WS    ──▶  Signaling relay (SDP / ICE only, max 16 KB/msg)     │
│             In-memory device registry  •  Presence broadcasts   │
│             Rate limiting  •  Pairing verification              │
│                                                                 │
│  ✗  Never sees file content                                     │
│  ✗  No database  •  No accounts  •  No logs                     │
└───────────────────────┬─────────────────────────────────────────┘
                        │  WebSocket (signaling only)
          ┌─────────────┴──────────────┐
          │                            │
   ┌──────▼──────┐              ┌──────▼──────┐
   │  Device A   │              │  Device B   │
   │  (Browser)  │◀────────────▶│  (Browser)  │
   └─────────────┘   WebRTC     └─────────────┘
                   DataChannel
                (direct P2P, no relay)
```

### Pairing Handshake

```
Device A                  Server                  Device B
   │                        │                        │
   │──── register ─────────▶│                        │
   │◀─── registered ────────│                        │
   │                        │◀────── register ───────│
   │                        │─────── registered ────▶│
   │                        │                        │
   │  (A shows QR; B scans and opens the pair URL)   │
   │                        │                        │
   │                        │◀── pair-request(A.id) ─│
   │◀─── pair-request ──────│                        │
   │                        │                        │
   │──── pair-accept ──────▶│                        │
   │                        │──── pair-accepted ────▶│
   │◀─── presence(B, online)│                        │
   │                        │── presence(A, online) ▶│
```

### WebRTC File Transfer

Each transfer opens its own connection; the full message format is in [docs/protocol.md](docs/protocol.md).

```
Sender                    Server                  Receiver
   │                        │                        │
   │── signal(SDP offer) ──▶│── relay ──────────────▶│
   │                        │◀── signal(SDP answer) ─│
   │◀─────────────── relay ─│                        │
   │── signal(ICE ×N) ─────▶│── relay ──────────────▶│
   │◀─────────────── relay ─│◀── signal(ICE ×N) ─────│
   │                        │                        │
   │◀══════════════ P2P DataChannel open ════════════▶│
   │                        │                        │
   │═══ [metadata frame] ══════════════════════════▶ │
   │═══ [16 KB chunk × N] ════════════════════════▶  │
   │═══ [end frame] ══════════════════════════════▶  │
   │◀══ [ack: ok, size, sha256] ═════════════════════ │
   │                        │              [download link rendered]
```

---

## Project Structure

```
docs/
├── install-cli.md          # Step-by-step CLI install guide (also linked from every release)
└── protocol.md             # Signaling + transfer protocol shared by web and CLI
fileswap-cli/               # Command-line node for Linux servers (Go) — see its README
fileswap/
├── client/
│   └── index.html          # Full SPA — HTML, CSS, and JS in a single file
│                           # No build step, no framework dependencies
├── server/
│   ├── index.js            # Signaling server (Node.js + ws + express-like HTTP)
│   ├── package.json
│   ├── .env.example
│   └── Dockerfile
├── docker-compose.yml
└── README.md
```

### Client modules (all inside `index.html`)

| Module | Responsibility |
|---|---|
| **State / Persistence** | Generates and restores `deviceSecret`, `deviceId`, `deviceName`, and `pairs[]` from `localStorage` |
| **SignalingClient** | WebSocket wrapper — register, pair, relay SDP/ICE, receive presence events |
| **PeerConnection** | `RTCPeerConnection` lifecycle — offer, answer, ICE exchange, connection state |
| **FileStreamer** | Reads file in 16 KB chunks via `FileReader`, sends over `RTCDataChannel` with backpressure |
| **FileReceiver** | Accumulates chunks, assembles `Blob`, creates `URL.createObjectURL` download link |
| **UI** | Screen management, device cards, QR rendering, progress bar, toasts — plain DOM, no framework |

### Server responsibilities

| Concern | Implementation |
|---|---|
| Static file serving | Minimal built-in HTTP, SPA fallback for `?pair=` links |
| Device registry | `Map<deviceId, {ws, name, pairedIds, pendingFrom}>` — in-memory, no persistence. `deviceId` is derived from a client secret (SHA-256) |
| Presence | On connect: notifies paired devices online. On disconnect: broadcasts offline |
| Pairing | Relays `pair-request` / `pair-accept` / `pair-reject` between devices |
| Signal relay | Forwards SDP and ICE messages only between mutually paired devices |
| Security | Max message size 16 KB · Rate limit 120 msg/min per socket · Max 50 000 sessions · Handler errors never crash the process |

---

## Running Locally

**Requirements:** Node.js 18+

```bash
cd fileswap/server
npm install
npm start
```

Open `http://localhost:3000` on your computer and `http://<YOUR-LOCAL-IP>:3000` on your phone (same Wi-Fi network).

Find your local IP:
- **macOS / Linux:** `ifconfig` or `ip a`
- **Windows:** `ipconfig`

---

### Tests

```bash
cd fileswap/server
npm test
```

Integration tests (`node:test`) spawn the real server and exercise registration, pairing, signal relay, reconnection and idle expiry over WebSocket.

---

## FileSwap CLI (for servers)

[`fileswap-cli/`](fileswap-cli/README.md) is a single static binary that joins the same network as the web app, so files can go **from a browser straight to a Linux server** (or back, or between servers) from the terminal:

```bash
fileswap init --name srv-01 --server 192.168.1.10:7341
fileswap start                           # online in the background, no open ports
fileswap sync <browser-code>             # pair; accept it in the browser
fileswap tfr laptop ./backup.tar.gz      # send to a paired device; received files land in ~/FileSwap
```

Install or update it with one command (Debian/Ubuntu, Arch or any Linux, amd64/arm64):

```bash
curl -fsSL https://raw.githubusercontent.com/SethSterling22/SwapFile/main/fileswap-cli/install.sh | sh
```

`.deb`, Arch packages and plain binaries are also on [GitHub Releases](https://github.com/SethSterling22/SwapFile/releases) (every release starts with these install commands) — see the [install guide](docs/install-cli.md) and the CLI README's [Install](fileswap-cli/README.md#install) for manual commands. It is in early development; see its [roadmap](fileswap-cli/README.md#roadmap). The web app's transfers can be cancelled from either side, and fail fast (with a reason) when the other device disconnects, stops making progress or has no space.

---

## Deployment

The server is stateless — everything lives in memory. Any Node.js host works.

### Docker

```bash
# Using docker compose (from fileswap/) — serves on http://localhost:7341
docker compose up --build -d

# Use another host port
FILESWAP_PORT=8080 docker compose up --build -d

# Or directly
docker build -f server/Dockerfile -t fileswap .
docker run -p 7341:3000 fileswap
```

### Railway

```bash
railway init
railway up
```

### Render

1. New service → **Web Service**
2. Root directory: `server`
3. Build command: `npm install`
4. Start command: `node index.js`

### Fly.io

```bash
fly launch --dockerfile server/Dockerfile
fly deploy
```

---

## About, Terms and Privacy

The footer links to **Terms of use**, **Privacy** and **About**, each opening in a modal (also reachable directly at `/#terms`, `/#privacy`, `/#about`).

Edit the `ABOUT` object near the top of the script in `fileswap/client/index.html` with your own details:

| Field | Used for |
|---|---|
| `name`, `role`, `bio` | About section (`role`/`bio` have `es` and `en` versions) |
| `email`, `website` | Links in About, and the contact shown in Terms/Privacy (falls back to `source`) |
| `source` | "Source code" link |
| `paypal` | Your PayPal.me link — the donation box and its QR code are generated from it. Leave empty to hide the box |

The legal texts live in the `LEGAL` object in the same file and describe what the code actually does (P2P transfer, in-memory signaling, Google STUN). If you change that behavior, update them and `LEGAL_UPDATED`. They are a starting point, not legal advice.

---

## Environment Variables

Copy `server/.env.example` → `server/.env`.

| Variable | Default | Description |
|---|---|---|
| `PORT` | `3000` | HTTP server port |
| `SESSION_TTL_MS` | `1800000` | Idle timeout in ms (30 min). Refreshed on every message; on expiry the socket is closed and the client reconnects |
| `MAX_MSG_BYTES` | `16384` | Max WebSocket message size — signaling only (16 KB) |
| `MAX_SESSIONS` | `50000` | Max concurrent devices in memory |
| `RATE_LIMIT_PER_MIN` | `120` | Max WebSocket messages per minute per device |
| `CORS_ORIGIN` | *(empty)* | CORS origin for the HTTP endpoints; empty sends no CORS header (the web app is served from the same origin and doesn't need one) |
| `ALLOWED_ORIGINS` | *(empty)* | Extra comma-separated origins whose pages may open the WebSocket. The server's own host is always allowed, and so are clients without an `Origin` (the CLI); any other website is refused with `403` |
| `TRUST_PROXY` | *(off)* | Set to `1` behind Caddy/nginx: the client IP is taken from the last `X-Forwarded-For` hop, and HSTS is sent on HTTPS. Leave it off if the port is reachable directly, or anyone could fake their IP |
| `MAX_CONN_PER_IP` | `20` | Max simultaneous WebSocket connections from one IP (`429` beyond it) |

Every page is served with a Content-Security-Policy (inline scripts allowed only by their SHA-256), `X-Frame-Options: DENY`, `nosniff`, `Referrer-Policy: no-referrer` and `Permissions-Policy`. No third-party script is loaded: the QR library is self-hosted in [`client/vendor/`](fileswap/client/vendor/README.md) with Subresource Integrity.

---

## Security Model

| Threat | Mitigation |
|---|---|
| Server reads file content | Impossible — max message size is 16 KB (SDP/ICE only). Any larger message is rejected. |
| Unauthorized signal relay | Server only forwards signals between devices that list each other as paired. `pair-accept` is only honored as the answer to a real `pair-request`. The receiver also ignores signals from unknown devices. |
| QR link interception | The attacker needs the target device to be online and accept the pairing request. |
| Impersonation / session hijack | Each device keeps a random 256-bit `deviceSecret` in `localStorage`; the public `deviceId` (shown in the QR) is `SHA-256(secret)`. Knowing a `deviceId` is not enough to register as it. A newer connection with the same secret replaces the old one (close code `4001`). |
| Abuse of a public server | Other websites cannot open the WebSocket (Origin check), connections per IP and messages per device are limited, and the page can't be framed (clickjacking). |
| Server-side data leakage | No database, no logs of content or file metadata. Server only knows device names and pairing relationships. |

> **Note:** In-transit encryption is provided by WebRTC's mandatory DTLS layer. End-to-end application-layer encryption (ECDH + AES-GCM via Web Crypto API) is planned for v1.1.

---

## Roadmap

- [ ] **v1.1** — E2E encryption (ECDH X25519 key exchange + AES-GCM 256-bit per-chunk)
- [x] **v1.1** — Several files and folders (pick or drag & drop), sent as one ZIP built on the sending device
- [x] **v1.1** — Clipboard per device: texts and files (24 h, 500 MB) that paired devices can see, copy and download; last seen list shown greyed out while offline
- [x] **v1.1** — Bidirectional transfer (one WebRTC connection per transfer, receipt ack + SHA-256)
- [ ] **v1.2** — CLI node for Linux servers ([fileswap-cli](fileswap-cli/README.md))
- [ ] **v1.2** — Image preview before download on receiver side
- [x] **v1.1** — Large files in the browser: written to disk (Origin Private File System) while receiving instead of kept in memory; browsers without it refuse files over 1.5 GB (512 MB on phones) up front
- [x] **v1.1** — Hardened public server (Origin check, per-IP limit, CSP and security headers, self-hosted QR library) and a "new version, reload" notice
- [ ] **v2.0** — One-to-many (broadcast to multiple paired devices simultaneously)
- [ ] **v2.0** — Encrypted inode-based virtual drive (compressed, ephemeral)
- [ ] **v2.0** — TURN server support for symmetric NAT (corporate networks)
- [ ] **v2.0** — PWA with Service Worker for offline client availability

---

## License

[GPL-3.0](LICENSE)
