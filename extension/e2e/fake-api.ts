import { createPrivateKey, randomBytes, sign, type KeyObject } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { createServer, type IncomingMessage, type Server, type ServerResponse } from 'node:http';
import { API_ORIGIN, API_PORT, KEY_FILE, KEY_ID, PAIR_CODE } from './constants';

/**
 * In-memory imitation of the Go API's /api/v1/ext endpoints (docs/browser-extension.md §11).
 * It signs recipe JSON with the e2e private key, exactly the bytes it sends.
 */

export interface FakeTask {
  id: string;
  status: 'QUEUED' | 'RUNNING' | 'WAITING_USER' | 'SUCCEEDED' | 'FAILED';
  capability: string;
  title: string;
  shop: { id: string; shopee_shop_id: string; name: string; region: string };
  preview: unknown;
  created_at: string;
  recipeJson: string;
  /** Change one byte of the recipe after signing it. */
  tamper?: boolean;
  leaseToken?: string;
}

export interface ResultRecord {
  taskId: string;
  body: Record<string, unknown>;
}

// 1x1 PNG.
export const BANNER_PNG = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==',
  'base64',
);

export class FakeApi {
  private server: Server | null = null;
  private key: KeyObject = createPrivateKey(readFileSync(KEY_FILE));
  tokens = new Set<string>();
  codeUsed = false;
  tasks = new Map<string, FakeTask>();
  results: ResultRecord[] = [];
  releases: string[] = [];
  heartbeats: { taskId: string; state: string; message?: string }[] = [];
  screenshots: { taskId: string; bytes: number; leaseOk: boolean }[] = [];
  requests: string[] = [];
  /** When true, every lease check fails with 409 lease_lost (another device took over). */
  leaseStolen = false;

  reset() {
    this.tokens.clear();
    this.codeUsed = false;
    this.tasks.clear();
    this.results = [];
    this.releases = [];
    this.heartbeats = [];
    this.screenshots = [];
    this.requests = [];
    this.leaseStolen = false;
  }

  addTask(t: Omit<FakeTask, 'status' | 'created_at'>) {
    this.tasks.set(t.id, { ...t, status: 'QUEUED', created_at: new Date().toISOString() });
  }

  async start() {
    this.server = createServer((req, res) => {
      this.handle(req, res).catch((e) => {
        console.error(e);
        this.problem(res, 500, 'internal', String(e));
      });
    });
    await new Promise<void>((r) => this.server!.listen(API_PORT, '127.0.0.1', r));
  }

  async stop() {
    await new Promise<void>((r) => this.server?.close(() => r()) ?? r());
  }

  private problem(res: ServerResponse, status: number, code: string, detail = code) {
    res.writeHead(status, { 'Content-Type': 'application/problem+json' });
    res.end(JSON.stringify({ type: `https://errors.example/${code}`, title: code, status, code, detail, request_id: randomBytes(4).toString('hex') }));
  }

  private json(res: ServerResponse, status: number, body: unknown) {
    res.writeHead(status, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify(body));
  }

  private async body(req: IncomingMessage): Promise<Buffer> {
    const chunks: Buffer[] = [];
    for await (const c of req) chunks.push(c as Buffer);
    return Buffer.concat(chunks);
  }

  private publicTask(t: FakeTask) {
    const { recipeJson: _r, tamper: _t, leaseToken: _l, ...rest } = t;
    return rest;
  }

