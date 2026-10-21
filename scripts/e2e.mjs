#!/usr/bin/env node
/**
 * 合奏 · 浏览器端到端测试
 *
 * 不依赖 Playwright / Puppeteer：直接用 CDP（Chrome DevTools Protocol）
 * 连本机 Edge 的 headless 实例，Node 内置 WebSocket 就够。
 *
 * 覆盖：登录 → 建群 → 发消息 → 看到流式回复 → 刷新后仍在。
 *
 * 用法：
 *   1. 另开一个终端跑后端（mock 模式即可，不消耗额度）
 *        DEEPSEEK_API_KEY= ./bin/ensemble
 *   2. node scripts/e2e.mjs
 */

import { spawn } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const BASE = process.env.E2E_BASE || 'http://localhost:8080';
const PORT = 9333;
const EDGE =
  process.env.EDGE_PATH ||
  '/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge';

let pass = 0;
let fail = 0;
const failed = [];

function ok(name, cond, detail) {
  if (cond) {
    pass++;
    console.log(`  \x1b[32m✓\x1b[0m ${name}`);
  } else {
    fail++;
    failed.push(name);
    console.log(`  \x1b[31m✗\x1b[0m ${name}${detail ? '  → ' + detail : ''}`);
  }
}

const sleep = ms => new Promise(r => setTimeout(r, ms));

/* ---------------- CDP 极简客户端 ---------------- */
class CDP {
  constructor(ws) {
    this.ws = ws;
    this.id = 0;
    this.pending = new Map();
    ws.addEventListener('message', ev => {
      const msg = JSON.parse(ev.data);
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
        if (this.pending.has(id)) {
          this.pending.delete(id);
          reject(new Error(`CDP 超时: ${method}`));
        }
      }, 40000);
    });
  }

  /** 在页面里执行一个表达式，支持 await */
  async eval(expr) {
    const r = await this.send('Runtime.evaluate', {
      expression: expr,
      awaitPromise: true,
      returnByValue: true,
      userGesture: true,
    });
    if (r.exceptionDetails) {
      throw new Error(r.exceptionDetails.exception?.description || 'JS 异常');
    }
    return r.result.value;
  }

  async waitFor(expr, timeout = 15000, label = expr) {
    const start = Date.now();
    while (Date.now() - start < timeout) {
      try {
        if (await this.eval(expr)) return true;
      } catch {
        /* 页面还没就绪，继续等 */
      }
      await sleep(150);
    }
    throw new Error(`等待超时：${label}`);
  }
}

/* ---------------- 启动浏览器 ---------------- */
async function launch() {
  const profile = mkdtempSync(join(tmpdir(), 'ens-e2e-'));
  const proc = spawn(
    EDGE,
    [
      '--headless=new',
      `--remote-debugging-port=${PORT}`,
      '--disable-gpu',
      '--no-first-run',
      '--no-default-browser-check',
      '--disable-extensions',
      '--hide-scrollbars',
      `--user-data-dir=${profile}`,
      'about:blank',
    ],
    { stdio: 'ignore' }
  );

  // 等调试端口起来
  for (let i = 0; i < 60; i++) {
    try {
      const res = await fetch(`http://127.0.0.1:${PORT}/json/version`);
      if (res.ok) break;
    } catch {
      /* 继续等 */
    }
    await sleep(200);
  }

  const list = await (await fetch(`http://127.0.0.1:${PORT}/json/list`)).json();
  const page = list.find(t => t.type === 'page');
  if (!page) throw new Error('没有可用的页面目标');

  const ws = new WebSocket(page.webSocketDebuggerUrl);
  await new Promise((res, rej) => {
    ws.addEventListener('open', res, { once: true });
    ws.addEventListener('error', rej, { once: true });
  });

  const cdp = new CDP(ws);
  await cdp.send('Page.enable');
  await cdp.send('Runtime.enable');

  return { cdp, proc, profile, ws };
}

