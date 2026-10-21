#!/usr/bin/env node
/**
 * 移动端端到端：用 CDP 的 Emulation.setDeviceMetricsOverride
 * 模拟真实 390×844 视口——这是 headless 截图做不到的。
 *
 * 用法：node scripts/e2e-mobile.mjs
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
    ws.addEventListener('message', ev => {
      const m = JSON.parse(ev.data);
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

    // 登录
    await cdp.eval(`(() => {
      document.querySelector('#phone').value = '13800000000';
      document.querySelector('#doLogin').click();
    })()`);
    await cdp.waitFor('document.querySelector("#app").classList.contains("on")', 20000, '进入主界面');
    ok('移动端登录成功', true);

    // 三栏应收敛为单栏
    ok('左栏隐藏', await cdp.eval('getComputedStyle(document.querySelector(".col-list")).display === "none"'));
    ok('右栏隐藏', await cdp.eval('getComputedStyle(document.querySelector(".col-info")).display === "none"'));
    ok('汉堡按钮出现', await cdp.eval('getComputedStyle(document.querySelector("#drawerBtn")).display !== "none"'));

    // 没有横向溢出
    const overflow = await cdp.eval('document.documentElement.scrollWidth - window.innerWidth');
    ok(`无横向溢出（超出 ${overflow}px）`, overflow <= 1);

    // 抽屉
    await cdp.eval('document.querySelector("#drawerBtn").click()');
    await cdp.waitFor('document.querySelector("#drawer").classList.contains("on")', 5000, '抽屉打开');
    ok('抽屉能打开', true);
    await cdp.eval('document.querySelector("#drawerMask").click()');
    await sleep(400);
    ok('点遮罩能关上', !(await cdp.eval('document.querySelector("#drawer").classList.contains("on")')));

    // 建群
    await cdp.eval('window.prompt = () => "移动端测试群"');
    await cdp.eval('document.querySelector("#newConvBtn").click()');
    await cdp.waitFor('document.querySelector("#scenes").classList.contains("on")', 8000, '场景页');
    const cols = await cdp.eval('getComputedStyle(document.querySelector(".scene-grid")).gridTemplateColumns.split(" ").length');
    ok(`场景单列（${cols} 列）`, cols === 1);

    await cdp.eval(`(() => {
      [...document.querySelectorAll('.scene')].find(s => s.dataset.k === 'companion').click();
      document.querySelector('#sceneCreate').click();
    })()`);
    // 建群先问群名（自绘弹层）
    await cdp.waitFor('!!document.querySelector(".ui-dlg .ui-dlg-input")', 8000, '群名弹层');
    await cdp.eval(`(() => {
      const i = document.querySelector('.ui-dlg-input');
      i.value = '手机端验证';
      i.dispatchEvent(new Event('input', { bubbles: true }));
      document.querySelector('.ui-dlg-btn[data-x=ok]').click();
    })()`);
    await cdp.waitFor('document.querySelector("#msgs").style.display !== "none"', 15000, '聊天区');
    ok('移动端建群成功', true);

    // 真机差异点：这些在 CDP 里能验，真机上立刻能感觉到
    ok('viewport 支持刘海屏', await cdp.eval(
      `document.querySelector('meta[name=viewport]').content.includes('viewport-fit=cover')`
    ));
    ok('点按高亮已禁用', await cdp.eval(
      `getComputedStyle(document.body).webkitTapHighlightColor === 'rgba(0, 0, 0, 0)'`
    ));
    // 底部安全区：CDP 默认模拟没有 Home 条，env() 算出来是 0，
    // 所以只能验 CSS 里有没有用这个变量，验不了运行时值。
    ok('底部用了 safe-area-inset', await cdp.eval(`(() => {
      for (const sheet of document.styleSheets) {
        let rules;
        try { rules = sheet.cssRules; } catch { continue; }
        for (const r of rules) {
          if (r.cssText && r.cssText.includes('.composer') && r.cssText.includes('safe-area-inset-bottom')) return true;
        }
      }
      return false;
    })()`));
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

    // 输入框在视口内
    const box = await cdp.eval(`(() => {
      const r = document.querySelector('.composer').getBoundingClientRect();
      return { bottom: Math.round(r.bottom), right: Math.round(r.right), w: window.innerWidth };
    })()`);
    ok(`输入框不超出屏幕（bottom=${box.bottom}, right=${box.right}）`, box.bottom <= 850 && box.right <= 390);

    // 建群后会自动开场（kickoff），那一轮可能还没跑完。
    // busy 为真时点发送会被静默忽略——桌面测试修过这个，移动端漏了。
    // 加上角色之间的间隔（Round 68）之后，一轮比以前长，更容易撞上。
    await cdp.waitFor('window.__ens && !window.__ens.state.busy', 40000, '自动开场结束');

    // 发一条，确认流式在窄屏也能正常渲染
    await cdp.eval(`(() => {
      const ta = document.querySelector('#ta');
      ta.value = '在吗';
      ta.dispatchEvent(new Event('input', { bubbles: true }));
      document.querySelector('#sendBtn').click();
    })()`);
    await cdp.waitFor('document.querySelectorAll(".msg.me").length > 0', 10000, '用户消息');
    await cdp.waitFor('document.querySelectorAll(".msg.speaking").length === 0 && document.querySelectorAll(".msg:not(.me)").length > 0', 40000, '角色回复');
    ok('窄屏下流式回复正常', true);

    const over2 = await cdp.eval('document.documentElement.scrollWidth - window.innerWidth');
    ok(`回复后仍无横向溢出（超出 ${over2}px）`, over2 <= 1);

    // 清理
    const id = await cdp.eval('document.querySelector(".conv").dataset.id');
    await cdp.eval(`fetch('/v1/conversations/${id}', {method:'DELETE', headers:{'Authorization':'Bearer '+localStorage.getItem('ens_token')}}).then(r=>r.status)`);

  } finally {
    ws.close(); proc.kill(); await sleep(300);
    try { rmSync(profile, { recursive: true, force: true }); } catch {}
  }

  console.log(`\n${pass} 通过, ${fail} 失败\n`);
  if (fail) { failed.forEach(f => console.log('  - ' + f)); process.exit(1); }
}

main().catch(e => { console.error('\n中断：', e.message); process.exit(1); });
