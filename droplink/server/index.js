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
const { WebSocketServer } = require('ws');
const { v4: uuidv4 }      = require('uuid');

// ─── Config ────────────────────────────────────────────────────────────────
const PORT            = parseInt(process.env.PORT            ?? '3000',  10);
const SESSION_TTL_MS  = parseInt(process.env.SESSION_TTL_MS  ?? '1800000', 10); // 30 min
const MAX_MSG_BYTES   = parseInt(process.env.MAX_MSG_BYTES   ?? '16384',  10); // 16 KB
const MAX_SESSIONS    = parseInt(process.env.MAX_SESSIONS    ?? '50000',  10);
const RATE_LIMIT      = parseInt(process.env.RATE_LIMIT_PER_MIN ?? '120', 10);
const CORS_ORIGIN     = process.env.CORS_ORIGIN ?? '*';

// ─── In-memory device registry ─────────────────────────────────────────────
// Map<deviceId, DeviceEntry>
// DeviceEntry = { ws, name, pairedIds: Set<string>, msgCount: number, resetAt: number }
const devices = new Map();

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
  const ip = req.headers['x-forwarded-for']?.split(',')[0].trim() ?? req.socket.remoteAddress;

  // ── Helpers ──────────────────────────────────────────────────────────────
  function send(targetWs, payload) {
    if (targetWs.readyState === 1 /* OPEN */) {
      targetWs.send(JSON.stringify(payload));
    }
  }

  function sendSelf(payload)  { send(ws, payload); }

  function notifyPresence(targetDeviceId, online) {
    const entry = devices.get(targetDeviceId);
    if (!entry) return;
    // Tell all of targetDevice's pairs about its new presence state
    for (const pairedId of entry.pairedIds) {
      const peer = devices.get(pairedId);
      if (peer) send(peer.ws, { type: 'presence', deviceId: targetDeviceId, name: entry.name, online });
    }
  }

  function rateOk() {
    if (!deviceId) return true;
    const entry = devices.get(deviceId);
    if (!entry) return true;
    const now = Date.now();
    if (now > entry.resetAt) { entry.msgCount = 0; entry.resetAt = now + 60_000; }
    entry.msgCount++;
    return entry.msgCount <= RATE_LIMIT;
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

    if (!rateOk()) {
      sendSelf({ type: 'error', code: 'RATE_LIMITED', message: 'Too many messages' });
      return;
    }

    switch (msg.type) {

      // ── register ─────────────────────────────────────────────────────────
      // Client sends this on connect (or reconnect with existing deviceId)
      case 'register': {
        const requestedId = typeof msg.deviceId === 'string' && msg.deviceId.length === 36
          ? msg.deviceId : null;

        // Reuse existing ID if provided and not already taken by another socket
        const existing = requestedId ? devices.get(requestedId) : null;
        if (existing && existing.ws !== ws) {
          // Previous session disconnected but entry wasn't cleaned yet — take it over
          existing.ws = ws;
          deviceId = requestedId;
          devices.set(deviceId, existing);
        } else if (existing && existing.ws === ws) {
          // Already registered on this socket
          deviceId = requestedId;
        } else {
          // New device
          if (devices.size >= MAX_SESSIONS) {
            sendSelf({ type: 'error', code: 'CAPACITY', message: 'Server at capacity' });
            ws.close(); return;
          }
          deviceId = requestedId ?? uuidv4();
          const pairedIds = Array.isArray(msg.pairedIds)
            ? new Set(msg.pairedIds.filter(id => typeof id === 'string'))
            : new Set();

          devices.set(deviceId, {
            ws,
            name: String(msg.name ?? 'Dispositivo').slice(0, 60),
            pairedIds,
            msgCount: 0,
            resetAt: Date.now() + 60_000,
          });
        }

        // Update name if changed
        const entry = devices.get(deviceId);
        if (msg.name) entry.name = String(msg.name).slice(0, 60);

        // Merge any newly-known pairedIds from client localStorage
        if (Array.isArray(msg.pairedIds)) {
          for (const id of msg.pairedIds) {
            if (typeof id === 'string') entry.pairedIds.add(id);
          }
        }

        sendSelf({ type: 'registered', deviceId });

        // Inform online pairs that this device is back
        for (const pairedId of entry.pairedIds) {
          const peer = devices.get(pairedId);
          if (peer) {
            send(peer.ws, { type: 'presence', deviceId, name: entry.name, online: true });
            // Also tell this device which pairs are currently online
            sendSelf({ type: 'presence', deviceId: pairedId, name: peer.name, online: true });
          }
        }

        // Schedule TTL cleanup (refreshed on each message via rateOk)
        if (!entry._ttl) {
          entry._ttl = setTimeout(() => {
            if (devices.get(deviceId) === entry) cleanup(deviceId);
          }, SESSION_TTL_MS);
          entry._ttl.unref?.();
        }
        break;
      }

      // ── pair-request ─────────────────────────────────────────────────────
      // Device A wants to pair with Device B (B is online, A scanned B's QR)
      case 'pair-request': {
        if (!deviceId) { sendSelf({ type: 'error', code: 'NOT_REGISTERED' }); return; }
        const target = devices.get(msg.to);
        if (!target) {
          sendSelf({ type: 'error', code: 'DEVICE_OFFLINE', message: 'El dispositivo no está conectado' });
          return;
        }
        const me = devices.get(deviceId);
        send(target.ws, { type: 'pair-request', from: { id: deviceId, name: me.name } });
        break;
      }

      // ── pair-accept ───────────────────────────────────────────────────────
      case 'pair-accept': {
        if (!deviceId) return;
        const target = devices.get(msg.to);
        const me     = devices.get(deviceId);
        if (!target || !me) return;

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
        if (!deviceId) return;
        const target = devices.get(msg.to);
        if (target) send(target.ws, { type: 'pair-rejected', from: { id: deviceId } });
        break;
      }

      // ── unpair ────────────────────────────────────────────────────────────
      case 'unpair': {
        if (!deviceId) return;
        const me = devices.get(deviceId);
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
        if (!deviceId) return;
        const me     = devices.get(deviceId);
        const target = devices.get(msg.to);

        if (!target) {
          sendSelf({ type: 'error', code: 'DEVICE_OFFLINE', message: 'El dispositivo receptor no está conectado' });
          return;
        }

        // Security: only relay between paired devices
        if (!me.pairedIds.has(msg.to) && !target.pairedIds.has(deviceId)) {
          sendSelf({ type: 'error', code: 'NOT_PAIRED', message: 'Dispositivos no vinculados' });
          return;
        }

        send(target.ws, { type: 'signal', from: deviceId, data: msg.data });
        break;
      }

      // ── ping ──────────────────────────────────────────────────────────────
      case 'ping':
        sendSelf({ type: 'pong', ts: Date.now() });
        break;

      default:
        sendSelf({ type: 'error', code: 'UNKNOWN_TYPE', message: `Unknown message type: ${msg.type}` });
    }
  });

  // ── Disconnect ─────────────────────────────────────────────────────────
  ws.on('close', () => { if (deviceId) cleanup(deviceId); });
  ws.on('error', (err) => { console.error(`[ws] ${ip} error:`, err.message); });
});

function cleanup(id) {
  const entry = devices.get(id);
  if (!entry) return;
  clearTimeout(entry._ttl);
  // Notify pairs
  for (const pairedId of entry.pairedIds) {
    const peer = devices.get(pairedId);
    if (peer && peer.ws.readyState === 1) {
      peer.ws.send(JSON.stringify({ type: 'presence', deviceId: id, name: entry.name, online: false }));
    }
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