/* ---------------- 测试主体 ---------------- */
async function run() {
  // 先确认后端活着
  const health = await fetch(`${BASE}/healthz`).catch(() => null);
  if (!health || !health.ok) {
    console.error(`\n后端没起来：${BASE}\n先跑 DEEPSEEK_API_KEY= ./bin/ensemble\n`);
    process.exit(2);
  }

  const { cdp, proc, profile, ws } = await launch();

  try {
    console.log('\n客户端 E2E\n');

    await cdp.send('Page.navigate', { url: `${BASE}/app/` });
    await cdp.waitFor('document.readyState === "complete"', 20000, '页面加载');

    // ---- 登录页 ----
    ok('登录页出现', await cdp.eval('!!document.querySelector("#login")'));
    ok(
      '三种登录方式都在',
      (await cdp.eval('!!document.querySelector("#form-phone") && !!document.querySelector("#form-email")'))
    );
    ok(
      '登录按钮可点',
      await cdp.eval(
        '(() => { const b = document.querySelector("#doLogin"); return !!b && !b.disabled; })()'
      )
    );

    // ---- 登录 ----
    await cdp.eval(`
      (() => {
        document.querySelector('#phone').value = '13800000000';
        document.querySelector('#doLogin').click();
      })()
    `);
    await cdp.waitFor(
      'document.querySelector("#app").classList.contains("on")',
      20000,
      '登录后进入主界面'
    );
    ok('登录成功，进入主界面', true);

    // ---- 主界面骨架 ----
    ok('三栏布局存在', await cdp.eval('!!document.querySelector(".col-list") && !!document.querySelector(".chat")'));
    ok(
      '顶栏显示品牌',
      (await cdp.eval('document.querySelector(".brand b")?.textContent')) === '合奏'
    );

    // ---- 场景选择 ----
    await cdp.eval('document.querySelector("#newConvBtn").click()');
    await cdp.waitFor('document.querySelector("#scenes").classList.contains("on")', 8000, '场景页打开');

    const sceneCount = await cdp.eval('document.querySelectorAll(".scene").length');
    ok(`场景数 ≥ 5（实际 ${sceneCount}）`, sceneCount >= 5);

    const sceneNames = await cdp.eval(
      '[...document.querySelectorAll(".scene-n")].map(e => e.textContent.trim())'
    );
    ok('包含竞标场景', sceneNames.some(n => n.includes('竞标')));
    ok('包含二次元场景', sceneNames.some(n => n.includes('二次元')));
    ok(
      '每个场景都画出了角色头像',
      await cdp.eval('[...document.querySelectorAll(".scene")].every(s => s.querySelectorAll(".scene-m .av").length > 0)')
    );

    // ---- 建群 ----
    // 拦截 prompt，直接给个名字
    await cdp.eval('window.prompt = () => "E2E 测试群"');
    await cdp.eval(`
      (() => {
        const el = [...document.querySelectorAll('.scene')].find(s => s.dataset.k === 'companion');
        el.click();
        document.querySelector('#sceneCreate').click();
      })()
    `);
    await cdp.waitFor('document.querySelectorAll(".conv").length > 0', 15000, '会话出现在左栏');
    ok('建群后出现在会话列表', true);

    await cdp.waitFor('document.querySelector("#msgs").style.display !== "none"', 15000, '聊天区打开');
    ok('聊天区已打开', true);

    const members = await cdp.eval('document.querySelectorAll("#memberList .member").length');
    ok(`成员面板有角色（${members} 个）`, members >= 2);

    // ---- 发消息，验证流式 ----
    await cdp.eval(`
      (() => {
        const ta = document.querySelector('#ta');
        ta.value = '你好呀';
        ta.dispatchEvent(new Event('input', { bubbles: true }));
        document.querySelector('#sendBtn').click();
      })()
    `);

    await cdp.waitFor('document.querySelectorAll(".msg.me").length > 0', 10000, '用户消息上屏');
    ok('用户消息已渲染', true);

    // 等至少一条 Agent 消息出现并结束
    await cdp.waitFor(
      'document.querySelectorAll(".msg:not(.me) .msg-text").length > 0',
      30000,
      'Agent 开始回复'
    );
    ok('Agent 开始流式回复', true);

    await cdp.waitFor(
      '[...document.querySelectorAll(".msg:not(.me) .msg-text")].some(t => t.textContent.length > 5)',
      30000,
      'Agent 输出内容'
    );
    const replyLen = await cdp.eval(
      '([...document.querySelectorAll(".msg:not(.me) .msg-text")].map(t => t.textContent.length).sort((a,b)=>b-a)[0]) || 0'
    );
    ok(`Agent 回复有内容（${replyLen} 字）`, replyLen > 5);

    // 等这一轮跑完：既没有正在输出的气泡，也没有思考中的三点
    // .speaking 从 start 加到 end，是判断"还在说"最可靠的信号
    await cdp.waitFor(
      'document.querySelectorAll(".msg.speaking").length === 0',
      60000,
      '本轮结束'
    );
    await sleep(800);

    const msgCount = await cdp.eval('document.querySelectorAll(".msg").length');
    ok(`消息总数 ${msgCount}（≥3）`, msgCount >= 3);

    // 右边流水线有进度
    const pipe = await cdp.eval('document.querySelector("#pipeCount")?.textContent || ""');
    const [doneN, totalN] = pipe.split('/').map(Number);
    ok(`流水线推进到 ${pipe}`, doneN > 0 && totalN > 0);

    // 有角色实际说过话
    const spoke = await cdp.eval('document.querySelectorAll(".msg:not(.me)").length');
    ok(`有 ${spoke} 个角色发言`, spoke >= 1);

    // ---- 「继续聊」：不发言也能让群里自己找话题 ----
    const before = await cdp.eval('document.querySelectorAll(".msg:not(.me)").length');
    await cdp.eval('document.querySelector("#nudgeBtn").click()');
    await cdp.waitFor(
      'document.querySelectorAll(".msg.speaking").length > 0',
      30000,
      '点了继续聊之后有人开口'
    );
    await cdp.waitFor(
      'document.querySelectorAll(".msg.speaking").length === 0',
      60000,
      '继续聊这一轮结束'
    );
    await sleep(500);
    const after = await cdp.eval('document.querySelectorAll(".msg:not(.me)").length');
    ok(`继续聊新增了 ${after - before} 条发言`, after > before);
    ok('按钮已恢复可点', await cdp.eval('!document.querySelector("#nudgeBtn").disabled'));

    // ---- 刷新后仍在（持久化 + 会话恢复）----
    const convId = await cdp.eval('window.__e2eConvId = document.querySelector(".conv").dataset.id; window.__e2eConvId');
    await cdp.send('Page.reload');
    await cdp.waitFor('document.readyState === "complete"', 20000, '刷新完成');
    await cdp.waitFor('document.querySelectorAll(".conv").length > 0', 20000, '刷新后会话仍在');
    ok('刷新后会话仍在（登录态保住了）', true);

    await cdp.eval(`document.querySelector('.conv[data-id="${convId}"]').click()`);
    await cdp.waitFor(
      'document.querySelectorAll(".msg").length >= 2',
      20000,
      '刷新后消息恢复'
    );
    const afterReload = await cdp.eval('document.querySelectorAll(".msg").length');
    ok(`刷新后历史消息恢复（${afterReload} 条）`, afterReload >= 2);

    // ---- 重置数据 ----
    const me = await cdp.eval(
      `fetch('/v1/conversations/${convId}', {method:'DELETE', headers:{'Authorization':'Bearer '+localStorage.getItem('ens_token')}}).then(r=>r.status)`
    );
    ok('清理测试会话', me === 200, `HTTP ${me}`);

  } finally {
    ws.close();
    proc.kill();
    await sleep(300);
    try { rmSync(profile, { recursive: true, force: true }); } catch { /* ignore */ }
  }

  console.log(`\n${pass} 通过, ${fail} 失败\n`);
  if (fail > 0) {
    console.log('失败项：');
    failed.forEach(f => console.log('  - ' + f));
    process.exit(1);
  }
}

run().catch(err => {
  console.error('\nE2E 中断：', err.message);
  process.exit(1);
});
