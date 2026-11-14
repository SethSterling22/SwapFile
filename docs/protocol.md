# FileSwap protocol

This is the contract between every FileSwap node — the web client (`fileswap/client/index.html`) and the CLI (`fileswap-cli/`) — and the signaling server (`fileswap/server/index.js`). A node that follows it can pair and exchange files with any other node.

There are two layers:

1. **Signaling** — JSON over a WebSocket to the server. Identity, pairing, presence, and relaying WebRTC offers/answers/candidates. The server never sees file content.
2. **Transfer** — frames over a WebRTC DataChannel, directly between the two nodes.

Protocol version: **1**. Changes from version 0 (the original web client) are marked *v1*; v1 nodes interoperate with v0 ones as described in [Compatibility](#compatibility).

---

## 1. Identity

Each node generates a random **secret** once: 32 bytes, encoded as 64 lowercase hex characters. It is private and is stored only on the node (`localStorage` in the browser, `~/.config/fileswap/identity.json` in the CLI).

The public **device code** is derived from it:

```
h    = hex(SHA-256(utf8(secret)))          # hash of the 64-char hex string, not of the raw bytes
code = h[0:8] "-" h[8:12] "-" h[12:16] "-" h[16:20] "-" h[20:32]
```

Example: secret `0123456789abcdef` × 4 → code `a8ae6e6e-e929-abea-3afc-fc5258c8ccd6`.

The code is shown to users, put in QR codes and pairing links, and used to address devices. The server derives it from the secret on `register`, so knowing a code is not enough to impersonate a device.

The **name** is a free label (max 60 characters on the server). Names can repeat; codes cannot.

**Pairing link:** `http(s)://<server>/?pair=<code>&pname=<url-encoded name>` opens the web app and asks to pair with `<code>`.

---

## 2. Signaling (WebSocket)

Endpoint: `ws://<host>:<port>/ws` (or `wss://` behind TLS). Every message is a JSON object with a `type`. Limits: 16 KB per message, 120 messages per minute per socket (both configurable on the server). Idle sockets are closed after 30 minutes (`SESSION_TTL_MS`); any message, including `ping`, resets the timer.

### Client → server

| `type` | Fields | Meaning |
|---|---|---|
| `register` | `secret`, `name`, `pairedIds: [code]` | Sent first, and again when the name or pairs change. `pairedIds` is the node's own list of paired codes; the server trusts it as the node's consent |
| `pair-request` | `to` | Ask device `to` (must be online) to pair |
| `pair-accept` | `to` | Accept a pending request from `to`. Only valid after a real `pair-request` from `to` |
| `pair-reject` | `to` | Reject a pending request from `to` |
| `unpair` | `with` | Remove the pairing with `with` on both sides |
| `signal` | `to`, `data` | Relay `data` (see §3) to `to`. Only between mutually paired, online devices |
| `ping` | — | Keep-alive; answered with `pong` |

### Server → client

| `type` | Fields | Meaning |
|---|---|---|
| `registered` | `deviceId` | Registration accepted; `deviceId` is this node's code |
| `presence` | `deviceId`, `name`, `online` | A mutually paired device came online / went offline (also sent for each online peer right after `register`) |
| `pair-request` | `from: {id, name}` | Someone wants to pair with this node |
| `pair-accepted` | `from: {id, name}` | Our request was accepted. Add `from.id` to the local pairs and re-`register` |
| `pair-rejected` | `from: {id}` | Our request was rejected |
| `unpaired` | `by` | `by` removed the pairing |
| `signal` | `from`, `data` | Relayed from a paired device |
| `pong` | `ts` | Answer to `ping` |
| `error` | `code`, `message` | See below |

Error codes: `INVALID_SECRET`, `ALREADY_REGISTERED`, `NOT_REGISTERED`, `DEVICE_OFFLINE`, `NOT_PAIRED`, `NO_PENDING_REQUEST`, `TOO_MANY_REQUESTS`, `RATE_LIMITED`, `CAPACITY`, `MSG_TOO_LARGE`, `INVALID_JSON`, `UNKNOWN_TYPE`, `INTERNAL`. Clients should translate known codes and fall back to `message`.

### Close codes

| Code | Meaning | Client should |
|---|---|---|
| `4001` | Replaced: another connection registered with the same secret | Not reconnect (the other tab/process owns the identity) |
| `4002` | Idle session expired | Reconnect and `register` again |
| other | Network / server restart | Reconnect with backoff and `register` again |

### Pairing flow

```
B → server: pair-request {to: A}
server → A: pair-request {from: {id: B, name}}
A → server: pair-accept {to: B}                 (or pair-reject)
server → B: pair-accepted {from: {id: A, name}}
server → A, B: presence {online: true}
A and B: store the pair locally, then register again with the updated pairedIds
```

Pairing survives reconnects because each node sends its `pairedIds` on every `register`; the server only relays between devices that list **each other**.

---

## 3. WebRTC setup (`signal.data`)

Each transfer uses **its own `RTCPeerConnection`**, identified by a transfer id *(v1)*. The sender creates the connection and the DataChannel; the receiver answers.

| `data.type` | Fields | Direction |
|---|---|---|
| `offer` | `sdp`, `tid` | sender → receiver |
| `answer` | `sdp`, `tid` | receiver → sender |
| `candidate` | `candidate` (`RTCIceCandidateInit`), `tid` | both, trickled |

- `tid` *(v1)*: 16 random bytes as 32 hex characters, chosen by the sender. The receiver echoes it in its `answer` and candidates. A `tid` belongs to one peer: signals from another device with the same `tid` are ignored.
- Candidates that arrive before the remote description is set must be **queued** and added after `setRemoteDescription` (they used to be dropped).
- ICE servers: `stun:stun.l.google.com:19302`, `stun:stun1.l.google.com:19302`. No TURN yet.
- Receivers must ignore signals from devices they are not paired with, even though the server already filters them.
- A connection is closed when its transfer ends; it is never reused for another transfer.

---

## 4. Transfer (DataChannel)

The sender opens one DataChannel per transfer: label `fileswap-file`, `ordered: true` (reliable). Text frames are JSON; binary frames are file data.

```
sender → receiver   text    meta    {"v":1,"name":"report.pdf","size":48213,"type":"application/pdf"}
sender → receiver   binary  chunk × N   (≤ 16 KB each, in order)
sender → receiver   text    end     {"__end":true,"sha256":"<64 hex>"}
receiver → sender   text    ack     {"__ack":true,"ok":true,"received":48213,"sha256":"<64 hex>"}
```

| Frame | Fields |
|---|---|
| meta | `name` (string), `size` (bytes, integer ≥ 0), `type` (MIME, default `application/octet-stream`), `v` *(v1)* protocol version |
| chunk | Raw bytes, at most 16 384. Senders should apply backpressure (the web client waits while `bufferedAmount` > 256 KB) |
| end | `__end: true`; `sha256` *(v1, optional)* — lowercase hex SHA-256 of the whole file |
| ack *(v1)* | `__ack: true`; `ok`; `received` (bytes); `sha256` (optional, as computed by the receiver); `error` when `ok` is false: `SIZE_MISMATCH`, `HASH_MISMATCH`, `WRITE_FAILED` |

Rules:

- The receiver checks `received == meta.size`. If both sides have a hash (end's `sha256` and its own), they must match. The result goes in the ack.
- The sender treats the transfer as done **only after `ack.ok == true`**, then closes the connection. The CLI exits non-zero on `ok: false` or a missing ack.
- `sha256` is optional because browsers have no Web Crypto on plain HTTP (only on HTTPS or `localhost`), and the web client only hashes files up to 512 MB. The CLI always sends and checks it.
- **Receivers must never use `meta.name` as a path.** The CLI keeps only the last path component, removes control characters and leading dots, and never overwrites an existing file (`name (1).ext`). Browsers hand the name to the download attribute, which the browser sanitizes.

Timeouts in the web client: 30 s for the channel to open, 60 s for the ack after `end`.

---

## Compatibility

v0 is the web client before this document: no `tid`, no `v`, no ack, no hash, and one connection per peer reused across transfers.

- **v1 receiver, v0 sender:** signals without `tid` are handled as a single transfer per peer (`legacy:<peer>`); a new offer replaces the previous connection. The missing hash is skipped.
- **v1 sender, v0 receiver:** the v0 receiver ignores unknown fields but answers without `tid`, so the connection does not open and the v1 sender gives up after 30 s. Reloading the old tab (the server always serves the current client) fixes it.
- **No ack within 60 s** after `end`: the web sender assumes an old receiver and reports success. The CLI reports failure instead.

Unknown fields must be ignored, so future versions can add them.
