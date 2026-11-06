/**
 * DropLink — Signaling Server
 *
 * Responsibilities:
 *  1. Serve the client static files (client/)
 *  2. Relay WebRTC SDP / ICE messages between paired devices
 *  3. Broadcast real-time presence (online / offline) to paired devices
 *  4. Enforce: max message size, rate limiting, pairing verification
 *
 * The server NEVER sees file content. Max message size is MAX_MSG_BYTES,
 * which is enough for SDP/ICE but not for any real payload.
 */

'use strict';

const http    = require('http');
const path    = require('path');
const fs      = require('fs');
const crypto  = require('crypto');
const { WebSocketServer } = require('ws');

// ─── Config ────────────────────────────────────────────────────────────────
const PORT            = parseInt(process.env.PORT            ?? '3000',  10);
const SESSION_TTL_MS  = parseInt(process.env.SESSION_TTL_MS  ?? '1800000', 10); // 30 min
const MAX_MSG_BYTES   = parseInt(process.env.MAX_MSG_BYTES   ?? '16384',  10); // 16 KB
const MAX_SESSIONS    = parseInt(process.env.MAX_SESSIONS    ?? '50000',  10);
const RATE_LIMIT      = parseInt(process.env.RATE_LIMIT_PER_MIN ?? '120', 10);
const CORS_ORIGIN     = process.env.CORS_ORIGIN ?? '*';

const MAX_PENDING     = 20; // max unanswered pair-requests queued per device

// Close codes sent to the client
const CLOSE_REPLACED  = 4001; // same device opened a newer connection (e.g. another tab)
const CLOSE_EXPIRED   = 4002; // no messages for SESSION_TTL_MS

// ─── In-memory device registry ─────────────────────────────────────────────
// Map<deviceId, DeviceEntry>
// DeviceEntry = { ws, name, pairedIds: Set<string>, pendingFrom: Set<string>, _ttl }
//   pairedIds   — devices this device has consented to (from its own localStorage
//                 or a completed handshake). A link is only valid if it is mutual.
//   pendingFrom — devices that sent a pair-request we have not answered yet
const devices = new Map();

// ─── Device identity ───────────────────────────────────────────────────────
// The client keeps a random 256-bit secret in localStorage and sends it on
// register. The public deviceId (shown in the QR) is derived from it, so
// knowing a deviceId is not enough to impersonate that device.
const SECRET_RE = /^[0-9a-f]{64}$/;
const ID_RE     = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;

function idFromSecret(secret) {
  const h = crypto.createHash('sha256').update(secret).digest('hex');
  return `${h.slice(0, 8)}-${h.slice(8, 12)}-${h.slice(12, 16)}-${h.slice(16, 20)}-${h.slice(20, 32)}`;
}

function isMutualPair(idA, idB) {
  const a = devices.get(idA);
  const b = devices.get(idB);
  return !!a && !!b && a.pairedIds.has(idB) && b.pairedIds.has(idA);
}

// Online devices that are mutually paired with `id`
function* mutualPeers(id) {
  const entry = devices.get(id);
  if (!entry) return;
  for (const pairedId of entry.pairedIds) {
    if (isMutualPair(id, pairedId)) yield [pairedId, devices.get(pairedId)];
  }
}

// ─── Minimal HTTP server (serves client + handles health check) ─────────────
const CLIENT_DIR = path.join(__dirname, '..', 'client');

const MIME = {
  '.html': 'text/html; charset=utf-8',
  '.js':   'application/javascript; charset=utf-8',
  '.css':  'text/css; charset=utf-8',
  '.ico':  'image/x-icon',
  '.png':  'image/png',
  '.svg':  'image/svg+xml',
  '.json': 'application/json',
};

const server = http.createServer((req, res) => {
  // CORS headers
  res.setHeader('Access-Control-Allow-Origin', CORS_ORIGIN);
  res.setHeader('Access-Control-Allow-Methods', 'GET, OPTIONS');

  if (req.method === 'OPTIONS') { res.writeHead(204); res.end(); return; }

  // Health check
  if (req.url === '/health') {
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({ status: 'ok', devices: devices.size }));
    return;
  }

  // Static file serving
  let filePath = path.join(CLIENT_DIR, req.url === '/' ? 'index.html' : req.url.split('?')[0]);

  // Prevent directory traversal
  if (!filePath.startsWith(CLIENT_DIR)) {
    res.writeHead(403); res.end(); return;
  }

  fs.readFile(filePath, (err, data) => {
    if (err) {
      // SPA fallback — always serve index.html for unknown routes (handles ?pair= links)
      fs.readFile(path.join(CLIENT_DIR, 'index.html'), (err2, html) => {
        if (err2) { res.writeHead(404); res.end('Not found'); return; }
        res.writeHead(200, { 'Content-Type': MIME['.html'] });
        res.end(html);
      });
      return;
    }
    const ext = path.extname(filePath);
    res.writeHead(200, { 'Content-Type': MIME[ext] ?? 'application/octet-stream' });
    res.end(data);
  });
});

