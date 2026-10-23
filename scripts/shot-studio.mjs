#!/usr/bin/env node
/**
 * 代码工作室 + 工作区选择弹窗的截图。
 *   node scripts/shot-studio.mjs [base]
 */
import { spawn } from 'node:child_process';
import { mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const BASE = process.argv[2] || 'http://127.0.0.1:18200';
const EDGE = process.env.EDGE_PATH || '/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge';
const PORT = 9544;
const W = Number(process.env.W || 1440);

const profile = mkdtempSync(join(tmpdir(), 'ens-studio-'));
const proc = spawn(EDGE, ['--headless=new', `--remote-debugging-port=${PORT}`,
  `--user-data-dir=${profile}`, '--no-first-run', '--disable-gpu', '--hide-scrollbars', 'about:blank'],
  { stdio: 'ignore' });

const sleep = ms => new Promise(r => setTimeout(r, ms));
await sleep(2400);

const list = await (await fetch(`http://127.0.0.1:${PORT}/json/list`)).json();
const ws = new WebSocket(list.find(t => t.type === 'page').webSocketDebuggerUrl);
await new Promise(r => ws.addEventListener('open', r));
let id = 0;
const pending = new Map();
ws.addEventListener('message', e => {
  const m = JSON.parse(e.data);
  if (m.id && pending.has(m.id)) {
    const p = pending.get(m.id); pending.delete(m.id);
    m.error ? p.reject(new Error(JSON.stringify(m.error))) : p.resolve(m.result);
  }
});
const send = (method, params = {}) => new Promise((resolve, reject) => {
  const i = ++id; pending.set(i, { resolve, reject });
  ws.send(JSON.stringify({ id: i, method, params }));
  setTimeout(() => { if (pending.has(i)) { pending.delete(i); reject(new Error('timeout ' + method)); } }, 30000);
});
const ev = async expr => (await send('Runtime.evaluate', { expression: expr, awaitPromise: true, returnByValue: true, userGesture: true })).result?.value;
const waitFor = async (expr, n = 100) => { for (let i = 0; i < n; i++) { if (await ev(expr)) return true; await sleep(180); } return false; };

async function shot(name) {
  const r = await send('Page.captureScreenshot', { format: 'png' });
  const path = `.ui-shots/${name}.png`;
  writeFileSync(path, Buffer.from(r.data, 'base64'));
  console.log('  ✓', path);
}

await send('Runtime.enable');
await send('Page.enable');
await send('Emulation.setDeviceMetricsOverride', { width: W, height: 900, deviceScaleFactor: 1, mobile: false });
await send('Page.navigate', { url: BASE + '/app/' });
await sleep(2000);

await ev(`localStorage.setItem('ens_onboarded','1')`);
await ev(`(()=>{document.querySelector('#phone').value='13800000000';document.querySelector('#doLogin').click()})()`);
await waitFor(`!document.querySelector('#app').classList.contains('hidden')`);
await sleep(900);
await ev(`document.querySelector('.onb-mask')?.remove()`);

// 进一个房间，再从房间进工作室
await ev(`location.hash='#/lobby'`);
await sleep(600);
await ev(`document.querySelector('.team-card [data-join]')?.click()`);
await waitFor(`location.hash.includes('/room/')`);
await sleep(1500);
await ev(`document.querySelector('.onb-mask')?.remove()`);

// 工作室不是独立路由，是房间的一种视图：?view=studio
const room = await ev(`location.hash.replace('#/room/','').split('?')[0]`);
await ev(`location.hash='#/room/${room}?view=studio'`);
await sleep(2200);

// 首次进入会自动弹工作区选择面板，正好截它。
// 弹窗有回弹动画，要等它停稳再截，否则截到半截高度。
await waitFor(`document.querySelector('.modal, .mask, [class*=modal]')`, 40);
await sleep(1800);
await shot('studio-workspace-picker');

// 关掉弹窗，截工作室本身（模态按钮用的是 data-x，不是 data-key）
const closeBtn = `document.querySelector('.modal-card [data-x=cancel], .modal-card [data-x=close]')`;
await ev(`${closeBtn}?.click()`);
await sleep(800);
await shot('studio-main');

console.log('\n控制台错误：');
const errs = await ev(`(window.__errs||[]).join('\\n')`);
console.log(errs || '  无');

ws.close();
proc.kill();
// 浏览器还在写 profile，等它退干净再删；删不掉也无所谓。
await sleep(600);
try { rmSync(profile, { recursive: true, force: true, maxRetries: 3 }); } catch {}
