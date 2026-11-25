'use strict';

// Integration tests: spawn the real server on a free port and talk to it over WebSocket.
// Run with: npm test

const { test, before, after } = require('node:test');
const assert = require('node:assert/strict');
const { spawn } = require('node:child_process');
const crypto = require('node:crypto');
const path = require('node:path');
const WebSocket = require('ws');

const SERVER = path.join(__dirname, '..', 'index.js');

function startServer(env = {}) {
  return new Promise((resolve, reject) => {
    const port = 20000 + Math.floor(Math.random() * 20000);
    const proc = spawn(process.execPath, [SERVER], {
      env: { ...process.env, PORT: String(port), ...env },
      stdio: ['ignore', 'pipe', 'pipe'],
    });
    proc.stderrText = '';
    proc.stderr.on('data', d => { proc.stderrText += d; });
    proc.stdout.on('data', d => { if (String(d).includes('listening')) resolve({ proc, port }); });
    proc.on('exit', code => reject(new Error(`server exited (${code}): ${proc.stderrText}`)));
  });
}

const secret = () => crypto.randomBytes(32).toString('hex');
const wait = ms => new Promise(r => setTimeout(r, ms));

// Connects, registers and resolves once the server confirms with `registered`
function device(port, { secret: s = secret(), name = 'dev', pairedIds = [], kind } = {}) {
  return new Promise((resolve, reject) => {
    const ws = new WebSocket(`ws://localhost:${port}/ws`);
    ws.msgs = [];
    ws.secret = s;
    ws.on('message', m => {
      const msg = JSON.parse(m);
      ws.msgs.push(msg);
      if (msg.type === 'registered') { ws.id = msg.deviceId; resolve(ws); }
      if (msg.type === 'error' && !ws.id) reject(new Error(msg.code));
    });
    ws.on('open', () => ws.json({ type: 'register', secret: s, name, pairedIds, kind }));
    ws.on('error', reject);
    ws.json = obj => ws.send(JSON.stringify(obj));
  });
}

const got = (ws, pred) => ws.msgs.some(pred);

async function pair(a, b) {
  b.json({ type: 'pair-request', to: a.id });
  await wait(50);
  a.json({ type: 'pair-accept', to: b.id });
  await wait(50);
}

let srv;
before(async () => { srv = await startServer(); });
after(() => srv.proc.kill());

test('deviceId is derived from the secret and stable across reconnects', async () => {
  const s = secret();
  const a1 = await device(srv.port, { secret: s });
  const a2 = await device(srv.port, { secret: s });
  const other = await device(srv.port);
  assert.equal(a1.id, a2.id);
  assert.notEqual(a1.id, other.id);
  assert.match(a1.id, /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/);
  [a1, a2, other].forEach(w => w.close());
});

test('the device kind travels with pairing and presence; unknown kinds are dropped', async () => {
  const a = await device(srv.port, { name: 'A', kind: 'server' });
  const b = await device(srv.port, { name: 'B', kind: 'rocket' });
  await pair(a, b);
  assert.ok(got(a, m => m.type === 'pair-request' && m.from.id === b.id && m.from.kind === ''));
  assert.ok(got(b, m => m.type === 'pair-accepted' && m.from.kind === 'server'));
  assert.ok(got(b, m => m.type === 'presence' && m.deviceId === a.id && m.kind === 'server'));
  a.close(); b.close();
});

test('register without a valid secret is rejected', async () => {
  await assert.rejects(device(srv.port, { secret: 'not-a-secret' }), /INVALID_SECRET/);
});

test('knowing a deviceId does not let you register as that device', async () => {
  const a = await device(srv.port);
  const ws = new WebSocket(`ws://localhost:${srv.port}/ws`);
  const reply = await new Promise(r => {
    ws.on('open', () => ws.send(JSON.stringify({ type: 'register', deviceId: a.id, name: 'evil' })));
    ws.on('message', m => r(JSON.parse(m)));
  });
  assert.equal(reply.code, 'INVALID_SECRET');
  ws.close(); a.close();
});

