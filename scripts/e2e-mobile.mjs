#!/usr/bin/env node
/**
 * 移动端端到端：用 CDP 的 Emulation.setDeviceMetricsOverride
 * 模拟真实 390×844 视口——这是 headless 截图做不到的。
 *
 * 新壳的窄屏契约（shell.css ≤860px）：
 *   - 左导航收成 56px 图标条，标签文字隐藏
 *   - 主内容始终可见（不能像旧版那样整页空白）
 *   - 右面板改成覆盖层，从右侧滑入，不再挤成第三列
 *   - 大厅 / 看板 / 资料库都收敛成一列
 *
 * 用法：E2E_BASE=http://127.0.0.1:8096 node scripts/e2e-mobile.mjs
 */

import { spawn } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const BASE = process.env.E2E_BASE || 'http://localhost:8080';
const PORT = 9334;
const EDGE = process.env.EDGE_PATH || '/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge';

let pass = 0, fail = 0;
const failed = [];
const ok = (name, cond, detail) => {
  if (cond) { pass++; console.log(`  \x1b[32m✓\x1b[0m ${name}`); }
  else { fail++; failed.push(name); console.log(`  \x1b[31m✗\x1b[0m ${name}${detail ? '  → ' + detail : ''}`); }
};
const sleep = ms => new Promise(r => setTimeout(r, ms));

