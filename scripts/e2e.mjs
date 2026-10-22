#!/usr/bin/env node
/**
 * 智团 · 浏览器端到端测试（三栏工作台改版）
 *
 * 不依赖 Playwright / Puppeteer：直接用 CDP（Chrome DevTools Protocol）
 * 连本机 Edge 的 headless 实例，Node 内置 WebSocket 就够。
 *
 * 覆盖：登录 → 团队大厅 → 加入团队 → 房间发消息看到流式回复 →
 *       右面板 → 各页面路由 → 404，并全程监听未捕获的 JS 异常。
 *
 * 用法：
 *   1. 另开一个终端跑后端（mock 模式即可，不消耗额度）
 *        ADDR=:8096 DATA_DIR=/tmp/e2e-new ./bin/ensemble
 *   2. E2E_BASE=http://127.0.0.1:8096 node scripts/e2e.mjs
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
    // 页面里未捕获的异常（事件回调、定时器、模块顶层）都会落到这里。
    // 这是改版回归最直接的信号：接口全绿、页面能渲染，但按钮点了没反应。
    this.exceptions = [];
    ws.addEventListener('message', ev => {
      const msg = JSON.parse(ev.data);
      if (msg.method === 'Runtime.exceptionThrown') {
        this.exceptions.push(msg.params.exceptionDetails);
        return;
      }
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
    // CI 的 runner 以 root 运行，不加这个 Chrome 起不来
    '--no-sandbox',
    '--disable-dev-shm-usage',
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

  // 跑之前把演示账号的额度清零。
  // 不然反复跑几十轮之后额度耗尽，开场直接返回 quota 事件，
  // 表现为「Agent 回复」超时——看起来像前端坏了。
  try {
    const lr = await fetch(BASE + '/v1/auth/login', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ kind: 'phone', target: '13800000000' }),
    }).then(x => x.json());
    await fetch(`${BASE}/admin/api/users/${lr.data.user.id}/reset-usage`, {
      method: 'POST', headers: { 'X-Admin-Token': 'admin-dev-token' },
    });

    // 上次跑挂了会留下「E2E 测试群」/ 预设团队。不清的话左侧房间列表
    // 越来越脏，演示数据也会被污染。
    const stale = await fetch(`${BASE}/v1/conversations`, {
      headers: { Authorization: 'Bearer ' + lr.data.token },
    }).then(x => x.json()).then(j => (j.data || []).filter(c => c.title.startsWith('E2E 测试群')));
    for (const c of stale) {
      await fetch(`${BASE}/v1/conversations/${c.id}`, {
        method: 'DELETE', headers: { Authorization: 'Bearer ' + lr.data.token },
      }).catch(() => {});
    }
    if (stale.length) console.log(`  清掉 ${stale.length} 个上次残留的测试群`);
  } catch { /* 重置失败就让后面的断言自己报 */ }

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
    console.error(`\n后端没起来：${BASE}\n先跑 ADDR=:8096 DATA_DIR=/tmp/e2e-new ./bin/ensemble\n`);
    process.exit(2);
  }

  const { cdp, proc, profile, ws } = await launch();

  try {
    console.log('\n客户端 E2E（三栏工作台）\n');

    await cdp.send('Page.navigate', { url: `${BASE}/app/` });
    await cdp.waitFor('document.readyState === "complete"', 20000, '页面加载');

    // ---- 登录页 ----
    ok('登录页出现', await cdp.eval('!!document.querySelector("#login") && !document.querySelector("#login").classList.contains("hidden")'));
    ok('登录页有手机号输入', await cdp.eval('!!document.querySelector("#phone") && !!document.querySelector("#code")'));
    ok('登录页有邮箱输入', await cdp.eval('!!document.querySelector("#email") && !!document.querySelector("#code2")'));
    ok(
      '登录按钮可点',
      await cdp.eval(
        '(() => { const b = document.querySelector("#doLogin"); return !!b && !b.disabled; })()'
      )
    );

    // 手机号 / 邮箱分段切换：切到邮箱时手机表单隐藏，切回来恢复。
    const tabs = await cdp.eval(`(() => {
      const btns = [...document.querySelectorAll('#loginSeg button')];
      const email = btns.find(b => b.dataset.k === 'email');
      const phone = btns.find(b => b.dataset.k === 'phone');
      if (!email || !phone) return 'missing';
      email.click();
      const emailOn = !document.querySelector('#loginEmail').classList.contains('hidden')
        && document.querySelector('#loginPhone').classList.contains('hidden')
        && email.classList.contains('on');
      phone.click();
      const phoneOn = !document.querySelector('#loginPhone').classList.contains('hidden')
        && document.querySelector('#loginEmail').classList.contains('hidden')
        && phone.classList.contains('on');
      return emailOn && phoneOn ? 'ok' : 'bad';
    })()`);
    ok('手机号 / 邮箱标签切换正常', tabs === 'ok', tabs);

    // ---- 登录 ----
    // 引导蒙版只在第一次登录出现，且会抢走路由（900ms 后弹）。
    // 必须在 enterApp() 之前就把标记写进去，不能等登录完成再写。
    await cdp.eval(`localStorage.setItem('ens_onboarded','1')`);
    await cdp.eval(`
      (() => {
        document.querySelector('#phone').value = '13800000000';
        document.querySelector('#doLogin').click();
      })()
    `);
    await cdp.waitFor(
      '!document.querySelector("#app").classList.contains("hidden") && document.querySelector("#login").classList.contains("hidden")',
      20000,
      '登录后进入主界面'
    );
    ok('登录成功，进入主界面', true);
    ok('没有弹出新手引导', await cdp.eval('!document.querySelector(".onb-mask")'));

    // ---- 大厅 ----
    await cdp.waitFor('location.hash.startsWith("#/lobby")', 8000, '落在大厅路由');
    ok('登录后落在 #/lobby', true);

    const nav = await cdp.eval(
      '[...document.querySelectorAll("#navList .nav-item")].map(e => e.dataset.key)'
    );
    ok(`导航有 6 项（${nav.join(' / ')}）`, nav.length === 6
      && ['lobby', 'room', 'goals', 'connectors', 'library', 'settings'].every(k => nav.includes(k)));

    const cardCount = await cdp.eval('document.querySelectorAll(".team-card").length');
    ok(`大厅有 4 张预设团队卡（实际 ${cardCount}）`, cardCount === 4);

    const joinCount = await cdp.eval('document.querySelectorAll(".team-card [data-join]").length');
    ok(`每张卡都有「加入团队」（${joinCount} 个）`, joinCount === cardCount);

    const h1 = await cdp.eval('document.querySelector(".page-head h1")?.textContent.trim()');
    ok(`大厅标题是「团队大厅」（${h1}）`, h1 === '团队大厅');
    ok('大厅有新建自定义团队入口', await cdp.eval('!!document.querySelector("#newTeam")'));

    // ---- 加入团队 → 房间 ----
    await cdp.eval(`document.querySelector('.team-card [data-join]').click()`);
    await cdp.waitFor('location.hash.startsWith("#/room/")', 15000, '加入后进入房间路由');
    const roomHash = await cdp.eval('location.hash');
    ok(`加入团队后跳到房间（${roomHash}）`, /^#\/room\/.+/.test(roomHash));

    await cdp.waitFor('!!document.querySelector(".room")', 15000, '房间渲染');
    const roomTitle = await cdp.eval('document.querySelector(".rh-title")?.textContent.trim()');
    ok(`房间标题非空（${roomTitle}）`, !!roomTitle);
    ok('左侧房间列表出现该房间', await cdp.eval(
      'document.querySelectorAll("#navRooms .nav-room").length > 0'
    ));

    await cdp.waitFor('document.querySelectorAll("#avatarRow .av-cell").length > 0', 15000, '头像条');
    const avatars = await cdp.eval('document.querySelectorAll("#avatarRow .av-cell").length');
    ok(`头像条有 ${avatars} 个角色（≥3）`, avatars >= 3);

    ok('输入条存在', await cdp.eval(
      '!!document.querySelector("#msgStream") && !!document.querySelector("#msgWrap") && !!document.querySelector("#ta") && !!document.querySelector("#sendBtn")'
    ));

    // ---- 发消息，验证流式 ----
    await cdp.eval(`
      (() => {
        const ta = document.querySelector('#ta');
        ta.value = '你好呀';
        ta.dispatchEvent(new Event('input', { bubbles: true }));
        document.querySelector('#sendBtn').click();
      })()
    `);

    await cdp.waitFor('document.querySelectorAll(".msg.me").length > 0', 15000, '用户消息上屏');
    ok('用户消息已渲染', true);

    // 至少两条：自己的 + 至少一个角色的回复。mock 上游也会正经吐字。
    await cdp.waitFor(
      '[...document.querySelectorAll(".msg:not(.me) .msg-text")].some(t => t.textContent.trim().length > 0)',
      60000,
      'Agent 回复有内容'
    );
    const msgCount = await cdp.eval('document.querySelectorAll(".msg").length');
    ok(`消息总数 ${msgCount}（≥2）`, msgCount >= 2);

    const replyLen = await cdp.eval(
      '([...document.querySelectorAll(".msg:not(.me) .msg-text")].map(t => t.textContent.trim().length).sort((a,b)=>b-a)[0]) || 0'
    );
    ok(`Agent 回复有内容（${replyLen} 字）`, replyLen > 0);

    await cdp.waitFor(
      'document.querySelectorAll(".msg.speaking").length === 0',
      60000,
      '本轮结束'
    );
    // 发送键在忙时是红色方块（.stop），闲时恢复成箭头。
    // 这里等到它真的复位，说明 runRound 的 finally 跑完了，
    // 后面的面板/路由操作不会和收尾逻辑抢 DOM。
    await cdp.waitFor(
      '!document.querySelector("#sendBtn").classList.contains("stop")',
      20000,
      '发送键复位'
    );
    ok('流式回复正常结束', true);

    // ---- 右面板 ----
    await cdp.eval('document.querySelector("#roomPanel").click()');
    await cdp.waitFor('document.querySelector("#panel").classList.contains("on")', 10000, '右面板打开');
    ok('点工作面板后 #panel 打开（class on）', true);
    ok('面板里有房间概览', await cdp.eval(
      '!!document.querySelector("#panelClose") && document.querySelector("#panelBody").textContent.includes("房间概览")'
    ));
    await cdp.eval('document.querySelector("#panelClose").click()');
    await cdp.waitFor('!document.querySelector("#panel").classList.contains("on")', 8000, '右面板关闭');
    ok('点 #panelClose 后面板关闭', true);

    // ---- 各页面路由与标题 ----
    // 路由是 hash 的，切页不能整页刷新（会重新走 boot）。
    const pages = [
      ['goals', '目标看板', '!!document.querySelector("#goalView") && !!document.querySelector("#goalFilter")'],
      ['connectors', '连接器', '!!document.querySelector("#newConn")'],
      ['library', '资料库', '!!document.querySelector("#libConv") && !!document.querySelector("#libKind")'],
      ['settings', '设置', '!!document.querySelector(".profile-card") && document.querySelectorAll(".settings-list .set-row").length >= 5'],
    ];
    for (const [route, title, extra] of pages) {
      await cdp.eval(`location.hash = '#/${route}'`);
      await cdp.waitFor(
        `document.querySelector('.page-head h1')?.textContent.trim() === ${JSON.stringify(title)}`,
        15000,
        `${title} 页面`
      );
      ok(`#/${route} 标题是「${title}」`, true);
      ok(`#/${route} 关键控件存在`, await cdp.eval(extra));
    }

    // 设置页的操作项齐全（data-act 是 JS 绑定的入口，缺一个就是死按钮）
    const acts = await cdp.eval(
      '[...document.querySelectorAll(".settings-list [data-act]")].map(e => e.dataset.act)'
    );
    ok(
      `设置页操作项齐全（${acts.join(',')}）`,
      ['editName', 'toggleNotify', 'security', 'privacy', 'logout'].every(a => acts.includes(a))
    );

    // ---- 404 ----
    await cdp.eval('location.hash = "#/nope"');
    await cdp.waitFor(
      'document.querySelector(".es-t")?.textContent.trim() === "页面不存在"',
      10000,
      '404 页面'
    );
    ok('#/nope 显示「页面不存在」', true);

    // ---- 全程没有未捕获的 JS 异常 ----
    // 接口 200、页面能渲染，不代表交互是活的：事件回调里的 TypeError
    // 只会体现在「点了没反应」。这里把整轮收集到的异常一次性报出来。
    const exs = cdp.exceptions.map(e =>
      (e.exception?.description || e.text || '').split('\n')[0]
    ).filter(Boolean);
    ok(`全程没有未捕获的 JS 异常（${exs.length} 个）`, exs.length === 0, exs.slice(0, 3).join(' | '));

    // ---- 清理：删掉本轮建的房间 ----
    // 不删的话每跑一次大厅就多一张「竞标工作室」，跑几十轮之后
    // 演示数据全是重复房间。用接口删，比在界面上找按钮可靠。
    const roomId = decodeURIComponent(roomHash.replace(/^#\/room\//, ''));
    const del = await cdp.eval(
      `fetch('/v1/conversations/${encodeURIComponent(roomId)}', {method:'DELETE', headers:{'Authorization':'Bearer '+localStorage.getItem('ens_token')}}).then(r=>r.status)`
    );
    ok('清理测试房间', del === 200, `HTTP ${del}`);

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
