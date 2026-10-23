#!/usr/bin/env node
/**
 * 单屏截图：只截某个视图，用来快速看视觉改动。
 *   node scripts/shot-views.mjs <view> [width] [base]
 *   view: lobby | room-empty | room | goals | connectors | library | settings
 */
import { spawn } from 'node:child_process';
import { mkdtempSync, rmSync, mkdirSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const VIEW = process.argv[2] || 'lobby';
const WIDTH = Number(process.argv[3] || 1440);
const BASE = process.argv[4] || process.env.SHOT_BASE || 'http://127.0.0.1:18111';
const EDGE = process.env.EDGE_PATH || '/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge';
const PORT = 9455;

const profile = mkdtempSync(join(tmpdir(), 'ens-shot-'));
const proc = spawn(EDGE, [
  '--headless=new', `--remote-debugging-port=${PORT}`, `--user-data-dir=${profile}`,
  '--no-first-run', '--disable-gpu', '--hide-scrollbars', 'about:blank',
], { stdio: 'ignore' });

const sleep = ms => new Promise(r => setTimeout(r, ms));
await sleep(2500);

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
const ev = async expr => (await send('Runtime.evaluate', { expression: expr, awaitPromise: true, returnByValue: true, userGesture: true })).result?.value;
const waitFor = async (expr, tries = 80) => {
  for (let i = 0; i < tries; i++) { if (await ev(expr)) return true; await sleep(200); }
  return false;
};

await send('Runtime.enable');
await send('Page.enable');
await send('Emulation.setDeviceMetricsOverride', {
  width: WIDTH, height: 900, deviceScaleFactor: 1, mobile: WIDTH < 600,
});
await send('Page.navigate', { url: BASE + '/app/' });
await sleep(2200);
await ev(`localStorage.setItem('ens_onboarded','1')`);
await ev(`(()=>{document.querySelector('#phone').value='13800000000';document.querySelector('#doLogin').click()})()`);
await waitFor(`!document.querySelector('#app').classList.contains('hidden')`);
await ev(`localStorage.setItem('ens_onboarded','1')`);
await sleep(500);

if (VIEW === 'room' || VIEW === 'room-empty') {
  await ev(`location.hash='#/lobby'`);
  await sleep(800);
  // 新群默认不会自己开场；等 1.2 秒看有没有历史消息
  await ev(`document.querySelector('.team-card [data-join]')?.click()`);
  await waitFor(`location.hash.includes('/room/')`);
  await sleep(VIEW === 'room' ? 9000 : 1200);
} else if (VIEW !== 'lobby') {
  await ev(`location.hash='#/${VIEW}'`);
  await sleep(1400);
}
await sleep(700);

mkdirSync('.ui-shots', { recursive: true });
const shot = await send('Page.captureScreenshot', { format: 'png', captureBeyondViewport: false });
const file = `.ui-shots/check-${VIEW}-${WIDTH}.png`;
writeFileSync(file, Buffer.from(shot.data, 'base64'));
console.log('✓', file);

ws.close();
proc.kill();
try { rmSync(profile, { recursive: true, force: true }); } catch (e) {}
process.exit(0);
