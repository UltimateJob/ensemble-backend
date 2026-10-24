#!/usr/bin/env node
/**
 * 量一下工作区选择弹窗的真实尺寸与各子块位置。
 *   node scripts/diag-modal.mjs [base]
 */
import { spawn } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const BASE = process.argv[2] || 'http://127.0.0.1:18200';
const EDGE = process.env.EDGE_PATH || '/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge';
const PORT = 9545;
const profile = mkdtempSync(join(tmpdir(), 'ens-diag-'));
const proc = spawn(EDGE, ['--headless=new', `--remote-debugging-port=${PORT}`,
  `--user-data-dir=${profile}`, '--no-first-run', '--disable-gpu', 'about:blank'], { stdio: 'ignore' });
const sleep = ms => new Promise(r => setTimeout(r, ms));
await sleep(2400);

const list = await (await fetch(`http://127.0.0.1:${PORT}/json/list`)).json();
const ws = new WebSocket(list.find(t => t.type === 'page').webSocketDebuggerUrl);
await new Promise(r => ws.addEventListener('open', r));
let id = 0; const pending = new Map();
ws.addEventListener('message', e => {
  const m = JSON.parse(e.data);
  if (m.id && pending.has(m.id)) { const p = pending.get(m.id); pending.delete(m.id); m.error ? p.reject(new Error(JSON.stringify(m.error))) : p.resolve(m.result); }
});
const send = (method, params = {}) => new Promise((resolve, reject) => {
  const i = ++id; pending.set(i, { resolve, reject });
  ws.send(JSON.stringify({ id: i, method, params }));
  setTimeout(() => { if (pending.has(i)) { pending.delete(i); reject(new Error('timeout ' + method)); } }, 30000);
});
const ev = async expr => (await send('Runtime.evaluate', { expression: expr, awaitPromise: true, returnByValue: true, userGesture: true })).result?.value;
const waitFor = async (expr, n = 100) => { for (let i = 0; i < n; i++) { if (await ev(expr)) return true; await sleep(200); } return false; };

await send('Runtime.enable');
await send('Page.enable');
await send('Emulation.setDeviceMetricsOverride', { width: 1440, height: 900, deviceScaleFactor: 1, mobile: false });
await send('Page.navigate', { url: BASE + '/app/' });
await sleep(2000);
await ev(`localStorage.setItem('ens_onboarded','1')`);
await ev(`(()=>{document.querySelector('#phone').value='13800000000';document.querySelector('#doLogin').click()})()`);
await waitFor(`!document.querySelector('#app').classList.contains('hidden')`);
await sleep(900);
await ev(`document.querySelector('.onb-mask')?.remove()`);
await ev(`location.hash='#/lobby'`);
await sleep(600);
await ev(`document.querySelector('.team-card [data-join]')?.click()`);
await waitFor(`location.hash.includes('/room/')`);
await sleep(1500);
const room = await ev(`location.hash.replace('#/room/','').split('?')[0]`);
await ev(`location.hash='#/room/${room}?view=studio'`);
await sleep(2500);
await waitFor(`document.querySelector('.modal-card')`, 40);
await sleep(1500);

const box = await ev(`(()=>{
  const q = s => { const e = document.querySelector(s); if(!e) return null;
    const r = e.getBoundingClientRect();
    return {t:Math.round(r.top),h:Math.round(r.height),w:Math.round(r.width),
            sh:e.scrollHeight, ch:e.clientHeight, ov:getComputedStyle(e).overflowY}; };
  return JSON.stringify({
    card: q('.modal-card'), head: q('.modal-head'), body: q('.modal-body'),
    foot: q('.modal-foot'), mask: q('.modal-mask'),
    rootChildren: document.getElementById('modalRoot')?.children.length,
    cardCount: document.querySelectorAll('.modal-card').length,
  }, null, 1);
})()`);
console.log(box);

ws.close(); proc.kill(); await sleep(600);
try { rmSync(profile, { recursive: true, force: true, maxRetries: 3 }); } catch {}
