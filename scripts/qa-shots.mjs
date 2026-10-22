#!/usr/bin/env node
/**
 * 企业级验收截图：每个页面 × 每个关键状态 × 三种宽度。
 *
 *   ./scripts/serve-test.sh start 18220
 *   node scripts/qa-shots.mjs http://127.0.0.1:18220
 *
 * 产出 .ui-shots/qa/<slug>.png，并统计每张图大小以发现空白页。
 */
import { spawn } from 'node:child_process';
import { mkdtempSync, rmSync, mkdirSync, writeFileSync, statSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const BASE = process.argv[2] || process.env.QA_BASE || 'http://127.0.0.1:18220';
const EDGE = process.env.EDGE_PATH || '/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge';
const PORT = 9480;
const OUT = '.ui-shots/qa';

const profile = mkdtempSync(join(tmpdir(), 'ens-qa-'));
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
const errors = [];
ws.addEventListener('message', e => {
  const m = JSON.parse(e.data);
  if (m.id && pending.has(m.id)) {
    const p = pending.get(m.id); pending.delete(m.id);
    m.error ? p.reject(new Error(JSON.stringify(m.error))) : p.resolve(m.result);
  }
  if (m.method === 'Runtime.exceptionThrown') {
    errors.push(m.params.exceptionDetails.exception?.description || m.params.exceptionDetails.text);
  }
});
const send = (method, params = {}) => new Promise((resolve, reject) => {
  const i = ++id; pending.set(i, { resolve, reject });
  ws.send(JSON.stringify({ id: i, method, params }));
  setTimeout(() => { if (pending.has(i)) { pending.delete(i); reject(new Error('timeout ' + method)); } }, 45000);
});
const ev = async expr => (await send('Runtime.evaluate', { expression: expr, awaitPromise: true, returnByValue: true, userGesture: true })).result?.value;
const waitFor = async (expr, n = 100) => { for (let i = 0; i < n; i++) { if (await ev(expr)) return true; await sleep(150); } return false; };

async function viewport(w, h = 900, mobile = false) {
  await send('Emulation.setDeviceMetricsOverride', { width: w, height: h, deviceScaleFactor: 1, mobile });
}
async function shot(name) {
  // 页面入场动画 + 错峰延迟最多约 0.5s，等它跑完再截，避免截到半透明状态
  await sleep(900);
  const r = await send('Page.captureScreenshot', { format: 'png' });
  const file = join(OUT, `${name}.png`);
  const buf = Buffer.from(r.data, 'base64');
  writeFileSync(file, buf);
  return Number((statSync(file).size / 1024).toFixed(0));
}

mkdirSync(OUT, { recursive: true });
await send('Runtime.enable');
await send('Page.enable');

const results = [];
const cap = async (name) => results.push([name, await shot(name)]);

/* 登录 */
await viewport(1440);
await send('Page.navigate', { url: BASE + '/app/' });
await sleep(2200);
await ev(`localStorage.clear()`);
await send('Page.reload');
await sleep(2000);
await cap('1440-login');

await ev(`(()=>{const e=document.querySelector('#loginErr');if(e){e.textContent='验证码不正确';e.classList.remove('hidden');}})()`);
await cap('1440-login-error');

/* 进大厅 */
await send('Page.reload');
await sleep(1600);
await ev(`localStorage.setItem('ens_onboarded','1')`);
await ev(`(()=>{document.querySelector('#phone').value='13800000000';document.querySelector('#doLogin').click()})()`);
await waitFor(`!document.querySelector('#app').classList.contains('hidden')`);
await sleep(1200);
await cap('1440-lobby');

/* 引导 */
await ev(`localStorage.removeItem('ens_onboarded'); location.hash='#/lobby'`);
await sleep(1600);
await cap('1440-onboarding');
await ev(`document.querySelector('.onb-mask')?.remove(); localStorage.setItem('ens_onboarded','1')`);

/* 房间 */
await ev(`document.querySelector('.team-card [data-join]')?.click()`);
await waitFor(`location.hash.includes('/room/')`);
await sleep(2600);
await cap('1440-room');

await ev(`document.querySelector('#roomPanel')?.click()`);
await sleep(900);
await cap('1440-room-panel');

await ev(`document.querySelector('#avatarRow .av-cell')?.click()`);
await sleep(1400);
await cap('1440-agent-panel');

await ev(`document.querySelector('#roomMemory')?.click()`);
await sleep(1000);
await cap('1440-memory-panel');

await ev(`document.querySelector('#roomSettings')?.click()`);
await sleep(1000);
await cap('1440-team-settings');
await ev(`document.querySelector('#panelClose')?.click()`);

/* 四个主页面 */
for (const v of ['goals', 'connectors', 'library', 'settings']) {
  await ev(`location.hash='#/${v}'`);
  await sleep(1500);
  await cap(`1440-${v}`);
}

/* 目标：新建 + 有数据 */
await ev(`location.hash='#/goals'`); await sleep(1200);
await ev(`document.querySelector('#newGoal')?.click()`); await sleep(900);
await cap('1440-goal-new');
await ev(`(()=>{const i=document.querySelector('#gTitle');if(i){i.value='园区改造标书交付';i.dispatchEvent(new Event('input',{bubbles:true}))}})()`);
await ev(`document.querySelector('#gSave')?.click()`); await sleep(1600);
await cap('1440-goals-filled');

/* 连接器：新建 */
await ev(`location.hash='#/connectors'`); await sleep(1200);
await ev(`document.querySelector('#newConn')?.click()`); await sleep(900);
await cap('1440-connector-new');

/* 404 + 弹窗 */
await ev(`location.hash='#/nope'`); await sleep(900);
await cap('1440-404');
await ev(`location.hash='#/settings'`); await sleep(1000);
await ev(`document.querySelector('[data-act=editName]')?.click()`); await sleep(800);
await cap('1440-modal');
await ev(`document.querySelector('[data-x=no]')?.click()`); await sleep(400);

/* 平板 */
await viewport(1180);
for (const v of ['lobby', 'goals', 'settings']) {
  await ev(`location.hash='#/${v}'`); await sleep(1300);
  await cap(`1180-${v}`);
}

/* 手机 */
await viewport(390, 844, true);
for (const v of ['lobby', 'goals', 'library', 'settings']) {
  await ev(`location.hash='#/${v}'`); await sleep(1300);
  await cap(`390-${v}`);
}

/* 报告 */
console.log('\n截图完成：\n');
const small = [];
for (const [name, kb] of results) {
  const flag = kb < 40 ? '  ⚠ 偏空' : '';
  console.log(`  ${name.padEnd(26)} ${String(kb).padStart(5)} KB${flag}`);
  if (kb < 40) small.push(name);
}
if (small.length) console.log(`\n偏空：${small.join(', ')}`);
console.log(errors.length ? `\n控制台错误：\n  ${errors.slice(0, 10).join('\n  ')}` : '\n控制台：无错误');

ws.close(); proc.kill();
try { rmSync(profile, { recursive: true, force: true }); } catch (e) {}
process.exit(0);
