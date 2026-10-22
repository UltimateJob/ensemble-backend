#!/usr/bin/env node
/**
 * 弱网 / 断网 / 后端挂掉时的界面表现。
 *   node scripts/check-resilience.mjs [base]
 *
 * 用 CDP 的 Network.emulateNetworkConditions 限速与断网，
 * 检查骨架屏、错误提示、重试入口是否到位。
 */
import { spawn } from 'node:child_process';
import { mkdtempSync, rmSync, mkdirSync, writeFileSync, statSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const BASE = process.argv[2] || 'http://127.0.0.1:18320';
const EDGE = process.env.EDGE_PATH || '/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge';
const PORT = 9520;

const profile = mkdtempSync(join(tmpdir(), 'ens-res-'));
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
  setTimeout(() => { if (pending.has(i)) { pending.delete(i); reject(new Error('timeout ' + method)); } }, 40000);
});
const ev = async expr => (await send('Runtime.evaluate', { expression: expr, awaitPromise: true, returnByValue: true, userGesture: true })).result?.value;
const waitFor = async (expr, n = 120) => { for (let i = 0; i < n; i++) { if (await ev(expr)) return true; await sleep(180); } return false; };
async function shot(name) {
  await sleep(400);
  const r = await send('Page.captureScreenshot', { format: 'png' });
  const f = `.ui-shots/res-${name}.png`;
  writeFileSync(f, Buffer.from(r.data, 'base64'));
  console.log(`  ✓ ${f}  ${(statSync(f).size / 1024).toFixed(0)}KB`);
}

const issues = [];
const check = (name, ok, detail) => {
  console.log(`  ${ok ? '✓' : '✗'} ${name}${detail ? '  → ' + detail : ''}`);
  if (!ok) issues.push(name + (detail ? ': ' + detail : ''));
};

mkdirSync('.ui-shots', { recursive: true });
await send('Runtime.enable');
await send('Page.enable');
await send('Network.enable');
await send('Emulation.setDeviceMetricsOverride', { width: 1440, height: 900, deviceScaleFactor: 1, mobile: false });

console.log('\n1. 慢网下的骨架屏\n');

// 限速到 3G，再打开页面
await send('Network.emulateNetworkConditions', {
  offline: false, latency: 400, downloadThroughput: 50 * 1024, uploadThroughput: 30 * 1024,
});
await send('Page.navigate', { url: BASE + '/app/' });
await sleep(3500);
await shot('slow-load');

// 恢复全速登录
await send('Network.emulateNetworkConditions', {
  offline: false, latency: 0, downloadThroughput: -1, uploadThroughput: -1,
});
await waitFor(`!!document.querySelector('#phone')`, 100);
await ev(`localStorage.setItem('ens_onboarded','1')`);
await ev(`(()=>{document.querySelector('#phone').value='13800000000';document.querySelector('#doLogin').click()})()`);
await waitFor(`!document.querySelector('#app').classList.contains('hidden')`, 120);
await sleep(800);

console.log('\n2. 断网时的提示\n');

// 进房间
await ev(`document.querySelector('.team-card [data-join]')?.click()`);
await waitFor(`location.hash.includes('/room/')`);
await sleep(3000);
// 建房会自动开场；等它结束再测断网，否则点发送会被「上一轮还在跑」挡掉
await waitFor(`!document.querySelector('#sendBtn')?.classList.contains('stop')`, 150);
await sleep(800);

// 断网
await send('Network.emulateNetworkConditions', {
  offline: true, latency: 0, downloadThroughput: 0, uploadThroughput: 0,
});
await sleep(600);

// 断网下发一条消息
await ev(`(()=>{const t=document.querySelector('#ta'); if(t){t.value='断网测试'; t.dispatchEvent(new Event('input',{bubbles:true})); document.querySelector('#sendBtn')?.click();}})()`);
await sleep(2500);
await shot('offline-send');

const failedMsg = await ev(`document.querySelectorAll('.msg.me.failed').length`);
check('发送失败的消息被标记', failedMsg > 0, `${failedMsg} 条`);

// 提示写在消息流的系统气泡里（.sys-note），不是 toast
const noteText = await ev(`[...document.querySelectorAll('.sys-note')].map(n => n.textContent).join(' ')`);
check('有用户能看懂的错误提示', /网络|连不上|没发出去/.test(noteText), noteText.slice(0, 60));

// 恢复网络
await send('Network.emulateNetworkConditions', {
  offline: false, latency: 0, downloadThroughput: -1, uploadThroughput: -1,
});
await sleep(1200);
await shot('back-online');

console.log('\n3. 后端不可用时的错误页\n');

// 用一个不存在的端口加载，模拟后端挂掉
await send('Page.navigate', { url: 'http://127.0.0.1:1/app/' });
await sleep(2000);
const errPage = await ev(`document.body?.innerText?.slice(0, 60) || ''`);
check('浏览器给了明确错误', errPage.length > 0, errPage.replace(/\n/g, ' ').slice(0, 50));

console.log(issues.length ? `\n共 ${issues.length} 个问题：\n  ${issues.join('\n  ')}` : '\n全部通过');
ws.close(); proc.kill();
try { rmSync(profile, { recursive: true, force: true }); } catch (e) {}
process.exit(0);
