# DropLink

> Peer-to-peer anonymous file transfer between devices — no accounts, no cloud storage, no file content ever touches the server.

DropLink connects two browser sessions directly using WebRTC DataChannels. The server's only job is to broker the initial handshake (WebRTC signaling). Once the connection is established, the server is out of the picture entirely.

---

## How It Works

1. **Device A** opens the app, enters a device name, and clicks **"Link new device"** — a QR code appears.
2. **Device B** scans the QR (or opens the shared link) from any browser.
3. Device A sees a pairing request and accepts it. Both devices are now linked.
4. The pairing is saved to `localStorage` — next time both devices come online, they recognize each other automatically.
5. When a paired device shows a **green indicator** (live session), click it to send a file.
6. The file travels directly from browser to browser. The receiver gets a download button.

If a paired device is offline (grey indicator), sending is blocked — there is nowhere for the file to go.

---

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                        DropLink Server                          │
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
   │                        │              [download link rendered]
```

---

## Project Structure

```
droplink/
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
| **State / Persistence** | Generates and restores `deviceId`, `deviceName`, and `pairs[]` from `localStorage` |
| **SignalingClient** | WebSocket wrapper — register, pair, relay SDP/ICE, receive presence events |
| **PeerConnection** | `RTCPeerConnection` lifecycle — offer, answer, ICE exchange, connection state |
| **FileStreamer** | Reads file in 16 KB chunks via `FileReader`, sends over `RTCDataChannel` with backpressure |
| **FileReceiver** | Accumulates chunks, assembles `Blob`, creates `URL.createObjectURL` download link |
| **UI** | Screen management, device cards, QR rendering, progress bar, toasts — plain DOM, no framework |

### Server responsibilities

| Concern | Implementation |
|---|---|
| Static file serving | Minimal built-in HTTP, SPA fallback for `?pair=` links |
| Device registry | `Map<deviceId, {ws, name, pairedIds, rateLimit}>` — in-memory, no persistence |
| Presence | On connect: notifies paired devices online. On disconnect: broadcasts offline |
| Pairing | Relays `pair-request` / `pair-accept` / `pair-reject` between devices |
| Signal relay | Forwards SDP and ICE messages only between verified paired devices |
| Security | Max message size 16 KB · Rate limit 120 msg/min · Max 50 000 sessions |

---

## Running Locally

**Requirements:** Node.js 18+

```bash
cd droplink/server
npm install
npm start
```

Open `http://localhost:3000` on your computer and `http://<YOUR-LOCAL-IP>:3000` on your phone (same Wi-Fi network).

Find your local IP:
- **macOS / Linux:** `ifconfig` or `ip a`
- **Windows:** `ipconfig`

---

## Deployment

The server is stateless — everything lives in memory. Any Node.js host works.

### Docker

```bash
# Using docker-compose
docker-compose up --build

# Or directly
docker build -f server/Dockerfile -t droplink .
docker run -p 3000:3000 droplink
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

## Environment Variables

Copy `server/.env.example` → `server/.env`.

| Variable | Default | Description |
|---|---|---|
| `PORT` | `3000` | HTTP server port |
| `SESSION_TTL_MS` | `1800000` | Session TTL in ms (30 min) |
| `MAX_MSG_BYTES` | `16384` | Max WebSocket message size — signaling only (16 KB) |
| `MAX_SESSIONS` | `50000` | Max concurrent devices in memory |
| `RATE_LIMIT_PER_MIN` | `120` | Max WebSocket messages per minute per device |
| `CORS_ORIGIN` | `*` | Allowed CORS origin (set to your domain in production) |

---

## Security Model

| Threat | Mitigation |
|---|---|
| Server reads file content | Impossible — max message size is 16 KB (SDP/ICE only). Any larger message is rejected. |
| Unauthorized signal relay | Server only forwards signals between devices that have completed a mutual pair handshake. |
| QR link interception | The attacker needs the target device to be online and accept the pairing request. |
| Replay / session hijack | `deviceId` is a UUID stored client-side. Sessions expire after `SESSION_TTL_MS`. |
| Server-side data leakage | No database, no logs of content or file metadata. Server only knows device names and pairing relationships. |

> **Note:** In-transit encryption is provided by WebRTC's mandatory DTLS layer. End-to-end application-layer encryption (ECDH + AES-GCM via Web Crypto API) is planned for v1.1.

---

## Roadmap

- [ ] **v1.1** — E2E encryption (ECDH X25519 key exchange + AES-GCM 256-bit per-chunk)
- [ ] **v1.1** — Multi-file drag-and-drop transfer
- [ ] **v1.1** — Bidirectional transfer (desktop → mobile)
- [ ] **v1.2** — Image preview before download on receiver side
- [ ] **v2.0** — File System Access API streaming for files > 2 GB
- [ ] **v2.0** — One-to-many (broadcast to multiple paired devices simultaneously)
- [ ] **v2.0** — Encrypted inode-based virtual drive (compressed, ephemeral)
- [ ] **v2.0** — TURN server support for symmetric NAT (corporate networks)
- [ ] **v2.0** — PWA with Service Worker for offline client availability

---

## License

MIT
