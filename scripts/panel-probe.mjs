#!/usr/bin/env node
/**
 * 右面板 / 审批弹窗 / 工作面板探针。
 * 用法：node scripts/panel-probe.mjs [base]
 */
import { spawn } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const BASE = process.argv[2] || process.env.PROBE_BASE || 'http://127.0.0.1:8094';
const EDGE = process.env.EDGE_PATH || '/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge';
const PORT = 9446;

const profile = mkdtempSync(join(tmpdir(), 'ens-panel-'));
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
});
const send = (method, params = {}) => new Promise((resolve, reject) => {
  const i = ++id;
  pending.set(i, { resolve, reject });
  ws.send(JSON.stringify({ id: i, method, params }));
  setTimeout(() => { if (pending.has(i)) { pending.delete(i); reject(new Error('timeout ' + method)); } }, 20000);
});
const ev = async expr => (await send('Runtime.evaluate', { expression: expr, awaitPromise: true, returnByValue: true, userGesture: true })).result?.value;
const waitFor = async (expr, tries = 80) => {
  for (let i = 0; i < tries; i++) { if (await ev(expr)) return true; await sleep(200); }
  return false;
};

await send('Runtime.enable');
await send('Page.enable');
await send('Page.navigate', { url: BASE + '/app/' });
await sleep(2000);
await ev(`(()=>{document.querySelector('#phone').value='13800000000';document.querySelector('#doLogin').click()})()`);
await waitFor(`!document.querySelector('#app').classList.contains('hidden')`);
await ev(`localStorage.setItem('ens_onboarded','1')`);
await sleep(600);

// 建房进房间
await ev(`document.querySelector('[data-join]')?.click()`);
await waitFor(`location.hash.includes('/room/')`);
await sleep(2000);
console.log('room:', await ev('location.hash'));

// 右面板：点房间概览按钮
await ev(`document.querySelector('#roomPanel')?.click()`);
await sleep(900);
console.log('panel open:', await ev(`document.querySelector('#panel')?.classList.contains('on')`));
console.log('panel title:', await ev(`document.querySelector('.panel-title')?.textContent`));
console.log('member list:', await ev(`document.querySelectorAll('#memberList .member').length`));
console.log('app grid class:', await ev(`document.querySelector('#app')?.className`));

// 点开一个角色头像 → Agent 工作面板
await ev(`document.querySelector('#avatarRow .av-cell')?.click()`);
await sleep(1200);
console.log('agent panel title:', await ev(`document.querySelector('.panel-title')?.textContent`));
console.log('agent panel task:', await ev(`document.querySelector('#apTask')?.textContent?.trim().slice(0,40)`));
console.log('agent panel cmd:', await ev(`!!document.querySelector('#apCmd')`));

// 记忆面板
await ev(`document.querySelector('#roomMemory')?.click()`);
await sleep(1000);
console.log('memory panel:', await ev(`document.querySelector('.panel-title')?.textContent`));

// 设置页 + modal
await ev(`location.hash='#/settings'`);
await sleep(1000);
console.log('settings profile:', await ev(`document.querySelector('.pf-name')?.textContent`));
await ev(`document.querySelector('[data-act=editName]')?.click()`);
await sleep(700);
console.log('modal open:', await ev(`!!document.querySelector('.modal-mask.on')`));
console.log('modal title:', await ev(`document.querySelector('.modal-head h3')?.textContent`));
await ev(`document.querySelector('[data-x=no]')?.click()`);
await sleep(400);
console.log('modal closed:', await ev(`!document.querySelector('.modal-mask.on')`));

// 目标：新建 → 卡片
await ev(`location.hash='#/goals'`);
await sleep(900);
await ev(`document.querySelector('#newGoal')?.click()`);
await sleep(700);
console.log('goal panel title:', await ev(`document.querySelector('.panel-title')?.textContent`));
await ev(`document.querySelector('#gTitle') && (document.querySelector('#gTitle').value='测试目标')`);
await ev(`document.querySelector('#gSave')?.click()`);
await sleep(1500);
console.log('goal cards:', await ev(`document.querySelectorAll('.goal-card').length`));

console.log('--- exceptions ---');
console.log(logs.length ? logs.join('\n') : '(none)');

ws.close();
proc.kill();
try { rmSync(profile, { recursive: true, force: true }); } catch (e) { /* 忽略 */ }
process.exit(0);
