/**
 * FileSwap — anonymous usage metrics (Prometheus text format)
 *
 * Only counts: every label takes one of a few fixed values (browser/cli, a
 * device kind, an event name). Device codes, names, IP addresses and anything
 * about files never become a label or a value, so the scrape output cannot
 * tell users apart.
 *
 * Served by its own HTTP listener on METRICS_PORT (off by default), never on
 * the public port that the reverse proxy forwards.
 */

'use strict';

const http   = require('http');
const crypto = require('crypto');

const families = []; // in registration order, which is also the output order

function family(name, type, help, labelNames) {
  const f = { name, type, help, labelNames, values: new Map(), collect: null };
  families.push(f);
  return f;
}

function labelKey(f, labels) {
  return f.labelNames.map(l => String(labels[l] ?? '')).join('\u0000');
}

// `known` label sets start at 0, so Prometheus sees each series before its
// first event (increase() would miss that event otherwise)
function counter(name, help, labelNames = [], known = [{}]) {
  const f = family(name, 'counter', help, labelNames);
  const inc = (labels = {}, n = 1) => {
    const key = labelKey(f, labels);
    const cur = f.values.get(key);
    if (cur) cur.value += n;
    else f.values.set(key, { labels: { ...labels }, value: n });
  };
  for (const labels of known) inc(labels, 0);
  return { inc };
}

const each = (label, values) => values.map(v => ({ [label]: v }));
const CLIENTS = ['browser', 'cli'];
const KINDS   = ['phone', 'tablet', 'laptop', 'desktop', 'server', 'unknown'];

// A gauge whose samples are computed at scrape time: collect() → [{ labels, value }]
function gauge(name, help, labelNames, collect) {
  family(name, 'gauge', help, labelNames).collect = collect;
}