// ─── WebSocket server ───────────────────────────────────────────────────────
const wss = new WebSocketServer({ server, path: '/ws', maxPayload: MAX_MSG_BYTES });

wss.on('connection', (ws, req) => {
  let deviceId = null;
  let msgCount = 0;
  let resetAt  = Date.now() + 60_000;
  const ip = req.headers['x-forwarded-for']?.split(',')[0].trim() ?? req.socket.remoteAddress;

  // ── Helpers ──────────────────────────────────────────────────────────────
  function sendSelf(payload)  { send(ws, payload); }

  // Registry entry owned by THIS socket, or null if never registered,
  // expired, or taken over by a newer connection of the same device.
  function self() {
    const entry = deviceId ? devices.get(deviceId) : null;
    return entry && entry.ws === ws ? entry : null;
  }

  // Rate limit is per socket, so it also applies before register
  function rateOk() {
    const now = Date.now();
    if (now > resetAt) { msgCount = 0; resetAt = now + 60_000; }
    msgCount++;
    return msgCount <= RATE_LIMIT;
  }

  // (Re)start the idle timer. On expiry the socket is closed, which cleans up
  // the entry through the normal close path and lets the client reconnect.
  function touch(entry) {
    clearTimeout(entry._ttl);
    entry._ttl = setTimeout(() => ws.close(CLOSE_EXPIRED, 'Session expired'), SESSION_TTL_MS);
    entry._ttl.unref?.();
  }

  // ── Message handler ───────────────────────────────────────────────────────
  ws.on('message', (raw) => {
    // Size guard (ws maxPayload already handles binary, but double-check string)
    if (raw.length > MAX_MSG_BYTES) {
      sendSelf({ type: 'error', code: 'MSG_TOO_LARGE', message: 'Message exceeds size limit' });
      return;
    }

    let msg;
    try { msg = JSON.parse(raw); }
    catch { sendSelf({ type: 'error', code: 'INVALID_JSON', message: 'Invalid JSON' }); return; }

    if (!msg || typeof msg !== 'object') {
      sendSelf({ type: 'error', code: 'INVALID_JSON', message: 'Invalid JSON' });
      return;
    }

    if (!rateOk()) {
      sendSelf({ type: 'error', code: 'RATE_LIMITED', message: 'Too many messages' });
      return;
    }

    // A bug in one handler must never take the whole server down
    try {
      handleMessage(msg);
    } catch (err) {
      console.error(`[ws] ${ip} error handling ${msg.type}:`, err);
      sendSelf({ type: 'error', code: 'INTERNAL', message: 'Error interno del servidor' });
    }
  });

  function handleMessage(msg) {
    const me = self();
    if (me) touch(me);

    switch (msg.type) {

      // ── register ─────────────────────────────────────────────────────────
      // Client sends this on connect (and again when its name changes)
      case 'register': {
        if (typeof msg.secret !== 'string' || !SECRET_RE.test(msg.secret)) {
          sendSelf({ type: 'error', code: 'INVALID_SECRET', message: 'Identidad de dispositivo inválida' });
          return;
        }
        const id = idFromSecret(msg.secret);

        // A socket cannot switch identities once registered
        if (deviceId && deviceId !== id) {
          sendSelf({ type: 'error', code: 'ALREADY_REGISTERED', message: 'Conexión ya registrada' });
          return;
        }

        let entry = devices.get(id);
        if (entry && entry.ws !== ws) {
          // Same device reconnecting (or a second tab): the newest socket wins.
          // The old socket's close handler won't clean up, since it no longer owns the entry.
          const oldWs = entry.ws;
          entry.ws = ws;
          oldWs.close(CLOSE_REPLACED, 'Replaced by a newer connection');
        } else if (!entry) {
          if (devices.size >= MAX_SESSIONS) {
            sendSelf({ type: 'error', code: 'CAPACITY', message: 'Server at capacity' });
            ws.close(); return;
          }
          entry = {
            ws,
            name: 'Dispositivo',
            pairedIds: new Set(),
            pendingFrom: new Set(),
          };
          devices.set(id, entry);
        }
        deviceId = id;

        if (msg.name) entry.name = String(msg.name).slice(0, 60);

        // The client's localStorage is the source of truth for its own consent
        if (Array.isArray(msg.pairedIds)) {
          entry.pairedIds = new Set(msg.pairedIds.filter(p => typeof p === 'string' && ID_RE.test(p) && p !== id));
        }

        sendSelf({ type: 'registered', deviceId });

        // Exchange presence only with devices that also list us as paired
        for (const [pairedId, peer] of mutualPeers(deviceId)) {
          send(peer.ws, { type: 'presence', deviceId, name: entry.name, online: true });
          sendSelf({ type: 'presence', deviceId: pairedId, name: peer.name, online: true });
        }

        touch(entry);
        break;
      }

      // ── pair-request ─────────────────────────────────────────────────────
      // Device B wants to pair with Device A (A is online, B scanned A's QR)
      case 'pair-request': {
        if (!me) { sendSelf({ type: 'error', code: 'NOT_REGISTERED' }); return; }
        if (msg.to === deviceId) return;
        const target = devices.get(msg.to);
        if (!target) {
          sendSelf({ type: 'error', code: 'DEVICE_OFFLINE', message: 'El dispositivo no está conectado' });
          return;
        }
        if (target.pendingFrom.size >= MAX_PENDING && !target.pendingFrom.has(deviceId)) {
          sendSelf({ type: 'error', code: 'TOO_MANY_REQUESTS', message: 'El dispositivo tiene demasiadas solicitudes pendientes' });
          return;
        }
        target.pendingFrom.add(deviceId);
        send(target.ws, { type: 'pair-request', from: { id: deviceId, name: me.name } });
        break;
      }

      // ── pair-accept ───────────────────────────────────────────────────────
      // Only valid as an answer to a pair-request the other device actually sent
      case 'pair-accept': {
        if (!me) return;
        if (!me.pendingFrom.delete(msg.to)) {
          sendSelf({ type: 'error', code: 'NO_PENDING_REQUEST', message: 'No hay solicitud pendiente de ese dispositivo' });
          return;
        }
        const target = devices.get(msg.to);
        if (!target) return;

        // Record pairing on server side
        me.pairedIds.add(msg.to);
        target.pairedIds.add(deviceId);

        send(target.ws, { type: 'pair-accepted', from: { id: deviceId, name: me.name } });
        // Mutual presence notification
        send(target.ws, { type: 'presence', deviceId, name: me.name, online: true });
        sendSelf({ type: 'presence', deviceId: msg.to, name: target.name, online: true });
        break;
      }

      // ── pair-reject ───────────────────────────────────────────────────────
      case 'pair-reject': {
        if (!me) return;
        if (!me.pendingFrom.delete(msg.to)) return;
        const target = devices.get(msg.to);
        if (target) send(target.ws, { type: 'pair-rejected', from: { id: deviceId } });
        break;
      }

      // ── unpair ────────────────────────────────────────────────────────────
      case 'unpair': {
        if (!me) return;
        me.pairedIds.delete(msg.with);
        const peer = devices.get(msg.with);
        if (peer) {
          peer.pairedIds.delete(deviceId);
          send(peer.ws, { type: 'unpaired', by: deviceId });
        }
        break;
      }

      // ── signal ────────────────────────────────────────────────────────────
      // WebRTC SDP offer / answer / ICE candidate relay
      case 'signal': {
        if (!me) return;

        // Security: only relay between mutually paired devices
        if (!devices.has(msg.to)) {
          if (me.pairedIds.has(msg.to)) {
            sendSelf({ type: 'error', code: 'DEVICE_OFFLINE', message: 'El dispositivo receptor no está conectado' });
          } else {
            sendSelf({ type: 'error', code: 'NOT_PAIRED', message: 'Dispositivos no vinculados' });
          }
          return;
        }
        if (!isMutualPair(deviceId, msg.to)) {
          sendSelf({ type: 'error', code: 'NOT_PAIRED', message: 'Dispositivos no vinculados' });
          return;
        }

        send(devices.get(msg.to).ws, { type: 'signal', from: deviceId, data: msg.data });
        break;
      }

      // ── ping ──────────────────────────────────────────────────────────────
      case 'ping':
        sendSelf({ type: 'pong', ts: Date.now() });
        break;

      default:
        sendSelf({ type: 'error', code: 'UNKNOWN_TYPE', message: `Unknown message type: ${msg.type}` });
    }
  }

  // ── Disconnect ─────────────────────────────────────────────────────────
  // Only clean up if this socket still owns the entry (not replaced by a reconnect)
  ws.on('close', () => { if (self()) cleanup(deviceId); });
  ws.on('error', (err) => { console.error(`[ws] ${ip} error:`, err.message); });
});

function send(targetWs, payload) {
  if (targetWs.readyState === 1 /* OPEN */) {
    targetWs.send(JSON.stringify(payload));
  }
}

function cleanup(id) {
  const entry = devices.get(id);
  if (!entry) return;
  clearTimeout(entry._ttl);
  // Notify pairs
  for (const [, peer] of mutualPeers(id)) {
    send(peer.ws, { type: 'presence', deviceId: id, name: entry.name, online: false });
  }
  devices.delete(id);
}

// ─── Start ─────────────────────────────────────────────────────────────────
server.listen(PORT, () => {
  console.log(`\n✅  DropLink server listening on http://localhost:${PORT}`);
  console.log(`   WebSocket endpoint: ws://localhost:${PORT}/ws`);
  console.log(`   Health check:       http://localhost:${PORT}/health\n`);
});

process.on('SIGTERM', () => { server.close(); process.exit(0); });
process.on('SIGINT',  () => { server.close(); process.exit(0); });
