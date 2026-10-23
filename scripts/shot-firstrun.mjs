#!/usr/bin/env node
/**
 * 新用户首次使用路径：登录 → 空大厅 → 建房 → 空房间引导 → 第一句话。
 *   node scripts/shot-firstrun.mjs [base]
 */
import { spawn } from 'node:child_process';
import { mkdtempSync, rmSync, mkdirSync, writeFileSync, statSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const BASE = process.argv[2] || 'http://127.0.0.1:18280';
const EDGE = process.env.EDGE_PATH || '/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge';
const PORT = 9502;

const profile = mkdtempSync(join(tmpdir(), 'ens-fr-'));
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
  await sleep(700);
  const r = await send('Page.captureScreenshot', { format: 'png' });
  const f = `.ui-shots/firstrun-${name}.png`;
  writeFileSync(f, Buffer.from(r.data, 'base64'));
  console.log(`✓ ${f}  ${(statSync(f).size / 1024).toFixed(0)}KB`);
}

mkdirSync('.ui-shots', { recursive: true });
await send('Runtime.enable');
await send('Page.enable');
await send('Emulation.setDeviceMetricsOverride', { width: 1440, height: 900, deviceScaleFactor: 1, mobile: false });
await send('Page.navigate', { url: BASE + '/app/' });
await sleep(2000);

// 用全新的手机号，模拟真正的第一次
await ev(`localStorage.clear()`);
await send('Page.reload');
await sleep(1800);
await shot('01-login');

await ev(`localStorage.setItem('ens_onboarded','1')`);
// 新号码要验证码；本地模式会把码回显在 devCode 里
await ev(`(()=>{document.querySelector('#phone').value='13700009999';document.querySelector('#sendCode').click()})()`);
await sleep(1200);
const code = await ev(`document.querySelector('#code')?.value || ''`);
console.log('验证码：', code || '(没拿到)');
await ev(`document.querySelector('#doLogin').click()`);
await waitFor(`!document.querySelector('#app').classList.contains('hidden')`);
await sleep(1400);
await ev(`document.querySelector('.onb-mask')?.remove()`);
await shot('02-lobby-empty');

// 建房：点「加入团队」
await ev(`document.querySelector('.team-card [data-join]')?.click()`);
await waitFor(`location.hash.includes('/room/')`);
// 建房后会自动 kickoff，先截开场前的瞬间
await sleep(400);
await shot('03-room-opening');
await sleep(4000);
await shot('04-room-talking');

// 点一个话头（如果有）
const seeds = await ev(`document.querySelectorAll('[data-seed]').length`);
console.log('话头数量：', seeds);

// 回大厅看「我的团队」
await ev(`location.hash='#/lobby'`);
await sleep(1400);
await shot('05-lobby-with-team');

console.log(errors.length ? '错误：\n' + errors.slice(0, 5).join('\n') : '控制台：无错误');
ws.close(); proc.kill();
try { rmSync(profile, { recursive: true, force: true }); } catch (e) {}
process.exit(0);
