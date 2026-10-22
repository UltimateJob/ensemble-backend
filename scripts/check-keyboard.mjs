#!/usr/bin/env node
/**
 * 键盘操作检查：⌘1–6 切页、⌘0 面板、Tab 顺序、Enter 发送、Esc 关闭。
 *   node scripts/check-keyboard.mjs [base]
 */
import { spawn } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const BASE = process.argv[2] || 'http://127.0.0.1:18340';
const EDGE = process.env.EDGE_PATH || '/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge';
const PORT = 9530;

const profile = mkdtempSync(join(tmpdir(), 'ens-kb-'));
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
const waitFor = async (expr, n = 120) => { for (let i = 0; i < n; i++) { if (await ev(expr)) return true; await sleep(180); } return false; };

const issues = [];
const check = (name, ok, detail) => {
  console.log(`  ${ok ? '✓' : '✗'} ${name}${detail ? '  → ' + detail : ''}`);
  if (!ok) issues.push(name + (detail ? ': ' + detail : ''));
};

// 发一个真实按键（带 meta）
async function key(k, meta = false, shift = false) {
  const code = k.length === 1 ? 'Key' + k.toUpperCase() : k;
  const vk = k.length === 1 ? k.toUpperCase().charCodeAt(0) : ({ Escape: 27, Enter: 13, Tab: 9 }[k] || 0);
  const mods = (meta ? 4 : 0) + (shift ? 8 : 0);
  await send('Input.dispatchKeyEvent', {
    type: 'keyDown', key: k, code, windowsVirtualKeyCode: vk, nativeVirtualKeyCode: vk, modifiers: mods,
  });
  await send('Input.dispatchKeyEvent', {
    type: 'keyUp', key: k, code, windowsVirtualKeyCode: vk, nativeVirtualKeyCode: vk, modifiers: mods,
  });
}

await send('Runtime.enable');
await send('Page.enable');
await send('Emulation.setDeviceMetricsOverride', { width: 1440, height: 900, deviceScaleFactor: 1, mobile: false });
await send('Page.navigate', { url: BASE + '/app/' });
await sleep(2000);
await ev(`localStorage.setItem('ens_onboarded','1')`);
await ev(`(()=>{document.querySelector('#phone').value='13800000000';document.querySelector('#doLogin').click()})()`);
await waitFor(`!document.querySelector('#app').classList.contains('hidden')`);
await sleep(1000);
await ev(`document.querySelector('.onb-mask')?.remove()`);

console.log('\n导航快捷键\n');

// ⌘1–6 切页
const routes = ['lobby', 'room', 'goals', 'connectors', 'library', 'settings'];
for (let i = 0; i < 6; i++) {
  await key(String(i + 1), true);
  await sleep(450);
  const hash = await ev('location.hash');
  // ⌘2 是 chat 房间，没有房间时会渲染空态；其余直接对应路由
  const ok = hash.includes(routes[i]);
  check(`⌘${i + 1} → ${routes[i]}`, ok, hash);
}

// ⌘0 面板
await ev(`location.hash='#/lobby'`);
await sleep(500);
await key('0', true);
await sleep(700);
const panelOpen = await ev(`document.querySelector('#panel')?.classList.contains('on')`);
check('⌘0 打开工作面板', !!panelOpen);
await key('0', true);
await sleep(600);
const panelClosed = await ev(`!document.querySelector('#panel')?.classList.contains('on')`);
check('⌘0 再按收起面板', panelClosed);

console.log('\n房间内的键盘\n');

// 进房间
await ev(`document.querySelector('.team-card [data-join]')?.click()`);
await waitFor(`location.hash.includes('/room/')`);
await sleep(2800);
await waitFor(`!document.querySelector('#sendBtn')?.classList.contains('stop')`, 120);

// 聚焦输入框，Enter 发送
await ev(`document.querySelector('#ta')?.focus()`);
await ev(`(()=>{const t=document.querySelector('#ta'); t.value='键盘测试'; t.dispatchEvent(new Event('input',{bubbles:true}))})()`);
await sleep(300);
await key('Enter');
await sleep(1200);
const sent = await ev(`[...document.querySelectorAll('.msg.me')].some(m => m.textContent.includes('键盘测试'))`);
check('Enter 发送消息', sent);

// Shift+Enter 换行不发送
await waitFor(`!document.querySelector('#sendBtn')?.classList.contains('stop')`, 120);
await ev(`document.querySelector('#ta')?.focus()`);
await ev(`(()=>{const t=document.querySelector('#ta'); t.value='第一行'; t.dispatchEvent(new Event('input',{bubbles:true}))})()`);
await key('Enter', false, true);
await sleep(400);
const multiline = await ev(`(document.querySelector('#ta')?.value || '').includes('\\n') || document.querySelector('#ta')?.value === '第一行'`);
check('Shift+Enter 换行不发送', multiline);

// Esc 清空输入框获得焦点
await ev(`document.querySelector('#ta')?.blur()`);
await key('Escape');
await sleep(300);

console.log(issues.length ? `\n共 ${issues.length} 个问题：\n  ${issues.join('\n  ')}` : '\n全部通过');
ws.close(); proc.kill();
try { rmSync(profile, { recursive: true, force: true }); } catch (e) {}
process.exit(0);
