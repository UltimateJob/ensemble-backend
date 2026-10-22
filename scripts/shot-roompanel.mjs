#!/usr/bin/env node
/** 截房间概览面板：进房间 → 点面板按钮 → 截图。 */
import { spawn } from 'node:child_process';
import { mkdtempSync, rmSync, mkdirSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const BASE = process.argv[2] || 'http://127.0.0.1:18240';
const EDGE = process.env.EDGE_PATH || '/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge';
const PORT = 9485;

const profile = mkdtempSync(join(tmpdir(), 'ens-rp-'));
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
const errors = [];
ws.addEventListener('message', e => {
  const m = JSON.parse(e.data);
  if (m.id && pending.has(m.id)) {
    const p = pending.get(m.id); pending.delete(m.id);
    m.error ? p.reject(new Error(JSON.stringify(m.error))) : p.resolve(m.result);
  }
  if (m.method === 'Runtime.exceptionThrown') errors.push(m.params.exceptionDetails.exception?.description || m.params.exceptionDetails.text);
});
const send = (method, params = {}) => new Promise((resolve, reject) => {
  const i = ++id; pending.set(i, { resolve, reject });
  ws.send(JSON.stringify({ id: i, method, params }));
  setTimeout(() => { if (pending.has(i)) { pending.delete(i); reject(new Error('timeout ' + method)); } }, 40000);
});
const ev = async expr => (await send('Runtime.evaluate', { expression: expr, awaitPromise: true, returnByValue: true, userGesture: true })).result?.value;
const waitFor = async (expr, n = 90) => { for (let i = 0; i < n; i++) { if (await ev(expr)) return true; await sleep(180); } return false; };

await send('Runtime.enable');
await send('Page.enable');
await send('Emulation.setDeviceMetricsOverride', { width: 1440, height: 900, deviceScaleFactor: 1, mobile: false });
await send('Page.navigate', { url: BASE + '/app/' });
await sleep(2000);
await ev(`localStorage.setItem('ens_onboarded','1')`);
await ev(`(()=>{document.querySelector('#phone').value='13800000000';document.querySelector('#doLogin').click()})()`);
await waitFor(`!document.querySelector('#app').classList.contains('hidden')`);
await sleep(600);
await ev(`document.querySelector('.team-card [data-join]')?.click()`);
await waitFor(`location.hash.includes('/room/')`);
await sleep(3000);
await ev(`document.querySelector('#roomPanel')?.click()`);
await sleep(1600);

mkdirSync('.ui-shots', { recursive: true });
const r = await send('Page.captureScreenshot', { format: 'png' });
writeFileSync('.ui-shots/check-roompanel.png', Buffer.from(r.data, 'base64'));
console.log('✓ .ui-shots/check-roompanel.png');
console.log('产物条目：', await ev(`document.querySelectorAll('#artifactList .ap-art').length`));
if (errors.length) console.log('错误：', errors.slice(0, 3).join('\n'));

ws.close(); proc.kill();
try { rmSync(profile, { recursive: true, force: true }); } catch (e) {}
process.exit(0);
