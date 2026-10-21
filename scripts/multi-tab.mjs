#!/usr/bin/env node
/**
 * 多标签页测试：同一个账号开两个页面同时用。
 *
 *   node scripts/multi-tab.mjs
 *
 * 现在所有 E2E 都是单页面。真实场景是：桌面开着、手机上还开着、
 * 或者一个人开了两个标签页对照着看。
 *
 * 验证：
 *   - 两个标签页共享登录态（localStorage）
 *   - 一个标签页发的消息，另一个刷新后能看到
 *   - 同时在一个群里说话，一个成功一个被 busy
 *   - 一个标签页切到另一个群，不会影响另一个标签页的当前会话
 */

import { spawn } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const BASE = process.env.E2E_BASE || 'http://localhost:8080';
const PORT = 9380;
const EDGE = process.env.EDGE_PATH || '/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge';

const sleep = ms => new Promise(r => setTimeout(r, ms));
let pass = 0, fail = 0;
const failed = [];
const ok = (n, c, d) => {
  if (c) { pass++; console.log(`  \x1b[32m✓\x1b[0m ${n}`); }
  else { fail++; failed.push(n); console.log(`  \x1b[31m✗\x1b[0m ${n}${d ? '  → ' + d : ''}`); }
};

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
    const r = await this.send('Runtime.evaluate', {
      expression: expr, awaitPromise: true, returnByValue: true, userGesture: true,
    });
    if (r.exceptionDetails) throw new Error(r.exceptionDetails.exception?.description || 'JS 异常');
    return r.result.value;
  }
  async waitFor(expr, timeout = 20000, label = expr) {
    const t0 = Date.now();
    while (Date.now() - t0 < timeout) {
      try { if (await this.eval(expr)) return true; } catch { /* 页面还在加载 */ }
      await sleep(150);
    }
    throw new Error('等待超时：' + label);
  }
}

async function connect(wsUrl) {
  const ws = new WebSocket(wsUrl);
  await new Promise((res, rej) => {
    ws.addEventListener('open', res, { once: true });
    ws.addEventListener('error', rej, { once: true });
  });
  const cdp = new CDP(ws);
  await cdp.send('Page.enable');
  await cdp.send('Runtime.enable');
  return cdp;
}