test('pair request + accept lets devices relay signals both ways', async () => {
  const a = await device(srv.port, { name: 'A' });
  const b = await device(srv.port, { name: 'B' });
  await pair(a, b);
  assert.ok(got(a, m => m.type === 'pair-request' && m.from.id === b.id));
  assert.ok(got(b, m => m.type === 'pair-accepted' && m.from.id === a.id));

  b.json({ type: 'signal', to: a.id, data: { type: 'offer', sdp: 'x' } });
  a.json({ type: 'signal', to: b.id, data: { type: 'answer', sdp: 'y' } });
  await wait(50);
  assert.ok(got(a, m => m.type === 'signal' && m.from === b.id));
  assert.ok(got(b, m => m.type === 'signal' && m.from === a.id));
  a.close(); b.close();
});

test('claiming a pairing in register is not enough to relay signals or see presence', async () => {
  const a = await device(srv.port, { name: 'A' });
  const x = await device(srv.port, { name: 'X', pairedIds: [a.id] });
  x.json({ type: 'signal', to: a.id, data: { type: 'offer', sdp: 'evil' } });
  await wait(50);
  assert.ok(!got(a, m => m.type === 'signal'));
  assert.ok(!got(a, m => m.type === 'presence' && m.deviceId === x.id));
  assert.ok(!got(x, m => m.type === 'presence' && m.deviceId === a.id));
  assert.ok(got(x, m => m.type === 'error' && m.code === 'NOT_PAIRED'));
  a.close(); x.close();
});

test('pair-accept without a matching pair-request does not pair', async () => {
  const a = await device(srv.port);
  const x = await device(srv.port);
  x.json({ type: 'pair-accept', to: a.id });
  await wait(50);
  x.json({ type: 'signal', to: a.id, data: { type: 'offer', sdp: 'evil' } });
  await wait(50);
  assert.ok(got(x, m => m.code === 'NO_PENDING_REQUEST'));
  assert.ok(!got(a, m => m.type === 'pair-accepted' || m.type === 'signal'));
  a.close(); x.close();
});

test('pairing survives reconnects when both sides list each other', async () => {
  const a = await device(srv.port);
  const b = await device(srv.port);
  await pair(a, b);
  b.close();
  await wait(50);
  const b2 = await device(srv.port, { secret: b.secret, pairedIds: [a.id] });
  await wait(50);
  assert.ok(got(b2, m => m.type === 'presence' && m.deviceId === a.id && m.online));
  b2.json({ type: 'signal', to: a.id, data: { type: 'offer', sdp: 'x' } });
  await wait(50);
  assert.ok(got(a, m => m.type === 'signal' && m.from === b.id));
  a.close(); b2.close();
});

test('a newer connection replaces the old one and closing the old one does not crash the server', async () => {
  const a = await device(srv.port);
  const b = await device(srv.port);
  await pair(a, b);

  const closed = new Promise(r => b.on('close', code => r(code)));
  const b2 = await device(srv.port, { secret: b.secret, pairedIds: [a.id] });
  assert.equal(await closed, 4001);

  // Previously this crashed the process (TypeError on a deleted entry)
  b2.json({ type: 'signal', to: a.id, data: { type: 'offer', sdp: 'x' } });
  await wait(100);
  assert.equal(srv.proc.exitCode, null, srv.proc.stderrText);
  assert.ok(got(a, m => m.type === 'signal' && m.from === b2.id));
  assert.ok(!got(a, m => m.type === 'presence' && m.deviceId === b.id && m.online === false));
  a.close(); b2.close();
});

