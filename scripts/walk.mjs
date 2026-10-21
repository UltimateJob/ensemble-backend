#!/usr/bin/env node
/**
 * 新用户走查。
 *
 *   node scripts/walk.mjs
 *
 * 这不是测试，是「扮演一个第一次用的人」，把每一步看到的打印出来。
 * 它不做断言——判断交给人。
 *
 * 为什么要单独有这个脚本：
 * 自动化测试验证的是「点了有反应」，走查问的是「知道该点哪里」。
 * 这个项目里三次走查抓到六个问题，没有一个能被 E2E 发现：
 *   空态只有文字没有按钮 / 随机场景详情空白 /
 *   按钮文案过时 / 弹窗没说可以不改 /
 *   焦点不回输入框 / placeholder 不贴场景
 *
 * 用法：跑完看输出，逐条问自己「新用户在这里会怎么想」。
 */

import { spawn } from 'node:child_process';
import { mkdtempSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const BASE = process.env.E2E_BASE || 'http://localhost:8080';
const PORT = 9420;
const EDGE = process.env.EDGE_PATH || '/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge';
const SHOT_DIR = process.env.SHOT_DIR || '/tmp/walk';

const sleep = ms => new Promise(r => setTimeout(r, ms));

class CDP {
  constructor(ws) {
    this.ws = ws; this.id = 0; this.pending = new Map();
    ws.addEventListener('message', e => {
      const m = JSON.parse(e.data);
      if (m.id && this.pending.has(m.id)) {
        const { resolve, reject } = this.pending.get(m.id);
        this.pending.delete(m.id);
        m.error ? reject(new Error(JSON.stringify(m.error))) : resolve(m.result);
      }
    });
  }
  send(method, params = {}) {
    const id = ++this.id;
    return new Promise((resolve, reject) => {
      this.pending.set(id, { resolve, reject });
      this.ws.send(JSON.stringify({ id, method, params }));
      setTimeout(() => {
        if (this.pending.has(id)) { this.pending.delete(id); reject(new Error('CDP 超时 ' + method)); }
      }, 30000);
    });
  }
  async eval(expr) {
    const r = await this.send('Runtime.evaluate', { expression: expr, awaitPromise: true, returnByValue: true });
    if (r.exceptionDetails) return '⚠ ' + (r.exceptionDetails.exception?.description || '').split('\n')[0];
    return r.result?.value;
  }
  async shot(name) {
    try {
      const p = await this.send('Page.captureScreenshot', { format: 'png' });
      writeFileSync(`${SHOT_DIR}-${name}.png`, Buffer.from(p.data, 'base64'));
    } catch { /* 截图失败不影响走查 */ }
  }
}

const step = n => console.log(`\n\x1b[1m【${n}】\x1b[0m`);
const kv = (k, v) => console.log(`    ${k}：${v}`);

(async () => {
  const health = await fetch(`${BASE}/healthz`).catch(() => null);
  if (!health || !health.ok) { console.error('后端没起来（make run）'); process.exit(2); }

  const profile = mkdtempSync(join(tmpdir(), 'walk-'));
  const proc = spawn(EDGE, [
    '--headless=new', `--remote-debugging-port=${PORT}`, '--disable-gpu',
    '--hide-scrollbars', '--no-first-run', `--user-data-dir=${profile}`, 'about:blank',
  ], { stdio: 'ignore' });

  for (let i = 0; i < 60; i++) {
    try { if ((await fetch(`http://127.0.0.1:${PORT}/json/version`)).ok) break; } catch {}
    await sleep(200);
  }
  const list = await (await fetch(`http://127.0.0.1:${PORT}/json/list`)).json();
  const ws = new WebSocket(list.find(t => t.type === 'page').webSocketDebuggerUrl);
  await new Promise(r => ws.addEventListener('open', r, { once: true }));
  const cdp = new CDP(ws);
  await cdp.send('Page.enable');
  await cdp.send('Runtime.enable');
  await cdp.send('Emulation.setDeviceMetricsOverride', {
    width: 1440, height: 950, deviceScaleFactor: 1, mobile: false,
  });

  // 用一个全新手机号——已有账号看不到空态
  const phone = '139' + String(Date.now()).slice(-8);
  let code = '123456';
  try {
    const r = await fetch(`${BASE}/v1/auth/send-code`, {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ kind: 'phone', target: phone }),
    }).then(x => x.json());
    code = r.data?.devCode || code;
  } catch { /* 用固定码 */ }
  const auth = await fetch(`${BASE}/v1/auth/login`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ kind: 'phone', target: phone, code }),
  }).then(x => x.json()).then(j => j.data);

  console.log(`\n新用户走查 · ${phone}\n${'─'.repeat(48)}`);

  await cdp.send('Page.navigate', { url: `${BASE}/app/?token=${auth.token}` });
  await sleep(4000);

  step('1 · 第一次进来');
  kv('进了主界面', await cdp.eval(`document.querySelector('#app').classList.contains('on')`));
  kv('左栏说的是', await cdp.eval(`document.querySelector('.conv-empty')?.textContent.replace(/\\s+/g,' ').trim()`));
  kv('有可点的东西吗', await cdp.eval(`!!document.querySelector('.conv-empty button')`));
  kv('上手清单', await cdp.eval(`document.querySelector('#onboard')?.textContent.replace(/\\s+/g,' ').trim().slice(0,70)`));
  await cdp.shot('01-empty');

  step('2 · 开第一个群');
  await cdp.eval(`document.querySelector('.conv-empty button')?.click()`);
  await sleep(1200);
  kv('场景页打开', await cdp.eval(`document.querySelector('#scenes').classList.contains('on')`));
  kv('分区', await cdp.eval(`[...document.querySelectorAll('.scene-sec')].map(e=>e.textContent.replace(/\\s+/g,' ').trim()).join(' | ')`));
  kv('场景数', await cdp.eval(`document.querySelectorAll('.scene').length`));
  kv('默认选中', await cdp.eval(`document.querySelector('.scene.on')?.querySelector('.scene-n')?.textContent || '(没选)'`));
  await cdp.shot('02-scenes');

  step('3 · 看一个随机场景');
  await cdp.eval(`[...document.querySelectorAll('.scene')].find(s => s.dataset.k === 'anime')?.click()`);
  await sleep(600);
  kv('角色区', await cdp.eval(`document.querySelectorAll('#sceneDetail .sd-agent').length + ' 个'`));
  kv('空白时有没有解释', await cdp.eval(`document.querySelector('.sd-agents-rand') ? '有' : '没有'`));
  kv('添加角色按钮叫', await cdp.eval(`document.querySelector('#openForge')?.textContent.trim()`));
  await cdp.shot('03-random');

  step('4 · 找角色');
  await cdp.eval(`document.querySelector('#openForge')?.click()`);
  await sleep(1500);
  kv('两个 tab', await cdp.eval(`[...document.querySelectorAll('.forge-tabs .ft')].map(b=>b.textContent.trim()).join(' | ')`));
  kv('默认停在', await cdp.eval(`document.querySelector('.forge-tabs .ft.on')?.textContent.trim()`));
  kv('搜索框提示', await cdp.eval(`document.querySelector('#fSearch')?.placeholder`));
  kv('初始能看见', await cdp.eval(`document.querySelectorAll('.char-item').length + ' 个角色'`));
  await cdp.shot('04-search');
  await cdp.eval(`document.querySelector('#forgeClose')?.click()`);
  await sleep(400);

  step('5 · 建群');
  await cdp.eval(`[...document.querySelectorAll('.scene')].find(s => s.dataset.k === 'companion')?.click()`);
  await sleep(400);
  await cdp.eval(`document.querySelector('#sceneCreate')?.click()`);
  await sleep(800);
  kv('弹窗问什么', await cdp.eval(`document.querySelector('.ui-dlg h3')?.textContent`));
  kv('默认填的', await cdp.eval(`document.querySelector('.ui-dlg-input')?.value`));
  kv('有没有说可以不改', await cdp.eval(`document.querySelector('.ui-dlg-hint')?.textContent || '(没说)'`));
  await cdp.shot('05-dialog');
  await cdp.eval(`document.querySelector('.ui-dlg-btn[data-x=ok]')?.click()`);
  await sleep(1200);

  step('6 · 开场的几秒');
  kv('顶栏', await cdp.eval(`document.querySelector('#chatSub')?.textContent.trim()`));
  kv('有正在输入的指示', await cdp.eval(`!!document.querySelector('.thinking')`));
  kv('输入框提示', await cdp.eval(`document.querySelector('#ta')?.placeholder`));
  await cdp.shot('06-opening');

  for (let i = 0; i < 90; i++) {
    if (!(await cdp.eval('window.__ens && window.__ens.state.busy'))) break;
    await sleep(500);
  }
  await sleep(800);

  step('7 · 轮到用户说话');
  kv('消息数', await cdp.eval(`document.querySelectorAll('.msg').length`));
  kv('焦点在输入框', await cdp.eval(`document.activeElement?.id === 'ta'`));
  kv('顶栏', await cdp.eval(`document.querySelector('#chatSub')?.textContent.trim()`));
  kv('上手清单', await cdp.eval(`document.querySelector('#onboard')?.textContent.replace(/\\s+/g,' ').trim().slice(0,40)`));
  await cdp.shot('07-ready');

  console.log(`\n${'─'.repeat(48)}`);
  console.log('截图在 ' + SHOT_DIR + '-*.png');
  console.log('\n逐条问自己：');
  console.log('  · 每一步我知道下一步点哪里吗');
  console.log('  · 有没有哪个空白让我怀疑「是不是坏了」');
  console.log('  · 有没有文案和实际行为对不上');
  console.log('  · 有没有需要我猜的地方\n');

  ws.close();
  proc.kill();
})();
