#!/usr/bin/env node
/**
 * 单独截新手引导：全新 profile（没有 localStorage 标记），登录后等它出现。
 *   node scripts/shot-onboarding.mjs [base]
 */
import { spawn } from 'node:child_process';
import { mkdtempSync, rmSync, mkdirSync, writeFileSync, statSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const BASE = process.argv[2] || 'http://127.0.0.1:18330';
const EDGE = process.env.EDGE_PATH || '/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge';
const PORT = 9525;

const profile = mkdtempSync(join(tmpdir(), 'ens-onb-'));
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
  setTimeout(() => { if (pending.has(i)) { pending.delete(i); reject(new Error('timeout ' + method)); } }, 30000);
});
const ev = async expr => (await send('Runtime.evaluate', { expression: expr, awaitPromise: true, returnByValue: true, userGesture: true })).result?.value;
const waitFor = async (expr, n = 120) => { for (let i = 0; i < n; i++) { if (await ev(expr)) return true; await sleep(180); } return false; };
async function shot(name) {
  await sleep(400);
  const r = await send('Page.captureScreenshot', { format: 'png' });
  const f = `.ui-shots/onb-${name}.png`;
  writeFileSync(f, Buffer.from(r.data, 'base64'));
  console.log(`✓ ${f}  ${(statSync(f).size / 1024).toFixed(0)}KB`);
}

mkdirSync('.ui-shots', { recursive: true });
await send('Runtime.enable');
await send('Page.enable');

for (const [w, h, tag] of [[1440, 900, '1440'], [390, 844, '390']]) {
  await send('Emulation.setDeviceMetricsOverride', {
    width: w, height: h, deviceScaleFactor: 1, mobile: w < 600,
  });
  await send('Page.navigate', { url: BASE + '/app/' });
  await sleep(1800);
  // 全新上下文：清掉所有存储，让引导重新出现
  await ev(`localStorage.clear(); sessionStorage.clear()`);
  await send('Page.reload');
  await sleep(1800);
  await ev(`(()=>{document.querySelector('#phone').value='13800000000';document.querySelector('#doLogin').click()})()`);
  await waitFor(`!document.querySelector('#app').classList.contains('hidden')`);
  // 引导在登录后 900ms 出现
  const appeared = await waitFor(`!!document.querySelector('.onb-mask')`, 60);
  console.log(`${tag} 引导出现：`, appeared);
  await shot(`${tag}-step1`);

  // 下一步
  const next = await ev(`!!document.querySelector('[data-x=next]')`);
  if (next) {
    await ev(`document.querySelector('[data-x=next]').click()`);
    await sleep(1200);
    await shot(`${tag}-step2`);
  }
  // 清理，避免影响下一轮
  await ev(`document.querySelector('.onb-mask')?.remove(); localStorage.setItem('ens_onboarded','1')`);
}

console.log(errors.length ? '错误：\n' + errors.slice(0, 3).join('\n') : '控制台：无错误');
ws.close(); proc.kill();
try { rmSync(profile, { recursive: true, force: true }); } catch (e) {}
process.exit(0);
