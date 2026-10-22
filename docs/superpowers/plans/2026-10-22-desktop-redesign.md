# 智团桌面端改版 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把智团 PC 端从「单聊天页 + 弹层」改造成三栏工作台（全套 16 屏），并修好 macOS 桌面壳「双击即用」。

**Architecture:** 前端保持零构建（vanilla ES modules），新增 hash 路由、全局壳、视图目录与全局 Run 订阅总线；后端只补静态资源查找路径与 `/v1/me` 字段；桌面壳用 AppKit + WKWebView，负责定位二进制、加载 .env、选端口、处理下载与通知。黏土头像离线预生成，前端查 manifest，未命中回退 CSS 首字头像。

**Tech Stack:** Go 1.24（标准库）、原生 JS ES modules、CSS 自定义属性、Swift/AppKit/WKWebView、Node 22（脚本）、CDP（E2E）。

**Spec:** `docs/superpowers/specs/2026-10-22-desktop-redesign-design.md`

## Global Constraints

- 主色 `#367BED`，页面底 `#FFFFFF`，次级面 `#F7F7F8`，分隔线 `#EDEDF0`。
- 圆角：卡片/弹层 `20px`，小控件 `12px`。阴影统一 `0 2px 10px rgba(0,0,0,.06)`。
- 禁止：渐变、发光、深色主题、厚投影、霓虹。图标 `stroke-width: 2`，无填充。
- 状态色 `#4CAF50 / #E6A23C / #F56C6C / #86868B` 只用于环、点、图标；正文用深色变体（`#2E7D32` / `#8A5A00` / `#B3261E` / `#5A5A63`）。
- 前端零构建：不引入 npm 依赖、不生成 bundle；模块用原生 `import`。
- 所有既有接口语义不变；新接口沿用 `{ok,data,error}` 信封（`internal/httpapi/response.go`）。
- 头像生成 Key 只从环境变量读（`IMAGE_API_BASE` / `IMAGE_API_KEY`），不得写进仓库或提交记录。
- 每个里程碑结束必须：`go build ./...`、`go test ./... -count=1`、`gofmt -l .` 无输出、E2E 通过、逐屏截图人工过目。

---

## 文件结构（先锁定）

**新建**

```
web/app/index.html                     新壳（替换现有）
web/app/css/tokens.css                 设计 token（唯一颜色来源）
web/app/css/shell.css                  左导航 / 顶栏 / 右面板 / 响应式
web/app/css/views.css                  页面级组件（大厅/目标/连接器/资料库/设置）
web/app/css/chat.css                   消息流 / 气泡 / 任务卡 / 输入条
web/app/css/components.css             按钮 / 卡片 / Toast / 骨架 / 弹层 / 空状态
web/app/js/router.js                   hash 路由
web/app/js/shell.js                    导航渲染 / 右面板 / 快捷键
web/app/js/runs.js                     Run 订阅总线（全局审批依赖）
web/app/js/modal.js                    通用弹层 + 审批弹窗 + 权限拒绝
web/app/js/avatars.js                  头像解析与回退
web/app/js/onboarding.js               四步引导
web/app/js/agentpanel.js               Agent 工作面板
web/app/views/lobby.js
web/app/views/room.js
web/app/views/goals.js
web/app/views/connectors.js
web/app/views/library.js
web/app/views/settings.js
web/app/views/studio.js               房间内全宽工作室视图
web/app/views/notfound.js
web/app/avatars/manifest.json
web/app/avatars/*.png                  （生成物，90 张角色 + 7 张团队封面）
scripts/avatars.mjs                    头像批量生成
scripts/ui-shots.mjs                   逐屏截图
scripts/check-console.mjs              控制台无报错检查（可并入 e2e）
```

**修改**

```
web/app/js/core.js                     state 扩展、API 保留、toast 改右上、骨架工具
web/app/js/chat.js                     适配新 DOM，消息渲染与 SSE 逻辑复用
web/app/js/dialog.js                   转为 modal.js 的一部分（保留导出以兼容）
web/app/js/convs.js                    只剩房间列表渲染（导航内联）
web/app/js/memory.js                   适配新右面板样式
internal/httpapi/server.go             webDir 支持 Contents/Resources/web
internal/httpapi/handler_me.go         /v1/me 增加 preferences
internal/domain/types.go               User 增加 Preferences
desktop/EnsembleDesktop.swift          重写
desktop/build.sh                       重写打包流程
scripts/e2e.mjs                        按新路由重写断言
scripts/e2e-mobile.mjs                 目标选择器更新
scripts/audit-ui.py                    选择器更新
Makefile                               增加 avatars / shots 目标
```

**删除**

```
web/app/app.css                        合入 css/ 下
web/app/js/app.js                      拆入 router/shell/views
```

---

# M1：壳、路由、登录、大厅、房间、右面板、审批

## Task 1: 设计 token 与蓝色 logo

**Files:**
- Create: `web/app/css/tokens.css`
- Modify: `web/site/brand/logo.svg`, `logo-dark.svg`, `logo-mono.svg`, `favicon.svg`
- Modify: `docs/brand/`（仅新增说明，不改旧图）
- Test: `scripts/ui-shots.mjs`（Task 12 建立后回归）

**Interfaces:**
- Produces: CSS 变量 `--pri --bg --surface --line --tx --tx-2 --tx-3 --ok --warn --bad --idle --r-lg --r-md --sh`，后续所有 CSS 只引用这些变量。

- [ ] **Step 1: 写 tokens.css**

```css
/* 智团 · 设计 token。所有颜色/圆角/阴影只从这里取。 */
:root{
  --pri:#367BED;
  --pri-hover:#2A6ADC;
  --pri-soft:#EAF2FE;
  --bg:#FFFFFF;
  --surface:#F7F7F8;
  --surface-2:#F2F3F5;
  --line:#EDEDF0;
  --line-2:#E1E2E6;
  --tx:#1A1A1D;
  --tx-2:#5A5A63;
  --tx-3:#86868B;
  --tx-4:#A6A6AD;
  --ok:#4CAF50;   --ok-tx:#2E7D32;   --ok-soft:#EDF7EE;
  --warn:#E6A23C; --warn-tx:#8A5A00; --warn-soft:#FDF3E6;
  --bad:#F56C6C;  --bad-tx:#B3261E;  --bad-soft:#FDEDED;
  --idle:#86868B; --idle-soft:#F0F0F2;
  --r-lg:20px; --r-md:12px; --r-sm:8px;
  --sh:0 2px 10px rgba(0,0,0,.06);
  --sh-lift:0 4px 16px rgba(0,0,0,.08);
  --nav-w:232px; --panel-w:380px; --panel-w-lg:480px;
  --font:-apple-system,BlinkMacSystemFont,"PingFang SC","Microsoft YaHei",sans-serif;
  --mono:ui-monospace,"SF Mono",Menlo,monospace;
}
```

- [ ] **Step 2: 重做四份 SVG**

单色扁平，保留「中心大星 + 环绕三星」形态。`logo.svg` 用 `#367BED` 填充；`logo-dark.svg` 用白色填充（深底用）；`logo-mono.svg` 用 `currentColor`；`favicon.svg` 加粗星形，16px 可辨。每份 `viewBox="0 0 64 64"`，无渐变、无阴影。

- [ ] **Step 3: 校验四份 SVG 可渲染**

```bash
for f in web/site/brand/logo.svg web/site/brand/logo-dark.svg web/site/brand/logo-mono.svg web/site/brand/favicon.svg; do
  python3 -c "import xml.dom.minidom,sys; xml.dom.minidom.parse('$f')" || echo "BAD $f"
done
grep -n "gradient\|filter\|url(#" web/site/brand/*.svg && echo "❌ 仍有渐变/滤镜" || echo "✓ 无渐变"
```

- [ ] **Step 4: 提交**

```bash
git add web/app/css/tokens.css web/site/brand docs/brand
git commit -m "feat(ui): 蓝色扁平设计 token 与单色 logo"
```

---

## Task 2: 新壳与路由

**Files:**
- Create: `web/app/index.html`, `web/app/js/router.js`, `web/app/js/shell.js`
- Test: `scripts/e2e.mjs`（新增路由断言）

**Interfaces:**
- Produces:
  - `router.js`: `navigate(path, {replace})`, `currentRoute() -> {name, params}`, `startRouter(views)`, `routeHref(name, params)`。`views` 形如 `{lobby: {mount(root, params), unmount()}, room: {...}, ...}`。
  - `shell.js`: `initShell()`, `setActiveNav(name)`, `openPanel({title, subtitle, render})`, `closePanel()`, `togglePanel()`, `panelOpen() -> bool`。
- Consumes: Task 1 的 CSS 变量。

- [ ] **Step 1: 写 index.html 骨架**

```html
<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1,viewport-fit=cover">
<title>智团</title>
<link rel="icon" type="image/svg+xml" href="/site/brand/favicon.svg">
<link rel="stylesheet" href="css/tokens.css?v=1">
<link rel="stylesheet" href="css/components.css?v=1">
<link rel="stylesheet" href="css/shell.css?v=1">
<link rel="stylesheet" href="css/views.css?v=1">
<link rel="stylesheet" href="css/chat.css?v=1">
</head>
<body>
<div id="login" class="login hidden"><!-- Task 4 --></div>
<div id="app" class="app hidden">
  <aside class="nav" id="nav">
    <div class="nav-brand">
      <img src="/site/brand/logo.svg" alt="" class="nav-logo"><b>智团</b>
    </div>
    <nav class="nav-list" id="navList"></nav>
    <div class="nav-foot" id="navFoot"></div>
  </aside>
  <main class="main" id="main"></main>
  <aside class="panel" id="panel" aria-hidden="true">
    <div class="panel-handle" id="panelHandle" title="展开工作面板"></div>
    <div class="panel-body" id="panelBody"></div>
  </aside>
</div>
<div id="toast" class="toast"></div>
<div id="modalRoot"></div>
<script src="icons.js?v=1"></script>
<script type="module" src="js/boot.js?v=1"></script>
</body>
</html>
```

- [ ] **Step 2: 写 router.js**

```js
/* hash 路由：无服务端改动，旧 ?c= 链接自动迁移。 */
const routes = [];
let active = null;

export function defineRoutes(list) { routes.length = 0; routes.push(...list); }

export function parse(hash) {
  const raw = (hash || location.hash || '#/lobby').replace(/^#/, '');
  const [path, qs] = raw.split('?');
  const parts = path.split('/').filter(Boolean);
  const q = new URLSearchParams(qs || '');
  if (!parts.length || parts[0] === 'lobby') return { name: 'lobby', params: {} };
  const [head, id] = parts;
  if (head === 'room') return { name: 'room', params: { id: id || q.get('c') || '' } };
  if (head === 'c' && id) return { name: 'room', params: { id } };
  if (['goals', 'connectors', 'library', 'settings'].includes(head)) return { name: head, params: {} };
  return { name: 'notfound', params: {} };
}

export function currentRoute() { return parse(); }

export function navigate(path, { replace = false } = {}) {
  const target = path.startsWith('#') ? path : '#' + path;
  if (replace) history.replaceState(null, '', target);
  else location.hash = target;
  if (replace) render();
}

export function routeHref(name, params = {}) {
  if (name === 'room') return '#/room/' + encodeURIComponent(params.id || '');
  return '#/' + name;
}

let viewInstances = {};
export function defineViews(v) { viewInstances = v; }

function render() {
  const r = parse();
  const host = document.getElementById('main');
  if (!host) return;
  if (active && active.unmount) { try { active.unmount(); } catch (e) { console.error(e); } }
  host.innerHTML = '';
  const view = viewInstances[r.name] || viewInstances.notfound;
  active = view;
  if (!view) return;
  view.mount(host, r.params);
  const { setActiveNav } = window.__shell || {};
  if (setActiveNav) setActiveNav(r.name);
  document.dispatchEvent(new CustomEvent('route:change', { detail: r }));
}

export function startRouter() {
  // 旧链接：/app/?c=<id> → #/room/<id>
  const qc = new URLSearchParams(location.search).get('c');
  if (qc && !location.hash) history.replaceState(null, '', '#/room/' + encodeURIComponent(qc));
  window.addEventListener('hashchange', render);
  render();
}
```

- [ ] **Step 3: 写 shell.js**

```js
import { $, esc, icon, hydrateIcons } from './core.js';

const NAV = [
  { key: 'lobby',      label: '团队大厅', ic: 'users' },
  { key: 'room',       label: '聊天房间', ic: 'message-circle' },
  { key: 'goals',      label: '目标看板', ic: 'trending-up' },
  { key: 'connectors', label: '连接器',   ic: 'share-2' },
  { key: 'library',    label: '资料库',   ic: 'book-open' },
  { key: 'settings',   label: '设置',     ic: 'settings' },
];

export function setActiveNav(name) {
  document.querySelectorAll('#navList .nav-item').forEach(el => {
    el.classList.toggle('on', el.dataset.key === name || (name === 'room' && el.dataset.key === 'room'));
  });
}

export function renderNav(convs = []) {
  const list = $('#navList');
  if (!list) return;
  list.innerHTML = NAV.map(n => `
    <a class="nav-item" data-key="${n.key}" href="#/${n.key}">
      ${icon(n.ic, 18)}<span>${n.label}</span>
    </a>`).join('') + `
    <div class="nav-rooms" id="navRooms">
      <div class="nav-rooms-h">房间</div>
      ${convs.map(c => `
        <a class="nav-room" data-id="${esc(c.id)}" href="#/room/${encodeURIComponent(c.id)}">
          <span class="dot"></span><span class="nm">${esc(c.title)}</span>
        </a>`).join('') || '<div class="nav-room-empty">还没有房间</div>'}
    </div>`;
  hydrateIcons(list);
  setActiveNav(location.hash.replace('#/', '').split('/')[0] || 'lobby');
}

export function openPanel({ title = '', subtitle = '', render }) {
  const panel = $('#panel'), body = $('#panelBody');
  if (!panel || !body) return;
  body.innerHTML = `
    <div class="panel-head">
      <div class="panel-title">${esc(title)}</div>
      ${subtitle ? `<div class="panel-sub">${esc(subtitle)}</div>` : ''}
      <button class="icon-btn panel-x" id="panelClose" title="收起">${icon('x', 16)}</button>
    </div>
    <div class="panel-content" id="panelContent"></div>`;
  panel.classList.add('on');
  panel.setAttribute('aria-hidden', 'false');
  const close = body.querySelector('#panelClose');
  if (close) close.addEventListener('click', closePanel);
  const content = body.querySelector('#panelContent');
  if (render) render(content);
}

export function closePanel() {
  const panel = $('#panel');
  if (!panel) return;
  panel.classList.remove('on');
  panel.setAttribute('aria-hidden', 'true');
}

export function panelOpen() {
  return !!$('#panel')?.classList.contains('on');
}

export function togglePanel() {
  if (panelOpen()) closePanel();
  else openPanel({ title: '工作面板', render: el => { el.innerHTML = '<div class="panel-empty">点开一个角色或产物，这里显示详情</div>'; } });
}

export function initShell() {
  window.__shell = { setActiveNav };
  document.addEventListener('keydown', e => {
    const mod = e.metaKey || e.ctrlKey;
    if (mod && e.key === '0') { e.preventDefault(); togglePanel(); }
    const n = Number(e.key);
    if (mod && n >= 1 && n <= 6) {
      e.preventDefault();
      location.hash = '#/' + NAV[n - 1].key;
    }
  });
}
```

- [ ] **Step 4: 写 boot.js（启动装配，替代 app.js）**

```js
import { state, $, api, hydrateIcons } from './core.js';
import { defineViews, startRouter, navigate } from './router.js';
import { initShell, renderNav } from './shell.js';
import * as lobby from '../views/lobby.js';
import * as room from '../views/room.js';
import * as goals from '../views/goals.js';
import * as connectors from '../views/connectors.js';
import * as library from '../views/library.js';
import * as settings from '../views/settings.js';
import * as notfound from '../views/notfound.js';

defineViews({ lobby, room, goals, connectors, library, settings, notfound });
initShell();

async function boot() {
  if (!state.token) return showLogin();
  try {
    const d = await api('/v1/me');
    state.user = d.user;
    state.me = d;
    await enterApp();
  } catch (e) {
    if (e.status === 401) { localStorage.removeItem('ens_token'); state.token = ''; showLogin(); }
    else showLogin('网络不通，或者服务正在重启');
  }
}

export async function enterApp() {
  $('#login')?.classList.add('hidden');
  $('#app')?.classList.remove('hidden');
  state.convs = await api('/v1/conversations').catch(() => []);
  state.scenarios = await api('/v1/scenarios').catch(() => []);
  renderNav(state.convs);
  startRouter();
}

function showLogin(msg = '') {
  $('#login')?.classList.remove('hidden');
  $('#app')?.classList.add('hidden');
  window.__loginMsg = msg;
  document.dispatchEvent(new CustomEvent('login:show', { detail: { msg } }));
}

window.__ens = { navigate, enterApp, boot };
boot();
```

- [ ] **Step 5: 跑起来确认壳渲染**

```bash
go build -o bin/ensemble ./cmd/server && (ADDR=:8091 DATA_DIR=/tmp/ui-m1 ./bin/ensemble &) && sleep 1
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8091/app/
curl -s http://127.0.0.1:8091/app/index.html | grep -c 'js/boot.js'
pkill -f 'ADDR=:8091' || true
```

Expected: `200`，第二行 `1`。

- [ ] **Step 6: 提交**

```bash
git add web/app/index.html web/app/js/router.js web/app/js/shell.js web/app/js/boot.js
git commit -m "feat(ui): 三栏壳与 hash 路由"
```

---

## Task 3: core.js 适配（toast 右上、骨架、状态扩展）

**Files:**
- Modify: `web/app/js/core.js`
- Test: E2E 断言 `#toast.toast--ok` 出现在右上

**Interfaces:**
- Consumes: 现有 `api/esc/toast/state/makeAv`。
- Produces: `toast(msg, {type, ms})`、`skeleton(kind)`、`emptyState({icon,title,sub,action})`、`state.route`。

- [ ] **Step 1: 替换 toast 实现**

```js
export function toast(msg, opt = {}) {
  const { type = 'info', ms = 2200 } = typeof opt === 'number' ? { ms: opt } : opt;
  const t = $('#toast');
  if (!t) return;
  t.className = 'toast toast--' + type + ' on';
  t.innerHTML = `${icon(type === 'ok' ? 'check' : type === 'err' ? 'x' : 'info', 16)}<span>${esc(msg)}</span>`;
  clearTimeout(t._t);
  t._t = setTimeout(() => t.classList.remove('on'), ms);
}
```