test('idle sessions are closed (not silently dropped) after SESSION_TTL_MS', async () => {
  const short = await startServer({ SESSION_TTL_MS: '200' });
  try {
    const a = await device(short.port);
    const code = await new Promise(r => a.on('close', c => r(c)));
    assert.equal(code, 4002);
    // Server keeps working and the device can register again
    const again = await device(short.port, { secret: a.secret });
    assert.equal(again.id, a.id);
    again.close();
  } finally {
    short.proc.kill();
  }
});

test('health endpoint responds', async () => {
  const res = await fetch(`http://localhost:${srv.port}/health`);
  assert.equal(res.status, 200);
  assert.equal((await res.json()).status, 'ok');
});

// Opens a raw WebSocket and resolves with 'open' or the HTTP status of the refusal
function tryConnect(port, headers = {}) {
  return new Promise(resolve => {
    const ws = new WebSocket(`ws://localhost:${port}/ws`, { headers });
    ws.on('open', () => resolve({ ok: true, ws }));
    ws.on('unexpected-response', (_req, res) => resolve({ ok: false, status: res.statusCode }));
    ws.on('error', () => {});
  });
}

test('the WebSocket refuses other websites but not the CLI (no Origin)', async () => {
  const own = await tryConnect(srv.port, { Origin: `http://localhost:${srv.port}` });
  assert.ok(own.ok, 'same-site origin accepted');
  own.ws.close();
  const cli = await tryConnect(srv.port);
  assert.ok(cli.ok, 'no Origin (CLI) accepted');
  cli.ws.close();
  const evil = await tryConnect(srv.port, { Origin: 'https://evil.example' });
  assert.equal(evil.status, 403);
});

test('connections per IP are limited; behind a proxy the last X-Forwarded-For hop counts', async () => {
  const s = await startServer({ MAX_CONN_PER_IP: '2', TRUST_PROXY: '1' });
  try {
    const a = await tryConnect(s.port, { 'X-Forwarded-For': '203.0.113.9, 198.51.100.1' });
    const b = await tryConnect(s.port, { 'X-Forwarded-For': '198.51.100.1' });
    const c = await tryConnect(s.port, { 'X-Forwarded-For': '10.0.0.5, 198.51.100.1' });
    assert.ok(a.ok && b.ok);
    assert.equal(c.status, 429, 'third connection from the same client IP refused');
    const other = await tryConnect(s.port, { 'X-Forwarded-For': '198.51.100.2' });
    assert.ok(other.ok, 'another IP still accepted');
    a.ws.close(); b.ws.close(); other.ws.close();
    await wait(100);
    const again = await tryConnect(s.port, { 'X-Forwarded-For': '198.51.100.1' });
    assert.ok(again.ok, 'slots are released when connections close');
    again.ws.close();
  } finally {
    s.proc.kill();
  }
});

test('the page has security headers, a CSP that allows its own inline scripts, and its version', async () => {
  const res = await fetch(`http://localhost:${srv.port}/`);
  const html = await res.text();
  const csp = res.headers.get('content-security-policy');
  assert.match(csp, /frame-ancestors 'none'/);
  assert.match(csp, /script-src 'self' 'sha256-/);
  assert.doesNotMatch(csp, /script-src[^;]*unsafe-inline/);
  assert.equal(res.headers.get('x-frame-options'), 'DENY');
  assert.equal(res.headers.get('x-content-type-options'), 'nosniff');
  const health = await (await fetch(`http://localhost:${srv.port}/health`)).json();
  assert.match(health.version, /^[0-9a-f]{12}$/);
  assert.ok(html.includes(`content="${health.version}"`), 'version injected in the page');
  for (const [, body] of html.matchAll(/<script>([\s\S]*?)<\/script>/g)) {
    const h = crypto.createHash('sha256').update(body, 'utf8').digest('base64');
    assert.ok(csp.includes(`'sha256-${h}'`), 'every inline script is allowed by hash');
  }
  const a = await device(srv.port);
  assert.ok(got(a, m => m.type === 'registered' && m.app === health.version));
  a.close();
});