const escapeLabel = (v) => String(v).replace(/\\/g, '\\\\').replace(/\n/g, '\\n').replace(/"/g, '\\"');

function render() {
  const out = [];
  for (const f of families) {
    out.push(`# HELP ${f.name} ${f.help}`, `# TYPE ${f.name} ${f.type}`);
    const samples = f.collect ? f.collect() : [...f.values.values()];
    for (const { labels, value } of samples) {
      const ls = f.labelNames.map(l => `${l}="${escapeLabel(labels[l] ?? '')}"`).join(',');
      out.push(`${f.name}${ls ? `{${ls}}` : ''} ${value}`);
    }
  }
  return out.join('\n') + '\n';
}

// ─── Distinct devices per day and per week ──────────────────────────────────
// Each device code is hashed with a random salt that only lives for the
// current period; when the period ends, the salt and the hashes are dropped
// and only the count was ever exported. Periods follow the server's local
// time (set TZ in the container).
function dayOf(d) {
  return `${d.getFullYear()}-${d.getMonth() + 1}-${d.getDate()}`;
}

function weekOf(d) {
  const monday = new Date(d.getFullYear(), d.getMonth(), d.getDate() - ((d.getDay() + 6) % 7));
  return dayOf(monday);
}

class PeriodUniques {
  constructor(periodOf) {
    this.periodOf = periodOf;
    this.period = null;
  }

  roll(now) {
    const p = this.periodOf(now);
    if (p !== this.period) {
      this.period = p;
      this.salt = crypto.randomBytes(32);
      this.seen = new Set();
    }
  }

  add(id, now = new Date()) {
    this.roll(now);
    this.seen.add(crypto.createHmac('sha256', this.salt).update(id).digest('base64').slice(0, 16));
  }

  count(now = new Date()) {
    this.roll(now);
    return this.seen.size;
  }
}

const uniques = { day: new PeriodUniques(dayOf), week: new PeriodUniques(weekOf) };

// ─── The metrics ────────────────────────────────────────────────────────────
const connections   = counter('fileswap_ws_connections_total', 'WebSocket connections opened (browsers send an Origin, the CLI does not)',
  ['client'], each('client', CLIENTS));
const rejected      = counter('fileswap_ws_rejected_total', 'Connections or messages refused: origin, ip_limit, rate_limit, too_big, capacity',
  ['reason'], each('reason', ['origin', 'ip_limit', 'rate_limit', 'too_big', 'capacity']));
const registrations = counter('fileswap_registrations_total', 'Devices that registered on a connection',
  ['client', 'kind'], CLIENTS.flatMap(client => KINDS.map(kind => ({ client, kind }))));
const pairings      = counter('fileswap_pairings_total', 'Pairing events: requested, accepted, rejected, unpaired',
  ['event'], each('event', ['requested', 'accepted', 'rejected', 'unpaired']));
const transfers     = counter('fileswap_transfers_started_total', 'WebRTC connections offered: file (a file, ZIP or clipboard attachment) or clip_list (reading a clipboard)',
  ['type'], each('type', ['file', 'clip_list']));
const aborts        = counter('fileswap_transfers_aborted_total', 'Transfers cancelled by one of the devices');

const startedAt = Date.now() / 1000;

/**
 * Wires the scrape-time gauges to the server's state.
 *   devices()    → iterable of { client, kind } for the devices online now
 *   appVersion() → the web app version being served
 */
function setup({ devices, appVersion }) {
  gauge('fileswap_devices_online', 'Devices online now', ['client', 'kind'], () => {
    const counts = new Map();
    for (const d of devices()) {
      const key = `${d.client}\u0000${d.kind}`;
      counts.set(key, (counts.get(key) ?? 0) + 1);
    }
    return [...counts].map(([key, value]) => {
      const [client, kind] = key.split('\u0000');
      return { labels: { client, kind }, value };
    });
  });
  gauge('fileswap_devices_unique', 'Distinct devices seen in the current day or week (local time)', ['period'], () => [
    { labels: { period: 'day' }, value: uniques.day.count() },
    { labels: { period: 'week' }, value: uniques.week.count() },
  ]);
  gauge('fileswap_build_info', 'Version of the web app being served', ['version'], () => [
    { labels: { version: appVersion() || 'unknown' }, value: 1 },
  ]);
  gauge('process_start_time_seconds', 'Start time of the process since the Unix epoch, in seconds', [], () => [
    { labels: {}, value: startedAt },
  ]);
  gauge('process_resident_memory_bytes', 'Resident memory size in bytes', [], () => [
    { labels: {}, value: process.memoryUsage().rss },
  ]);
}

/** Serves GET /metrics on `port`; with `token`, only with "Authorization: Bearer <token>". */
function listen(port, token) {
  const expected = token ? crypto.createHash('sha256').update(`Bearer ${token}`).digest() : null;
  const server = http.createServer((req, res) => {
    if (req.method !== 'GET' || req.url.split('?')[0] !== '/metrics') { res.writeHead(404); res.end(); return; }
    if (expected) {
      const got = crypto.createHash('sha256').update(String(req.headers.authorization ?? '')).digest();
      if (!crypto.timingSafeEqual(got, expected)) { res.writeHead(401); res.end(); return; }
    }
    res.writeHead(200, { 'Content-Type': 'text/plain; version=0.0.4; charset=utf-8' });
    res.end(render());
  });
  server.listen(port);
  return server;
}

module.exports = {
  setup, listen, render, PeriodUniques,
  // Events, called from the signaling server
  connection: (client) => connections.inc({ client }),
  rejected: (reason) => rejected.inc({ reason }),
  registered: (id, client, kind) => {
    registrations.inc({ client, kind: kind || 'unknown' });
    uniques.day.add(id);
    uniques.week.add(id);
  },
  pairing: (event) => pairings.inc({ event }),
  signal: (data) => {
    if (data?.type === 'offer') transfers.inc({ type: data.kind === 'clip' ? 'clip_list' : 'file' });
    else if (data?.type === 'abort') aborts.inc();
  },
};