- [ ] **Step 2: 加骨架与空状态**

```js
export function skeleton(kind = 'card', n = 3) {
  return `<div class="sk sk--${kind}">${Array.from({ length: n }, () => '<div class="sk-row"><div class="sk-b"></div><div class="sk-b short"></div></div>').join('')}</div>`;
}

export function emptyState({ icon: ic = 'file', title, sub = '', action = '' }) {
  return `<div class="empty-state">
    <div class="es-ic">${icon(ic, 22)}</div>
    <div class="es-t">${esc(title)}</div>
    ${sub ? `<div class="es-d">${esc(sub)}</div>` : ''}
    ${action}
  </div>`;
}
```

- [ ] **Step 3: state 补字段**

```js
export const state = {
  token: urlToken || localStorage.getItem('ens_token') || '',
  user: null, me: null,
  scenarios: [], convs: [], cur: null,
  agents: [], people: [],
  route: { name: 'lobby', params: {} },
  busy: false, oldestID: null, hasMore: false, loadingMore: false,
  library: [], picked: [],
  panels: {},
};
```

- [ ] **Step 3b: 补齐缺失图标**

`web/app/icons.js` 现有集合缺六个本次要用的图标：`panel-right`、`bell`、`lock`、`folder`、`image`、`pen`。在 `D` 对象末尾追加（Lucide 官方 path）：

```js
"panel-right": "<rect width=\"18\" height=\"18\" x=\"3\" y=\"3\" rx=\"2\" /> <path d=\"M15 3v18\" />",
"bell": "<path d=\"M10.268 21a2 2 0 0 0 3.464 0\" /> <path d=\"M3.262 15.326A1 1 0 0 0 4 17h16a1 1 0 0 0 .74-1.673C19.41 13.956 18 12.499 18 8A6 6 0 0 0 6 8c0 4.499-1.411 5.956-2.738 7.326\" />",
"lock": "<rect width=\"18\" height=\"11\" x=\"3\" y=\"11\" rx=\"2\" ry=\"2\" /> <path d=\"M7 11V7a5 5 0 0 1 10 0v4\" />",
"folder": "<path d=\"M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z\" />",
"image": "<rect width=\"18\" height=\"18\" x=\"3\" y=\"3\" rx=\"2\" ry=\"2\" /> <circle cx=\"9\" cy=\"9\" r=\"2\" /> <path d=\"m21 15-3.086-3.086a2 2 0 0 0-2.828 0L6 21\" />",
"pen": "<path d=\"M21.174 6.812a1 1 0 0 0-3.986-3.987L3.842 16.174a2 2 0 0 0-.5.83l-1.321 4.352a.5.5 0 0 0 .623.622l4.353-1.32a2 2 0 0 0 .83-.497z\" />",
```

校验：

```bash
node -e "global.window={}; require('./web/app/icons.js'); console.log(['panel-right','bell','lock','folder','image','pen'].every(k=>window.LUCIDE[k]))"
```

Expected: `true`。

- [ ] **Step 4: 提交**

```bash
git add web/app/js/core.js web/app/icons.js
git commit -m "refactor(ui): core 支持右上 toast、骨架与空状态；补三个图标"
```

---

## Task 4: 登录页

**Files:**
- Create: `web/app/views/login.js`（由 boot.js 引入）
- Modify: `web/app/js/boot.js`（改为 `import { mountLogin } from '../views/login.js'`）
- Test: `scripts/e2e.mjs` 登录段

**Interfaces:**
- Produces: `mountLogin(host)` 渲染登录卡；成功后调 `window.__ens.enterApp()`。

- [ ] **Step 1: 写 login.js**

```js
import { $, api, state, toast } from '../js/core.js';
import { icon, hydrateIcons } from '../js/core.js';

export function mountLogin(host) {
  host.classList.remove('hidden');
  host.innerHTML = `
  <div class="login-card">
    <img class="login-logo" src="/site/brand/logo.svg" alt="智团">
    <h1 class="login-h1">把一件真事，交给一个智团。</h1>
    <p class="login-sub">几个角色分工协作，每一步都落成文件，最后由你确认。</p>
    <div class="login-seg" id="loginSeg">
      <button class="on" data-k="phone">手机号</button>
      <button data-k="email">邮箱</button>
    </div>
    <div id="loginPhone">
      <label class="field"><span>手机号</span>
        <input id="phone" type="tel" placeholder="138 0000 0000" autocomplete="tel"></label>
      <label class="field"><span>验证码</span>
        <div class="field-row">
          <input id="code" inputmode="numeric" maxlength="6" placeholder="6 位数字">
          <button class="btn btn-line" id="sendCode">获取验证码</button>
        </div>
      </label>
    </div>
    <div id="loginEmail" class="hidden">
      <label class="field"><span>邮箱</span>
        <input id="email" type="email" placeholder="you@company.com" autocomplete="email"></label>
      <label class="field"><span>验证码</span>
        <div class="field-row">
          <input id="code2" inputmode="numeric" maxlength="6" placeholder="6 位数字">
          <button class="btn btn-line" id="sendCode2">获取验证码</button>
        </div>
      </label>
    </div>
    <div class="login-err hidden" id="loginErr"></div>
    <button class="btn btn-primary login-submit" id="doLogin">登录 / 注册</button>
    <p class="login-foot">未注册的账号会自动创建 · 演示账号 13800000000 可直接登录</p>
  </div>`;
  hydrateIcons(host);

  let kind = 'phone';
  host.querySelectorAll('#loginSeg button').forEach(b => b.addEventListener('click', () => {
    host.querySelectorAll('#loginSeg button').forEach(x => x.classList.remove('on'));
    b.classList.add('on');
    kind = b.dataset.k;
    $('#loginPhone').classList.toggle('hidden', kind !== 'phone');
    $('#loginEmail').classList.toggle('hidden', kind !== 'email');
  }));

  async function sendCode(btn, targetId, codeId) {
    const target = $('#' + targetId).value.trim();
    if (!target) return toast('先填手机号或邮箱', { type: 'err' });
    btn.disabled = true;
    try {
      const d = await api('/v1/auth/send-code', { method: 'POST', body: { kind, target } });
      if (d.devCode) { $('#' + codeId).value = d.devCode; toast('验证码已填入（本地模式）', { type: 'ok' }); }
      else toast('验证码已发送', { type: 'ok' });
      let n = 60;
      const t = setInterval(() => { n--; btn.textContent = n + 's'; if (n <= 0) { clearInterval(t); btn.disabled = false; btn.textContent = '获取验证码'; } }, 1000);
    } catch (e) { btn.disabled = false; toast(e.message, { type: 'err' }); }
  }
  $('#sendCode').addEventListener('click', e => sendCode(e.currentTarget, 'phone', 'code'));
  $('#sendCode2').addEventListener('click', e => sendCode(e.currentTarget, 'email', 'code2'));

  async function login() {
    const target = kind === 'phone' ? $('#phone').value.trim() : $('#email').value.trim();
    const code = kind === 'phone' ? $('#code').value.trim() : $('#code2').value.trim();
    if (!target) return showErr('请填写手机号或邮箱');
    showErr('');
    const btn = $('#doLogin');
    btn.disabled = true; btn.textContent = '登录中…';
    try {
      const d = await api('/v1/auth/login', { method: 'POST', body: { kind, target, code } });
      state.token = d.token; state.user = d.user;
      localStorage.setItem('ens_token', d.token);
      host.classList.add('hidden');
      await window.__ens.enterApp();
    } catch (e) {
      showErr(e.message); btn.disabled = false; btn.textContent = '登录 / 注册';
    }
  }
  function showErr(m) {
    const el = $('#loginErr'); el.textContent = m; el.classList.toggle('hidden', !m);
  }
  $('#doLogin').addEventListener('click', login);
  ['#phone', '#code', '#email', '#code2'].forEach(s => $(s).addEventListener('keydown', e => { if (e.key === 'Enter') login(); }));
}
```

- [ ] **Step 2: boot.js 用 mountLogin**

```js
import { mountLogin } from '../views/login.js';
function showLogin(msg = '') {
  const host = $('#login');
  host.classList.remove('hidden');
  $('#app')?.classList.add('hidden');
  mountLogin(host);
  if (msg) setTimeout(() => { const el = $('#loginErr'); if (el) { el.textContent = msg; el.classList.remove('hidden'); } }, 0);
}
```

- [ ] **Step 3: 手工验证**

```bash
(ADDR=:8091 DATA_DIR=/tmp/ui-m1 ./bin/ensemble &) ; sleep 1
open http://127.0.0.1:8091/app/
```

Expected: 白底居中卡，蓝色按钮，手机号/邮箱切换可用，演示账号 13800000000 空验证码可登录（DEV_BYPASS）。

- [ ] **Step 4: 提交**

```bash
git add web/app/views/login.js web/app/js/boot.js
git commit -m "feat(ui): 新登录页（保留验证码登录）"
```

---

## Task 5: 团队大厅

**Files:**
- Create: `web/app/views/lobby.js`
- Test: `scripts/e2e.mjs` 大厅段

**Interfaces:**
- Consumes: `state.scenarios`（含 `key/name/tagline/category/icon/accent/agents/random/custom/studioId`）、`state.convs`、`api()`。
- Produces: 进入房间 `navigate('#/room/'+id)`；建房 `POST /v1/conversations {title,studio}`。

- [ ] **Step 1: 写 lobby.js**

```js
import { $, api, state, esc, icon, hydrateIcons, emptyState, toast } from '../js/core.js';
import { navigate } from '../js/router.js';
import { avatarHTML } from '../js/avatars.js';

const PRESET = ['bid', 'xhs', 'code', 'anime'];
const COVER = {
  bid: 'cover-bid.png', xhs: 'cover-xhs.png', code: 'cover-code.png',
  anime: 'cover-anime.png', research: 'cover-research.png', companion: 'cover-companion.png',
};

let root = null;

export function mount(host) {
  root = host;
  render();
  document.addEventListener('conv:change', render);
}

export function unmount() {
  document.removeEventListener('conv:change', render);
  root = null;
}

function presetCard(sc) {
  const agents = (sc.agents || []).slice(0, 5);
  return `<article class="team-card" data-key="${esc(sc.key)}">
    <div class="team-cover"><img src="avatars/${COVER[sc.key] || 'cover-fallback.png'}" alt=""></div>
    <div class="team-body">
      <h3>${esc(sc.name)}</h3>
      <p>${esc(sc.tagline || '')}</p>
      <div class="team-agents">
        ${agents.map(a => avatarHTML(a.name, a.color, { size: 28 })).join('')}
        ${sc.random ? '<span class="team-rand">开局随机</span>' : ''}
      </div>
    </div>
    <div class="team-foot">
      <button class="btn btn-primary" data-join="${esc(sc.key)}">加入团队</button>
    </div>
  </article>`;
}

function render() {
  if (!root) return;
  const presets = state.scenarios.filter(s => PRESET.includes(s.key));
  const mine = state.convs;
  root.innerHTML = `
    <div class="page">
      <header class="page-head">
        <div>
          <h1>团队大厅</h1>
          <p class="page-sub">选一组现成的角色，或者自己组一个</p>
        </div>
        <button class="btn btn-primary" id="newTeam">${icon('plus', 16)}新建自定义团队</button>
      </header>
      <section class="sec">
        <h2 class="sec-h">开箱即用预设团队</h2>
        <div class="team-grid">${presets.map(presetCard).join('')}</div>
      </section>
      <section class="sec">
        <h2 class="sec-h">我的自定义团队</h2>
        ${mine.length ? `<div class="team-list">${mine.map(c => `
          <div class="team-row">
            ${avatarHTML(c.title, scColor(c), { size: 36 })}
            <div class="tr-body">
              <div class="tr-name">${esc(c.title)}</div>
              <div class="tr-meta">${agentCount(c)} 个角色 · ${esc(scName(c))}</div>
            </div>
            <a class="btn btn-line" href="#/room/${encodeURIComponent(c.id)}">进入房间</a>
          </div>`).join('')}</div>`
      : emptyState({ icon: 'users', title: '还没有自定义团队', sub: '从上面的预设开始，或者自己组一个', action: '<button class="btn btn-primary" id="emptyNew">新建团队</button>' })}
      </section>
    </div>`;
  hydrateIcons(root);
  root.querySelectorAll('[data-join]').forEach(b => b.addEventListener('click', () => join(b.dataset.join)));
  $('#newTeam')?.addEventListener('click', () => window.__newTeam?.());
  $('#emptyNew')?.addEventListener('click', () => window.__newTeam?.());
}

function scName(c) {
  const sc = state.scenarios.find(s => s.key === c.studio);
  return sc ? sc.name : '自定义';
}
function scColor(c) {
  const sc = state.scenarios.find(s => s.key === c.studio);
  return sc?.accent || c.agents?.[0]?.color || '#367BED';
}
function agentCount(c) {
  return (c.agents?.length) || (c.members?.length) || 0;
}

async function join(key) {
  const sc = state.scenarios.find(s => s.key === key);
  if (!sc) return;
  try {
    const conv = await api('/v1/conversations', { method: 'POST', body: { title: sc.name, studio: key } });
    state.convs = await api('/v1/conversations');
    document.dispatchEvent(new CustomEvent('conv:change'));
    navigate('#/room/' + encodeURIComponent(conv.id));
  } catch (e) { toast(e.message, { type: 'err' }); }
}
```

- [ ] **Step 2: 注册到 boot**

```js
import * as lobby from '../views/lobby.js';
defineViews({ lobby, room, goals, connectors, library, settings, notfound });
```

- [ ] **Step 3: 验证**

```bash
(ADDR=:8091 DATA_DIR=/tmp/ui-m1 ./bin/ensemble &) ; sleep 1
open 'http://127.0.0.1:8091/app/#/lobby'
```

Expected: 四个预设卡（竞标审核 / 小红书创作 / 代码开发 / 草帽一伙），点「加入团队」建房并跳转房间。

- [ ] **Step 4: 提交**

```bash
git add web/app/views/lobby.js web/app/js/boot.js
git commit -m "feat(ui): 团队大厅"
```

---

## Task 6: 聊天房间

**Files:**
- Create: `web/app/views/room.js`
- Modify: `web/app/js/chat.js`（选择器适配、导出复用）
- Test: `scripts/e2e.mjs` 房间段

**Interfaces:**
- Consumes: `chat.js` 导出的 `openConv(id)`、`send()`、`kickoff()`、`stopRound()`、`renderMembers()`、`runRound()`；`/v1/conversations/{id}/activity`。
- Produces: 房间 DOM 契约 `.room-head .avatar-row .msg-stream .composer`，右面板通过 `openAgentPanel(agentKey)` 打开。

- [ ] **Step 1: 写 room.js 外壳**

```js
import { $, api, state, esc, icon, hydrateIcons, skeleton } from '../js/core.js';
import { openConv, renderMembers } from '../js/chat.js';
import { renderNav } from '../js/shell.js';
import { openAgentPanel } from '../js/agentpanel.js';
import { avatarHTML } from '../js/avatars.js';

let root = null, actTimer = null;

export async function mount(host, params) {
  root = host;
  const conv = state.convs.find(c => c.id === params.id);
  if (!conv) {
    host.innerHTML = `<div class="page"><div class="empty-state"><div class="es-t">房间不存在或已删除</div><a class="btn btn-primary" href="#/lobby">回团队大厅</a></div></div>`;
    return;
  }
  state.cur = conv;
  host.innerHTML = `
    <div class="room">
      <header class="room-head">
        <div class="rh-left">
          <h2 class="rh-title">${esc(conv.title)}</h2>
          <div class="rh-sub">${esc(scenarioName(conv))}</div>
        </div>
        <div class="avatar-row" id="avatarRow"></div>
        <div class="rh-right">
          <button class="icon-btn" id="roomMemory" title="他们记得">${icon('brain', 17)}</button>
          <button class="icon-btn" id="roomSearch" title="搜索">${icon('search', 17)}</button>
          <button class="icon-btn" id="roomPanel" title="工作面板">${icon('panel-right', 17)}</button>
        </div>
      </header>
      <div class="msg-stream" id="msgStream"></div>
      <div class="composer" id="roomComposer">
        <div class="composer-in">
          <textarea id="ta" rows="1" placeholder="说点什么，或者直接派活…"></textarea>
          <div class="composer-bar">
            <button class="icon-btn" id="attachBtn" title="附件">${icon('paperclip', 16)}</button>
            <button class="icon-btn" id="mentionBtn" title="@ 角色">${icon('at-sign', 16)}</button>
            <button class="send" id="sendBtn" disabled title="发送">${icon('arrow-up', 16)}</button>
          </div>
        </div>
      </div>
    </div>`;
  hydrateIcons(root);
  await openConv(conv.id);
  renderAvatarRow();
  $('#roomPanel').addEventListener('click', () => window.__shell?.togglePanel?.());
  $('#roomMemory').addEventListener('click', () => window.__openMemory?.());
  actTimer = setInterval(renderAvatarRow, 4000);
}

export function unmount() {
  if (actTimer) clearInterval(actTimer);
  actTimer = null;
  root = null;
}

function scenarioName(conv) {
  const sc = state.scenarios.find(s => s.key === conv.studio);
  return sc ? sc.tagline || sc.name : '自定义团队';
}

async function renderAvatarRow() {
  const box = $('#avatarRow');
  if (!box || !state.cur) return;
  const agents = state.agents || [];
  let acts = [];
  try { acts = await api('/v1/conversations/' + state.cur.id + '/activity'); } catch (e) { acts = []; }
  const byKey = new Map((acts || []).map(a => [a.key, a]));
  box.innerHTML = agents.map(a => {
    const st = byKey.get(a.key)?.state || 'idle';
    return `<button class="av-cell" data-key="${esc(a.key)}" title="${esc(a.name)}">
      ${avatarHTML(a.name, a.color, { size: 34, ring: st })}
      <span class="av-name">${esc(a.name)}</span>
    </button>`;
  }).join('');
  box.querySelectorAll('.av-cell').forEach(b => b.addEventListener('click', () => {
    const a = agents.find(x => x.key === b.dataset.key);
    openAgentPanel(a, state.cur);
  }));
}
```

