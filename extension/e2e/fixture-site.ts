import { createServer, type Server } from 'node:http';
import { SELLER_ORIGIN, SELLER_PORT } from './constants';

/**
 * Local pages that imitate a seller-centre-like flow. They are invented for
 * tests and do not copy any real Seller Centre markup.
 */

const layout = (title: string, body: string, script = '') => `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>${title}</title>
<style>body{font-family:sans-serif;margin:24px} .hidden-input{display:none} header{margin-bottom:16px}</style>
</head><body>${body}<script>${script}</script></body></html>`;

export class FixtureSite {
  private server: Server | null = null;
  shopId = '123456';
  requireVerification = false;
  hits: string[] = [];
  saved: { name: string; size: number; type: string }[] = [];

  reset() {
    this.shopId = '123456';
    this.requireVerification = false;
    this.hits = [];
    this.saved = [];
  }

  async start() {
    this.server = createServer(async (req, res) => {
      const url = new URL(req.url ?? '/', SELLER_ORIGIN);
      const path = url.pathname;
      if (path === '/favicon.ico') return res.writeHead(204).end();
      if (!path.startsWith('/__fixture/')) this.hits.push(path);
      const html = (s: string) => {
        res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' });
        res.end(s);
      };

      if (path === '/__fixture/saved' && req.method === 'POST') {
        const chunks: Buffer[] = [];
        for await (const c of req) chunks.push(c as Buffer);
        this.saved.push(JSON.parse(Buffer.concat(chunks).toString()));
        return res.writeHead(204).end();
      }
      if (path === '/__fixture/verified' && req.method === 'POST') {
        this.requireVerification = false;
        return res.writeHead(204).end();
      }
      if (path === '/portal/home') {
        if (this.requireVerification) {
          res.writeHead(302, { Location: '/account/verify?next=/portal/home' });
          return res.end();
        }
        return html(
          layout(
            'Seller home',
            `<header><strong>Demo Seller Portal</strong> · <span data-testid="shop-id">Shop ID: ${this.shopId}</span></header>
             <nav><a href="/portal/decoration">Shop decoration</a> · <a href="/portal/orders">Orders</a></nav>
             <h1>Welcome back</h1>`,
          ),
        );
      }
      if (path === '/portal/decoration') {
        return html(
          layout(
            'Shop decoration',
            `<header><span data-testid="shop-id">Shop ID: ${this.shopId}</span></header>
             <h1>Shop banner</h1>
             <div data-banner></div>
             <button id="edit" type="button">Edit</button>
             <section id="editor" hidden>
               <label class="pick">Choose image <input class="hidden-input" type="file" accept="image/*"></label>
               <button id="save" type="button">Save</button>
             </section>
             <p id="status" role="status"></p>`,
            `document.getElementById('edit').onclick = () => { document.getElementById('editor').hidden = false; };
             document.getElementById('save').onclick = async () => {
               const f = document.querySelector('input[type=file]').files[0];
               if (!f) { document.getElementById('status').textContent = 'Choose an image first'; return; }
               await fetch('/__fixture/saved', { method: 'POST', body: JSON.stringify({ name: f.name, size: f.size, type: f.type }) });
               const img = document.createElement('img');
               img.alt = 'Shop banner'; img.width = 120; img.height = 40; img.src = URL.createObjectURL(f);
               document.querySelector('[data-banner]').replaceChildren(img);
               document.getElementById('editor').hidden = true;
               setTimeout(() => { document.getElementById('status').textContent = 'Saved'; }, 300);
             };`,
          ),
        );
      }
      if (path === '/account/verify') {
        return html(
          layout(
            'Verify',
            `<h1>Verify your identity</h1><p>Enter the code we sent to your phone.</p>
             <input aria-label="Code"><button id="done" type="button">I'm done</button>`,
            `document.getElementById('done').onclick = async () => {
               await fetch('/__fixture/verified', { method: 'POST' });
               location.href = new URLSearchParams(location.search).get('next') || '/portal/home';
             };`,
          ),
        );
      }
      if (path === '/') return html(layout('Start', '<p>Not signed in to any page yet.</p>'));
      res.writeHead(404, { 'Content-Type': 'text/plain' }).end('not found');
    });
    await new Promise<void>((r) => this.server!.listen(SELLER_PORT, '127.0.0.1', r));
  }

  async stop() {
    await new Promise<void>((r) => this.server?.close(() => r()) ?? r());
  }
}