class CDP {
  constructor(ws) {
    this.ws = ws; this.id = 0; this.pending = new Map();
    this.exceptions = [];
    ws.addEventListener('message', ev => {
      const m = JSON.parse(ev.data);
      if (m.method === 'Runtime.exceptionThrown') { this.exceptions.push(m.params.exceptionDetails); return; }
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
    const r = await this.send('Runtime.evaluate', { expression: expr, awaitPromise: true, returnByValue: true, userGesture: true });
    if (r.exceptionDetails) throw new Error(r.exceptionDetails.exception?.description || 'JS 异常');
    return r.result.value;
  }
  async waitFor(expr, timeout = 15000, label = expr) {
    const t0 = Date.now();
    while (Date.now() - t0 < timeout) {
      try { if (await this.eval(expr)) return true; } catch {}
      await sleep(150);
    }
    throw new Error('等待超时：' + label);
  }
}

async function main() {
  const health = await fetch(`${BASE}/healthz`).catch(() => null);
  if (!health || !health.ok) { console.error('后端没起来'); process.exit(2); }

  const profile = mkdtempSync(join(tmpdir(), 'ens-m-'));
  const proc = spawn(EDGE, [
    '--headless=new',
    // CI 的 runner 以 root 运行，不加这个 Chrome 起不来
    '--no-sandbox',
    '--disable-dev-shm-usage', `--remote-debugging-port=${PORT}`, '--disable-gpu',
    '--no-first-run', '--hide-scrollbars', `--user-data-dir=${profile}`, 'about:blank',
  ], { stdio: 'ignore' });

  for (let i = 0; i < 60; i++) {
    try { if ((await fetch(`http://127.0.0.1:${PORT}/json/version`)).ok) break; } catch {}
    await sleep(200);
  }
  const list = await (await fetch(`http://127.0.0.1:${PORT}/json/list`)).json();
  const page = list.find(t => t.type === 'page');
  const ws = new WebSocket(page.webSocketDebuggerUrl);
  await new Promise((res, rej) => {
    ws.addEventListener('open', res, { once: true });
    ws.addEventListener('error', rej, { once: true });
  });
  const cdp = new CDP(ws);
  await cdp.send('Page.enable');
  await cdp.send('Runtime.enable');

  // 关键：模拟 iPhone 尺寸
  await cdp.send('Emulation.setDeviceMetricsOverride', {
    width: 390, height: 844, deviceScaleFactor: 3, mobile: true,
  });
  await cdp.send('Emulation.setTouchEmulationEnabled', { enabled: true, maxTouchPoints: 5 });

  try {
    console.log('\n移动端 E2E（390×844）\n');
    await cdp.send('Page.navigate', { url: `${BASE}/app/` });
    await cdp.waitFor('document.readyState === "complete"', 20000, '加载');

    const w = await cdp.eval('window.innerWidth');
    ok(`视口宽度为 390（实际 ${w}）`, w === 390);

    // 登录（先关掉新手引导，否则它会抢走路由）
    await cdp.eval(`localStorage.setItem('ens_onboarded','1')`);
    await cdp.eval(`(() => {
      document.querySelector('#phone').value = '13800000000';
      document.querySelector('#doLogin').click();
    })()`);
    await cdp.waitFor('!document.querySelector("#app").classList.contains("hidden")', 20000, '进入主界面');
    ok('移动端登录成功', true);
    await cdp.waitFor('location.hash.startsWith("#/lobby")', 8000, '大厅路由');

    // ---- 窄屏外壳契约 ----
    const shell = await cdp.eval(`(() => {
      const navW = Math.round(document.querySelector('.nav').getBoundingClientRect().width);
      const main = document.querySelector('.main');
      const mainRect = main.getBoundingClientRect();
      const mainDisplay = getComputedStyle(main).display;
      const labelDisplay = getComputedStyle(document.querySelector('.nav-item span')).display;
      const roomsDisplay = getComputedStyle(document.querySelector('#navRooms')).display;
      return JSON.stringify({ navW, mainDisplay, mainW: Math.round(mainRect.width), labelDisplay, roomsDisplay });
    })()`);
    const sh = JSON.parse(shell);
    ok(`左导航收成图标条（${sh.navW}px）`, sh.navW <= 60, shell);
    ok('主内容仍然可见（不是整页空白）', sh.mainDisplay !== 'none' && sh.mainW > 300, shell);
    ok('导航文字隐藏、只剩图标', sh.labelDisplay === 'none' && sh.roomsDisplay === 'none', shell);

    const overflow0 = await cdp.eval('document.documentElement.scrollWidth - window.innerWidth');
    ok(`无横向溢出（超出 ${overflow0}px）`, overflow0 <= 1);

    // 大厅单列，卡片不会被挤成豆腐块
    ok('6 个导航项都在', await cdp.eval('document.querySelectorAll("#navList .nav-item").length === 6'));
    const cols = await cdp.eval(
      'getComputedStyle(document.querySelector(".team-grid")).gridTemplateColumns.split(" ").length'
    );
    ok(`大厅卡片单列（${cols} 列）`, cols === 1);
    ok('4 张预设团队卡都在', await cdp.eval('document.querySelectorAll(".team-card").length === 4'));

    // ---- 右面板在窄屏是覆盖层 ----
    // 直接点面板把手在触屏上太难点，用 #roomPanel 之前先确认默认不可见，
    // 打开后覆盖主内容、且不产生横向溢出。
    const panelClosed = await cdp.eval(`(() => {
      const p = document.querySelector('#panel');
      const r = p.getBoundingClientRect();
      return Math.round(r.left) >= window.innerWidth - 1;
    })()`);
    ok('右面板默认在屏幕外（覆盖层收起）', panelClosed);

    // ---- 建房间 ----
    await cdp.eval(`document.querySelector('.team-card [data-join]').click()`);
    await cdp.waitFor('location.hash.startsWith("#/room/")', 15000, '进入房间');
    await cdp.waitFor('!!document.querySelector(".room")', 15000, '房间渲染');
    // 头像条是 mount 之后异步拉的（renderAvatarRow 里要等 /activity），
    // 只等 .room 会读到 0 个。
    await cdp.waitFor('document.querySelectorAll("#avatarRow .av-cell").length > 0', 15000, '头像条');
    ok('移动端加入团队成功', true);

    const room = await cdp.eval(`(() => {
      const avs = document.querySelectorAll('#avatarRow .av-cell').length;
      const ta = document.querySelector('#ta');
      const send = document.querySelector('#sendBtn');
      const r = document.querySelector('#composer').getBoundingClientRect();
      return JSON.stringify({
        avs, ta: !!ta, send: !!send,
        composerBottom: Math.round(r.bottom), composerRight: Math.round(r.right),
        title: document.querySelector('.rh-title')?.textContent || '',
      });
    })()`);
    const rm = JSON.parse(room);
    ok(`房间头像条有角色（${rm.avs} 个，≥3）`, rm.avs >= 3, room);
    ok('输入条在窄屏存在且不超出屏幕', rm.ta && rm.send && rm.composerBottom <= 845 && rm.composerRight <= 391, room);

    const overflow1 = await cdp.eval('document.documentElement.scrollWidth - window.innerWidth');
    ok(`房间页无横向溢出（超出 ${overflow1}px）`, overflow1 <= 1);

    // 右面板打开后覆盖在内容之上，且面板自身不溢出
    await cdp.eval('document.querySelector("#roomPanel").click()');
    await cdp.waitFor('document.querySelector("#panel").classList.contains("on")', 10000, '面板打开');
    // 覆盖层是 transform 滑入的（0.2s）。class 一加上就开始过渡，
    // 立刻量会拿到还没就位的坐标，所以等它真的滑进来再断言。
    await cdp.waitFor(
      '(() => { const r = document.querySelector("#panel").getBoundingClientRect(); return r.left >= 0 && r.right <= window.innerWidth + 1; })()',
      5000,
      '面板滑入并完整可见'
    );
    const panelOpen = await cdp.eval(`(() => {
      const r = document.querySelector('#panel').getBoundingClientRect();
      return JSON.stringify({ left: Math.round(r.left), right: Math.round(r.right), w: Math.round(r.width), vw: window.innerWidth });
    })()`);
    const po = JSON.parse(panelOpen);
    ok(`右面板覆盖在内容上（left=${po.left}, vw=${po.vw}）`, po.left >= 0 && po.right <= po.vw + 1, panelOpen);
    await cdp.eval('document.querySelector("#panelClose").click()');
    await cdp.waitFor('!document.querySelector("#panel").classList.contains("on")', 8000, '面板关闭');
    ok('右面板能关闭', true);

    // ---- 窄屏流式回复 ----
    // 建房间不会自动开场，先等上一轮结束再发，避免点发送被静默忽略
    await cdp.waitFor('window.__ens && !document.body.textContent.includes("正在说")', 5000, '空闲').catch(() => {});
    await cdp.eval(`(() => {
      const ta = document.querySelector('#ta');
      ta.value = '在吗';
      ta.dispatchEvent(new Event('input', { bubbles: true }));
      document.querySelector('#sendBtn').click();
    })()`);
    await cdp.waitFor('document.querySelectorAll(".msg.me").length > 0', 10000, '用户消息');
    await cdp.waitFor(
      '[...document.querySelectorAll(".msg:not(.me) .msg-text")].some(t => t.textContent.trim().length > 0)',
      60000,
      '角色回复'
    );
    await cdp.waitFor('document.querySelectorAll(".msg.speaking").length === 0', 60000, '本轮结束');
    ok('窄屏下流式回复正常', true);

    const msgCount = await cdp.eval('document.querySelectorAll(".msg").length');
    ok(`窄屏消息数 ${msgCount}（≥2）`, msgCount >= 2);

    const over2 = await cdp.eval('document.documentElement.scrollWidth - window.innerWidth');
    ok(`回复后仍无横向溢出（超出 ${over2}px）`, over2 <= 1);

    // 视口变矮时（地址栏/键盘顶起），输入框不能被顶出屏幕
    await cdp.send('Emulation.setDeviceMetricsOverride', { width: 390, height: 640, deviceScaleFactor: 3, mobile: true });
    await sleep(500);
    const inView = await cdp.eval(`(() => {
      const r = document.querySelector('#composer').getBoundingClientRect();
      return r.bottom <= window.innerHeight + 1;
    })()`);
    ok('视口变矮时输入框仍在屏幕内', inView);
    await cdp.send('Emulation.setDeviceMetricsOverride', { width: 390, height: 844, deviceScaleFactor: 3, mobile: true });
    await sleep(300);

    // ---- 各页面在窄屏也能打开 ----
    for (const [route, title] of [['goals', '目标看板'], ['connectors', '连接器'], ['library', '资料库'], ['settings', '设置']]) {
      await cdp.eval(`location.hash = '#/${route}'`);
      await cdp.waitFor(
        `document.querySelector('.page-head h1')?.textContent.trim() === ${JSON.stringify(title)}`,
        15000, title
      );
      const of = await cdp.eval('document.documentElement.scrollWidth - window.innerWidth');
      ok(`#/${route} 窄屏可用且无溢出（${of}px）`, of <= 1);
    }

    // 404 也要在窄屏可用
    await cdp.eval(`location.hash = '#/nope'`);
    await cdp.waitFor('document.querySelector(".es-t")?.textContent.trim() === "页面不存在"', 10000, '404');
    ok('窄屏 404 正常', true);

    // ---- 全程没有未捕获的 JS 异常 ----
    const exs = cdp.exceptions.map(e => (e.exception?.description || e.text || '').split('\n')[0]).filter(Boolean);
    ok(`全程没有未捕获的 JS 异常（${exs.length} 个）`, exs.length === 0, exs.slice(0, 3).join(' | '));

    // 清理：删掉本轮建的房间，别让大厅越跑越脏
    const convId = await cdp.eval(`document.querySelector('.nav-room')?.dataset.id || ''`);
    if (convId) {
      const status = await cdp.eval(
        `fetch('/v1/conversations/' + encodeURIComponent(${JSON.stringify(convId)}), {method:'DELETE', headers:{'Authorization':'Bearer '+localStorage.getItem('ens_token')}}).then(r=>r.status)`
      );
      ok('清理测试房间', status === 200, `HTTP ${status}`);
    }

  } finally {
    ws.close(); proc.kill(); await sleep(300);
    try { rmSync(profile, { recursive: true, force: true }); } catch {}
  }

  console.log(`\n${pass} 通过, ${fail} 失败\n`);
  if (fail) { failed.forEach(f => console.log('  - ' + f)); process.exit(1); }
}

main().catch(e => { console.error('\n中断：', e.message); process.exit(1); });