- [ ] **Step 2: chat.js 选择器适配**

把 `openConv` 里对旧 DOM 的引用改成新契约：

```js
// 旧：#chatEmpty/#msgs/#composer/#chatTitle
// 新：#msgStream（容器）、.room 存在即房间已打开
$('#msgStream').innerHTML = '';
// 移除 chatEmpty / chatHead / composer 的显示切换，改为
document.body.classList.add('in-room');
```

同时补齐 `buildMsgEl` 的 class：`.msg .msg--user` / `.msg--agent`、`.msg-text.md`、`.task-card`。

- [ ] **Step 3: 验证**

```bash
(ADDR=:8091 DATA_DIR=/tmp/ui-m1 ./bin/ensemble &) ; sleep 1
open 'http://127.0.0.1:8091/app/#/lobby'
```

Expected: 进入房间后能看到头像条、消息流、输入框；发一条消息后角色依次开口；状态环在跑任务时变蓝脉冲。

- [ ] **Step 4: 提交**

```bash
git add web/app/views/room.js web/app/js/chat.js
git commit -m "feat(ui): 聊天房间新布局"
```

---

## Task 7: 全局 Run 订阅与审批弹窗

**Files:**
- Create: `web/app/js/runs.js`, `web/app/js/modal.js`
- Modify: `web/app/js/studio.js`（订阅改为 `trackRun`，去掉自己的 SSE 解析）
- Test: `scripts/e2e.mjs` 审批段

**Interfaces:**
- Produces:
  - `runs.js`: `trackRun(runId)`, `untrackRun(runId)`, `onRunEvent(fn) -> unsubscribe`, `activeRuns() -> string[]`。
  - `modal.js`: `openModal({title, body, actions, width})`, `closeModal()`, `approvalModal({approval, runId})`, `confirmModal({title, text, okText, danger})`。

- [ ] **Step 1: 写 runs.js**

```js
import { api, state } from './core.js';
import { approvalModal } from './modal.js';

const subs = new Map();      // runId -> AbortController
const handlers = new Set();  // (runId, ev) => void

export function onRunEvent(fn) { handlers.add(fn); return () => handlers.delete(fn); }

export function activeRuns() { return [...subs.keys()]; }

function emit(runId, ev) {
  handlers.forEach(fn => { try { fn(runId, ev); } catch (e) { console.error(e); } });
  if (ev.type === 'approval.required') {
    approvalModal({ approval: ev.data || {}, runId });
    notifyNative('需要你的审批', ev.data?.summary || '有一个操作在等你确认');
  }
  if (ev.type === 'run.end') { subs.delete(runId); }
}

export function trackRun(runId) {
  if (!runId || subs.has(runId)) return;
  const ctrl = new AbortController();
  subs.set(runId, ctrl);
  (async () => {
    try {
      const res = await fetch(`/v1/runs/${runId}/events`, {
        headers: { Authorization: 'Bearer ' + state.token },
        signal: ctrl.signal,
      });
      if (!res.ok || !res.body) throw new Error('HTTP ' + res.status);
      const reader = res.body.getReader();
      const dec = new TextDecoder();
      let buf = '';
      for (;;) {
        const { done, value } = await reader.read();
        if (done) break;
        buf += dec.decode(value, { stream: true });
        const parts = buf.split('\n\n');
        buf = parts.pop();
        for (const p of parts) {
          const line = p.trim();
          if (!line.startsWith('data:')) continue;
          try { emit(runId, JSON.parse(line.slice(5).trim())); } catch (e) { /* 忽略坏帧 */ }
        }
      }
    } catch (e) {
      if (e.name !== 'AbortError') console.warn('run stream', runId, e.message);
    } finally { subs.delete(runId); }
  })();
}

export function untrackRun(runId) {
  const c = subs.get(runId);
  if (c) { c.abort(); subs.delete(runId); }
}

function notifyNative(title, body) {
  try { window.webkit?.messageHandlers?.notify?.postMessage({ title, body }); } catch (e) { /* 浏览器忽略 */ }
}
```

- [ ] **Step 2: 写 modal.js**

```js
import { api, esc, icon, toast } from './core.js';

let current = null;

export function openModal({ title, body, actions = [], width = 520, onMount }) {
  closeModal();
  const host = document.getElementById('modalRoot');
  const mask = document.createElement('div');
  mask.className = 'modal-mask';
  mask.innerHTML = `<div class="modal-card" style="max-width:${width}px" role="dialog" aria-modal="true">
    <div class="modal-head"><h3>${esc(title)}</h3>
      <button class="icon-btn" data-x="close">${icon('x', 16)}</button></div>
    <div class="modal-body">${body}</div>
    ${actions.length ? `<div class="modal-foot">${actions.map(a =>
      `<button class="btn ${a.kind === 'primary' ? 'btn-primary' : 'btn-line'}${a.danger ? ' danger' : ''}" data-x="${a.key}">${esc(a.label)}</button>`).join('')}</div>` : ''}
  </div>`;
  host.appendChild(mask);
  requestAnimationFrame(() => mask.classList.add('on'));
  current = mask;
  mask.addEventListener('click', e => { if (e.target === mask) closeModal(); });
  mask.querySelector('[data-x=close]').addEventListener('click', closeModal);
  actions.forEach(a => mask.querySelector(`[data-x="${a.key}"]`)?.addEventListener('click', async () => {
    const keep = await a.onClick?.(mask);
    if (keep !== false) closeModal();
  }));
  document.addEventListener('keydown', escClose);
  onMount?.(mask);
  return mask;
}

function escClose(e) { if (e.key === 'Escape') closeModal(); }

export function closeModal() {
  if (!current) return;
  const m = current;
  current = null;
  document.removeEventListener('keydown', escClose);
  m.classList.remove('on');
  setTimeout(() => m.remove(), 160);
}

export function approvalModal({ approval, runId }) {
  return openModal({
    title: '需要你的审批',
    width: 560,
    body: `
      <div class="apv-sum">${esc(approval.summary || approval.tool || '一次操作')}</div>
      ${approval.command ? `<pre class="apv-cmd">${esc(approval.command)}</pre>` : ''}
      ${approval.diff ? `<pre class="apv-diff">${esc(approval.diff)}</pre>` : ''}
      <div class="apv-risk">${icon('shield-alert', 15)} 风险：${esc(approval.risk || '未知')} · 本地运行时，命令以你的用户权限执行</div>`,
    actions: [
      { key: 'run', label: '本任务都允许', kind: 'line', onClick: () => decide(runId, approval.id, true, 'run') },
      { key: 'no', label: '拒绝', kind: 'line', danger: true, onClick: () => decide(runId, approval.id, false, 'once') },
      { key: 'yes', label: '允许本次执行', kind: 'primary', onClick: () => decide(runId, approval.id, true, 'once') },
    ],
  });
}

async function decide(runId, approvalId, approve, scope) {
  try {
    await api(`/v1/runs/${runId}/approve`, { method: 'POST', body: { approvalId, approve, scope } });
    toast(approve ? '已允许' : '已拒绝', { type: approve ? 'ok' : 'info' });
  } catch (e) { toast('审批失败：' + e.message, { type: 'err' }); }
  return true;
}

export function confirmModal({ title, text, okText = '确定', danger = false }) {
  return new Promise(resolve => {
    openModal({
      title, body: `<p class="modal-text">${esc(text)}</p>`,
      actions: [
        { key: 'no', label: '取消', kind: 'line', onClick: () => { resolve(false); } },
        { key: 'yes', label: okText, kind: 'primary', danger, onClick: () => { resolve(true); } },
      ],
    });
  });
}

export function denyModal(text = '暂无该操作权限') {
  return openModal({
    title: '无法继续', body: `<p class="modal-text">${esc(text)}</p>`,
    actions: [{ key: 'ok', label: '知道了', kind: 'primary' }],
  });
}
```

- [ ] **Step 3: 在 boot 里启动订阅总线**

```js
import { onRunEvent } from './runs.js';
onRunEvent((runId, ev) => document.dispatchEvent(new CustomEvent('run:event', { detail: { runId, ev } })));
```

- [ ] **Step 4: 验证审批弹窗**

```bash
# 以 approval 权限跑一个需要命令的任务，确认无论当前在哪个页面都会弹
(ADDR=:8091 DATA_DIR=/tmp/ui-m1 ./bin/ensemble &) ; sleep 1
```

Expected: 弹窗标题「需要你的审批」，三个按钮，点「允许本次执行」后 Run 继续。

- [ ] **Step 5: 提交**

```bash
git add web/app/js/runs.js web/app/js/modal.js web/app/js/boot.js
git commit -m "feat(ui): 全局 Run 订阅与审批弹窗"
```

---

## Task 8: Agent 工作面板

**Files:**
- Create: `web/app/js/agentpanel.js`
- Test: E2E：点头像后 `#panel.on` 存在且含角色名

**Interfaces:**
- Consumes: `shell.openPanel`、`runs.onRunEvent`、`/v1/conversations/{id}/activity`、`/v1/workspaces`、`/v1/library?convId=`。
- Produces: `openAgentPanel(agent, conv)`。

- [ ] **Step 1: 写 agentpanel.js**

```js
import { api, esc, icon, state, toast } from './core.js';
import { openPanel } from './shell.js';
import { avatarHTML } from './avatars.js';
import { onRunEvent, trackRun } from './runs.js';

let off = null;

export function openAgentPanel(agent, conv) {
  if (!agent) return;
  if (off) { off(); off = null; }
  openPanel({
    title: agent.name,
    subtitle: agent.title || '',
    render: el => {
      el.innerHTML = `
        <div class="ap-head">${avatarHTML(agent.name, agent.color, { size: 56, ring: 'working' })}</div>
        <section class="ap-sec">
          <h4>当前任务</h4>
          <div id="apTask" class="ap-task"><div class="panel-empty">还没有在跑的任务</div></div>
        </section>
        <section class="ap-sec">
          <h4>最近产出</h4>
          <div id="apArts" class="ap-arts"><div class="panel-empty">暂无产出</div></div>
        </section>
        <section class="ap-sec">
          <h4>单独发指令</h4>
          <textarea id="apCmd" class="ap-cmd" rows="3" placeholder="只发给 ${esc(agent.name)}…"></textarea>
          <button class="btn btn-primary ap-send" id="apSend">发送</button>
        </section>`;
      load(agent, conv, el);
      el.querySelector('#apSend').addEventListener('click', () => sendDirect(agent, conv, el));
    },
  });
}

async function load(agent, conv, el) {
  // 当前任务：从会话状态环拿 runId，再取详情
  try {
    const acts = await api('/v1/conversations/' + conv.id + '/activity');
    const a = (acts || []).find(x => x.key === agent.key);
    const box = el.querySelector('#apTask');
    if (a && a.runId) {
      trackRun(a.runId);
      const run = await api('/v1/runs/' + a.runId);
      box.innerHTML = `
        <div class="ap-run">
          <span class="state-dot ${esc(a.state)}"></span>
          <b>${esc(run.task || '')}</b>
          <div class="ap-run-meta">第 ${run.step}/${run.maxSteps} 步 · ${esc(run.status)}</div>
          <div class="ap-steps">${(run.toolCalls || []).slice(-6).map(t =>
            `<div class="ap-step"><code>${esc(t.tool)}</code><span class="${t.status === 'error' || t.status === 'denied' ? 'bad' : ''}">${esc(t.status)}</span></div>`).join('')}</div>
          <button class="btn btn-line" id="apOpen">展开时间线</button>
        </div>`;
      el.querySelector('#apOpen').addEventListener('click', () => {
        location.hash = '#/room/' + encodeURIComponent(conv.id) + '?view=studio';
      });
    }
  } catch (e) { /* 没有工作区时正常 */ }

  try {
    const arts = await api('/v1/library?convId=' + encodeURIComponent(conv.id) + '&limit=10');
    const list = (arts || []).filter(a => a.agentKey === agent.key || a.agentName === agent.name);
    const box = el.querySelector('#apArts');
    if (list.length) {
      box.innerHTML = list.map(a => `
        <button class="ap-art" data-id="${esc(a.id)}">
          ${icon('file-text', 15)}<span>${esc(a.title)}</span>
        </button>`).join('');
      box.querySelectorAll('.ap-art').forEach(b => b.addEventListener('click', () => {
        document.dispatchEvent(new CustomEvent('artifact:open', { detail: { id: b.dataset.id } }));
      }));
    }
  } catch (e) { /* 资料库未启用 */ }
}

async function sendDirect(agent, conv, el) {
  const ta = el.querySelector('#apCmd');
  const text = ta.value.trim();
  if (!text) return toast('先写一句指令', { type: 'err' });
  const btn = el.querySelector('#apSend');
  btn.disabled = true;
  try {
    const list = await api('/v1/workspaces');
    const ws = (list || []).find(w => w.conversationId === conv.id);
    if (!ws) {
      // 没绑工作区：退化为在群里 @ 他
      await api(`/v1/conversations/${conv.id}/messages`, { method: 'POST', body: { content: `@${agent.name} ${text}` } });
      toast('已作为群消息发出（这个房间还没绑工作区）', { type: 'info' });
      ta.value = '';
      return;
    }
    const run = await api(`/v1/workspaces/${ws.id}/runs`, {
      method: 'POST',
      body: { task: text, operatorKey: agent.key, permission: 'approval', maxSteps: 12, autoReview: true },
    });
    trackRun(run.id);
    toast('已派给 ' + agent.name, { type: 'ok' });
    ta.value = '';
  } catch (e) {
    toast(e.message, { type: 'err' });
  } finally { btn.disabled = false; }
}
```

- [ ] **Step 2: 提交**

```bash
git add web/app/js/agentpanel.js
git commit -m "feat(ui): Agent 工作面板（当前任务/产出/单独指令）"
```

---

## Task 8b: 房间内全宽工作室视图

**Files:**
- Create: `web/app/views/studio.js`
- Modify: `web/app/js/studio.js`（订阅改 `trackRun`；DOM 选择器换新契约）
- Modify: `web/app/views/room.js`（`?view=studio` 时渲染全宽视图）
- Test: E2E：代码房间可打开工作室、跑一个只读任务、看到 diff

**Interfaces:**
- Consumes: `runs.trackRun`、`/v1/workspaces`、`/v1/workspaces/{id}/runs`、`/v1/runs/{id}/diff`、`/v1/runs/{id}/revert`。
- Produces: `mountStudio(host, convId)`；`window.__ensemblePickDirectory`（桌面壳 ⌘O 注入目标）。

- [ ] **Step 1: 写 views/studio.js**

