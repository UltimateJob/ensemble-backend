#!/usr/bin/env node
/**
 * 移动端房间截图（确保跳过引导）。
 *   node scripts/shot-mobile.mjs [base]
 */
import { spawn } from 'node:child_process';
import { mkdtempSync, rmSync, mkdirSync, writeFileSync, statSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const BASE = process.argv[2] || 'http://127.0.0.1:18260';
const EDGE = process.env.EDGE_PATH || '/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge';
const PORT = 9498;

const profile = mkdtempSync(join(tmpdir(), 'ens-mob-'));
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
const waitFor = async (expr, n = 120) => { for (let i = 0; i < n; i++) { if (await ev(expr)) return true; await sleep(180); } return false; };
async function shot(name) {
  await sleep(600);
  const r = await send('Page.captureScreenshot', { format: 'png' });
  const f = `.ui-shots/mobile-${name}.png`;
  writeFileSync(f, Buffer.from(r.data, 'base64'));
  console.log(`✓ ${f}  ${(statSync(f).size / 1024).toFixed(0)}KB`);
}

mkdirSync('.ui-shots', { recursive: true });
await send('Runtime.enable');
await send('Page.enable');
await send('Emulation.setDeviceMetricsOverride', { width: 390, height: 844, deviceScaleFactor: 1, mobile: true });
await send('Page.navigate', { url: BASE + '/app/' });
await sleep(2000);

// 关键：登录前就把引导标记置上，登录后就不会弹
await ev(`localStorage.setItem('ens_onboarded','1')`);
await ev(`(()=>{document.querySelector('#phone').value='13800000000';document.querySelector('#doLogin').click()})()`);
await waitFor(`!document.querySelector('#app').classList.contains('hidden')`);
await sleep(1000);
// 二次保险：万一还是弹了，直接移除
await ev(`document.querySelector('.onb-mask')?.remove()`);

await ev(`location.hash='#/lobby'`);
await sleep(1200);
await shot('lobby');

await ev(`document.querySelector('.team-card [data-join]')?.click()`);
await waitFor(`location.hash.includes('/room/')`);
await sleep(3000);
await ev(`document.querySelector('.onb-mask')?.remove()`);
await shot('room');

await ev(`document.querySelector('#roomPanel')?.click()`);
await sleep(1400);
await shot('panel');

await ev(`document.querySelector('#panelClose')?.click()`);
await sleep(600);
await ev(`(()=>{const t=document.querySelector('#ta'); if(t){t.value='帮我看看这个标书的关键条款'; t.dispatchEvent(new Event('input',{bubbles:true})); document.querySelector('#sendBtn')?.click();}})()`);
await sleep(3000);
await shot('sending');

await ev(`location.hash='#/goals'`);
await sleep(1400);
await shot('goals');

await ev(`location.hash='#/library'`);
await sleep(1500);
await shot('library');

await ev(`location.hash='#/connectors'`);
await sleep(1400);
await shot('connectors');

console.log(errors.length ? '错误：\n' + errors.slice(0, 5).join('\n') : '控制台：无错误');
ws.close(); proc.kill();
try { rmSync(profile, { recursive: true, force: true }); } catch (e) {}
process.exit(0);
