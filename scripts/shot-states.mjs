#!/usr/bin/env node
/**
 * 截「进行中」的状态：发送中、流式输出、审批弹窗、工作室时间线。
 *   node scripts/shot-states.mjs [base]
 */
import { spawn } from 'node:child_process';
import { mkdtempSync, rmSync, mkdirSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const BASE = process.argv[2] || 'http://127.0.0.1:18250';
const EDGE = process.env.EDGE_PATH || '/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge';
const PORT = 9490;

const profile = mkdtempSync(join(tmpdir(), 'ens-st-'));
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
  await sleep(400);
  const r = await send('Page.captureScreenshot', { format: 'png' });
  const f = `.ui-shots/state-${name}.png`;
  writeFileSync(f, Buffer.from(r.data, 'base64'));
  const { statSync } = await import('node:fs');
  console.log(`✓ ${f}  ${(statSync(f).size / 1024).toFixed(0)}KB`);
}

mkdirSync('.ui-shots', { recursive: true });
await send('Runtime.enable');
await send('Page.enable');
await send('Emulation.setDeviceMetricsOverride', { width: 1440, height: 900, deviceScaleFactor: 1, mobile: false });
await send('Page.navigate', { url: BASE + '/app/' });
await sleep(2000);
await ev(`localStorage.setItem('ens_onboarded','1')`);
await ev(`(()=>{document.querySelector('#phone').value='13800000000';document.querySelector('#doLogin').click()})()`);
await waitFor(`!document.querySelector('#app').classList.contains('hidden')`);
await sleep(600);

// 建房进房间
await ev(`document.querySelector('.team-card [data-join]')?.click()`);
await waitFor(`location.hash.includes('/room/')`);
await sleep(2500);

// 等开场结束
await waitFor(`!document.querySelector('#sendBtn')?.classList.contains('stop')`, 120);
await sleep(600);

// 发送一条，截「思考中」
await ev(`(()=>{const t=document.querySelector('#ta'); t.value='给我拆一下这次投标的关键节点'; t.dispatchEvent(new Event('input',{bubbles:true})); document.querySelector('#sendBtn').click();})()`);
await sleep(700);
await shot('thinking');

// 等流式中途截一张
await waitFor(`!!document.querySelector('.msg-text.md')`, 60);
await sleep(900);
await shot('streaming');

// 等这一轮结束
await waitFor(`!document.querySelector('#sendBtn')?.classList.contains('stop')`, 200);
await sleep(800);
await shot('round-done');

// 工作室视图
await ev(`location.hash = location.hash.split('?')[0] + '?view=studio'`);
await sleep(2000);
await shot('studio');

// 资料库（现在应该有产物了）
await ev(`location.hash='#/library'`);
await sleep(1600);
await shot('library-filled');

console.log(errors.length ? '错误：\n' + errors.slice(0, 5).join('\n') : '控制台：无错误');
ws.close(); proc.kill();
try { rmSync(profile, { recursive: true, force: true }); } catch (e) {}
process.exit(0);