```js
import { $, api, state, esc, icon, hydrateIcons, toast, emptyState } from '../js/core.js';
import { trackRun } from '../js/runs.js';

let root = null, wsId = null, runId = null;

export async function mountStudio(host, convId) {
  root = host;
  root.innerHTML = `
    <div class="studio-view">
      <header class="sv-head">
        <div><h2>代码工作室</h2><p class="page-sub">选一个本机目录，派任务；每一步都在时间线上，改动可回滚</p></div>
        <div class="sv-actions">
          <a class="btn btn-line" href="#/room/${encodeURIComponent(convId)}">返回聊天</a>
        </div>
      </header>
      <div class="sv-body">
        <aside class="sv-side">
          <label class="field"><span>工作区（本机绝对路径）</span>
            <div class="field-row">
              <input id="wsRoot" placeholder="/Users/you/project" spellcheck="false">
              <button class="btn btn-line" id="wsPick">选择…</button>
            </div>
            <div class="hint">桌面端可用 ⌘O 调原生目录选择器；浏览器里请直接填路径。</div>
          </label>
          <div class="studio-warn hidden" id="wsWarn"></div>
          <label class="field"><span>权限</span>
            <select id="runPerm">
              <option value="readonly">只读（不能改文件、不能跑命令）</option>
              <option value="approval" selected>命令需批准</option>
              <option value="full">完全访问（等同当前系统用户）</option>
            </select>
            <div class="hint">本地运行时不是沙箱。full 下命令能做的事和你本人一样多。</div>
          </label>
          <label class="field"><span>任务</span>
            <textarea id="runTask" rows="5" placeholder="比如：修一下 calc.go 里 Add 的 bug，跑 go test 确认通过"></textarea>
          </label>
          <label class="studio-check"><input type="checkbox" id="autoReview" checked><span>执行完自动复审一遍（只读，能跑测试）</span></label>
          <button class="btn btn-primary" id="runStart">开始执行</button>
          <div class="ws-wrap"><div class="sec-h">已绑定工作区</div><div id="wsList"></div></div>
        </aside>
        <section class="sv-tl">
          <div class="sec-h">执行时间线</div>
          <div class="timeline" id="runTimeline"><div class="tl-empty">还没有任务</div></div>
          <div class="sec-h">改动</div>
          <div id="runDiff" class="diff-box"></div>
        </section>
      </div>
    </div>`;
  hydrateIcons(root);
  window.__ensemblePickDirectory = path => {
    const inp = $('#wsRoot');
    if (inp) { inp.value = path; toast('已选择：' + path, { type: 'ok' }); }
  };
  $('#wsPick').addEventListener('click', () => toast('用菜单「文件 → 选择工作区目录」（⌘O）'));
  $('#runStart').addEventListener('click', () => startRun(convId));
  await refreshWorkspaces(convId);
}

export function unmount() { root = null; }

async function refreshWorkspaces(convId) {
  let list = [];
  try { list = await api('/v1/workspaces'); } catch (e) { list = []; }
  const box = $('#wsList');
  if (!box) return;
  if (!list.length) { box.innerHTML = '<div class="panel-empty">还没有工作区，填目录后点「开始执行」会先绑定</div>'; return; }
  box.innerHTML = list.map(w => `
    <button class="ws-item${w.id === wsId ? ' on' : ''}" data-id="${esc(w.id)}" data-root="${esc(w.root)}">
      ${icon('folder', 14)}<span>${esc(w.root)}</span>
    </button>`).join('');
  box.querySelectorAll('.ws-item').forEach(b => b.addEventListener('click', () => {
    wsId = b.dataset.id;
    $('#wsRoot').value = b.dataset.root;
    box.querySelectorAll('.ws-item').forEach(x => x.classList.toggle('on', x === b));
  }));
  const mine = list.find(w => w.conversationId === convId);
  if (mine) { wsId = mine.id; $('#wsRoot').value = mine.root; }
}

async function bindIfNeeded(convId) {
  const rootPath = $('#wsRoot').value.trim();
  if (!rootPath) throw new Error('先选择或填写一个本机目录');
  const list = await api('/v1/workspaces');
  const hit = (list || []).find(w => w.root === rootPath && (!convId || w.conversationId === convId));
  if (hit) { wsId = hit.id; return wsId; }
  const w = await api('/v1/workspaces', { method: 'POST', body: { root: rootPath, conversationId: convId } });
  wsId = w.id;
  await refreshWorkspaces(convId);
  return wsId;
}

async function startRun(convId) {
  const task = $('#runTask').value.trim();
  if (!task) return toast('先写一句要做什么', { type: 'err' });
  if (runId) return toast('上一轮还在跑', { type: 'err' });
  const btn = $('#runStart');
  btn.disabled = true;
  try {
    const id = await bindIfNeeded(convId);
    const permission = $('#runPerm').value;
    const run = await api(`/v1/workspaces/${id}/runs`, {
      method: 'POST',
      body: { task, permission, maxSteps: 20, autoReview: $('#autoReview').checked },
    });
    runId = run.id;
    const tl = $('#runTimeline');
    tl.innerHTML = '';
    add(tl, '调度', `任务已提交 · 权限 <b>${esc(permission)}</b>`, 'info');
    trackRun(run.id);
    document.addEventListener('run:event', onEvent);
  } catch (e) {
    toast(e.message, { type: 'err' });
    btn.disabled = false;
  }
}

function onEvent(e) {
  const { runId: rid, ev } = e.detail;
  if (rid !== runId) return;
  const tl = $('#runTimeline');
  const d = ev.data || {};
  switch (ev.type) {
    case 'run.start': add(tl, '开始', `工作区 <code>${esc(d.root || '')}</code>`, 'info'); break;
    case 'step.start': add(tl, `第 ${d.step}/${d.maxSteps} 步`, '', 'step'); break;
    case 'assistant.message': add(tl, '说明', esc(d.text || ''), 'say'); break;
    case 'tool.start': add(tl, d.tool, `<code>${esc(d.args || '')}</code>`, 'tool', d.toolCallId); break;
    case 'tool.end': endTool(tl, d); break;
    case 'diff': add(tl, '改动', `${d.files} 个文件`, 'info'); break;
    case 'run.end':
      add(tl, '结束', `状态 <b>${esc(d.status)}</b>`, d.status === 'done' ? 'ok' : 'err');
      loadDiff(rid);
      runId = null;
      $('#runStart').disabled = false;
      document.removeEventListener('run:event', onEvent);
      break;
  }
}

function add(box, title, html, kind, id) {
  const el = document.createElement('div');
  el.className = 'tl ' + (kind || '');
  el.innerHTML = `<div class="tl-h">${esc(title)}</div>${html ? `<div class="tl-b">${html}</div>` : ''}`;
  if (id) el.dataset.toolCall = id;
  box.appendChild(el);
  box.scrollTop = box.scrollHeight;
}

function endTool(box, d) {
  const el = box.querySelector(`.tl[data-tool-call="${d.toolCallId}"]`);
  if (!el) return;
  el.classList.add(d.status === 'ok' ? 'ok' : 'err');
  el.querySelector('.tl-b').innerHTML = `<pre class="tl-out">${esc((d.output || '').slice(0, 900))}</pre>`;
}

async function loadDiff(id) {
  const box = $('#runDiff');
  if (!box) return;
  try {
    const d = await api(`/v1/runs/${id}/diff`);
    if (!d.files?.length) { box.innerHTML = '<div class="panel-empty">没有文件变化</div>'; return; }
    box.innerHTML = d.files.map(f => `
      <div class="df">
        <div class="df-h"><span class="df-st ${esc(f.status)}">${esc(f.status)}</span>
          <code>${esc(f.path)}</code><span class="dim">+${f.added}/-${f.removed}</span></div>
        ${f.patch ? `<pre class="df-patch">${esc(f.patch)}</pre>` : ''}
      </div>`).join('') + `<button class="btn btn-line" id="revertBtn">回滚这次改动</button>`;
    box.querySelector('#revertBtn').addEventListener('click', async () => {
      try {
        const r = await api(`/v1/runs/${id}/revert`, { method: 'POST' });
        toast(`${r.reverted} 个文件已恢复`, { type: 'ok' });
      } catch (e) { toast('回滚失败：' + e.message, { type: 'err' }); }
    });
  } catch (e) { box.innerHTML = ''; }
}
```

- [ ] **Step 2: room.js 支持 `?view=studio`**

```js
export async function mount(host, params) {
  const q = new URLSearchParams((location.hash.split('?')[1]) || '');
  if (q.get('view') === 'studio') {
    const { mountStudio } = await import('./studio.js');
    return mountStudio(host, params.id);
  }
  // ... 原有房间渲染
}
```

- [ ] **Step 3: 删除旧 studio.js 弹层逻辑**

`web/app/js/studio.js` 只保留 `openStudio/closeStudio` 的兼容壳（转调 `location.hash = '#/room/<id>?view=studio'`），删除其 SSE 解析与审批卡片（审批已由 `modal.js` 统一处理）。若其他模块不再引用，直接删除该文件并从 `boot.js` 移除引用。

- [ ] **Step 4: 验证**

```bash
open 'http://127.0.0.1:8091/app/#/room/<房间id>?view=studio'
```

Expected: 全宽工作室；填目录、选只读权限、跑一个「列出目录结构」任务；时间线逐步出现工具；结束后显示 diff 区（只读任务无 diff 时显示「没有文件变化」）。

- [ ] **Step 5: 提交**

```bash
git add web/app/views/studio.js web/app/views/room.js web/app/js/studio.js web/app/js/boot.js
git commit -m "feat(ui): 房间内全宽工作室视图"
```

---

## Task 9: 头像生成脚本与前端回退

**Files:**
- Create: `scripts/avatars.mjs`, `web/app/js/avatars.js`, `web/app/avatars/manifest.json`
- Modify: `web/app/js/core.js`（`makeAv` 指向新实现）
- Test: `node scripts/avatars.mjs --dry-run`

**Interfaces:**
- Produces:
  - manifest：`{"characters": {"<slug>": "characters/<slug>.png"}, "covers": {"bid": "covers/bid.png"}}`
  - `avatars.js`: `avatarHTML(name, color, {size, ring}) -> string`，`avatarUrl(name) -> string|null`。

- [ ] **Step 1: 写 avatars.mjs**

```js
#!/usr/bin/env node
/**
 * 批量生成黏土头像。Key 只从环境变量读。
 *   IMAGE_API_BASE=https://origingateway.com/v1 IMAGE_API_KEY=... node scripts/avatars.mjs
 *   --dry-run   只打印清单，不发请求
 *   --only=名字  只生成指定角色
 */
import { mkdir, writeFile, readFile, access } from 'node:fs/promises';
import { existsSync } from 'node:fs';
import { join, dirname } from 'node:path';

const OUT = 'web/app/avatars';
const BASE = process.env.IMAGE_API_BASE || 'https://origingateway.com/v1';
const KEY = process.env.IMAGE_API_KEY || '';
const MODEL = process.env.IMAGE_MODEL || 'nano-banana-2';
const CONC = 3;

// 角色气质描述：只描述外形与神态，不提作品名，产出为原创黏土肖像。
const CHARACTERS = JSON.parse(await readFile('scripts/avatar-prompts.json', 'utf8'));

const COVERS = {
  bid: 'a clay 3d diorama of a small team reviewing a large document on a table, four simple clay figures, warm pastel palette, plain light background, soft studio light, no text',
  xhs: 'a clay 3d diorama of a creative desk with notebook, coffee cup and small clay figures, pink pastel palette, plain light background, no text',
  code: 'a clay 3d diorama of a small dev team around a laptop with code symbols, blue pastel palette, plain light background, no text',
  anime: 'a clay 3d diorama of a tiny sailing ship with four cheerful clay figures, warm sunset pastel palette, plain light background, no text',
  research: 'a clay 3d diorama of charts and stacked papers with tiny clay figures, purple pastel palette, plain light background, no text',
  companion: 'a clay 3d diorama of a cozy living room with two clay figures chatting, warm pastel palette, plain light background, no text',
};

const args = process.argv.slice(2);
const dry = args.includes('--dry-run');
const onlyArg = args.find(a => a.startsWith('--only='));
const only = onlyArg ? onlyArg.split('=')[1].split(',') : null;

if (!dry && !KEY) { console.error('缺 IMAGE_API_KEY'); process.exit(2); }

await mkdir(join(OUT, 'characters'), { recursive: true });
await mkdir(join(OUT, 'covers'), { recursive: true });

function promptFor(name, desc) {
  return `soft clay 3d portrait of ${desc}, matte clay material, low saturation pastel colors, plain light neutral background, soft studio lighting, head and shoulders, centered, clean minimalist, high detail, no text, no logo, no watermark — character named ${name}`;
}

const jobs = [];
for (const [slug, desc] of Object.entries(CHARACTERS)) {
  const file = join(OUT, 'characters', slug + '.png');
  if (existsSync(file)) continue;
  if (only && !only.includes(slug)) continue;
  jobs.push({ kind: 'characters', slug, prompt: promptFor(slug, desc), file });
}
for (const [slug, p] of Object.entries(COVERS)) {
  const file = join(OUT, 'covers', slug + '.png');
  if (existsSync(file)) continue;
  jobs.push({ kind: 'covers', slug, prompt: p, file });
}

console.log(`待生成 ${jobs.length} 张（已存在跳过）`);
if (dry) { jobs.forEach(j => console.log(' -', j.kind, j.slug)); process.exit(0); }

async function gen(job) {
  for (let attempt = 1; attempt <= 3; attempt++) {
    try {
      const res = await fetch(BASE + '/images/generations', {
        method: 'POST',
        headers: { Authorization: 'Bearer ' + KEY, 'Content-Type': 'application/json' },
        body: JSON.stringify({ model: MODEL, prompt: job.prompt, n: 1, size: '512x512' }),
      });
      if (!res.ok) throw new Error('HTTP ' + res.status + ' ' + (await res.text()).slice(0, 200));
      const d = await res.json();
      const b64 = d.data?.[0]?.b64_json;
      if (!b64) throw new Error('没有 b64_json');
      await writeFile(job.file, Buffer.from(b64, 'base64'));
      console.log('✓', job.kind, job.slug);
      return;
    } catch (e) {
      console.warn('×', job.slug, 'attempt', attempt, e.message);
      await new Promise(r => setTimeout(r, 1500 * attempt));
    }
  }
  throw new Error('生成失败：' + job.slug);
}

let i = 0, failed = [];
await Promise.all(Array.from({ length: CONC }, async () => {
  while (i < jobs.length) {
    const job = jobs[i++];
    try { await gen(job); } catch (e) { failed.push(job.slug); }
  }
}));

// manifest：由目录实际内容生成
const manifest = { characters: {}, covers: {} };
for (const [slug] of Object.entries(CHARACTERS)) {
  if (existsSync(join(OUT, 'characters', slug + '.png'))) manifest.characters[slug] = 'characters/' + slug + '.png';
}
for (const slug of Object.keys(COVERS)) {
  if (existsSync(join(OUT, 'covers', slug + '.png'))) manifest.covers[slug] = 'covers/' + slug + '.png';
}
await writeFile(join(OUT, 'manifest.json'), JSON.stringify(manifest, null, 1));
console.log(`完成：角色 ${Object.keys(manifest.characters).length}，封面 ${Object.keys(manifest.covers).length}`);
if (failed.length) { console.error('失败：', failed.join(', ')); process.exit(1); }
```

- [ ] **Step 2: 写 avatar-prompts.json（示例，执行时从角色库生成全量）**

```json
{
  "雪之下雪乃": "a young woman with long straight black hair, calm cool expression, small navy ribbon in hair, elegant",
  "由比滨结衣": "a cheerful young woman with peach-colored shoulder length hair, warm smile, soft pink cardigan",
  "工头": "a sturdy middle-aged man with short hair and a beard, work shirt, confident calm expression",
  "架构": "a thoughtful man in his thirties with glasses, short neat hair, light blue shirt",
  "文案写手": "a young woman with shoulder-length brown hair, focused expression, cream sweater",
  "路飞": "a cheerful young man wearing a straw hat, big grin, red vest",
  "索隆": "a stern young man with green hair, three earrings, dark green coat"
}
```

执行时用 `scripts/avatars.mjs --list-missing` 对照 `internal/service/character.go` 与 `scenario_onepiece.go` 补齐到约 90 条。补全规则：每条只写「发色/长度、神态、上装、显著配饰」，不写作品名。

- [ ] **Step 3: 写 avatars.js**

```js
import { esc, hexOf, shift } from './core.js';

let manifest = { characters: {}, covers: {} };
let ready = null;

export function loadAvatars() {
  if (!ready) {
    ready = fetch('avatars/manifest.json').then(r => r.ok ? r.json() : manifest).then(m => { manifest = m || manifest; }).catch(() => {});
  }
  return ready;
}
loadAvatars();

export function avatarUrl(name) {
  const hit = manifest.characters?.[name];
  return hit ? 'avatars/' + hit : null;
}

const RING = { idle: 'ring-idle', working: 'ring-working', done: 'ring-done', blocked: 'ring-blocked' };

export function avatarHTML(name, color, { size = 32, ring = '' } = {}) {
  const url = avatarUrl(name);
  const cls = ['av', ring ? RING[ring] || '' : ''].filter(Boolean).join(' ');
  const sizeCss = `width:${size}px;height:${size}px`;
  if (url) {
    return `<span class="${cls}" style="${sizeCss}"><img src="${url}" alt="" loading="lazy"></span>`;
  }
  const base = hexOf(color || autoColor(name || ''));
  return `<span class="${cls} av--fallback" style="${sizeCss};background:linear-gradient(140deg,${base},${shift(base, -18)})"><b>${esc((name || '?').trim().slice(0, 1))}</b></span>`;
}

function autoColor(name) {
  let h = 0;
  for (let i = 0; i < name.length; i++) h = (h * 31 + name.charCodeAt(i)) >>> 0;
  return `hsl(${h % 360} 46% 58%)`;
}
```

- [ ] **Step 4: 小批量试跑（各 2 张）**

```bash
IMAGE_API_BASE=https://origingateway.com/v1 IMAGE_API_KEY="$IMAGE_API_KEY" \
  node scripts/avatars.mjs --only=工头,架构
ls -la web/app/avatars/characters | head
node -e "console.log(require('./web/app/avatars/manifest.json'))"
```

Expected: 两张 512×512 PNG，manifest 含这两个 key。

- [ ] **Step 5: 全量生成（约 90 + 7 张，并发 3，约 10–12 分钟）**

```bash
IMAGE_API_BASE=https://origingateway.com/v1 IMAGE_API_KEY="$IMAGE_API_KEY" node scripts/avatars.mjs
```

- [ ] **Step 6: 抽查十张并提交**

```bash
# 随机抽 10 张，人工过目风格是否统一、有无文字/畸形手指
node -e "const m=require('./web/app/avatars/manifest.json');console.log(Object.keys(m.characters).slice(0,10).join('\n'))"
git add web/app/avatars web/app/js/avatars.js scripts/avatars.mjs scripts/avatar-prompts.json
git commit -m "feat(ui): 黏土头像素材与前端回退"
```

---

## Task 10: 后端静态资源查找

**Files:**
- Modify: `internal/httpapi/server.go:243-259`
- Create: `internal/httpapi/webdir_test.go`
- Test: `go test ./internal/httpapi -run TestWebDir`

**Interfaces:**
- Produces: `webDir(sub string) http.Dir` 额外查找 `exeDir/../Resources/web/<sub>`。

- [ ] **Step 1: 写失败测试**

```go
package httpapi

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWebDirFindsBundleResources(t *testing.T) {
	// 模拟 .app 布局：App.app/Contents/MacOS/<exe> 与 App.app/Contents/Resources/web/app
	root := t.TempDir()
	dir := filepath.Join(root, "App.app", "Contents", "Resources", "web", "app")
	if err := os.MkdirAll(dir, 0o755); err != nil { t.Fatal(err) }
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("x"), 0o644); err != nil { t.Fatal(err) }

	old := exeDirFn
	exeDirFn = func() (string, error) {
		return filepath.Join(root, "App.app", "Contents", "MacOS"), nil
	}
	defer func() { exeDirFn = old }()

	got := string(webDir("app"))
	want := filepath.Join(root, "App.app", "Contents", "Resources", "web", "app")
	if got != want {
		t.Fatalf("webDir = %q, want %q", got, want)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
go test ./internal/httpapi -run TestWebDirFindsBundleResources -v
```

Expected: FAIL —— `undefined: exeDirFn`。

- [ ] **Step 3: 实现**

```go
// exeDirFn 便于测试注入可执行文件目录。
var exeDirFn = func() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Dir(exe), nil
}

func webDir(sub string) http.Dir {
	var tried []string
	if exeDir, err := exeDirFn(); err == nil {
		for _, dir := range []string{
			filepath.Join(exeDir, "web", sub),                        // make build 后 bin/ 旁
			filepath.Join(exeDir, "..", "Resources", "web", sub),     // macOS .app 包内
		} {
			tried = append(tried, dir)
			if st, err := os.Stat(dir); err == nil && st.IsDir() {
				return http.Dir(dir)
			}
		}
	}
	dir := filepath.Join("web", sub)
	tried = append(tried, dir)
	if st, err := os.Stat(dir); err == nil && st.IsDir() {
		return http.Dir(dir)
	}
	logx.L().Error("静态资源目录未找到，页面将全部 404（接口不受影响）", "tried", tried)
	return http.Dir(dir)
}
```

- [ ] **Step 4: 测试通过**

```bash
go test ./internal/httpapi -run TestWebDir -v && go test ./... -count=1
```

- [ ] **Step 5: 提交**

```bash
git add internal/httpapi/server.go internal/httpapi/webdir_test.go
git commit -m "fix(http): 桌面包内静态资源查找路径"
```

---

## Task 11: `/v1/me` 偏好字段

**Files:**
- Modify: `internal/domain/types.go`（User 增加 Preferences）
- Modify: `internal/httpapi/handler_me.go`
- Test: `internal/httpapi/api_test.go` 新增用例

**Interfaces:**
- Produces: `User.Preferences map[string]string`（`json:"preferences,omitempty"`）；`/v1/me` 返回 `preferences`；`PATCH /v1/me` 接受 `preferences`（合并而非覆盖）。

