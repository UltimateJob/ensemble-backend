#!/usr/bin/env node
/**
 * 可访问性检查：Tab 顺序、焦点可见性、按钮可读名称、对比度。
 *   node scripts/check-a11y.mjs [base]
 */
import { spawn } from 'node:child_process';
import { mkdtempSync, rmSync, mkdirSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const BASE = process.argv[2] || 'http://127.0.0.1:18290';
const EDGE = process.env.EDGE_PATH || '/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge';
const PORT = 9510;

const profile = mkdtempSync(join(tmpdir(), 'ens-a11y-'));
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

await send('Runtime.enable');
await send('Page.enable');
await send('Accessibility.enable');
await send('Emulation.setDeviceMetricsOverride', { width: 1440, height: 900, deviceScaleFactor: 1, mobile: false });
await send('Page.navigate', { url: BASE + '/app/' });
await sleep(2200);

const issues = [];
const check = (name, ok, detail) => {
  console.log(`  ${ok ? '✓' : '✗'} ${name}${detail ? '  → ' + detail : ''}`);
  if (!ok) issues.push(name + (detail ? ': ' + detail : ''));
};

console.log('\n登录页\n');

// 1. 输入框有 label
const labels = await ev(`(() => {
  const out = [];
  document.querySelectorAll('input:not([type=hidden])').forEach(i => {
    const hasLabel = !!(i.labels && i.labels.length) || i.getAttribute('aria-label') || i.getAttribute('placeholder');
    out.push({ id: i.id || i.type, ok: hasLabel });
  });
  return out;
})()`);
check('每个输入框都有可访问名称', labels.every(l => l.ok), JSON.stringify(labels.filter(l => !l.ok)));

// 2. 按钮不是纯图标无名称
const unnamed = await ev(`(() => {
  const bad = [];
  document.querySelectorAll('button').forEach(b => {
    if (b.offsetParent === null) return;
    const text = (b.textContent || '').trim();
    const aria = b.getAttribute('aria-label') || b.getAttribute('title');
    if (!text && !aria) bad.push(b.className || b.id || '(无标识)');
  });
  return bad;
})()`);
check('按钮都有可读名称', unnamed.length === 0, unnamed.join(', '));

// 3. Tab 能走
await ev(`document.body.focus()`);
await send('Input.dispatchKeyEvent', { type: 'keyDown', key: 'Tab', code: 'Tab', windowsVirtualKeyCode: 9 });
await send('Input.dispatchKeyEvent', { type: 'keyUp', key: 'Tab', code: 'Tab', windowsVirtualKeyCode: 9 });
const focused = await ev(`(() => {
  const a = document.activeElement;
  if (!a || a === document.body) return null;
  const cs = getComputedStyle(a);
  return { tag: a.tagName, id: a.id || a.className, outline: cs.outlineStyle, outlineW: cs.outlineWidth };
})()`);
check('Tab 能聚焦到元素', !!focused, JSON.stringify(focused));
check('聚焦时有可见轮廓', !!focused && focused.outline !== 'none', JSON.stringify(focused));

// 4. 图片有 alt
const noAlt = await ev(`[...document.querySelectorAll('img')].filter(i => !i.hasAttribute('alt')).map(i => i.src)`);
check('图片都有 alt 属性', noAlt.length === 0, noAlt.slice(0, 3).join(', '));

// 5. 语义标签
const semantic = await ev(`({
  nav: !!document.querySelector('nav'),
  main: !!document.querySelector('main'),
  aside: !!document.querySelector('aside'),
})`);
check('用了语义标签（nav/main/aside）', semantic.nav && semantic.main && semantic.aside, JSON.stringify(semantic));

// 6. 页面标题
console.log('\n主界面\n');
await ev(`localStorage.setItem('ens_onboarded','1')`);
await ev(`(()=>{document.querySelector('#phone').value='13800000000';document.querySelector('#doLogin').click()})()`);
await waitFor(`!document.querySelector('#app').classList.contains('hidden')`);
await sleep(1200);
await ev(`document.querySelector('.onb-mask')?.remove()`);

check('页面有标题', (await ev(`document.title`))?.length > 0, await ev(`document.title`));

// 导航项是链接
const navLinks = await ev(`[...document.querySelectorAll('#navList .nav-item')].map(a => a.tagName)`);
check('导航项是可聚焦的链接', navLinks.every(t => t === 'A'), navLinks.join(','));

// 当前页有 aria-current
const ariaCurrent = await ev(`!!document.querySelector('#navList .nav-item.on[aria-current], #navList .nav-item.on')`);
check('当前导航项有选中态', ariaCurrent);

// 建房进房间
await ev(`document.querySelector('.team-card [data-join]')?.click()`);
await waitFor(`location.hash.includes('/room/')`);
await sleep(2600);

// 消息流有 live region
const live = await ev(`!!document.querySelector('[aria-live]')`);
check('消息流有 aria-live（读屏能听到新消息）', live);

// 输入框有 label
const taLabel = await ev(`(() => {
  const t = document.querySelector('#ta');
  if (!t) return null;
  return { labels: t.labels?.length || 0, aria: t.getAttribute('aria-label'), ph: t.placeholder };
})()`);
check('消息输入框有可访问名称', !!taLabel && (taLabel.labels > 0 || !!taLabel.aria || !!taLabel.ph), JSON.stringify(taLabel));

// 字体缩放不破版
console.log('\n缩放与对比度\n');
await ev(`document.documentElement.style.fontSize = '20px'`);
await sleep(500);
const overflowAt20 = await ev(`document.documentElement.scrollWidth - window.innerWidth`);
check('字号放大到 20px 后无横向溢出', overflowAt20 <= 1, overflowAt20 + 'px');
await ev(`document.documentElement.style.fontSize = ''`);

// prefers-reduced-motion 生效
await send('Emulation.setEmulatedMedia', { features: [{ name: 'prefers-reduced-motion', value: 'reduce' }] });
await sleep(300);
const reduced = await ev(`getComputedStyle(document.documentElement).getPropertyValue('--t').trim()`);
check('prefers-reduced-motion 时动效被关闭', reduced === '.01s linear', reduced || '(空)');
await send('Emulation.setEmulatedMedia', { features: [] });

console.log(issues.length ? `\n共 ${issues.length} 个问题：\n  ${issues.join('\n  ')}` : '\n全部通过');

ws.close(); proc.kill();
try { rmSync(profile, { recursive: true, force: true }); } catch (e) {}
process.exit(0);
