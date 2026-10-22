#!/usr/bin/env node
/**
 * 用真实渲染结果做对比度检查：遍历所有可见文字节点，
 * 算它相对实际背景色的对比度，报出低于 WCAG AA 的。
 *   node scripts/check-contrast.mjs [base]
 */
import { spawn } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const BASE = process.argv[2] || 'http://127.0.0.1:18300';
const EDGE = process.env.EDGE_PATH || '/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge';
const PORT = 9515;

const profile = mkdtempSync(join(tmpdir(), 'ens-ct-'));
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
const waitFor = async (expr, n = 100) => { for (let i = 0; i < n; i++) { if (await ev(expr)) return true; await sleep(180); } return false; };

// 在页面里注入一个对比度扫描器
const SCAN = `(() => {
  function parse(c) {
    const m = c.match(/rgba?\\(([^)]+)\\)/);
    if (!m) return null;
    const p = m[1].split(',').map(v => parseFloat(v));
    return { r: p[0], g: p[1], b: p[2], a: p.length > 3 ? p[3] : 1 };
  }
  function lum(c) {
    const f = v => { v /= 255; return v <= .03928 ? v / 12.92 : Math.pow((v + .055) / 1.055, 2.4); };
    return .2126 * f(c.r) + .7152 * f(c.g) + .0722 * f(c.b);
  }
  function ratio(a, b) {
    const la = lum(a), lb = lum(b);
    return (Math.max(la, lb) + .05) / (Math.min(la, lb) + .05);
  }
  // 背景色要一路往上找，遇到透明就继续
  function bgOf(el) {
    let n = el;
    while (n && n !== document.documentElement) {
      const cs = getComputedStyle(n);
      // 渐变背景（头像、进度条）的 backgroundColor 是透明的，
      // 但实际渲染出来的颜色来自 gradient。算不出来就跳过这一项，
      // 否则会把白字压纸色渐变误判成白底白字。
      if (cs.backgroundImage && cs.backgroundImage !== 'none') return null;
      const c = parse(cs.backgroundColor);
      if (c && c.a > .85) return c;
      n = n.parentElement;
    }
    const body = parse(getComputedStyle(document.body).backgroundColor);
    return body || { r: 255, g: 255, b: 255, a: 1 };
  }
  const bad = [];
  const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
  const seen = new Set();
  while (walker.nextNode()) {
    const node = walker.currentNode;
    const text = (node.textContent || '').trim();
    if (!text || seen.has(node.parentElement)) continue;
    const el = node.parentElement;
    seen.add(el);
    if (!el || el.offsetParent === null) continue;
    const cs = getComputedStyle(el);
    if (cs.visibility === 'hidden' || parseFloat(cs.opacity) < .5) continue;
    const fg = parse(cs.color);
    if (!fg) continue;
    const bg = bgOf(el);
    if (!bg) continue;   // 渐变背景，无法可靠计算
    const r = ratio(fg, bg);
    const size = parseFloat(cs.fontSize);
    const bold = parseInt(cs.fontWeight, 10) >= 600;
    // WCAG AA：正文 4.5，大号（>=18.66px 或 14px 粗体）3.0
    const need = (size >= 18.66 || (bold && size >= 14)) ? 3.0 : 4.5;
    if (r < need - 0.05) {
      bad.push({
        text: text.slice(0, 24),
        cls: (el.className || '').toString().slice(0, 40),
        color: cs.color, bg: \`rgb(\${bg.r},\${bg.g},\${bg.b})\`,
        ratio: Math.round(r * 100) / 100, need, size,
      });
    }
  }
  return bad;
})()`;

await send('Runtime.enable');
await send('Page.enable');
await send('Emulation.setDeviceMetricsOverride', { width: 1440, height: 900, deviceScaleFactor: 1, mobile: false });
await send('Page.navigate', { url: BASE + '/app/' });
await sleep(2200);

const all = [];
const scan = async (label) => {
  await sleep(700);
  const bad = await ev(SCAN);
  if (bad?.length) {
    console.log(`\n${label}：${bad.length} 处`);
    bad.slice(0, 8).forEach(b => console.log(`  ${b.ratio} < ${b.need}  「${b.text}」  ${b.color} on ${b.bg}  .${b.cls}`));
    all.push(...bad.map(b => ({ ...b, page: label })));
  } else {
    console.log(`${label}：通过`);
  }
};

await scan('登录页');

await ev(`localStorage.setItem('ens_onboarded','1')`);
await ev(`(()=>{document.querySelector('#phone').value='13800000000';document.querySelector('#doLogin').click()})()`);
await waitFor(`!document.querySelector('#app').classList.contains('hidden')`);
await sleep(1200);
await ev(`document.querySelector('.onb-mask')?.remove()`);
await scan('团队大厅');

for (const v of ['goals', 'connectors', 'library', 'settings']) {
  await ev(`location.hash='#/${v}'`);
  await sleep(1200);
  await scan(v);
}

await ev(`document.querySelector('.team-card [data-join]')?.click()`);
await waitFor(`location.hash.includes('/room/')`);
await sleep(2600);
await scan('聊天房间');

await ev(`document.querySelector('#roomPanel')?.click()`);
await sleep(1000);
await scan('右面板');
await ev(`document.querySelector('#panelClose')?.click()`);

console.log(all.length ? `\n合计 ${all.length} 处低于 AA` : '\n全部达到 WCAG AA');

ws.close(); proc.kill();
try { rmSync(profile, { recursive: true, force: true }); } catch (e) {}
process.exit(0);