- [ ] **Step 1: 写失败测试**

```go
func TestUpdateMeMergesPreferences(t *testing.T) {
	srv, h := newTestServer(t)
	tok := login(t, h, "13800000000")

	rec, _ := doJSON(t, h, "PATCH", "/v1/me", tok, map[string]interface{}{
		"preferences": map[string]string{"notify": "false"},
	})
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	me, _ := doJSON(t, h, "GET", "/v1/me", tok, nil)
	userID := me["data"].(map[string]interface{})["user"].(map[string]interface{})["id"].(string)

	got, _ := srv.store.GetUser(userID)
	if got.Preferences["notify"] != "false" {
		t.Fatalf("prefs = %v", got.Preferences)
	}

	rec, _ = doJSON(t, h, "PATCH", "/v1/me", tok, map[string]interface{}{
		"preferences": map[string]string{"theme": "light"},
	})
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	got, _ = srv.store.GetUser(userID)
	if got.Preferences["notify"] != "false" || got.Preferences["theme"] != "light" {
		t.Fatalf("合并失败：%v", got.Preferences)
	}
}
```

（助手签名已核对：`newTestServer(t) (*Server, http.Handler)`、`doJSON(t, h, method, path, token, body)`、`login(t, h, phone)`，均在 `internal/httpapi/api_test.go`。）

- [ ] **Step 2: 实现**

`domain/types.go` 的 User 增加：

```go
// Preferences 界面偏好（通知、默认视图等）。键值都是字符串，
// 避免为每个开关加一个字段；空值不落盘。
Preferences map[string]string `json:"preferences,omitempty"`
```

`handler_me.go`：

```go
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found { unauthorized(w, "未登录"); return }
	ok(w, map[string]interface{}{
		"user": u, "remaining": u.Remaining(),
		"demo": !s.cfg.UpstreamConfigured(), "upstream": upstreamName(s.cfg),
		"preferences": u.Preferences,
	})
}
```

`handleUpdateMe` 增加：

```go
Preferences map[string]string `json:"preferences"`
```

并在读取 `cur` 之后合并：

```go
if len(req.Preferences) > 0 {
	if cur.Preferences == nil { cur.Preferences = map[string]string{} }
	for k, v := range req.Preferences {
		if v == "" { delete(cur.Preferences, k); continue }
		cur.Preferences[k] = v
	}
}
```

- [ ] **Step 3: 测试并提交**

```bash
go test ./internal/httpapi -run TestUpdateMeMergesPreferences -v
go test ./... -count=1
git add internal/domain/types.go internal/httpapi/handler_me.go internal/httpapi/api_test.go
git commit -m "feat(api): /v1/me 支持界面偏好"
```

---

## Task 12: 逐屏截图脚本

**Files:**
- Create: `scripts/ui-shots.mjs`
- Modify: `Makefile`（`shots` 目标）

**Interfaces:**
- Produces: `.ui-shots/<width>-<view>.png`，供每个里程碑人工过目。

- [ ] **Step 1: 写脚本（复用 e2e 的 CDP 启动方式）**

```js
#!/usr/bin/env node
/** 逐屏截图：1440 / 1180 / 390。用法：node scripts/ui-shots.mjs [view...] */
import { spawn } from 'node:child_process';
import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const BASE = process.env.SHOT_BASE || 'http://localhost:8080';
const EDGE = process.env.EDGE_PATH || '/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge';
const OUT = '.ui-shots';
const WIDTHS = [1440, 1180, 390];
const VIEWS = process.argv.slice(2).length ? process.argv.slice(2) : ['lobby', 'room', 'goals', 'connectors', 'library', 'settings'];

mkdirSync(OUT, { recursive: true });

// ... 与 scripts/e2e.mjs 相同的 CDP 启动代码（launch/CDP 类）...
// 关键步骤：
//   1. 启动 headless Edge，连上，Emulation.setDeviceMetricsOverride({width, height:900, deviceScaleFactor:1, mobile:false})
//   2. 登录（演示账号 13800000000）
//   3. 逐视图 navigate 到 #/<view>，等 800ms，Page.captureScreenshot
//   4. 写入 `${OUT}/${w}-${view}.png`
```

实现时直接从 `scripts/e2e.mjs` 复制 `launch()` / `CDP` 两个块（已在仓库里、可用），只替换最后的用例部分。

- [ ] **Step 2: 加 Makefile 目标**

```makefile
# 逐屏截图（需要后端在 :8080 跑着）
shots:
	@node scripts/ui-shots.mjs
```

- [ ] **Step 3: 跑一次**

```bash
(ADDR=:8080 DATA_DIR=/tmp/shots ./bin/ensemble &) ; sleep 1
node scripts/ui-shots.mjs
ls .ui-shots
```

Expected: 每个宽度每个视图一张 PNG。

- [ ] **Step 4: 提交**

```bash
git add scripts/ui-shots.mjs Makefile
git commit -m "test(ui): 逐屏截图脚本"
```

---

## M1 验收清单

- [ ] `go build ./... && go test ./... -count=1` 全绿
- [ ] `gofmt -l .` 无输出
- [ ] 登录 → 大厅 → 加入预设团队 → 房间发消息 → 角色流式回复
- [ ] 点角色头像 → 右面板显示当前任务/产出/指令框
- [ ] 审批权限任务 → 任意页面都会弹审批弹窗
- [ ] `node scripts/ui-shots.mjs` 六屏三宽度无白屏/错位
- [ ] 人工看 `.ui-shots/*`：主色蓝、无渐变、圆角 20、阴影柔和

---

# M2：目标看板、连接器、资料库

## Task 13: 目标看板

**Files:**
- Create: `web/app/views/goals.js`
- Test: `scripts/e2e.mjs` 目标段

**Interfaces:**
- Consumes: `GET/POST/PATCH/DELETE /v1/goals`。响应字段：`id,title,description,status,assignees,runIds,manualProgress,progress,progressSource,conversationId,updatedAt`。
- Produces: 卡片拖拽更新 `PATCH {status}`；右面板显示详情。

- [ ] **Step 1: 写 goals.js**

```js
import { $, api, state, esc, icon, hydrateIcons, emptyState, toast } from '../js/core.js';
import { openPanel } from '../js/shell.js';
import { avatarHTML } from '../js/avatars.js';
import { confirmModal } from '../js/modal.js';

const COLS = [
  { key: 'todo', label: '待规划' },
  { key: 'doing', label: '进行中' },
  { key: 'done', label: '已完成' },
];
let root = null, view = 'kanban', filter = 'active', goals = [];

export async function mount(host) {
  root = host;
  await load();
}

export function unmount() { root = null; }

async function load() {
  try { goals = await api('/v1/goals'); } catch (e) { goals = []; }
  render();
}

function visible() {
  return filter === 'active' ? goals.filter(g => g.status !== 'done') : goals;
}

function card(g) {
  const conv = state.convs.find(c => c.id === g.conversationId);
  const agents = (conv?.agents || []).filter(a => (g.assignees || []).includes(a.key));
  return `<article class="goal-card" draggable="true" data-id="${esc(g.id)}">
    <div class="gc-title">${esc(g.title)}</div>
    ${g.description ? `<div class="gc-desc">${esc(g.description)}</div>` : ''}
    <div class="gc-meta">
      <div class="gc-avs">${agents.map(a => avatarHTML(a.name, a.color, { size: 22 })).join('')}</div>
      <span class="gc-pct">${g.progress}%</span>
    </div>
    <div class="gc-bar"><i style="width:${g.progress}%"></i></div>
  </article>`;
}

function render() {
  if (!root) return;
  const list = visible();
  root.innerHTML = `
    <div class="page">
      <header class="page-head">
        <div><h1>目标看板</h1><p class="page-sub">进度由关联任务派生，不靠手填</p></div>
        <div class="page-actions">
          <div class="seg" id="goalFilter">
            <button class="${filter === 'active' ? 'on' : ''}" data-f="active">进行中</button>
            <button class="${filter === 'all' ? 'on' : ''}" data-f="all">全部</button>
          </div>
          <div class="seg" id="goalView">
            <button class="${view === 'kanban' ? 'on' : ''}" data-v="kanban">看板</button>
            <button class="${view === 'list' ? 'on' : ''}" data-v="list">列表</button>
          </div>
          <button class="btn btn-primary" id="newGoal">${icon('plus', 16)}新建目标</button>
        </div>
      </header>
      ${list.length ? (view === 'kanban' ? kanban(list) : listView(list)) : emptyState({
        icon: 'trending-up', title: '暂无进行中的团队目标', sub: '建一个目标，把角色和任务挂上去',
        action: '<button class="btn btn-primary" id="emptyGoal">新建团队目标</button>',
      })}
    </div>`;
  hydrateIcons(root);
  bind();
}

function kanban(list) {
  return `<div class="kanban">${COLS.map(c => `
    <section class="kb-col" data-col="${c.key}">
      <div class="kb-h"><span>${c.label}</span><span class="kb-n">${list.filter(g => g.status === c.key).length}</span></div>
      <div class="kb-body">${list.filter(g => g.status === c.key).map(card).join('')}</div>
    </section>`).join('')}</div>`;
}

function listView(list) {
  return `<div class="goal-list">${list.map(g => `
    <div class="goal-row" data-id="${esc(g.id)}">
      <span class="gr-status ${esc(g.status)}"></span>
      <div class="gr-body"><div class="gr-title">${esc(g.title)}</div>
        <div class="gr-sub">${esc(g.description || '')}</div></div>
      <span class="gr-pct">${g.progress}%</span>
    </div>`).join('')}</div>`;
}

function bind() {
  root.querySelectorAll('#goalFilter button').forEach(b => b.addEventListener('click', () => { filter = b.dataset.f; render(); }));
  root.querySelectorAll('#goalView button').forEach(b => b.addEventListener('click', () => { view = b.dataset.v; render(); }));
  $('#newGoal')?.addEventListener('click', newGoal);
  $('#emptyGoal')?.addEventListener('click', newGoal);
  root.querySelectorAll('.goal-card,.goal-row').forEach(el => el.addEventListener('click', () => openDetail(el.dataset.id)));
  root.querySelectorAll('.goal-card').forEach(el => {
    el.addEventListener('dragstart', e => e.dataTransfer.setData('text/plain', el.dataset.id));
  });
  root.querySelectorAll('.kb-col').forEach(col => {
    col.addEventListener('dragover', e => e.preventDefault());
    col.addEventListener('drop', async e => {
      e.preventDefault();
      const id = e.dataTransfer.getData('text/plain');
      if (!id || !col.dataset.col) return;
      await patch(id, { status: col.dataset.col });
    });
  });
}

async function patch(id, body) {
  try { await api('/v1/goals/' + id, { method: 'PATCH', body }); await load(); }
  catch (e) { toast(e.message, { type: 'err' }); }
}

function newGoal() {
  openPanel({
    title: '新建目标',
    render: el => {
      el.innerHTML = `
        <label class="field"><span>标题</span><input id="gTitle" maxlength="100" placeholder="比如：园区改造标书交付"></label>
        <label class="field"><span>描述</span><textarea id="gDesc" rows="3" placeholder="可选"></textarea></label>
        <label class="field"><span>关联房间</span><select id="gConv">
          <option value="">不关联</option>
          ${state.convs.map(c => `<option value="${esc(c.id)}">${esc(c.title)}</option>`).join('')}
        </select></label>
        <button class="btn btn-primary" id="gSave" style="width:100%">创建</button>`;
      el.querySelector('#gSave').addEventListener('click', async () => {
        const title = el.querySelector('#gTitle').value.trim();
        if (!title) return toast('标题必填', { type: 'err' });
        try {
          await api('/v1/goals', { method: 'POST', body: {
            title, description: el.querySelector('#gDesc').value.trim(),
            conversationId: el.querySelector('#gConv').value,
          } });
          toast('已创建', { type: 'ok' });
          await load();
        } catch (e) { toast(e.message, { type: 'err' }); }
      });
    },
  });
}

function openDetail(id) {
  const g = goals.find(x => x.id === id);
  if (!g) return;
  openPanel({
    title: g.title,
    subtitle: g.progress + '% · ' + (g.progressSource === 'manual' ? '手动' : '按任务派生'),
    render: el => {
      el.innerHTML = `
        <div class="gd-desc">${esc(g.description || '没有描述')}</div>
        <div class="ap-sec"><h4>状态</h4>
          <div class="seg" id="gdStatus">${COLS.map(c =>
            `<button class="${g.status === c.key ? 'on' : ''}" data-s="${c.key}">${c.label}</button>`).join('')}</div>
        </div>
        <div class="ap-sec"><h4>关联任务</h4>
          ${(g.runIds || []).length ? (g.runIds || []).map(r => `<div class="gd-run"><code>${esc(r)}</code></div>`).join('') : '<div class="panel-empty">还没有关联任务</div>'}
        </div>
        <button class="btn btn-line danger" id="gdDel">删除目标</button>`;
      el.querySelectorAll('#gdStatus button').forEach(b => b.addEventListener('click', () => patch(g.id, { status: b.dataset.s })));
      el.querySelector('#gdDel').addEventListener('click', async () => {
        if (!await confirmModal({ title: '删除目标', text: '删除后不能恢复。', okText: '删除', danger: true })) return;
        try { await api('/v1/goals/' + g.id, { method: 'DELETE' }); await load(); toast('已删除', { type: 'ok' }); }
        catch (e) { toast(e.message, { type: 'err' }); }
      });
    },
  });
}
```

- [ ] **Step 2: 验证**

```bash
(ADDR=:8091 DATA_DIR=/tmp/ui-m2 ./bin/ensemble &) ; sleep 1
open 'http://127.0.0.1:8091/app/#/goals'
```

Expected: 空态 → 新建目标 → 卡片出现在「待规划」→ 拖到「进行中」后刷新仍是 doing。

- [ ] **Step 3: 提交**

```bash
git add web/app/views/goals.js
git commit -m "feat(ui): 目标看板（看板/列表、拖拽改状态）"
```

---

## Task 14: 连接器

**Files:**
- Create: `web/app/views/connectors.js`
- Test: E2E 连接器段

**Interfaces:**
- Consumes: `GET /v1/connectors`（数组，字段含 `id,scope,kind,name,enabled,config,secretKeys,status,lastError`）、`POST/PATCH/DELETE`、`POST /v1/connectors/{id}/probe`。
- Produces: 开关 `PATCH {enabled}`；权限预览 popover。

- [ ] **Step 1: 写 connectors.js**

```js
import { $, api, esc, icon, hydrateIcons, emptyState, toast } from '../js/core.js';
import { openPanel } from '../js/shell.js';
import { confirmModal } from '../js/modal.js';

const KIND = {
  feishu: { label: '飞书文档', ic: 'file-text', fields: ['appId', 'appSecret'] },
  notion: { label: 'Notion', ic: 'book-open', fields: ['token', 'databaseId'] },
  github: { label: 'GitHub', ic: 'code', fields: ['token', 'repo'] },
  calendar: { label: '企业日历', ic: 'calendar', fields: ['token'] },
  webhook: { label: 'Webhook', ic: 'share-2', fields: ['url', 'secret'] },
};

let root = null, list = [];

export async function mount(host) { root = host; await load(); }
export function unmount() { root = null; }

async function load() {
  try { list = await api('/v1/connectors'); } catch (e) { list = []; }
  render();
}

function row(c) {
  const k = KIND[c.kind] || { label: c.kind, ic: 'share-2' };
  return `<div class="conn-row" data-id="${esc(c.id)}">
    <span class="conn-ic">${icon(k.ic, 18)}</span>
    <div class="conn-body">
      <div class="conn-name">${esc(c.name)} <span class="conn-kind">${esc(k.label)}</span></div>
      <div class="conn-desc">${esc(c.lastError || (c.status === 'ok' ? '连通正常' : '未测试'))}</div>
    </div>
    <button class="switch ${c.enabled ? 'on' : ''}" data-toggle="${esc(c.id)}" title="${c.enabled ? '关闭' : '启用'}"></button>
    <button class="btn btn-line" data-perm="${esc(c.id)}">查看权限</button>
  </div>`;
}

function render() {
  if (!root) return;
  const team = list.filter(c => c.scope === 'team');
  const mine = list.filter(c => c.scope !== 'team');
  root.innerHTML = `
    <div class="page">
      <header class="page-head">
        <div><h1>连接器</h1><p class="page-sub">给 Agent 接上外部服务；凭据只在本地加密保存</p></div>
        <button class="btn btn-primary" id="newConn">${icon('plus', 16)}添加连接器</button>
      </header>
      <section class="sec"><h2 class="sec-h">团队公共连接器</h2>
        ${team.length ? team.map(row).join('') : emptyState({ icon: 'share-2', title: '暂未绑定任何第三方服务', sub: '团队公共连接器对所有成员可见', action: '<button class="btn btn-primary" id="emptyTeamConn">立即绑定</button>' })}
      </section>
      <section class="sec"><h2 class="sec-h">我的私有连接器</h2>
        ${mine.length ? mine.map(row).join('') : emptyState({ icon: 'share-2', title: '还没有私有连接器', sub: '私有连接器只有你自己能用' })}
      </section>
    </div>`;
  hydrateIcons(root);
  root.querySelectorAll('[data-toggle]').forEach(b => b.addEventListener('click', async () => {
    const c = list.find(x => x.id === b.dataset.toggle);
    try { await api('/v1/connectors/' + c.id, { method: 'PATCH', body: { enabled: !c.enabled } }); await load(); }
    catch (e) { toast(e.message, { type: 'err' }); }
  }));
  root.querySelectorAll('[data-perm]').forEach(b => b.addEventListener('click', () => {
    const c = list.find(x => x.id === b.dataset.perm);
    openPanel({
      title: c.name, subtitle: '权限预览',
      render: el => {
        const k = KIND[c.kind] || { fields: [] };
        el.innerHTML = `
          <div class="ap-sec"><h4>已配置凭据</h4>
            ${(c.secretKeys || []).length ? (c.secretKeys || []).map(s => `<div class="perm-row"><code>${esc(s)}</code><span class="ok">已填写</span></div>`).join('') : '<div class="panel-empty">还没有填写凭据</div>'}
          </div>
          <div class="ap-sec"><h4>Agent 可读字段</h4>
            ${(k.fields || []).map(f => `<div class="perm-row"><code>${esc(f)}</code><span class="${(c.secretKeys || []).includes(f) ? 'ok' : 'dim'}">${(c.secretKeys || []).includes(f) ? '可用' : '未配置'}</span></div>`).join('')}
          </div>
          <div class="ap-sec"><h4>操作</h4>
            <button class="btn btn-line" id="probe">测试连通</button>
            <button class="btn btn-line danger" id="del">删除连接器</button>
          </div>`;
        el.querySelector('#probe').addEventListener('click', async () => {
          try { const r = await api(`/v1/connectors/${c.id}/probe`, { method: 'POST' }); toast(r.status === 'ok' ? '连通正常' : (r.lastError || '连接失败'), { type: r.status === 'ok' ? 'ok' : 'err' }); await load(); }
          catch (e) { toast(e.message, { type: 'err' }); }
        });
        el.querySelector('#del').addEventListener('click', async () => {
          if (!await confirmModal({ title: '删除连接器', text: '删除后需要重新填写凭据。', okText: '删除', danger: true })) return;
          try { await api('/v1/connectors/' + c.id, { method: 'DELETE' }); await load(); toast('已删除', { type: 'ok' }); }
          catch (e) { toast(e.message, { type: 'err' }); }
        });
      },
    });
  }));
  $('#newConn')?.addEventListener('click', addConn);
  $('#emptyTeamConn')?.addEventListener('click', addConn);
}

function addConn() {
  openPanel({
    title: '添加连接器',
    render: el => {
      el.innerHTML = `
        <label class="field"><span>服务</span><select id="cKind">
          ${Object.entries(KIND).map(([k, v]) => `<option value="${k}">${v.label}</option>`).join('')}
        </select></label>
        <label class="field"><span>名称</span><input id="cName" maxlength="40" placeholder="比如：团队飞书"></label>
        <label class="field"><span>作用域</span><select id="cScope">
          <option value="private">我的私有</option><option value="team">团队公共</option>
        </select></label>
        <div id="cFields"></div>
        <button class="btn btn-primary" id="cSave" style="width:100%">保存</button>`;
      const fields = () => {
        const k = KIND[el.querySelector('#cKind').value] || { fields: [] };
        el.querySelector('#cFields').innerHTML = k.fields.map(f =>
          `<label class="field"><span>${f}</span><input data-secret="${f}" placeholder="${f}" autocomplete="off"></label>`).join('');
      };
      el.querySelector('#cKind').addEventListener('change', fields);
      fields();
      el.querySelector('#cSave').addEventListener('click', async () => {
        const secrets = {};
        el.querySelectorAll('[data-secret]').forEach(i => { if (i.value.trim()) secrets[i.dataset.secret] = i.value.trim(); });
        try {
          await api('/v1/connectors', { method: 'POST', body: {
            kind: el.querySelector('#cKind').value,
            name: el.querySelector('#cName').value.trim() || KIND[el.querySelector('#cKind').value].label,
            scope: el.querySelector('#cScope').value,
            secrets,
          } });
          toast('已保存', { type: 'ok' }); await load();
        } catch (e) { toast(e.message, { type: 'err' }); }
      });
    },
  });
}
```