  private async handle(req: IncomingMessage, res: ServerResponse) {
    const url = new URL(req.url ?? '/', API_ORIGIN);
    const path = url.pathname;
    this.requests.push(`${req.method} ${path}`);

    if (path === '/files/banner.png') {
      res.writeHead(200, { 'Content-Type': 'image/png' });
      res.end(BANNER_PNG);
      return;
    }
    if (!path.startsWith('/api/v1/ext/')) return this.problem(res, 404, 'not_found');
    const p = path.slice('/api/v1/ext'.length);

    if (req.method === 'POST' && p === '/pair') {
      const b = JSON.parse((await this.body(req)).toString() || '{}');
      if (b.code !== PAIR_CODE || this.codeUsed || !b.device_name || !b.extension_version) return this.problem(res, 422, 'invalid_code');
      this.codeUsed = true;
      const token = randomBytes(32).toString('base64url');
      this.tokens.add(token);
      return this.json(res, 200, {
        token,
        expires_at: new Date(Date.now() + 30 * 864e5).toISOString(),
        device: { id: 'dev-1', name: b.device_name },
        org: { id: 'org-1', name: 'Acme Fashion', slug: 'acme' },
        user: { id: 'user-1', email: 'seller@example.test', name: 'Seller' },
      });
    }

    const auth = req.headers.authorization ?? '';
    const token = auth.startsWith('Bearer ') ? auth.slice(7) : '';
    if (!this.tokens.has(token)) return this.problem(res, 401, 'device_unauthorized');

    if (req.method === 'GET' && p === '/me') {
      return this.json(res, 200, { device: { id: 'dev-1', name: 'x' }, org: { id: 'org-1', name: 'Acme Fashion' }, user: { id: 'user-1', email: 'seller@example.test' } });
    }
    if (req.method === 'DELETE' && p === '/device') {
      this.tokens.delete(token);
      res.writeHead(204).end();
      return;
    }
    if (req.method === 'POST' && p === '/token/rotate') {
      this.tokens.delete(token);
      const t = randomBytes(32).toString('base64url');
      this.tokens.add(t);
      return this.json(res, 200, { token: t, expires_at: new Date(Date.now() + 30 * 864e5).toISOString() });
    }
    if (req.method === 'GET' && p === '/tasks') {
      const data = [...this.tasks.values()].filter((t) => ['QUEUED', 'RUNNING', 'WAITING_USER'].includes(t.status)).map((t) => this.publicTask(t));
      return this.json(res, 200, { data });
    }

    const m = /^\/tasks\/([^/]+)\/(claim|heartbeat|screenshots|result|release)$/.exec(p);
    if (!m || req.method !== 'POST') return this.problem(res, 404, 'not_found');
    const task = this.tasks.get(decodeURIComponent(m[1]!));
    if (!task) return this.problem(res, 404, 'not_found');
    const op = m[2];

    if (op === 'claim') {
      if (task.status !== 'QUEUED') return this.problem(res, 409, 'task_unavailable');
      task.status = 'RUNNING';
      task.leaseToken = randomBytes(16).toString('hex');
      const signature = sign(null, Buffer.from(task.recipeJson, 'utf8'), this.key).toString('base64');
      // Tampering after signing: a compromised path changing one character.
      const json = task.tamper ? task.recipeJson.replace('Shop decoration', 'Shop decoratioN') : task.recipeJson;
      return this.json(res, 200, {
        lease_token: task.leaseToken,
        lease_expires_at: new Date(Date.now() + 600_000).toISOString(),
        recipe: { json, signature, key_id: KEY_ID },
        inputs: {
          banner_image: { type: 'files', files: [{ name: 'banner.png', url: `${API_ORIGIN}/files/banner.png`, content_type: 'image/png' }] },
        },
        task: { shopee_shop_id: task.shop.shopee_shop_id, region: task.shop.region, capability: task.capability },
      });
    }

    const raw = await this.body(req);
    if (op === 'screenshots') {
      const text = raw.toString('latin1');
      const lease = /name="lease_token"\r\n\r\n([^\r]*)/.exec(text)?.[1];
      const leaseOk = lease === task.leaseToken && !this.leaseStolen;
      if (!leaseOk) return this.problem(res, 409, 'lease_lost');
      this.screenshots.push({ taskId: task.id, bytes: raw.length, leaseOk });
      return this.json(res, 201, { id: `shot-${this.screenshots.length}` });
    }
    const b = JSON.parse(raw.toString() || '{}');
    if (b.lease_token !== task.leaseToken || this.leaseStolen) return this.problem(res, 409, 'lease_lost');
    if (op === 'heartbeat') {
      this.heartbeats.push({ taskId: task.id, state: b.state, message: b.message });
      task.status = b.state;
      return this.json(res, 200, { lease_expires_at: new Date(Date.now() + 600_000).toISOString() });
    }
    if (op === 'release') {
      this.releases.push(task.id);
      task.status = 'QUEUED';
      task.leaseToken = undefined;
      res.writeHead(204).end();
      return;
    }
    // result
    this.results.push({ taskId: task.id, body: b });
    task.status = b.status;
    res.writeHead(204).end();
  }
}
