#!/usr/bin/env node
/**
 * 纸团 · 工作流 E2E：单聊 → 绑工作区 → @/派活执行 → 团队入口 → 设置密码 → 密码登录
 *
 * 与 e2e.mjs 共用同一套 CDP 手法；后端用 mock 上游即可，不消耗额度。
 *   E2E_BASE=http://127.0.0.1:8097 node scripts/e2e-coding.mjs
 */

import { spawn } from 'node:child_process';
import { mkdtempSync, rmSync, mkdirSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const BASE = process.env.E2E_BASE || 'http://localhost:8080';
const PORT = 9334;
const EDGE = process.env.EDGE_PATH || '/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge';
const WORKDIR = process.env.E2E_WORKDIR || '/tmp/ens-e2e-solo';
const PHONE = '13800000000';
const PASSWORD = 'e2e-password-123';

let pass = 0, fail = 0;
const failed = [];
function ok(name, cond, detail) {
  if (cond) { pass++; console.log(`  \x1b[32m✓\x1b[0m ${name}`); }
  else { fail++; failed.push(name); console.log(`  \x1b[31m✗\x1b[0m ${name}${detail ? '  → ' + detail : ''}`); }
}
const sleep = ms => new Promise(r => setTimeout(r, ms));

class CDP {
  constructor(ws) {
    this.ws = ws; this.id = 0; this.pending = new Map(); this.exceptions = [];
    ws.addEventListener('message', ev => {
      const msg = JSON.parse(ev.data);
      if (msg.method === 'Runtime.exceptionThrown') { this.exceptions.push(msg.params.exceptionDetails); return; }
      if (msg.id && this.pending.has(msg.id)) {
        const { resolve, reject } = this.pending.get(msg.id);
        this.pending.delete(msg.id);
        msg.error ? reject(new Error(JSON.stringify(msg.error))) : resolve(msg.result);
      }
    });
  }
  send(method, params = {}) {
    const id = ++this.id;
    return new Promise((resolve, reject) => {
      this.pending.set(id, { resolve, reject });
      this.ws.send(JSON.stringify({ id, method, params }));
      setTimeout(() => {
        if (this.pending.has(id)) { this.pending.delete(id); reject(new Error('CDP 超时: ' + method)); }
      }, 40000);
    });
  }
  async eval(expr) {
    const r = await this.send('Runtime.evaluate', { expression: expr, awaitPromise: true, returnByValue: true, userGesture: true });
    if (r.exceptionDetails) throw new Error(r.exceptionDetails.exception?.description || 'JS 异常');
    return r.result.value;
  }
  async waitFor(expr, timeout = 15000, label = expr) {
    const start = Date.now();
    while (Date.now() - start < timeout) {
      try { if (await this.eval(expr)) return true; } catch { /* 页面未就绪 */ }
      await sleep(150);
    }
    throw new Error('等待超时：' + label);
  }
}

async function launch() {
  const profile = mkdtempSync(join(tmpdir(), 'ens-e2e-coding-'));
  const proc = spawn(EDGE, [
    '--headless=new', '--no-sandbox', '--disable-dev-shm-usage',
    `--remote-debugging-port=${PORT}`, '--disable-gpu', '--no-first-run',
    '--no-default-browser-check', '--disable-extensions', '--hide-scrollbars',
    `--user-data-dir=${profile}`, 'about:blank',
  ], { stdio: 'ignore' });
  for (let i = 0; i < 60; i++) {
    try { const r = await fetch(`http://127.0.0.1:${PORT}/json/version`); if (r.ok) break; } catch {}
    await sleep(200);
  }
  const list = await (await fetch(`http://127.0.0.1:${PORT}/json/list`)).json();
  const page = list.find(t => t.type === 'page');
  const ws = new WebSocket(page.webSocketDebuggerUrl);
  await new Promise((res, rej) => { ws.addEventListener('open', res, { once: true }); ws.addEventListener('error', rej, { once: true }); });
  const cdp = new CDP(ws);
  await cdp.send('Page.enable');
  await cdp.send('Runtime.enable');
  return { cdp, proc, profile, ws };
}

async function api(method, path, body, token) {
  const res = await fetch(BASE + path, {
    method,
    headers: { 'Content-Type': 'application/json', ...(token ? { Authorization: 'Bearer ' + token } : {}) },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  let json = {};
  try { json = await res.json(); } catch {}
  return { status: res.status, json };
}

async function run() {
  const health = await fetch(`${BASE}/healthz`).catch(() => null);
  if (!health || !health.ok) {
    console.error(`\n后端没起来：${BASE}\n先跑 ADDR=:8097 DATA_DIR=/tmp/e2e-new ./bin/ensemble\n`);
    process.exit(2);
  }
  mkdirSync(WORKDIR, { recursive: true });

  // 演示账号额度归零，避免上一轮跑空额度。
  const lr = await api('POST', '/v1/auth/login', { kind: 'phone', target: PHONE, code: '123456' });
  const adminToken = process.env.ADMIN_TOKEN || 'admin-dev-token';
  if (lr.json?.data?.user?.id) {
    await fetch(`${BASE}/admin/api/users/${lr.json.data.user.id}/reset-usage`, {
      method: 'POST', headers: { 'X-Admin-Token': adminToken },
    }).catch(() => {});
  }

  const { cdp, proc, profile, ws } = await launch();
  let convId = '';
  try {
    console.log('\n客户端 E2E（单聊 / 执行 / 团队 / 账号）\n');
    await cdp.send('Page.navigate', { url: `${BASE}/app/` });
    await cdp.waitFor('document.readyState === "complete"', 20000, '页面加载');

    // ---- 登录 ----
    await cdp.eval(`localStorage.setItem('ens_onboarded','1')`);
    await cdp.eval(`(() => {
      document.querySelector('#phone').value = '${PHONE}';
      document.querySelector('#doLogin').click();
    })()`);
    await cdp.waitFor(`!!document.querySelector('#app') && !document.querySelector('#app').classList.contains('hidden')`, 20000, '进入主界面');
    ok('验证码登录成功', true);

    // ---- 单聊入口 ----
    await cdp.waitFor(`location.hash === '#/lobby'`, 8000, '大厅');
    ok('大厅有单聊入口', await cdp.eval(`!!document.querySelector('#soloBtn')`));
    await cdp.eval(`document.querySelector('#soloBtn').click()`);
    await cdp.waitFor(`!!document.querySelector('.modal-mask.on #soloQuick .solo-card')`, 8000, '单聊选择器');
    ok('单聊可选代码角色', (await cdp.eval(`document.querySelectorAll('#soloQuick .solo-card').length`)) >= 4);
    await cdp.eval(`document.querySelector('#soloQuick .solo-card').click()`);
    await cdp.waitFor(`location.hash.startsWith('#/room/')`, 12000, '进入单聊房间');
    convId = await cdp.eval(`location.hash.split('/')[2]`);
    ok('单聊房间已创建', !!convId);
    await cdp.waitFor(`document.querySelectorAll('#avatarRow .av-cell').length === 1`, 12000, '单聊只有一位角色');
    ok('单聊只有一位角色', true);
    ok('房间标题标注单聊', /单聊/.test(await cdp.eval(`document.querySelector('#chatTitle').textContent`)));

    // 后端视角也确认 Members 只有一个，编排不会拉全场景的人。
    const token = await cdp.eval(`localStorage.getItem('ens_token')`);
    const convs = await api('GET', '/v1/conversations', undefined, token);
    const conv = (convs.json.data || []).find(c => c.id === convId);
    ok('会话成员只有一个', conv?.members?.length === 1, JSON.stringify(conv?.members));

    // ---- 绑定工作区 ----
    await cdp.waitFor(`!!document.querySelector('#wsBindBtn')`, 8000, '未绑定工作区提示');
    ok('代码房间提示先绑工作区', true);
    await cdp.eval(`document.querySelector('#wsBindBtn').click()`);
    await cdp.waitFor(`!!document.querySelector('.modal-mask.on #wsRootInput')`, 8000, '工作区弹窗');
    ok('工作区弹窗区分云端/本地', (await cdp.eval(`document.querySelectorAll('.ws-kind-card').length`)) === 2);
    ok('云端工作区置灰', await cdp.eval(`document.querySelector('.ws-kind-card').classList.contains('disabled')`));
    await cdp.eval(`(() => {
      const i = document.querySelector('#wsRootInput');
      i.value = ${JSON.stringify(WORKDIR)};
      i.dispatchEvent(new Event('input', { bubbles: true }));
      document.querySelector('.modal-mask.on [data-x=save]').click();
    })()`);
    await cdp.waitFor(`!!document.querySelector('.ws-bar')`, 12000, '绑定完成');
    ok('工作区绑定成功', (await cdp.eval(`document.querySelector('.ws-b-path').textContent`)).includes(WORKDIR));
    await cdp.waitFor(`document.querySelector('#modeLabel').textContent === '派活'`, 6000, '绑定后进入派活模式');
    ok('绑定后默认进入派活模式', true);

    // ---- 派活执行 ----
    await cdp.eval(`(() => {
      const t = document.querySelector('#ta');
      t.value = '读一下 README，告诉我怎么跑测试';
      t.dispatchEvent(new Event('input', { bubbles: true }));
      document.querySelector('#sendBtn').click();
    })()`);
    await cdp.waitFor(`!!document.querySelector('.run-msg')`, 15000, '执行气泡');
    ok('发送后出现执行气泡', true);
    await cdp.waitFor(`(() => {
      const p = document.querySelector('.run-msg .run-pill');
      return p && /已完成|未完成|已停止/.test(p.textContent);
    })()`, 60000, '执行结束');
    const pill = await cdp.eval(`document.querySelector('.run-msg .run-pill').textContent`);
    ok('执行气泡收尾', /已完成|未完成|已停止/.test(pill), pill);
    ok('执行气泡能展开过程', await cdp.eval(`!!document.querySelector('.run-msg [data-act=toggle]')`));

    // 刷新后应该从服务端恢复 run 消息（kind=run），且不再显示实时气泡
    await cdp.send('Page.reload');
    await cdp.waitFor(`document.readyState === 'complete'`, 15000, '刷新');
    await cdp.waitFor(`document.querySelectorAll('#msgWrap .msg').length > 0`, 15000, '历史消息');
    ok('刷新后执行结果仍在聊天里', await cdp.eval(`!!document.querySelector('.msg .msg-atts, .msg .att-chip') || [...document.querySelectorAll('.msg-title')].some(e => e.textContent.includes('执行'))`));

    // ---- 团队任务入口 ----
    await cdp.waitFor(`!!document.querySelector('.ws-bar')`, 12000, '刷新后工作区仍在');
    await cdp.eval(`document.querySelector('#teamBtn').click()`);
    await cdp.waitFor(`!!document.querySelector('.modal-mask.on #teamTask')`, 8000, '团队任务弹窗');
    ok('团队任务弹窗列出参与角色', (await cdp.eval(`document.querySelectorAll('.tw-chip').length`)) >= 1);
    ok('逐项批准时提示可一键提权', await cdp.eval(`!!document.querySelector('#teamEnablePerm')`));
    await cdp.eval(`document.querySelector('#teamEnablePerm').click()`);
    await sleep(600);
    const after = (await api('GET', '/v1/conversations/' + convId, undefined, token)).json.data;
    ok('一键切到自动接受编辑', after?.defaultPermission === 'approval', after?.defaultPermission);
    await cdp.eval(`document.querySelector('.modal-mask.on [data-x=cancel]')?.click()`);

    // ---- 设置密码 ----
    await cdp.eval(`location.hash = '#/settings'`);
    await cdp.waitFor(`document.querySelector('.page-head h1')?.textContent === '设置'`, 8000, '设置页');
    ok('设置页有额度卡', await cdp.eval(`!!document.querySelector('.quota-card')`));
    await cdp.eval(`document.querySelector('[data-act=security]').click()`);
    await cdp.waitFor(`!!document.querySelector('.modal-mask.on #newPass')`, 8000, '设置密码弹窗');
    await cdp.eval(`(() => {
      document.querySelector('#newPass').value = '${PASSWORD}';
      document.querySelector('#newPass2').value = '${PASSWORD}';
      document.querySelector('.modal-mask.on [data-x=save]').click();
    })()`);
    await cdp.waitFor(`window.__ens.state?.me?.hasPassword === true || !!document.querySelector('[data-act=security] .set-s')?.textContent.includes('已设置')`, 10000, '密码已设置');
    ok('设置密码成功', true);
    const loginPw = await api('POST', '/v1/auth/login', { kind: 'phone', target: PHONE, mode: 'password', password: PASSWORD });
    ok('密码可登录', loginPw.status === 200 && !!loginPw.json?.data?.token, String(loginPw.status));

    // ---- 登录页密码模式 ----
    await cdp.eval(`localStorage.removeItem('ens_token'); window.__ens.showLogin()`);
    await cdp.waitFor(`!document.querySelector('#login').classList.contains('hidden')`, 8000, '回到登录页');
    await cdp.eval(`document.querySelector('#modeSeg [data-m=password]').click()`);
    ok('登录页有密码模式', await cdp.eval(`!document.querySelector('#modePassword').classList.contains('hidden')`));
    await cdp.eval(`(() => {
      document.querySelector('#phone').value = '${PHONE}';
      document.querySelector('#password').value = '${PASSWORD}';
      document.querySelector('#doLogin').click();
    })()`);
    await cdp.waitFor(`!!document.querySelector('#app') && !document.querySelector('#app').classList.contains('hidden')`, 20000, '密码登录进入主界面');
    ok('密码登录成功', true);

    // ---- 收尾 ----
    ok('全程没有未捕获的 JS 异常', cdp.exceptions.length === 0,
      cdp.exceptions.map(e => e.exception?.description || e.text).slice(0, 3).join(' | '));
    if (convId) await api('DELETE', '/v1/conversations/' + convId, undefined, token).catch(() => {});
  } finally {
    try { ws.close(); } catch {}
    proc.kill('SIGKILL');
    rmSync(profile, { recursive: true, force: true });
  }

  console.log(`\n${pass} 通过, ${fail} 失败`);
  if (fail) {
    console.log('\n失败项：');
    failed.forEach(f => console.log('  - ' + f));
    process.exit(1);
  }
}

run().catch(e => { console.error('\n测试崩溃：', e); process.exit(1); });