- [ ] **Step 2: 验证**

```bash
open 'http://127.0.0.1:8091/app/#/connectors'
```

Expected: 空态 → 添加一个 Webhook → 出现在「我的私有」→ 开关可切 → 「查看权限」显示字段 → 「测试连通」返回真实结果。

- [ ] **Step 3: 提交**

```bash
git add web/app/views/connectors.js
git commit -m "feat(ui): 连接器页"
```

---

## Task 15: 资料库

**Files:**
- Create: `web/app/views/library.js`
- Test: E2E 资料库段

**Interfaces:**
- Consumes: `GET /v1/library?convId=&limit=`（数组，含 `id,title,kind,summary,agentName,conversationId,conversationTitle,createdAt`）、`GET /v1/library/{id}`（含 `content`）、`DELETE`。
- Produces: 右侧大预览（markdown / 表格 / 代码 / 图片）。

- [ ] **Step 1: 写 library.js**

```js
import { $, api, state, esc, icon, hydrateIcons, emptyState, toast, fullTimeStr } from '../js/core.js';
import { openPanel } from '../js/shell.js';
import { avatarHTML } from '../js/avatars.js';
import { confirmModal } from '../js/modal.js';
import { mdRender } from '../js/chat.js';

const KINDS = [
  { key: '', label: '全部' },
  { key: 'doc', label: '文档' },
  { key: 'code', label: '代码' },
  { key: 'sheet', label: '表格' },
  { key: 'note', label: '笔记' },
];
let root = null, items = [], convFilter = '', kindFilter = '';

export async function mount(host) { root = host; await load(); }
export function unmount() { root = null; }

async function load() {
  const q = new URLSearchParams({ limit: '60' });
  if (convFilter) q.set('convId', convFilter);
  try { items = await api('/v1/library?' + q); } catch (e) { items = []; }
  render();
}

function filtered() { return kindFilter ? items.filter(i => i.kind === kindFilter) : items; }

function render() {
  if (!root) return;
  const list = filtered();
  root.innerHTML = `
    <div class="page">
      <header class="page-head">
        <div><h1>资料库</h1><p class="page-sub">角色产出的标书、文案、代码都在这里</p></div>
        <div class="page-actions">
          <select id="libConv" class="select">
            <option value="">全部团队</option>
            ${state.convs.map(c => `<option value="${esc(c.id)}" ${c.id === convFilter ? 'selected' : ''}>${esc(c.title)}</option>`).join('')}
          </select>
        </div>
      </header>
      <div class="seg seg--filter" id="libKind">
        ${KINDS.map(k => `<button class="${k.key === kindFilter ? 'on' : ''}" data-k="${k.key}">${k.label}</button>`).join('')}
      </div>
      ${list.length ? `<div class="lib-grid">${list.map(card).join('')}</div>` : emptyState({
        icon: 'book-open', title: '暂无团队产出文件', sub: 'AI 团队生成的标书、代码、文案会自动归档到这里',
      })}
    </div>`;
  hydrateIcons(root);
  root.querySelectorAll('#libKind button').forEach(b => b.addEventListener('click', () => { kindFilter = b.dataset.k; render(); }));
  root.querySelector('#libConv').addEventListener('change', e => { convFilter = e.target.value; load(); });
  root.querySelectorAll('.lib-card').forEach(el => el.addEventListener('click', () => openArtifact(el.dataset.id)));
}

function card(a) {
  return `<article class="lib-card" data-id="${esc(a.id)}">
    <div class="lib-thumb">${icon(a.kind === 'code' ? 'code' : a.kind === 'sheet' ? 'chart' : 'file-text', 28)}</div>
    <div class="lib-body">
      <div class="lib-title">${esc(a.title || '未命名')}</div>
      <div class="lib-sum">${esc(a.summary || '')}</div>
      <div class="lib-meta">
        ${a.agentName ? avatarHTML(a.agentName, '', { size: 20 }) : ''}
        <span>${esc(a.agentName || '')}</span>
        <span class="dim">${esc(fullTimeStr(a.createdAt))}</span>
      </div>
    </div>
  </article>`;
}

async function openArtifact(id) {
  let a;
  try { a = await api('/v1/library/' + id); } catch (e) { return toast('打不开：' + e.message, { type: 'err' }); }
  openPanel({
    title: a.title || '产物',
    subtitle: (a.agentName || '') + ' · ' + fullTimeStr(a.createdAt),
    render: el => {
      el.innerHTML = `
        <div class="art-body">${renderContent(a)}</div>
        <div class="art-actions">
          <button class="btn btn-line" id="artCopy">复制</button>
          <button class="btn btn-line" id="artDl">下载</button>
          <button class="btn btn-line danger" id="artDel">删除</button>
        </div>`;
      el.querySelector('#artCopy').addEventListener('click', async () => {
        try { await navigator.clipboard.writeText(a.content || ''); toast('已复制', { type: 'ok' }); }
        catch (e) { toast('复制失败', { type: 'err' }); }
      });
      el.querySelector('#artDl').addEventListener('click', () => download(a));
      el.querySelector('#artDel').addEventListener('click', async () => {
        if (!await confirmModal({ title: '删除产物', text: '删除后不能恢复。', okText: '删除', danger: true })) return;
        try { await api('/v1/library/' + a.id, { method: 'DELETE' }); await load(); toast('已删除', { type: 'ok' }); }
        catch (e) { toast(e.message, { type: 'err' }); }
      });
    },
  });
}

function renderContent(a) {
  const c = a.content || '';
  if (a.kind === 'sheet') return `<pre class="art-pre">${esc(c)}</pre>`;
  if (a.kind === 'code') return `<pre class="art-pre art-code">${esc(c)}</pre>`;
  return `<div class="md">${mdRender(c)}</div>`;
}

function download(a) {
  const ext = a.kind === 'code' ? 'txt' : 'md';
  const blob = new Blob([a.content || ''], { type: 'text/plain;charset=utf-8' });
  const url = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = url;
  link.download = (a.title || 'artifact') + '.' + ext;
  document.body.appendChild(link);
  link.click();
  link.remove();
  setTimeout(() => URL.revokeObjectURL(url), 3000);
}
```

- [ ] **Step 2: 验证**

```bash
open 'http://127.0.0.1:8091/app/#/library'
```

Expected: 有产物时网格卡片；点开右面板显示全文；复制/下载可用（桌面壳里下载走原生）。

- [ ] **Step 3: 提交**

```bash
git add web/app/views/library.js
git commit -m "feat(ui): 资料库与产物预览"
```

---

## M2 验收清单

- [ ] 看板拖拽改状态并持久化
- [ ] 连接器增删改查、开关、权限预览、真实 probe
- [ ] 资料库筛选、预览、复制、下载、删除
- [ ] `.ui-shots` 三屏三宽度无错位

---

# M3：新建团队、设置、引导、空状态与全局组件

## Task 16: 新建自定义团队

**Files:**
- Create: `web/app/views/newteam.js`（挂到 `window.__newTeam`）
- Modify: `web/app/js/boot.js`
- Test: E2E 新建团队段

**Interfaces:**
- Consumes: `state.library`（`GET /v1/characters`）、`POST /v1/conversations {title, studio:'custom', agents:[{key,name,title,emoji,color,prompt,permission,scope}]}`。
- Produces: `openNewTeam()`。

- [ ] **Step 1: 写 newteam.js**

```js
import { $, api, state, esc, icon, toast } from '../js/core.js';
import { openModal, closeModal } from '../js/modal.js';
import { navigate } from '../js/router.js';
import { avatarHTML } from '../js/avatars.js';

let picked = [];

export function openNewTeam() {
  picked = [];
  openModal({
    title: '新建自定义团队', width: 640,
    body: `
      <label class="field"><span>团队名称</span><input id="ntName" maxlength="20" placeholder="比如：增长小组"></label>
      <label class="field"><span>团队简介</span><input id="ntDesc" maxlength="60" placeholder="一句话说明这组角色负责什么"></label>
      <div class="nt-roles"><div class="nt-h">角色</div><div id="ntList"></div>
        <button class="btn btn-line" id="ntAdd">${icon('plus', 15)}添加角色</button></div>`,
    actions: [
      { key: 'cancel', label: '取消', kind: 'line' },
      { key: 'save', label: '保存并进入', kind: 'primary', onClick: save },
    ],
    onMount: mask => {
      renderPicked(mask);
      mask.querySelector('#ntAdd').addEventListener('click', () => pickRole(mask));
    },
  });
}

function renderPicked(mask) {
  const box = mask.querySelector('#ntList');
  box.innerHTML = picked.map((a, i) => `
    <div class="nt-row">
      ${avatarHTML(a.name, a.color, { size: 32 })}
      <div class="nt-body"><div class="nt-name">${esc(a.name)}</div><div class="nt-title">${esc(a.title || '')}</div></div>
      <button class="icon-btn" data-del="${i}">${icon('x', 15)}</button>
    </div>`).join('') || '<div class="panel-empty">还没有角色，至少加一个</div>';
  box.querySelectorAll('[data-del]').forEach(b => b.addEventListener('click', () => {
    picked.splice(Number(b.dataset.del), 1);
    renderPicked(mask);
  }));
}

async function pickRole(mask) {
  if (!state.library.length) {
    // 接口返回 {name, work, tags, tone}，不是 SavedAgent 形状
    try { state.library = await api('/v1/characters?q='); } catch (e) { state.library = []; }
  }
  const box = mask.querySelector('.nt-roles');
  box.insertAdjacentHTML('beforeend', `
    <div class="nt-picker" id="ntPicker">
      <input id="ntSearch" placeholder="搜名字、作品或性格">
      <div class="nt-results" id="ntResults"></div>
    </div>`);
  const render = q => {
    const list = state.library.filter(c => !q || (c.name + (c.work || '') + (c.tags || []).join('')).includes(q)).slice(0, 30);
    box.querySelector('#ntResults').innerHTML = list.map(c => `
      <button class="nt-res" data-n="${esc(c.name)}">${avatarHTML(c.name, '', { size: 24 })}<span>${esc(c.name)}</span><em>${esc(c.work || '')}</em></button>`).join('');
    box.querySelectorAll('.nt-res').forEach(b => b.addEventListener('click', () => {
      const c = state.library.find(x => x.name === b.dataset.n);
      if (c && !picked.some(p => p.name === c.name)) {
        picked.push({
          key: 'lib_' + c.name, characterId: 'lib:' + c.name,
          name: c.name, title: (c.tags || []).join(' / '),
          emoji: [...c.name][0], color: '', prompt: c.tone || '',
        });
        renderPicked(mask);
      }
    }));
  };
  render('');
  box.querySelector('#ntSearch').addEventListener('input', e => render(e.target.value.trim()));
  box.querySelector('#ntSearch').focus();
}

async function save(mask) {
  const name = mask.querySelector('#ntName').value.trim();
  if (!name) { toast('先写团队名称', { type: 'err' }); return false; }
  if (!picked.length) { toast('至少加一个角色', { type: 'err' }); return false; }
  try {
    const conv = await api('/v1/conversations', {
      method: 'POST',
      body: { title: name, studio: 'custom', agents: picked },
    });
    state.convs = await api('/v1/conversations');
    document.dispatchEvent(new CustomEvent('conv:change'));
    toast('团队已创建', { type: 'ok' });
    setTimeout(() => navigate('#/room/' + encodeURIComponent(conv.id)), 120);
    return true;
  } catch (e) { toast(e.message, { type: 'err' }); return false; }
}
```

- [ ] **Step 2: boot 注册**

```js
import { openNewTeam } from '../views/newteam.js';
window.__newTeam = openNewTeam;
```

- [ ] **Step 3: 验证并提交**

```bash
open 'http://127.0.0.1:8091/app/#/lobby'
git add web/app/views/newteam.js web/app/js/boot.js
git commit -m "feat(ui): 新建自定义团队弹窗"
```

---

## Task 17: 设置 / 个人中心

**Files:**
- Create: `web/app/views/settings.js`
- Test: E2E 设置段

**Interfaces:**
- Consumes: `GET /v1/me`、`PATCH /v1/me {name,avatar,preferences}`（偏好合并来自 Task 11）。
- Produces: 头像上传存 `localStorage.ens_avatar`；通知开关写 `preferences.notify`。

- [ ] **Step 1: 写 settings.js**

```js
import { $, api, state, esc, icon, hydrateIcons, toast } from '../js/core.js';
import { avatarHTML } from '../js/avatars.js';
import { confirmModal } from '../js/modal.js';

let root = null;

export function mount(host) { root = host; render(); }
export function unmount() { root = null; }

function prefs() { return state.me?.preferences || {}; }

function render() {
  if (!root) return;
  const u = state.user || {};
  const localAvatar = localStorage.getItem('ens_avatar') || '';
  root.innerHTML = `
    <div class="page">
      <header class="page-head"><div><h1>设置</h1><p class="page-sub">账号、通知与偏好</p></div></header>
      <section class="profile-card">
        <div class="pf-av">${localAvatar ? `<img src="${esc(localAvatar)}" alt="">` : avatarHTML(u.name || '我', '#367BED', { size: 64 })}</div>
        <div class="pf-body">
          <div class="pf-name">${esc(u.name || '未命名')}</div>
          <div class="pf-id">${esc(u.phone || u.email || '')} · ${esc(u.plan || 'free')}</div>
          <div class="pf-usage">剩余额度 ${state.me?.remaining ?? '-'} 次</div>
        </div>
        <label class="btn btn-line">更换头像<input type="file" id="avatarUp" accept="image/*" hidden></label>
      </section>
      <section class="settings-list">
        ${row('user-round', '个人资料', '修改昵称', 'editName')}
        ${row('bell', '消息通知', '审批与任务完成提醒', 'toggleNotify', prefs().notify !== 'false')}
        ${row('shield-alert', '账号安全', '登录设备与验证方式', 'security')}
        ${row('lock', '隐私设置', '数据与可见范围', 'privacy')}
        ${row('log-out', '退出登录', '', 'logout', true)}
      </section>
    </div>`;
  hydrateIcons(root);

  root.querySelector('#avatarUp').addEventListener('change', async e => {
    const f = e.target.files?.[0];
    if (!f) return;
    if (f.size > 400 * 1024) return toast('图片别超过 400KB', { type: 'err' });
    const reader = new FileReader();
    reader.onload = () => {
      localStorage.setItem('ens_avatar', String(reader.result));
      render();
      toast('头像已更新', { type: 'ok' });
    };
    reader.readAsDataURL(f);
  });
  root.querySelector('[data-act=editName]').addEventListener('click', async () => {
    const el = document.querySelector('#modalRoot');
    const { openModal } = await import('../js/modal.js');
    openModal({
      title: '修改昵称',
      body: `<label class="field"><span>昵称</span><input id="nm" maxlength="20" value="${esc(u.name || '')}"></label>`,
      actions: [
        { key: 'no', label: '取消', kind: 'line' },
        { key: 'ok', label: '保存', kind: 'primary', onClick: async mask => {
          const name = mask.querySelector('#nm').value.trim();
          if (!name) { toast('昵称不能为空', { type: 'err' }); return false; }
          try {
            state.user = await api('/v1/me', { method: 'PATCH', body: { name } });
            state.user = state.user.user || state.user;
            render(); toast('已保存', { type: 'ok' });
          } catch (e) { toast(e.message, { type: 'err' }); return false; }
        } },
      ],
    });
  });
  root.querySelector('[data-act=toggleNotify]').addEventListener('click', async e => {
    const next = prefs().notify === 'false' ? 'true' : 'false';
    e.currentTarget.classList.toggle('on', next === 'true');
    try {
      await api('/v1/me', { method: 'PATCH', body: { preferences: { notify: next } } });
      state.me.preferences = { ...prefs(), notify: next };
      toast(next === 'true' ? '已开启通知' : '已关闭通知', { type: 'ok' });
    } catch (err) { toast(err.message, { type: 'err' }); }
  });
  root.querySelector('[data-act=security]').addEventListener('click', () => toast('当前使用手机号/邮箱验证码登录'));
  root.querySelector('[data-act=privacy]').addEventListener('click', () => toast('工作区路径只存在本机，不上传'));
  root.querySelector('[data-act=logout]').addEventListener('click', async () => {
    if (!await confirmModal({ title: '退出登录', text: '退出后需要重新验证。', okText: '退出', danger: true })) return;
    localStorage.removeItem('ens_token');
    location.reload();
  });
}

function row(ic, title, sub, act, on = null) {
  return `<button class="set-row" data-act="${act}">
    <span class="set-ic">${icon(ic, 18)}</span>
    <span class="set-body"><span class="set-t">${title}</span>${sub ? `<span class="set-s">${sub}</span>` : ''}</span>
    ${on !== null ? `<span class="switch ${on ? 'on' : ''}"></span>` : icon('chevron-right', 16)}
  </button>`;
}
```

