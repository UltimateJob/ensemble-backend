#!/usr/bin/env node
/**
 * 把纸团原图合成到一个深色圆角底色上，做成「徽章」版 logo。
 * 奶油色纸团放在浅底上会糊掉；压一个深色底就立住了。
 *
 * 不依赖图像库：用无头浏览器画 canvas，再导出 PNG。
 */
import { spawn } from 'node:child_process';
import { mkdtempSync, rmSync, readFileSync, writeFileSync, mkdirSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const EDGE = process.env.EDGE_PATH || '/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge';
const PORT = 9470;
const SRC = 'docs/brand/zhituan/logo-b.png';
const OUT_DIR = 'web/site/brand';

const profile = mkdtempSync(join(tmpdir(), 'ens-logo-'));
const proc = spawn(EDGE, [
  '--headless=new', `--remote-debugging-port=${PORT}`, `--user-data-dir=${profile}`,
  '--no-first-run', '--disable-gpu', 'about:blank',
], { stdio: 'ignore' });

const sleep = ms => new Promise(r => setTimeout(r, ms));
await sleep(2200);

const list = await (await fetch(`http://127.0.0.1:${PORT}/json/list`)).json();
const ws = new WebSocket(list.find(t => t.type === 'page').webSocketDebuggerUrl);
await new Promise(r => ws.addEventListener('open', r));

let id = 0;
const pending = new Map();
ws.addEventListener('message', e => {
  const m = JSON.parse(e.data);
  if (m.id && pending.has(m.id)) {
    const p = pending.get(m.id);
    pending.delete(m.id);
    m.error ? p.reject(new Error(JSON.stringify(m.error))) : p.resolve(m.result);
  }
});
const send = (method, params = {}) => new Promise((resolve, reject) => {
  const i = ++id;
  pending.set(i, { resolve, reject });
  ws.send(JSON.stringify({ id: i, method, params }));
  setTimeout(() => { if (pending.has(i)) { pending.delete(i); reject(new Error('timeout ' + method)); } }, 25000);
});

const b64 = readFileSync(SRC).toString('base64');
const dataUrl = 'data:image/png;base64,' + b64;

// 三档尺寸各出一张
const SIZES = [512, 128, 64];
mkdirSync(OUT_DIR, { recursive: true });

for (const size of SIZES) {
  const html = `<!DOCTYPE html><html><body style="margin:0">
  <canvas id="c" width="${size}" height="${size}"></canvas>
  <script>
  const img = new Image();
  img.onload = () => {
    const c = document.getElementById('c');
    const ctx = c.getContext('2d');
    const S = ${size};
    const r = S * 0.23;

    // 原图里纸团约占中间 62%，放大 1.45 倍让它在图标里更有存在感。
    const off = document.createElement('canvas');
    off.width = S; off.height = S;
    const o = off.getContext('2d');
    const side = Math.min(img.width, img.height);
    const sx = (img.width - side) / 2;
    const sy = (img.height - side) / 2;
    const zoom = 1.45;
    const ds = S * zoom;
    o.drawImage(img, sx, sy, side, side, (S - ds) / 2, (S - ds) / 2, ds, ds);

    // 只裁圆角，不叠深底——纸团自己的奶油底就是它的背景。
    ctx.save();
    ctx.beginPath();
    ctx.roundRect(0, 0, S, S, r);
    ctx.clip();
    ctx.drawImage(off, 0, 0);
    ctx.restore();

    // 外圈发丝线：浅底上也能看清边界
    ctx.save();
    ctx.beginPath();
    ctx.roundRect(0.5, 0.5, S - 1, S - 1, r);
    ctx.strokeStyle = 'rgba(74,62,44,0.18)';
    ctx.lineWidth = Math.max(1, S / 256);
    ctx.stroke();
    ctx.restore();

    window.__png = c.toDataURL('image/png');
  };
  img.src = ${JSON.stringify(dataUrl)};
  </script></body></html>`;

  await send('Page.navigate', { url: 'data:text/html;charset=utf-8,' + encodeURIComponent(html) });
  await sleep(1200);

  let png = null;
  for (let i = 0; i < 40; i++) {
    const r = await send('Runtime.evaluate', { expression: 'window.__png || ""', returnByValue: true });
    png = r.result?.value;
    if (png) break;
    await sleep(150);
  }
  if (!png) { console.error('×', size, '渲染失败'); continue; }

  const buf = Buffer.from(png.split(',')[1], 'base64');
  const name = size === 512 ? 'logo-mark.png' : `logo-mark-${size}.png`;
  writeFileSync(join(OUT_DIR, name), buf);
  console.log('✓', join(OUT_DIR, name), (buf.length / 1024).toFixed(0) + 'KB');
}

ws.close();
proc.kill();
try { rmSync(profile, { recursive: true, force: true }); } catch (e) {}
process.exit(0);
