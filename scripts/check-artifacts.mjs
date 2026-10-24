#!/usr/bin/env node
/**
 * 检查消息流里有没有「假文件卡」。
 *
 * 历史缺陷：角色声明了产物类型（后端→接口定义），他说的每句话都会
 * 在消息流里再追加一张同内容的文件卡。表现是「一段话 + 一张卡片，
 * 点开还是那段话」。
 *
 *   node scripts/check-artifacts.mjs [base]
 */
import { spawn } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const BASE = process.argv[2] || 'http://127.0.0.1:18200';
const EDGE = process.env.EDGE_PATH || '/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge';
const PORT = 9551;
const profile = mkdtempSync(join(tmpdir(), 'ens-art-'));
const proc = spawn(EDGE, ['--headless=new', `--remote-debugging-port=${PORT}`,
  `--user-data-dir=${profile}`, '--no-first-run', '--disable-gpu', 'about:blank'], { stdio: 'ignore' });
const sleep = ms => new Promise(r => setTimeout(r, ms));
await sleep(2400);

const list = await (await fetch(`http://127.0.0.1:${PORT}/json/list`)).json();
const ws = new WebSocket(list.find(t => t.type === 'page').webSocketDebuggerUrl);
await new Promise(r => ws.addEventListener('open', r));
let id = 0; const pending = new Map();
ws.addEventListener('message', e => {
  const m = JSON.parse(e.data);
  if (m.id && pending.has(m.id)) { const p = pending.get(m.id); pending.delete(m.id); m.error ? p.reject(new Error(JSON.stringify(m.error))) : p.resolve(m.result); }
});
const send = (method, params = {}) => new Promise((resolve, reject) => {
  const i = ++id; pending.set(i, { resolve, reject });
  ws.send(JSON.stringify({ id: i, method, params }));
  setTimeout(() => { if (pending.has(i)) { pending.delete(i); reject(new Error('timeout ' + method)); } }, 30000);
});
const ev = async expr => (await send('Runtime.evaluate', { expression: expr, awaitPromise: true, returnByValue: true, userGesture: true })).result?.value;
const waitFor = async (expr, n = 100) => { for (let i = 0; i < n; i++) { if (await ev(expr)) return true; await sleep(200); } return false; };

await send('Runtime.enable');
await send('Page.enable');
await send('Emulation.setDeviceMetricsOverride', { width: 1440, height: 900, deviceScaleFactor: 1, mobile: false });
await send('Page.navigate', { url: BASE + '/app/' });
await sleep(2000);
await ev(`localStorage.setItem('ens_onboarded','1')`);
await ev(`(()=>{document.querySelector('#phone').value='13800000000';document.querySelector('#doLogin').click()})()`);
await waitFor(`!document.querySelector('#app').classList.contains('hidden')`);
await sleep(1200);
await ev(`document.querySelector('.onb-mask')?.remove()`);
await ev(`location.hash='#/lobby'`);
await sleep(700);

// 进代码工作室
await ev(`(()=>{const cards=[...document.querySelectorAll('.team-card')];
  const c=cards.find(x=>x.textContent.includes('代码工作室')); (c?.querySelector('[data-join]')||c)?.click();})()`);
await waitFor(`location.hash.includes('/room/')`);
await sleep(3000);
await ev(`document.querySelector('.onb-mask')?.remove()`);

const report = await ev(`(()=>{
  const wrap = document.querySelector('#msgWrap');
  if (!wrap) return JSON.stringify({error:'没有消息流'});
  const cards = [...wrap.querySelectorAll('.artifact')];
  const msgs = [...wrap.querySelectorAll('.msg')];
  // 找「卡片标题与正文高度雷同」的情况
  const dup = cards.map(c => {
    const title = c.querySelector('.an')?.textContent || '';
    const detail = c.querySelector('.ad')?.textContent || '';
    return { title, detail: detail.slice(0, 40) };
  });
  return JSON.stringify({
    messages: msgs.length,
    fakeCards: cards.length,
    cards: dup,
  }, null, 1);
})()`);

console.log(report);
const shot = await send("Page.captureScreenshot", { format: "png" });
  const fs = await import("node:fs");
  fs.writeFileSync(".ui-shots/check-artifacts.png", Buffer.from(shot.data, "base64"));
  console.log("  ✓ .ui-shots/check-artifacts.png");
ws.close(); proc.kill(); await sleep(600);
try { rmSync(profile, { recursive: true, force: true, maxRetries: 3 }); } catch {}