- [ ] **Step 2: 验证并提交**

```bash
open 'http://127.0.0.1:8091/app/#/settings'
git add web/app/views/settings.js
git commit -m "feat(ui): 设置与个人中心"
```

---

## Task 17b: 团队设置右滑面板

**Files:**
- Create: `web/app/views/teamsettings.js`
- Modify: `web/app/views/room.js`（顶栏加设置入口）
- Test: 手工：改群名后左栏与顶栏同步

**Interfaces:**
- Consumes: `PATCH /v1/conversations/{id}`（现有 `handleUpdateConv`）、`POST /v1/studios {fromConvId}`、`DELETE /v1/conversations/{id}`。
- Produces: `openTeamSettings(conv)`。

- [ ] **Step 1: 写 teamsettings.js**

```js
import { api, state, esc, icon, toast } from '../js/core.js';
import { openPanel } from '../js/shell.js';
import { confirmModal, openModal } from '../js/modal.js';
import { avatarHTML } from '../js/avatars.js';
import { navigate } from '../js/router.js';

export function openTeamSettings(conv) {
  if (!conv) return;
  openPanel({
    title: '团队设置',
    subtitle: conv.title,
    render: el => {
      el.innerHTML = `
        <div class="ts-cover">${avatarHTML(conv.title, '#367BED', { size: 56 })}</div>
        <button class="set-row" data-act="rename">${icon('pencil', 17)}<span class="set-t">修改团队名称</span>${icon('chevron-right', 16)}</button>
        <button class="set-row" data-act="cover">${icon('image', 17)}<span class="set-t">更换封面</span>${icon('chevron-right', 16)}</button>
        <button class="set-row" data-act="members">${icon('users', 17)}<span class="set-t">查看全部角色</span>${icon('chevron-right', 16)}</button>
        <button class="set-row" data-act="copy">${icon('copy', 17)}<span class="set-t">复制团队模板</span>${icon('chevron-right', 16)}</button>
        <button class="set-row danger" data-act="dissolve">${icon('trash', 17)}<span class="set-t">解散团队</span></button>`;

      el.querySelector('[data-act=rename]').addEventListener('click', () => {
        openModal({
          title: '修改团队名称',
          body: `<label class="field"><span>名称</span><input id="tn" maxlength="20" value="${esc(conv.title)}"></label>`,
          actions: [
            { key: 'no', label: '取消', kind: 'line' },
            { key: 'ok', label: '保存', kind: 'primary', onClick: async mask => {
              const title = mask.querySelector('#tn').value.trim();
              if (!title) { toast('名称不能为空', { type: 'err' }); return false; }
              try {
                await api('/v1/conversations/' + conv.id, { method: 'PATCH', body: { title } });
                conv.title = title;
                state.convs = await api('/v1/conversations');
                document.dispatchEvent(new CustomEvent('conv:change'));
                openTeamSettings(conv);
                toast('已保存', { type: 'ok' });
              } catch (e) { toast(e.message, { type: 'err' }); return false; }
            } },
          ],
        });
      });

      el.querySelector('[data-act=cover]').addEventListener('click', () => {
        const covers = ['bid', 'xhs', 'code', 'anime', 'research', 'companion'];
        openModal({
          title: '更换封面',
          width: 560,
          body: `<div class="cover-pick">${covers.map(c => `
            <button class="cover-opt" data-c="${c}"><img src="avatars/covers/${c}.png" alt="${c}"></button>`).join('')}</div>
            <div class="hint">封面只影响本机的团队大厅展示，不上传。</div>`,
          actions: [{ key: 'no', label: '关闭', kind: 'line' }],
          onMount: mask => {
            mask.querySelectorAll('.cover-opt').forEach(b => b.addEventListener('click', () => {
              localStorage.setItem('ens_cover_' + conv.id, b.dataset.c);
              toast('封面已更新（本机）', { type: 'ok' });
            }));
          },
        });
      });

      el.querySelector('[data-act=members]').addEventListener('click', async () => {
        const agents = conv.agents?.length ? conv.agents : (state.scenarios.find(s => s.key === conv.studio)?.agents || []);
        openModal({
          title: '全部角色', width: 520,
          body: agents.map(a => `<div class="nt-row">${avatarHTML(a.name, a.color, { size: 32 })}
            <div class="nt-body"><div class="nt-name">${esc(a.name)}</div><div class="nt-title">${esc(a.title || '')}</div></div></div>`).join(''),
          actions: [{ key: 'no', label: '关闭', kind: 'line' }],
        });
      });

      el.querySelector('[data-act=copy]').addEventListener('click', async () => {
        try {
          await api('/v1/studios', { method: 'POST', body: { fromConvId: conv.id, name: conv.title + '（副本）' } });
          toast('已存为模板，可在变体里换人', { type: 'ok' });
        } catch (e) { toast(e.message, { type: 'err' }); }
      });

      el.querySelector('[data-act=dissolve]').addEventListener('click', async () => {
        if (!await confirmModal({ title: '解散团队', text: '会话与消息会一起删除，不能恢复。', okText: '解散', danger: true })) return;
        try {
          await api('/v1/conversations/' + conv.id, { method: 'DELETE' });
          state.convs = await api('/v1/conversations');
          document.dispatchEvent(new CustomEvent('conv:change'));
          navigate('#/lobby');
          toast('已解散', { type: 'ok' });
        } catch (e) { toast(e.message, { type: 'err' }); }
      });
    },
  });
}
```

- [ ] **Step 2: 房间顶栏接入**

`room.js` 顶栏右侧加：

```js
<button class="icon-btn" id="roomSettings" title="团队设置">${icon('settings', 17)}</button>
```

并绑定：

```js
import { openTeamSettings } from './teamsettings.js';
$('#roomSettings').addEventListener('click', () => openTeamSettings(state.convs.find(c => c.id === params.id)));
```

- [ ] **Step 3: 验证并提交**

```bash
open 'http://127.0.0.1:8091/app/#/room/<id>'
git add web/app/views/teamsettings.js web/app/views/room.js
git commit -m "feat(ui): 团队设置右滑面板"
```

---

## Task 18: 新手引导

**Files:**
- Create: `web/app/js/onboarding.js`
- Modify: `web/app/js/boot.js`
- Test: 手工走查四步

**Interfaces:**
- Produces: `maybeStartOnboarding()`，`localStorage.ens_onboarded === '1'` 时跳过。

- [ ] **Step 1: 写 onboarding.js**

```js
import { esc, icon } from './core.js';

const STEPS = [
  { sel: '#newTeam', title: '从这里开始', text: '点「新建自定义团队」，挑几个角色组一个团队。' },
  { sel: '#avatarRow', title: '角色状态', text: '灰环空闲、蓝环在干活、绿环刚完成、红环等你审批。', route: 'room' },
  { sel: '#navList .nav-item[data-key=goals]', title: '目标看板', text: '把目标挂上角色和任务，进度自动派生。', route: 'goals' },
];

export function maybeStartOnboarding() {
  if (localStorage.getItem('ens_onboarded') === '1') return;
  setTimeout(() => start(0), 900);
}

function start(i) {
  if (i >= STEPS.length) return done();
  const s = STEPS[i];
  const target = document.querySelector(s.sel);
  if (!target) {
    if (s.route) { location.hash = '#/' + s.route; setTimeout(() => start(i), 700); return; }
    return start(i + 1);
  }
  const r = target.getBoundingClientRect();
  const mask = document.createElement('div');
  mask.className = 'onb-mask';
  mask.innerHTML = `
    <div class="onb-ring" style="left:${r.left - 6}px;top:${r.top - 6}px;width:${r.width + 12}px;height:${r.height + 12}px"></div>
    <div class="onb-tip" style="left:${Math.min(Math.max(16, r.left), window.innerWidth - 320)}px;top:${Math.min(r.bottom + 14, window.innerHeight - 180)}px">
      <h4>${esc(s.title)}</h4><p>${esc(s.text)}</p>
      <div class="onb-act">
        <button class="btn btn-line" data-x="skip">跳过</button>
        <button class="btn btn-primary" data-x="next">${i === STEPS.length - 1 ? '完成' : '下一步'}</button>
      </div>
    </div>`;
  document.body.appendChild(mask);
  mask.querySelector('[data-x=skip]').addEventListener('click', () => { mask.remove(); done(); });
  mask.querySelector('[data-x=next]').addEventListener('click', () => {
    mask.remove();
    if (i + 1 < STEPS.length) { location.hash = '#/' + 'lobby'; setTimeout(() => start(i + 1), 500); }
    else done();
  });
}

function done() {
  localStorage.setItem('ens_onboarded', '1');
}
```

- [ ] **Step 2: boot 里调用**

```js
import { maybeStartOnboarding } from './onboarding.js';
// enterApp 末尾：
maybeStartOnboarding();
```

- [ ] **Step 3: 验证并提交**

```bash
# 清掉标记重新登录，确认四步蒙版按顺序出现，可跳过
localStorage.removeItem('ens_onboarded')
git add web/app/js/onboarding.js web/app/js/boot.js
git commit -m "feat(ui): 新手引导四步"
```

---

## Task 19: 空状态合集与 404

**Files:**
- Create: `web/app/views/notfound.js`
- Modify: `web/app/css/components.css`（空状态样式）
- Test: E2E 访问未知路由

**Interfaces:**
- Consumes: `core.emptyState`。
- Produces: `notfound.mount(host)`。

- [ ] **Step 1: 写 notfound.js**

```js
import { emptyState } from '../js/core.js';

export function mount(host) {
  host.innerHTML = `<div class="page page--center">${emptyState({
    icon: 'search', title: '页面不存在', sub: '地址可能写错了，或者页面已经被移走',
    action: '<a class="btn btn-primary" href="#/lobby">返回首页</a>',
  })}</div>`;
}
export function unmount() {}
```

- [ ] **Step 2: 补六种空状态的文案常量（供各视图复用）**

`views/` 各页已按以下文案使用：暂无团队 / 暂无对话 / 暂无目标 / 暂无绑定服务 / 暂无产出 / 搜索无结果。搜索无结果在 `chat.js` 搜索面板里替换为 `emptyState({icon:'search',title:'未找到匹配内容',sub:'尝试更换关键词搜索'})`。

- [ ] **Step 3: 验证并提交**

```bash
open 'http://127.0.0.1:8091/app/#/nope'
git add web/app/views/notfound.js web/app/css/components.css web/app/js/chat.js
git commit -m "feat(ui): 404 与空状态合集"
```

---

## Task 20: 全局组件补全（Toast/骨架/权限拒绝）

**Files:**
- Modify: `web/app/css/components.css`
- Modify: `web/app/js/modal.js`（已含 denyModal）
- Test: `.ui-shots` 目视

- [ ] **Step 1: 补齐组件 CSS**

`components.css` 必须包含：`.toast`（右上浮动、20px 圆角、三种状态色）、`.sk`（浅灰 shimmer 动画）、`.empty-state`、`.btn-primary`、`.btn-line`、`.switch`、`.seg`、`.field`、`.modal-mask/.modal-card`、`.onb-mask/.onb-ring/.onb-tip`。阴影统一 `var(--sh)`，圆角 `var(--r-lg)`。

- [ ] **Step 2: shimmer 动画**

```css
@keyframes sk-shimmer { 0%{background-position:-200px 0} 100%{background-position:200px 0} }
.sk-b{height:14px;border-radius:8px;background:linear-gradient(90deg,#F1F2F4 0%,#F8F8FA 50%,#F1F2F4 100%);background-size:400px 100%;animation:sk-shimmer 1.2s linear infinite}
```

- [ ] **Step 3: 提交**

```bash
git add web/app/css/components.css
git commit -m "feat(ui): 全局组件样式"
```

---

## M3 验收清单

- [ ] 新建团队 → 选角色 → 进入房间
- [ ] 设置页改昵称、传头像、开关通知并刷新保持
- [ ] 首次登录出现四步引导，可跳过，完成后不再出现
- [ ] 六个空状态与 404 均可达
- [ ] Toast 右上、骨架屏、权限拒绝弹窗均可用

---

# M4：桌面壳重做、打包、E2E、验收

## Task 21: 重写桌面壳

**Files:**
- Modify: `desktop/EnsembleDesktop.swift`（整体替换）
- Test: 干净目录双击 `.app` 全流程

**Interfaces:**
- Consumes: `Bundle.main.executableURL`、`Contents/Resources/web`、`~/Library/Application Support/智团/.env`。
- Produces: `WKScriptMessageHandler` 名 `notify`（前端 `notifyNative` 调它）。

- [ ] **Step 1: 写壳**

