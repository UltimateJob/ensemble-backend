#!/usr/bin/env node
/**
 * 逐屏截图：每个主视图在三种宽度下各拍一张，供人眼走查。
 *
 * 不是测试，不设断言——只负责把界面稳定地截下来。
 * 用法：node scripts/ui-shots.mjs [视图...]
 *   视图：lobby room goals connectors library settings（默认全部）
 * 环境变量：SHOT_BASE（默认 http://127.0.0.1:8092）、EDGE_PATH
 *
 * 产物：.ui-shots/<宽度>-<视图>.png
 */

import { spawn } from 'node:child_process';
import { mkdtempSync, mkdirSync, rmSync, statSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const BASE = process.env.SHOT_BASE || 'http://127.0.0.1:8092';
const EDGE =
  process.env.EDGE_PATH ||
  '/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge';
const PORT = 9445;
const OUT = '.ui-shots';
const WIDTHS = [1440, 1180, 390];

const ALL_VIEWS = ['lobby', 'room', 'goals', 'connectors', 'library', 'settings'];
// 页面标题是「页面真的挂上了」最可靠的信号
const HEADINGS = {
  lobby: '团队大厅',
  goals: '目标看板',
  connectors: '连接器',
  library: '资料库',
  settings: '设置',
};

const wanted = process.argv.slice(2).filter(a => !a.startsWith('-'));
const views = wanted.length ? wanted : ALL_VIEWS;
for (const v of views) {
  if (!ALL_VIEWS.includes(v)) {
    console.error(`未知视图：${v}\n可选：${ALL_VIEWS.join(' ')}`);
    process.exit(2);
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
  const profile = mkdtempSync(join(tmpdir(), 'ens-shots-'));
  const proc = spawn(
    EDGE,
    [
      '--headless=new',
      '--no-sandbox',
      '--disable-dev-shm-usage',
      `--remote-debugging-port=${PORT}`,
      '--disable-gpu',
      '--no-first-run',
      '--no-default-browser-check',
      '--disable-extensions',
      // 滚动条会吃掉十几像素宽，截图里看起来像布局溢出
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

/* ---------------- 截图主体 ---------------- */
async function run() {
  const health = await fetch(`${BASE}/healthz`).catch(() => null);
  if (!health || !health.ok) {
    console.error(`\n后端没起来：${BASE}\n先跑 ADDR=:8092 ./bin/ensemble\n`);
    process.exit(2);
  }

  mkdirSync(OUT, { recursive: true });

  const written = [];
  const empty = [];
  const skipped = [];
  // 浏览器或连接在 launch() 中途失败时也要能收尾，所以放在 try 外先占位
  let cdp;
  let proc;
  let profile;
  let ws;

  try {
    ({ cdp, proc, profile, ws } = await launch());

    await cdp.send('Page.navigate', { url: `${BASE}/app/` });
    await cdp.waitFor('document.readyState === "complete"', 20000, '页面加载');
    await cdp.waitFor('!!document.querySelector("#phone")', 15000, '登录页');

    // 演示账号空验证码即可登录（DEV_BYPASS）。
    // 引导会在 900ms 后弹蒙版并可能改路由，所以点完登录立刻落标记。
    await cdp.eval(`(() => {
      document.querySelector('#phone').value = '13800000000';
      document.querySelector('#doLogin').click();
      localStorage.setItem('ens_onboarded', '1');
    })()`);
    await cdp.waitFor(
      '!document.querySelector("#app").classList.contains("hidden") && document.querySelectorAll("#navList .nav-item").length > 0',
      30000,
      '登录后进入主界面'
    );

    // 大厅卡片依赖 /v1/scenarios，等它渲染出来再截图 / 建房间
    await cdp.waitFor(
      'document.querySelectorAll(".team-card [data-join]").length > 0',
      20000,
      '大厅预设团队卡片'
    );

    // 先建一个房间：直接点第一张卡片的「加入团队」
    let roomId = '';
    try {
      await cdp.eval(`document.querySelector('.team-card [data-join]').click()`);
      await cdp.waitFor(`location.hash.startsWith('#/room/')`, 25000, '进入房间');
      roomId = await cdp.eval(
        `decodeURIComponent(location.hash.replace('#/room/', ''))`
      );
      await cdp.waitFor('!!document.querySelector(".rh-title")', 20000, '房间标题');
      console.log(`已创建房间：${roomId}`);
    } catch (e) {
      console.log(`房间创建失败，跳过 room：${e.message}`);
    }

    for (const width of WIDTHS) {
      await cdp.send('Emulation.setDeviceMetricsOverride', {
        width,
        height: 900,
        deviceScaleFactor: 1,
        mobile: width === 390,
      });
      // 等布局按新宽度重排完，再开始导航
      await sleep(400);

      for (const view of views) {
        if (view === 'room' && !roomId) {
          skipped.push(`${width}-room`);
          continue;
        }

        const hash =
          view === 'room'
            ? '#/room/' + encodeURIComponent(roomId)
            : '#/' + view;

        try {
          await cdp.eval(`location.hash = ${JSON.stringify(hash)}`);

          // 等页面标题出现，确认不是还停在上一页
          if (view === 'room') {
            await cdp.waitFor(
              `location.hash.startsWith('#/room/') && !!document.querySelector('.rh-title')`,
              20000,
              '房间标题'
            );
          } else {
            await cdp.waitFor(
              `document.querySelector('.page-head h1')?.textContent.trim() === ${JSON.stringify(HEADINGS[view])}`,
              20000,
              `${view} 标题`
            );
          }

          // 视图挂载后还会拉数据、铺骨架，等它稳定下来
          await sleep(1200);

          // 内容区被 CSS 藏起来时 innerText 仍能拿到文字，必须看实际盒子和可见性，
          // 否则「整页空白」会被当成正常截下来
          const info = await cdp.eval(`(() => {
            const main = document.querySelector('#main');
            const box = main?.getBoundingClientRect();
            const shown = !!main && getComputedStyle(main).display !== 'none' &&
              !!box && box.width > 0 && box.height > 0;
            return JSON.stringify({
              shown,
              heading: (document.querySelector('.page-head h1') || document.querySelector('.rh-title'))?.textContent.trim() || '',
              textLen: shown ? (main.innerText || '').replace(/\\s/g, '').length : 0,
            });
          })()`);
          const meta = JSON.parse(info);

          const file = join(OUT, `${width}-${view}.png`);
          const shot = await cdp.send('Page.captureScreenshot', { format: 'png' });
          writeFileSync(file, Buffer.from(shot.data, 'base64'));
          const size = statSync(file).size;
          written.push({ file, size });

          // 只有标题没有正文，通常说明数据请求失败或视图没挂全
          if (!meta.shown || meta.textLen < 40) empty.push(`${width}-${view}`);
          console.log(
            `  ${String(width).padStart(4)} ${view.padEnd(10)} ${(size / 1024).toFixed(1)} KB  ${meta.shown ? meta.heading : '(内容区不可见)'}`
          );
        } catch (e) {
          skipped.push(`${width}-${view}`);
          console.log(`  ${String(width).padStart(4)} ${view.padEnd(10)} 跳过：${e.message}`);
        }
      }
    }

    console.log(`\n已生成 ${written.length} 张截图（${OUT}/）：`);
    for (const f of written) {
      console.log(`  ${f.file}  ${(f.size / 1024).toFixed(1)} KB`);
    }
    if (skipped.length) console.log(`\n跳过：${skipped.join('、')}`);
    if (empty.length) console.log(`\n内容偏空：${empty.join('、')}`);
  } finally {
    ws?.close();
    proc?.kill();
    await sleep(300);
    // Chrome 退出时可能还攥着 profile 里的文件，删不掉不影响结果
    try {
      if (profile) rmSync(profile, { recursive: true, force: true });
    } catch {
      /* 忽略 */
    }
  }
}

run().catch(err => {
  console.error('\n截图中断：', err.message);
  process.exit(1);
});
