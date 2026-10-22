#!/usr/bin/env node
/**
 * 界面上不该出现 undefined / NaN / null（checklist C18、M14）。
 *
 *   node scripts/junk.mjs
 *
 * 这些字符串只有一类来源：某个字段没取到，模板里又直接拼了进去。
 * 视觉上它不破坏布局，所以截图看不出来；单测和 E2E 也不会断言它。
 * 用户看到的是「Token 用量 undefined」——一眼就知道这产品没做完。
 */
import { spawn } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const PORT = 9502, BASE = process.env.BASE || 'http://localhost:8080';
const sleep = ms => new Promise(r => setTimeout(r, ms));
const profile = mkdtempSync(join(tmpdir(), 'jk-'));
const proc = spawn('/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge',
  ['--headless=new', '--no-sandbox', `--remote-debugging-port=${PORT}`, '--disable-gpu',
   '--no-first-run', '--no-default-browser-check', '--disable-extensions', '--hide-scrollbars',
   `--user-data-dir=${profile}`, 'about:blank'], { stdio: 'ignore' });

for (let i = 0; i < 60; i++) {
  try { if ((await fetch(`http://127.0.0.1:${PORT}/json/version`)).ok) break; } catch {}
  await sleep(200);
}
const list = await (await fetch(`http://127.0.0.1:${PORT}/json/list`)).json();
const ws = new WebSocket(list.find(t => t.type === 'page').webSocketDebuggerUrl);
await new Promise(r => ws.addEventListener('open', r, { once: true }));
let id = 0; const pend = new Map();
ws.addEventListener('message', e => {
  const m = JSON.parse(e.data);
  if (m.id && pend.has(m.id)) { pend.get(m.id)(m.result); pend.delete(m.id); }
});
const send = (x, p = {}) => new Promise(r => { const i = ++id; pend.set(i, r); ws.send(JSON.stringify({ id: i, method: x, params: p })); });
const ev = async e => {
  const r = await send('Runtime.evaluate', { expression: e, awaitPromise: true, returnByValue: true });
  if (r.exceptionDetails) return 'ERR';
  return r.result?.value;
};
await send('Page.enable');
await send('Runtime.enable');
await send('Emulation.setDeviceMetricsOverride', { width: 1440, height: 900, deviceScaleFactor: 1, mobile: false });

// 只看可见文本，跳过 <script>/<style>——它们里面当然有 null
const probe = `(() => {
  const bad = [];
  const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
  let n;
  while ((n = walker.nextNode())) {
    const el = n.parentElement;
    if (!el) continue;
    if (el.closest('script,style,noscript')) continue;
    const t = n.nodeValue || '';
    if (/\\b(undefined|NaN|null)\\b/.test(t)) {
      bad.push((el.className || el.tagName) + ' → ' + t.trim().slice(0, 60));
    }
  }
  return JSON.stringify(bad.slice(0, 6));
})()`;

const token = process.env.TOKEN || '';
const conv = process.env.CID || '';
const targets = [
  ['聊天页', `${BASE}/app/?token=${token}&c=${conv}`],
  ['场景页', `${BASE}/app/?token=${token}&scenes=1`],
  ['造角色', `${BASE}/app/?token=${token}&forge=1`],
  ['空状态', `${BASE}/app/?token=${token}`],
];

let bad = 0;
for (const [label, url] of targets) {
  await send('Page.navigate', { url });
  await sleep(3000);
  const out = JSON.parse(await ev(probe));
  if (out.length) { bad += out.length; console.log(`  ✗ ${label}: ${out.join(' | ')}`); }
  else console.log(`  ✓ ${label}`);
}

await send('Page.navigate', { url: `${BASE}/admin/?token=admin-dev-token` });
await sleep(2500);
for (const p of ['dash', 'models', 'users', 'logs', 'slow', 'convs']) {
  await ev(`document.querySelector('[data-p="${p}"]')?.click()`);
  await sleep(900);
  const out = JSON.parse(await ev(probe));
  if (out.length) { bad += out.length; console.log(`  ✗ 后台/${p}: ${out.join(' | ')}`); }
  else console.log(`  ✓ 后台/${p}`);
}

ws.close();
proc.kill();
await sleep(200);
try { rmSync(profile, { recursive: true, force: true }); } catch {}
console.log(`\n${bad ? bad + ' 处脏值' : '没有脏值'}\n`);
process.exit(bad ? 1 : 0);