```swift
// 智团桌面壳：窗口 + 原生选目录 + 后端自启 + 下载 + 通知。
import Cocoa
import WebKit
import UserNotifications

private let appDirName = "智团"
private let supportDir: URL = {
    let base = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first!
    return base.appendingPathComponent(appDirName, isDirectory: true)
}()
private var server: Process?

final class AppDelegate: NSObject, NSApplicationDelegate, WKNavigationDelegate,
                         WKUIDelegate, WKDownloadDelegate, WKScriptMessageHandler {
    private var window: NSWindow!
    private var webView: WKWebView!
    private var baseURL = "http://127.0.0.1:8080"

    func applicationDidFinishLaunching(_ n: Notification) {
        try? FileManager.default.createDirectory(at: supportDir, withIntermediateDirectories: true)
        buildMenu()
        buildWindow()
        requestNotifyPermission()
        startFlow()
    }

    func applicationShouldTerminateAfterLastWindowClosed(_ s: NSApplication) -> Bool { true }

    func applicationWillTerminate(_ n: Notification) {
        if let p = server, p.isRunning { p.terminate() }
    }

    // MARK: 窗口

    private func buildWindow() {
        let cfg = WKWebViewConfiguration()
        cfg.preferences.setValue(true, forKey: "developerExtrasEnabled")
        cfg.websiteDataStore = .default()
        cfg.userContentController.add(self, name: "notify")

        webView = WKWebView(frame: .zero, configuration: cfg)
        webView.navigationDelegate = self
        webView.uiDelegate = self
        webView.underPageBackgroundColor = .white

        window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 1320, height: 900),
                          styleMask: [.titled, .closable, .miniaturizable, .resizable, .fullSizeContentView],
                          backing: .buffered, defer: false)
        window.title = "智团"
        window.minSize = NSSize(width: 1080, height: 720)
        window.contentView = webView
        window.setFrameAutosaveName("ZhituanMainWindow")
        window.center()
        window.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
    }

    private func buildMenu() {
        let main = NSMenu()
        let appItem = NSMenuItem(); let appMenu = NSMenu()
        appMenu.addItem(withTitle: "关于智团", action: #selector(NSApplication.orderFrontStandardAboutPanel(_:)), keyEquivalent: "")
        appMenu.addItem(.separator())
        appMenu.addItem(withTitle: "重新载入", action: #selector(reload), keyEquivalent: "r")
        appMenu.addItem(withTitle: "退出", action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q")
        appItem.submenu = appMenu; main.addItem(appItem)

        let fileItem = NSMenuItem(); let fileMenu = NSMenu(title: "文件")
        let pick = NSMenuItem(title: "选择工作区目录…", action: #selector(pickWorkspace), keyEquivalent: "o")
        pick.target = self; fileMenu.addItem(pick)
        fileItem.submenu = fileMenu; main.addItem(fileItem)

        let goItem = NSMenuItem(); let goMenu = NSMenu(title: "前往")
        let names = ["团队大厅", "聊天房间", "目标看板", "连接器", "资料库", "设置"]
        for (i, name) in names.enumerated() {
            let it = NSMenuItem(title: name, action: #selector(goTo(_:)), keyEquivalent: "\(i + 1)")
            it.tag = i; it.keyEquivalentModifierMask = [.command]; it.target = self
            goMenu.addItem(it)
        }
        goMenu.addItem(.separator())
        let panel = NSMenuItem(title: "切换工作面板", action: #selector(togglePanel), keyEquivalent: "0")
        panel.target = self; goMenu.addItem(panel)
        goItem.submenu = goMenu; main.addItem(goItem)

        let viewItem = NSMenuItem(); let viewMenu = NSMenu(title: "显示")
        viewMenu.addItem(withTitle: "放大", action: #selector(NSView.performZoom(_:)), keyEquivalent: "")
        viewMenu.addItem(.separator())
        viewMenu.addItem(withTitle: "开发者工具", action: #selector(devtools), keyEquivalent: "i").target = self
        viewItem.submenu = viewMenu; main.addItem(viewItem)

        let editItem = NSMenuItem(); let editMenu = NSMenu(title: "编辑")
        editMenu.addItem(withTitle: "复制", action: #selector(NSText.copy(_:)), keyEquivalent: "c")
        editMenu.addItem(withTitle: "粘贴", action: #selector(NSText.paste(_:)), keyEquivalent: "v")
        editMenu.addItem(withTitle: "全选", action: #selector(NSText.selectAll(_:)), keyEquivalent: "a")
        editItem.submenu = editMenu; main.addItem(editItem)

        NSApp.mainMenu = main
    }

    // MARK: 启动

    private func startFlow() {
        if ping(baseURL) { load(baseURL); return }
        guard let port = freePort(from: 8080, to: 8099) else { showError("没有可用端口（8080–8099）"); return }
        baseURL = "http://127.0.0.1:\(port)"
        startBackend(port: port)
        pollUntilReady(tries: 150)
    }

    private func ping(_ base: String) -> Bool {
        guard let url = URL(string: base + "/healthz") else { return false }
        var req = URLRequest(url: url); req.timeoutInterval = 0.6
        let sem = DispatchSemaphore(value: 0)
        var alive = false
        URLSession.shared.dataTask(with: req) { _, resp, _ in
            alive = (resp as? HTTPURLResponse)?.statusCode == 200; sem.signal()
        }.resume()
        _ = sem.wait(timeout: .now() + 1.2)
        return alive
    }

    private func pollUntilReady(tries: Int) {
        DispatchQueue.global().async { [weak self] in
            guard let self else { return }
            for i in 0..<tries {
                Thread.sleep(forTimeInterval: 0.1)
                if i > 30, self.server?.isRunning == false {
                    DispatchQueue.main.async { self.showError("后端启动失败。日志：\(supportDir.path)/ensemble.log") }
                    return
                }
                if self.ping(self.baseURL) { DispatchQueue.main.async { self.load(self.baseURL) }; return }
            }
            DispatchQueue.main.async { self.showError("后端 15 秒内没有就绪。日志：\(supportDir.path)/ensemble.log") }
        }
    }

    private func startBackend(port: Int) {
        guard let exe = Bundle.main.executableURL?.deletingLastPathComponent().appendingPathComponent("ensemble"),
              FileManager.default.isExecutableFile(atPath: exe.path) else {
            showError("应用包内缺少 ensemble 二进制，请重新运行 desktop/build.sh")
            return
        }
        var env = ProcessInfo.processInfo.environment
        for (k, v) in loadDotEnv() { env[k] = v }
        env["ADDR"] = ":\(port)"
        if env["DATA_DIR"] == nil { env["DATA_DIR"] = supportDir.appendingPathComponent("data").path }

        let p = Process()
        p.executableURL = exe
        p.environment = env
        p.currentDirectoryURL = Bundle.main.bundleURL
        let log = supportDir.appendingPathComponent("ensemble.log")
        FileManager.default.createFile(atPath: log.path, contents: nil)
        if let h = try? FileHandle(forWritingTo: log) { p.standardOutput = h; p.standardError = h }
        do { try p.run(); server = p } catch { showError("启动后端失败：\(error.localizedDescription)") }
    }

    /// 依次读应用支持目录与仓库根的 .env。
    private func loadDotEnv() -> [String: String] {
        var out: [String: String] = [:]
        var candidates = [supportDir.appendingPathComponent(".env")]
        if let cwd = ProcessInfo.processInfo.environment["PWD"] {
            candidates.append(URL(fileURLWithPath: cwd).appendingPathComponent(".env"))
        }
        for url in candidates {
            guard let text = try? String(contentsOf: url, encoding: .utf8) else { continue }
            for line in text.split(separator: "\n") {
                let t = line.trimmingCharacters(in: .whitespaces)
                if t.isEmpty || t.hasPrefix("#") { continue }
                guard let eq = t.firstIndex(of: "=") else { continue }
                let k = String(t[..<eq]).trimmingCharacters(in: .whitespaces)
                var v = String(t[t.index(after: eq)...]).trimmingCharacters(in: .whitespaces)
                if v.hasPrefix("\"") && v.hasSuffix("\"") && v.count >= 2 { v = String(v.dropFirst().dropLast()) }
                if !k.isEmpty && out[k] == nil { out[k] = v }
            }
        }
        return out
    }

    private func freePort(from: Int, to: Int) -> Int? {
        for p in from...to {
            let fd = socket(AF_INET, SOCK_STREAM, 0)
            if fd < 0 { continue }
            defer { close(fd) }
            var addr = sockaddr_in()
            addr.sin_family = sa_family_t(AF_INET)
            addr.sin_port = in_port_t(UInt16(p).bigEndian)
            addr.sin_addr.s_addr = inet_addr("127.0.0.1")
            let bound = withUnsafePointer(to: &addr) { ptr in
                ptr.withMemoryRebound(to: sockaddr.self, capacity: 1) { bind(fd, $0, socklen_t(MemoryLayout<sockaddr_in>.size)) }
            }
            if bound == 0 { return p }
        }
        return nil
    }

    private func load(_ base: String) {
        webView.load(URLRequest(url: URL(string: base + "/app/")!))
    }

    private func showError(_ msg: String) {
        let html = """
        <html><body style="font:15px -apple-system;background:#fff;color:#1A1A1D;display:flex;align-items:center;justify-content:center;height:100vh;margin:0">
        <div style="max-width:520px;text-align:center">
        <h2 style="font-size:20px;margin:0 0 12px">打不开智团</h2>
        <p style="color:#5A5A63;line-height:1.7">\(msg)</p>
        <button onclick="location.reload()" style="margin-top:20px;padding:10px 20px;border:0;border-radius:12px;background:#367BED;color:#fff;font-size:14px">重试</button>
        </div></body></html>
        """
        webView.loadHTMLString(html, baseURL: nil)
    }

    // MARK: 动作

    @objc private func reload() { webView.reload() }
    @objc private func devtools() { webView.configuration.preferences.setValue(true, forKey: "developerExtrasEnabled") }

    @objc private func goTo(_ item: NSMenuItem) {
        let routes = ["lobby", "room", "goals", "connectors", "library", "settings"]
        webView.evaluateJavaScript("location.hash = '#/\(routes[item.tag])'", completionHandler: nil)
    }

    @objc private func togglePanel() {
        webView.evaluateJavaScript("document.dispatchEvent(new KeyboardEvent('keydown',{key:'0',metaKey:true}))", completionHandler: nil)
    }

    @objc private func pickWorkspace() {
        let panel = NSOpenPanel()
        panel.canChooseFiles = false; panel.canChooseDirectories = true
        panel.allowsMultipleSelection = false; panel.canCreateDirectories = true
        panel.prompt = "使用这个目录"
        panel.message = "选择代码工作室的工作区（本机绝对路径，不会上传）"
        panel.begin { [weak self] resp in
            guard resp == .OK, let url = panel.url else { return }
            let escaped = url.path.replacingOccurrences(of: "\\", with: "\\\\")
                .replacingOccurrences(of: "'", with: "\\'")
            self?.webView.evaluateJavaScript("window.__ensemblePickDirectory && window.__ensemblePickDirectory('\(escaped)')", completionHandler: nil)
        }
    }

    // MARK: WK

    func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) { window.title = "智团" }

    func webView(_ webView: WKWebView, decidePolicyFor navigationAction: WKNavigationAction,
                 decisionHandler: @escaping (WKNavigationActionPolicy) -> Void) {
        if let url = navigationAction.request.url, navigationAction.navigationType == .linkActivated {
            if url.scheme == "http" || url.scheme == "https", !url.host!.hasPrefix("127.0.0.1") {
                NSWorkspace.shared.open(url); decisionHandler(.cancel); return
            }
        }
        decisionHandler(.allow)
    }

    func webView(_ webView: WKWebView, navigationAction: WKNavigationAction, didBecome download: WKDownload) {
        download.delegate = self
    }

    func download(_ download: WKDownload, decideDestinationUsing response: URLResponse,
                  suggestedFilename: String, completionHandler: @escaping (URL?) -> Void) {
        let dir = FileManager.default.urls(for: .downloadsDirectory, in: .userDomainMask).first!
        completionHandler(dir.appendingPathComponent(suggestedFilename))
    }

    func webView(_ webView: WKWebView, runJavaScriptAlertPanelWithMessage message: String,
                 initiatedByFrame frame: WKFrameInfo, completionHandler: @escaping () -> Void) {
        let alert = NSAlert(); alert.messageText = message; alert.runModal(); completionHandler()
    }

    func userContentController(_ ucc: WKUserContentController, didReceive message: WKScriptMessage) {
        guard message.name == "notify", let body = message.body as? [String: Any] else { return }
        let content = UNMutableNotificationContent()
        content.title = body["title"] as? String ?? "智团"
        content.body = body["body"] as? String ?? ""
        let req = UNNotificationRequest(identifier: UUID().uuidString, content: content, trigger: nil)
        UNUserNotificationCenter.current().add(req)
    }

    private func requestNotifyPermission() {
        UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound]) { _, _ in }
    }
}

NSSetUncaughtExceptionHandler { exc in
    let msg = "UNCAUGHT \(exc.name.rawValue): \(exc.reason ?? "?")\n\(exc.callStackSymbols.joined(separator: "\n"))\n"
    FileHandle.standardError.write(msg.data(using: .utf8) ?? Data())
}

let app = NSApplication.shared
app.setActivationPolicy(.regular)
let delegate = AppDelegate()
app.delegate = delegate
app.run()
```

- [ ] **Step 2: 编译**

```bash
swiftc -O -swift-version 5 -framework Cocoa -framework WebKit -framework UserNotifications \
  -o /tmp/TeamWise-test desktop/EnsembleDesktop.swift && echo SWIFT_OK
```

- [ ] **Step 3: 提交**

```bash
git add desktop/EnsembleDesktop.swift
git commit -m "feat(desktop): 壳重做——包内二进制定位、.env 加载、端口探测、下载与通知"
```

---

## Task 22: 打包脚本与图标

**Files:**
- Modify: `desktop/build.sh`
- Create: `desktop/make-icon.sh`
- Test: `./desktop/build.sh && open desktop/build/智团.app`

**Interfaces:**
- Produces: `desktop/build/智团.app`，含 `Contents/MacOS/{智团,ensemble}` 与 `Contents/Resources/web/`、`AppIcon.icns`。

- [ ] **Step 1: 写 make-icon.sh**

```bash
#!/bin/bash
# 由 logo.svg 生成 AppIcon.icns（需要 rsvg-convert 或 qlmanage）。
set -euo pipefail
cd "$(dirname "$0")/.."
SRC=web/site/brand/logo.svg
TMP=$(mktemp -d)
for s in 16 32 64 128 256 512 1024; do
  if command -v rsvg-convert >/dev/null 2>&1; then
    rsvg-convert -w $s -h $s "$SRC" -o "$TMP/icon_$s.png"
  else
    qlmanage -t -s $s -o "$TMP" "$SRC" >/dev/null 2>&1
    mv "$TMP/logo.svg.png" "$TMP/icon_$s.png"
  fi
done
mkdir -p "$TMP/AppIcon.iconset"
for s in 16 32 128 256 512; do
  cp "$TMP/icon_$s.png" "$TMP/AppIcon.iconset/icon_${s}x${s}.png"
  cp "$TMP/icon_$((s*2)).png" "$TMP/AppIcon.iconset/icon_${s}x${s}@2x.png"
done
iconutil -c icns "$TMP/AppIcon.iconset" -o desktop/AppIcon.icns
rm -rf "$TMP"
echo "已生成 desktop/AppIcon.icns"
```

- [ ] **Step 2: 写 build.sh**

```bash
#!/bin/bash
# 构建智团桌面端：Go 后端 + WKWebView 壳 + web 资源 + 图标。
set -euo pipefail
cd "$(dirname "$0")/.."
ROOT="$(pwd)"
APP="desktop/build/智团.app"

echo "==> 编译后端"
GOOS=darwin GOARCH=$(uname -m | sed 's/x86_64/amd64/;s/arm64/arm64/') \
  go build -ldflags="-s -w" -o bin/ensemble ./cmd/server

echo "==> 图标"
[ -f desktop/AppIcon.icns ] || ./desktop/make-icon.sh

echo "==> 桌面壳"
rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources/web"
swiftc -O -swift-version 5 -framework Cocoa -framework WebKit -framework UserNotifications \
  -o "$APP/Contents/MacOS/智团" desktop/EnsembleDesktop.swift
cp bin/ensemble "$APP/Contents/MacOS/ensemble"
cp -R web/app web/admin web/site "$APP/Contents/Resources/web/"
cp desktop/AppIcon.icns "$APP/Contents/Resources/AppIcon.icns"

# 后端按可执行文件旁找 web/；Resources/web 由 webDir 的第二条路径命中。
cat > "$APP/Contents/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key><string>智团</string>
  <key>CFBundleDisplayName</key><string>智团</string>
  <key>CFBundleIdentifier</key><string>com.ultimatejob.zhituan.desktop</string>
  <key>CFBundleExecutable</key><string>智团</string>
  <key>CFBundleIconFile</key><string>AppIcon</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>1.2.0</string>
  <key>CFBundleVersion</key><string>1</string>
  <key>LSMinimumSystemVersion</key><string>13.0</string>
  <key>NSHighResolutionCapable</key><true/>
  <key>NSAppTransportSecurity</key><dict><key>NSAllowsLocalNetworking</key><true/></dict>
</dict>
</plist>
PLIST

codesign --force --deep --sign - "$APP" >/dev/null 2>&1 || echo "（签名跳过）"
echo "==> 完成：$ROOT/$APP"
```

- [ ] **Step 3: 构建并从干净目录启动**

```bash
chmod +x desktop/build.sh desktop/make-icon.sh
./desktop/build.sh
cp -R desktop/build/智团.app /tmp/zt-clean/ && (cd /tmp && open -n /tmp/zt-clean/智团.app)
sleep 5
lsof -iTCP:8080 -sTCP:LISTEN -n -P | head -3
curl -s -o /dev/null -w 'app=%{http_code}\n' http://127.0.0.1:8080/app/
```

Expected: 端口监听存在，`app=200`。

- [ ] **Step 4: 提交**

```bash
git add desktop/build.sh desktop/make-icon.sh desktop/AppIcon.icns
git commit -m "build(desktop): 打包 web 资源、图标与 .app"
```

---

## Task 23: E2E 重写与文档

**Files:**
- Modify: `scripts/e2e.mjs`, `scripts/e2e-mobile.mjs`, `scripts/audit-ui.py`
- Modify: `README.md`, `docs/team-guide.md`, `docs/acceptance-work-layer.md`, `CHANGELOG.md`
- Test: `node scripts/e2e.mjs`

**Interfaces:**
- Consumes: 新路由与 DOM 契约（`.nav-item`、`.team-card`、`.msg-stream`、`#panel`、`.modal-card`、`.goal-card`、`.conn-row`、`.lib-card`）。
- Produces: 之后所有改动的回归基线。

- [ ] **Step 1: 重写 e2e 登录与主流程断言**

替换旧断言（`.col-list`、`#scenes`、`.scene`）为新选择器；新增：

```js
// 登录
ok('登录页出现', await cdp.eval('!!document.querySelector("#login:not(.hidden)")'));
// 路由与壳
await cdp.waitFor('location.hash.startsWith("#/lobby")', 10000, '进入大厅');
ok('左侧导航六项', (await cdp.eval('document.querySelectorAll("#navList .nav-item").length')) === 6);
ok('四个预设团队', (await cdp.eval('document.querySelectorAll(".team-card").length')) === 4);
// 加入团队
await cdp.eval('document.querySelector(".team-card [data-join]").click()');
await cdp.waitFor('location.hash.includes("/room/")', 15000, '进入房间');
ok('房间头像条', (await cdp.eval('document.querySelectorAll("#avatarRow .av-cell").length')) > 0);
// 发消息
await cdp.eval(`(() => { const t=document.querySelector('#ta'); t.value='你好'; t.dispatchEvent(new Event('input',{bubbles:true})); document.querySelector('#sendBtn').click(); })()`);
await cdp.waitFor('document.querySelectorAll(".msg").length >= 2', 30000, '收到流式回复');
// 目标 / 连接器 / 资料库 / 设置 均可导航
for (const v of ['goals','connectors','library','settings']) {
  location.hash = '#/' + v;
  await cdp.waitFor(`location.hash === "#/${v}"`, 5000, v);
  ok(`${v} 页无报错`, await cdp.eval('!document.querySelector(".page")?.textContent.includes("undefined")'));
}
```

并在启动时挂控制台监听：

```js
await cdp.send('Runtime.enable');
cdp.ws.addEventListener('message', ev => {
  const m = JSON.parse(ev.data);
  if (m.method === 'Runtime.exceptionThrown') console.log('  ⚠ JS 异常:', m.params.exceptionDetails.text);
});
```

- [ ] **Step 2: 更新 mobile / audit 选择器**

`e2e-mobile.mjs`：`.col-list` → `.nav`（抽屉触发）、`#newConvBtn` → `#newTeam`。
`audit-ui.py`：按新 index.html 的按钮 id 重新登记。

- [ ] **Step 3: 跑 E2E**

```bash
(ADDR=:8080 DATA_DIR=/tmp/e2e-m1 ./bin/ensemble > /tmp/e2e.log 2>&1 &) ; sleep 1
EDGE_PATH="$(which chrome)" node scripts/e2e.mjs
```

Expected: 全绿，无 JS 异常。

- [ ] **Step 4: 文档更新**

- `README.md`：桌面产物改为 `desktop/build/智团.app`；「原生桌面端」一节补数据/配置位置；截图替换
- `docs/team-guide.md`：同上
- `docs/acceptance-work-layer.md`：并行上限 6（与 `agent.go` 一致）；界面入口改为新路由
- `CHANGELOG.md`：新增 `## 1.2.0` 段落，列 M1–M4

- [ ] **Step 5: 最终全量验证**

```bash
gofmt -l .
go vet ./...
go test ./... -count=1
node scripts/ui-shots.mjs
```

- [ ] **Step 6: 提交**

```bash
git add scripts/e2e.mjs scripts/e2e-mobile.mjs scripts/audit-ui.py README.md docs CHANGELOG.md
git commit -m "test/docs: 新路由 E2E 与文档对齐"
```

---

## M4 验收清单

- [ ] 干净目录双击 `.app`：自启后端、页面 200、登录可用
- [ ] ⌘O 选目录 → 工作室路径填入
- [ ] ⌘1–⌘6 切页、⌘0 右面板
- [ ] 产物下载落到 `~/Downloads`
- [ ] 审批出现时窗口在后台会收到通知
- [ ] 退出应用后子进程不残留
- [ ] README/团队指南/验收文档与新产物名一致
- [ ] `go test` 全绿、E2E 全绿、`gofmt -l` 无输出
