#!/usr/bin/env node
/**
 * 快速探针：登录 → 大厅 → 房间 → 各页面，打印关键 DOM 与所有控制台错误。
 * 用法：node scripts/probe.mjs [base]
 */
import { spawn } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const BASE = process.argv[2] || process.env.PROBE_BASE || 'http://127.0.0.1:8091';
const EDGE = process.env.EDGE_PATH || '/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge';
const PORT = 9444;

const profile = mkdtempSync(join(tmpdir(), 'ens-probe-'));
const proc = spawn(EDGE, [
  '--headless=new', `--remote-debugging-port=${PORT}`, `--user-data-dir=${profile}`,
  '--no-first-run', '--disable-gpu', 'about:blank',
], { stdio: 'ignore' });

const sleep = ms => new Promise(r => setTimeout(r, ms));
await sleep(2500);

const list = await (await fetch(`http://127.0.0.1:${PORT}/json/list`)).json();
const target = list.find(t => t.type === 'page');
const ws = new WebSocket(target.webSocketDebuggerUrl);
await new Promise(r => ws.addEventListener('open', r));

let id = 0;
const pending = new Map();
const logs = [];

ws.addEventListener('message', e => {
  const m = JSON.parse(e.data);
  if (m.id && pending.has(m.id)) {
    const p = pending.get(m.id);
    pending.delete(m.id);
    m.error ? p.reject(new Error(JSON.stringify(m.error))) : p.resolve(m.result);
  }
  if (m.method === 'Runtime.exceptionThrown') {
    logs.push('EXC ' + (m.params.exceptionDetails.exception?.description || m.params.exceptionDetails.text));
  }
  if (m.method === 'Runtime.consoleAPICalled' && m.params.type === 'error') {
    logs.push('ERR ' + m.params.args.map(a => a.value ?? a.description).join(' '));
  }
  if (m.method === 'Log.entryAdded' && m.params.entry.level === 'error') {
    logs.push('LOG ' + m.params.entry.text + ' ' + (m.params.entry.url || ''));
  }
});

function send(method, params = {}) {
  return new Promise((resolve, reject) => {
    const i = ++id;
    pending.set(i, { resolve, reject });
    ws.send(JSON.stringify({ id: i, method, params }));
    setTimeout(() => {
      if (pending.has(i)) { pending.delete(i); reject(new Error('timeout ' + method)); }
    }, 20000);
  });
}

async function ev(expr) {
  const r = await send('Runtime.evaluate', { expression: expr, awaitPromise: true, returnByValue: true, userGesture: true });
  return r.result?.value;
}

async function waitFor(expr, tries = 60) {
  for (let i = 0; i < tries; i++) {
    if (await ev(expr)) return true;
    await sleep(250);
  }
  return false;
}

await send('Runtime.enable');
await send('Log.enable');
await send('Page.enable');
await send('Page.navigate', { url: BASE + '/app/' });
await sleep(2500);

// 房间跳转后页面重渲染，探测前先等 URL 稳定
async function waitHash(part, tries = 80) {
  for (let i = 0; i < tries; i++) {
    if ((await ev('location.hash'))?.includes(part)) return true;
    await sleep(250);
  }
  return false;
}

console.log('login visible:', await ev(`!document.querySelector('#login').classList.contains('hidden')`));
await ev(`(()=>{document.querySelector('#phone').value='13800000000';document.querySelector('#doLogin').click()})()`);
const entered = await waitFor(`!document.querySelector('#app').classList.contains('hidden')`);
console.log('entered app:', entered);
// 引导会在 900ms 后强制跳转，先把标记置上，免得和探测抢路由
await ev(`localStorage.setItem('ens_onboarded','1')`);
console.log('hash:', await ev('location.hash'));
console.log('nav items:', await ev(`document.querySelectorAll('#navList .nav-item').length`));
console.log('team cards:', await ev(`document.querySelectorAll('.team-card').length`));
console.log('page title:', await ev(`document.querySelector('.page-head h1')?.textContent`));
console.log('logo loaded:', await ev(`document.querySelector('.nav-logo')?.naturalWidth > 0`));

await ev(`document.querySelector('[data-join]')?.click()`);
const inRoom = await waitHash('/room/', 120);
await sleep(2500);
console.log('in room:', inRoom, await ev('location.hash'));
console.log('avatars:', await ev(`document.querySelectorAll('#avatarRow .av-cell').length`));
console.log('msgWrap:', await ev(`!!document.querySelector('#msgWrap')`));
console.log('composer:', await ev(`!!document.querySelector('#ta')`));
console.log('room title:', await ev(`document.querySelector('.rh-title')?.textContent`));

for (const v of ['goals', 'connectors', 'library', 'settings']) {
  await ev(`location.hash='#/${v}'`);
  await sleep(1400);
  const h1 = await ev(`document.querySelector('.page-head h1')?.textContent || document.querySelector('.es-t')?.textContent`);
  console.log(v + ':', h1);
}

await ev(`location.hash='#/nope'`);
await sleep(900);
console.log('404:', await ev(`document.querySelector('.es-t')?.textContent`));

console.log('--- console errors ---');
console.log(logs.length ? logs.join('\n') : '(none)');

ws.close();
proc.kill();
// Chrome 退出时还在写 profile，删不掉不影响探测结果
try { rmSync(profile, { recursive: true, force: true }); } catch (e) { /* 忽略 */ }
process.exit(0);