(async () => {
  const health = await fetch(`${BASE}/healthz`).catch(() => null);
  if (!health || !health.ok) { console.error('后端没起来'); process.exit(2); }

  const profile = mkdtempSync(join(tmpdir(), 'ens-tabs-'));
  const proc = spawn(EDGE, [
    '--headless=new', `--remote-debugging-port=${PORT}`,
    '--disable-gpu', '--hide-scrollbars', '--no-first-run',
    `--user-data-dir=${profile}`, 'about:blank',
  ], { stdio: 'ignore' });

  for (let i = 0; i < 60; i++) {
    try { if ((await fetch(`http://127.0.0.1:${PORT}/json/version`)).ok) break; } catch { /* 等 */ }
    await sleep(200);
  }

  // 准备一个账号和一个群
  const mkUser = async () => {
    const phone = '137' + String(Date.now()).slice(-8);
    let code = '123456';
    try {
      const r = await fetch(`${BASE}/v1/auth/send-code`, {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ kind: 'phone', target: phone }),
      }).then(x => x.json());
      code = r.data?.devCode || code;
    } catch { /* 用固定码 */ }
    const d = await fetch(`${BASE}/v1/auth/login`, {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ kind: 'phone', target: phone, code }),
    }).then(x => x.json());
    return d.data;
  };
  const auth = await mkUser();

  const mkConv = (title, studio) => fetch(`${BASE}/v1/conversations`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: 'Bearer ' + auth.token },
    body: JSON.stringify({ title, studio }),
  }).then(r => r.json()).then(j => j.data);

  const convA = await mkConv('标签页 A', 'companion');
  const convB = await mkConv('标签页 B', 'anime');

  // 打开两个标签页
  const page1 = (await (await fetch(`http://127.0.0.1:${PORT}/json/list`)).json()).find(t => t.type === 'page');
  const tab1 = await connect(page1.webSocketDebuggerUrl);

  await fetch(`http://127.0.0.1:${PORT}/json/new?about:blank`, { method: 'PUT' });
  await sleep(500);
  const pages = (await (await fetch(`http://127.0.0.1:${PORT}/json/list`)).json()).filter(t => t.type === 'page');
  const page2 = pages.find(p => p.id !== page1.id) || pages[1];
  const tab2 = await connect(page2.webSocketDebuggerUrl);

  console.log(`\n多标签页测试\n`);

  try {
    // 标签 1 登录
    await tab1.send('Page.navigate', { url: `${BASE}/app/?token=${auth.token}&c=${convA.id}` });
    await tab1.waitFor('document.readyState === "complete"', 20000, '标签1 加载');
    await tab1.waitFor('document.querySelector("#app").classList.contains("on")', 20000, '标签1 进主界面');
    ok('标签 1 登录并打开群 A', true);

    // 标签 2 直接进（不需要再登录，共享 localStorage）
    await tab2.send('Page.navigate', { url: `${BASE}/app/?c=${convB.id}` });
    await tab2.waitFor('document.readyState === "complete"', 20000, '标签2 加载');
    await tab2.waitFor('document.querySelector("#app").classList.contains("on")', 20000, '标签2 自动登录');
    ok('标签 2 共享登录态，无需重新登录', true);

    // 两边各自打开不同的群
    await tab1.waitFor('document.querySelector("#chatTitle").textContent.includes("A")', 15000, '标签1 群A');
    await tab2.waitFor('document.querySelector("#chatTitle").textContent.includes("B")', 15000, '标签2 群B');
    ok('两个标签页各自停在自己的群', true);

    const t1 = await tab1.eval('document.querySelector("#chatTitle").textContent');
    const t2 = await tab2.eval('document.querySelector("#chatTitle").textContent');
    ok(`互不干扰（${t1} / ${t2}）`, t1 !== t2);

    // 等两边的自动开场跑完
    for (const [name, tab] of [['标签1', tab1], ['标签2', tab2]]) {
      try {
        await tab.waitFor('document.querySelectorAll(".msg.speaking").length > 0 || window.__ens.state.busy === true', 12000, name + '开场启动');
        await tab.waitFor('document.querySelectorAll(".msg.speaking").length === 0 && window.__ens.state.busy === false', 90000, name + '开场结束');
      } catch { /* 没有开场也没关系 */ }
    }

    // 标签 1 发一条，标签 2 刷新后能不能看到
    const before = await tab1.eval('document.querySelectorAll(".msg").length');
    await tab1.eval(`(() => {
      const ta = document.querySelector('#ta');
      ta.value = '来自标签页 A 的消息';
      ta.dispatchEvent(new Event('input', { bubbles: true }));
      document.querySelector('#sendBtn').click();
    })()`);
    await tab1.waitFor('document.querySelectorAll(".msg.me").length > 0', 15000, '标签1 消息上屏');
    ok('标签 1 发出消息', true);

    // 标签 2 打开同一个群，应看到这条
    await tab2.eval(`(() => {
      const el = [...document.querySelectorAll('.conv')].find(c => c.querySelector('.conv-name').textContent.includes('A'));
      if (el) el.click();
    })()`);
    await sleep(1500);
    const seen = await tab2.eval(
      '[...document.querySelectorAll(".msg-text")].some(t => t.textContent.includes("来自标签页 A"))'
    );
    ok('标签 2 能看到标签 1 发的消息', seen);

    // 同一个群同时说话：一个成功，一个 busy
    await tab1.eval(`(() => {
      const el = [...document.querySelectorAll('.conv')].find(c => c.querySelector('.conv-name').textContent.includes('A'));
      if (el) el.click();
    })()`);
    await sleep(1200);

    const fire = tab => tab.eval(`(() => {
      const ta = document.querySelector('#ta');
      ta.value = '同一秒说话';
      ta.dispatchEvent(new Event('input', { bubbles: true }));
      document.querySelector('#sendBtn').click();
      return 'sent';
    })()`);

    // 发之前确认两边都空闲——否则本地 busy 会直接把 send 挡掉，
    // 连请求都不会发出，测的就不是后端的锁了
    await tab1.waitFor('window.__ens.state.busy === false', 60000, '标签1 空闲');
    await tab2.waitFor('window.__ens.state.busy === false', 60000, '标签2 空闲');

    await Promise.all([fire(tab1), fire(tab2)]);
    await sleep(7000);

    const notes = (await tab1.eval(
      '[...document.querySelectorAll(".sys-note, .pass-note")].map(e => e.textContent).join("|")'
    )) + (await tab2.eval(
      '[...document.querySelectorAll(".sys-note, .pass-note")].map(e => e.textContent).join("|")'
    ));
    const busySeen = /正在说话|等一下再试/.test(notes);
    ok('同一群并发时有一边收到 busy 提示', busySeen, notes.slice(0, 80));

    // 两条用户消息都应落库
    const msgs = await fetch(
      `${BASE}/v1/conversations/${convA.id}/messages?limit=200`,
      { headers: { Authorization: 'Bearer ' + auth.token } }
    ).then(r => r.json()).then(j => j.data);
    const mine = (Array.isArray(msgs) ? msgs : msgs.messages)
      .filter(m => m.role === 'user' && m.content.includes('同一秒说话')).length;
    ok(`两条并发消息都落库（${mine} 条）`, mine === 2);

  } finally {
    // 清理
    await fetch(`${BASE}/v1/conversations/${convA.id}`, { method: 'DELETE', headers: { Authorization: 'Bearer ' + auth.token } }).catch(() => {});
    await fetch(`${BASE}/v1/conversations/${convB.id}`, { method: 'DELETE', headers: { Authorization: 'Bearer ' + auth.token } }).catch(() => {});
    proc.kill();
    await sleep(300);
    try { rmSync(profile, { recursive: true, force: true }); } catch { /* ignore */ }
  }

  console.log(`\n${pass} 通过, ${fail} 失败\n`);
  if (fail) { failed.forEach(f => console.log('  - ' + f)); process.exit(1); }
})();
