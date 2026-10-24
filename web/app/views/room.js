/* ============================================================
   聊天房间：顶栏头像条 + 消息流 + 输入条
   消息渲染与 SSE 复用 chat.js，这里只负责房间骨架与交互接线。
   ============================================================ */

import { $, api, state, esc, icon, hydrateIcons, skeleton, toast } from '../js/core.js';
import { openConv, send, kickoff, stopRound, setBusyUI } from '../js/chat.js';
import { openPanel } from '../js/shell.js';
import { avatarHTML } from '../js/avatars.js';
import { loadActivity, stateLabel, ringOf, permissionLabel, isExecutable } from '../js/activity.js';
import { workspaceBannerHTML, openWorkspaceDialog, workspacePath } from '../js/workspace.js';
import { resumeRuns, sendExecute, clearRunBubbles } from '../js/exec.js';
import { openTeamTask, teamBadge, openCurrentTeam, loadLatestTeam } from '../js/team.js';

let root = null;
let actTimer = null;
let convId = '';
let activity = { agents: [], runs: [], workspace: { bound: false }, defaultPermission: '' };

export async function mount(host, params, query) {
  root = host;
  convId = params.id;
  const conv = (state.convs || []).find(c => c.id === convId);
  if (!conv) {
    const convs = state.convs || [];
    // ⌘2 或点「聊天房间」时没有具体 id：进最近那个房间，
    // 而不是弹一句「房间不存在」——用户只是想去聊天。
    if (!convId && convs.length) {
      const recent = [...convs].sort((a, b) =>
        new Date(b.updatedAt || b.createdAt || 0) - new Date(a.updatedAt || a.createdAt || 0))[0];
      location.replace('#/room/' + encodeURIComponent(recent.id));
      return;
    }
    if (!convId) {
      host.innerHTML = `<div class="page page--center">
        <div class="empty-state">
          <div class="es-ic">${icon('message-circle', 22)}</div>
          <div class="es-t">还没有房间</div>
          <div class="es-d">先去团队大厅选一组角色，房间会自动建好。</div>
          <a class="btn btn-primary" href="#/lobby">去团队大厅</a>
        </div></div>`;
      return;
    }
    host.innerHTML = `<div class="page page--center">
      <div class="empty-state">
        <div class="es-ic">${icon('message-circle', 22)}</div>
        <div class="es-t">房间不存在或已删除</div>
        <div class="es-d">它可能被解散了，或者你没有访问权限。</div>
        <a class="btn btn-primary" href="#/lobby">回团队大厅</a>
      </div></div>`;
    return;
  }
  state.cur = conv;

  const isFun = conv.studio === 'companion' || conv.studio === 'anime';
  const placeholder = isFun ? '随便聊聊，或者说说今天怎么样' : '说点什么，或者直接派活…';

  host.innerHTML = `
    <div class="room">
      <header class="room-head">
        <div class="rh-left">
          <h2 class="rh-title" id="chatTitle">${esc(conv.title)}</h2>
          <div class="rh-sub" id="chatSub"><span class="dot-live"></span><span>${esc(scName(conv))}</span></div>
        </div>
        <div class="avatar-row" id="avatarRow"></div>
        <div class="rh-right">
          <span id="teamSlot"></span>
          <button class="icon-btn" id="teamBtn" title="交给团队：规划、并行、验证、评分">${icon('users', 17)}</button>
          <button class="icon-btn" id="wsBtn" title="工作区与权限">${icon('folder', 17)}</button>
          <button class="icon-btn" id="nudgeBtn" title="让他们继续聊">${icon('sparkles', 17)}</button>
          <button class="icon-btn" id="roomMemory" title="他们记得">${icon('brain', 17)}</button>
          <button class="icon-btn" id="roomSettings" title="团队设置">${icon('settings', 17)}</button>
          <button class="icon-btn" id="roomPanel" title="工作面板">${icon('panel-right', 17)}</button>
        </div>
      </header>

      <div id="wsBannerBox"></div>

      <div class="msg-stream" id="msgStream" role="log" aria-live="polite" aria-relevant="additions" aria-label="群聊消息">
        <div class="msg-wrap" id="msgWrap"></div>
      </div>

      <div class="composer" id="composer">
        <div class="composer-in">
          <textarea id="ta" rows="1" placeholder="${esc(placeholder)}"></textarea>
          <div class="composer-bar">
            <button class="icon-btn" id="attachBtn" title="附件">${icon('paperclip', 16)}</button>
            <button class="icon-btn" id="mentionBtn" title="@ 角色">${icon('at-sign', 16)}</button>
            <button class="send" id="sendBtn" title="发送">${icon('arrow-up', 16)}</button>
          </div>
        </div>
        <div class="composer-hint"><span>回车发送，Shift + 回车换行 · 以 @ 开头的消息会直接派活</span></div>
      </div>
    </div>`;

  hydrateIcons(root);
  wireComposer();

  // chat.js 会读 state.agents / 渲染到 #msgWrap
  await openConv(conv.id);
  renderEmptyState();
  await refreshWork();
  actTimer = setInterval(refreshWork, 5000);

  if (query?.get('view') === 'studio') {
    const { mountStudio } = await import('./studio.js');
    mountStudio(host, conv.id, query);
  }

  // 从搜索结果或其他地方带着 ?msg=<id> 进来：滚到那条并高亮。
  // 要等消息渲染完（openConv 是异步的，上面已经 await），再加一帧
  // 确保布局稳定，否则 scrollIntoView 会算错位置。
  const focusMsg = query?.get('msg');
  if (focusMsg) {
    requestAnimationFrame(() => {
      const el = document.querySelector(`[data-mid="${CSS.escape(focusMsg)}"]`);
      if (!el) return;
      el.scrollIntoView({ block: 'center', behavior: 'smooth' });
      el.classList.add('msg-focus');
      setTimeout(() => el.classList.remove('msg-focus'), 2600);
    });
  }
}

export function unmount() {
  if (actTimer) clearInterval(actTimer);
  actTimer = null;
  // 离开房间时断掉实时投影；任务本身在服务端继续跑，回来会重新接上。
  clearRunBubbles();
  root = null;
  convId = '';
  stopRound(true);
}

/**
 * 空房间不放一片空白：给几句能直接点的话头。
 * 有消息时这个块自动消失（消息渲染会清空 #msgWrap）。
 */
function renderEmptyState() {
  const wrap = $('#msgWrap');
  if (!wrap) return;
  if (wrap.querySelector('.msg')) return;   // 已有历史消息
  // 加载失败时 chat.js 会放一张带「重试」的卡片。这张卡必须留着——
  // 无条件写空态会把它覆盖掉，用户看到的是「房间是空的」，
  // 以为历史消息丢了，而不是「网断了」。
  if (wrap.querySelector('.err-card')) return;
  const agents = (state.agents || []).slice(0, 4);
  const seeds = seedFor(state.cur?.studio);
  wrap.innerHTML = `
    <div class="room-empty">
      <div class="re-avs">${agents.map(a => avatarHTML(a.name, a.color, { size: 52 })).join('')}</div>
      <h3>${esc(eq(agents))}</h3>
      <p>${esc(seedLead(state.cur?.studio))}</p>
      <div class="re-acts">${seeds.map(s => `<button class="re-chip" data-seed="${esc(s)}">${esc(s)}</button>`).join('')}</div>
    </div>`;
  wrap.querySelectorAll('[data-seed]').forEach(b => b.addEventListener('click', () => {
    const ta = $('#ta');
    if (!ta) return;
    ta.value = b.dataset.seed;
    ta.dispatchEvent(new Event('input', { bubbles: true }));
    ta.focus();
    // 点一下等于替用户写好，再点发送即可——不擅自发送
  }));
}

function eq(agents) {
  if (!agents.length) return '这个房间还没有角色';
  const names = agents.map(a => a.name);
  return names.length <= 2 ? names.join(' 和 ') + ' 在等你开口' : '他们都在等你开口';
}

function seedLead(studio) {
  if (studio === 'onepiece') return '上船说句话，他们会自己接下去。';
  if (studio === 'anime' || studio === 'companion') return '随便聊点什么，或者说说今天怎么样。';
  return '派一件真事给他们：说清目标、约束和交付物。';
}

function seedFor(studio) {
  if (studio === 'onepiece') return ['我们下一站去哪', '今天航线上有什么', '谁来讲个故事'];
  if (studio === 'anime' || studio === 'companion') return ['今天有点累', '最近在看什么', '聊聊周末'];
  if (studio === 'xhs') return ['做一篇平价咖啡机种草', '这周的热点能蹭吗', '标题再想三个'];
  if (studio === 'code') return ['修一下登录模块的 bug', '设计一个接口', '跑一遍测试'];
  return ['读一下这份招标文件', '列出废标风险', '先给个拆解方案'];
}

function scName(conv) {
  const sc = (state.scenarios || []).find(s => s.key === conv.studio);
  return sc ? (sc.tagline || sc.name) : '自定义团队';
}

/* ---------------- 输入条 ---------------- */

function wireComposer() {
  const ta = $('#ta');
  const btn = $('#sendBtn');

  const sync = () => {
    if (!ta) return;
    ta.style.height = 'auto';
    ta.style.height = Math.min(160, ta.scrollHeight) + 'px';
    const busy = state.busy;
    btn.classList.toggle('stop', busy);
    btn.title = busy ? '停止这一轮' : '发送';
    btn.innerHTML = icon(busy ? 'square' : 'arrow-up', 16);
    btn.disabled = busy ? false : !ta.value.trim();
  };

  ta?.addEventListener('input', sync);
  ta?.addEventListener('keydown', e => {
    if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) {
      e.preventDefault();
      if (state.busy) return;
      if (ta.value.trim()) composeSend();
    }
  });
  btn?.addEventListener('click', () => {
    if (state.busy) { stopRound(); sync(); return; }
    composeSend();
  });

  // @ 是派活的唯一入口，不需要再点一个模式按钮。
  // 手动切换按钮既占地方，又和「@ 就是指定谁去做」的直觉重复：
  // 用户想派活会自然地 @ 角色，不想派活就是普通群聊。

  // 发送/停止后按钮状态由 chat.js 的 setBusyUI 通知，这里再补一次
  document.addEventListener('round:state', sync);
  sync();

  $('#nudgeBtn')?.addEventListener('click', () => kickoff(state.cur.id));
  $('#teamBtn')?.addEventListener('click', () => openTeamTask(state.cur, activity, {
    onBound: () => refreshWork(),
  }));
  $('#wsBtn')?.addEventListener('click', () => openWorkspaceDialog(state.cur, {
    onDone: () => refreshWork(),
  }));
  // 右面板默认显示房间概览；再点一次收起。
  $('#roomPanel')?.addEventListener('click', async e => {
    const { panelOpen, closePanel } = await import('../js/shell.js');
    if (panelOpen()) { closePanel(); return; }
    openRoomPanel();
  });
  $('#roomMemory')?.addEventListener('click', async () => {
    const m = await import('../js/memory.js');
    m.openMemory?.();
  });
  $('#roomSettings')?.addEventListener('click', async () => {
    const m = await import('./teamsettings.js');
    m.openTeamSettings(state.convs.find(c => c.id === state.cur.id) || state.cur);
  });

  $('#mentionBtn')?.addEventListener('click', e => openMention(ta, e.currentTarget));
  $('#attachBtn')?.addEventListener('click', () => {
    const input = document.createElement('input');
    input.type = 'file';
    input.accept = '.txt,.md,.csv,.json';
    input.addEventListener('change', async () => {
      const f = input.files?.[0];
      if (!f) return;
      if (f.size > 100 * 1024) return;
      const text = await f.text();
      ta.value = (ta.value ? ta.value + '\n\n' : '') + `【${f.name}】\n${text}`;
      ta.dispatchEvent(new Event('input', { bubbles: true }));
    });
    input.click();
  });
}

function openMention(ta, anchor) {
  const agents = state.agents || [];
  if (!agents.length) return;
  document.querySelector('.mention-pop')?.remove();
  const pop = document.createElement('div');
  pop.className = 'mention-pop';
  pop.innerHTML = agents.map((a, i) => `
    <button class="mention-item" data-i="${i}">${avatarHTML(a.name, a.color, { size: 22 })}<span>${esc(a.name)}</span></button>`).join('');
  const rect = anchor.getBoundingClientRect();
  pop.style.position = 'fixed';
  pop.style.left = rect.left + 'px';
  pop.style.top = (rect.top - 8 - Math.min(240, agents.length * 36)) + 'px';
  document.body.appendChild(pop);
  pop.querySelectorAll('.mention-item').forEach(b => b.addEventListener('click', () => {
    const a = agents[+b.dataset.i];
    ta.value = (ta.value ? ta.value + ' ' : '') + '@' + a.name + ' ';
    ta.focus();
    ta.dispatchEvent(new Event('input', { bubbles: true }));
    pop.remove();
  }));
  const close = ev => { if (!pop.contains(ev.target)) { pop.remove(); document.removeEventListener('mousedown', close); } };
  setTimeout(() => document.addEventListener('mousedown', close), 0);
}

/* ---------------- 发送分流 ---------------- */

function hasMention(text) {
  const agents = state.agents || [];
  return agents.some(a => text.includes('@' + a.name) || text.includes('＠' + a.name) ||
    text.includes('@' + a.key) || text.includes('＠' + a.key));
}

async function composeSend() {
  const ta = $('#ta');
  if (!ta) return;
  const text = ta.value.trim();
  if (!text) return;
  const bound = !!workspacePath(state.cur);
  // 派活只由 @ 触发，没有别的开关。@ 谁，就是让谁去做。
  const work = bound || state.cur.studio === 'code' || state.cur.studio === 'custom';
  const shouldExec = work && hasMention(text);
  if (shouldExec && !bound) {
    openWorkspaceDialog(state.cur, { onDone: () => refreshWork() });
    toast('先选一个本地工作区，再派活', { type: 'info' });
    return;
  }
  if (!shouldExec) { send(); return; }
  // 多人群里「派活」必须点名：否则不知道让谁改。单聊可直接发。
  if (state.agents.length > 1 && !hasMention(text)) {
    toast('派活时 @ 一个角色；想让团队一起做，点右上角「交给团队」', { type: 'info', ms: 3600 });
    openMention(ta, $('#mentionBtn'));
    return;
  }

  ta.value = '';
  ta.style.height = 'auto';
  ta.dispatchEvent(new Event('input', { bubbles: true }));
  await sendExecute(state.cur, text);
  setTimeout(refreshWork, 900);
}

/* ---------------- 房间工作状态 ---------------- */

async function refreshWork() {
  if (!state.cur || !root) return;
  activity = await loadActivity(state.cur.id);
  state.activity = activity;
  // 单聊没有「团队」可交；隐藏入口而不是让用户点了才被拒。
  const teamBtn = $('#teamBtn');
  if (teamBtn) teamBtn.style.display = (state.agents || []).length >= 2 ? '' : 'none';
  await renderAvatarRow();
  renderWorkspaceBanner();
  await renderTeamSlot();
  resumeRuns(state.cur, activity);
}

function renderWorkspaceBanner() {
  const box = $('#wsBannerBox');
  if (!box) return;
  const conv = state.cur;
  const rootPath = workspacePath(conv);
  if (rootPath) {
    // 已绑定：只留一条极薄的路径条，不占视线。
    box.innerHTML = `<div class="ws-bar">
      <span class="ws-b-icon">${icon('folder', 14)}</span>
      <span class="ws-b-path" title="${esc(rootPath)}">${esc(rootPath)}</span>
      <span class="ws-b-perm">${esc(permissionLabel(conv.defaultPermission || activity.defaultPermission))}</span>
      <button class="ws-b-link" id="wsChange">更换</button>
    </div>`;
    box.querySelector('#wsChange').addEventListener('click', () => openWorkspaceDialog(conv, {
      onDone: () => refreshWork(),
    }));
    return;
  }
  if (conv.studio !== 'code' && conv.studio !== 'custom') { box.innerHTML = ''; return; }
  box.innerHTML = workspaceBannerHTML(conv);
  box.querySelector('#wsBindBtn')?.addEventListener('click', () => openWorkspaceDialog(conv, {
    onDone: () => refreshWork(),
  }));
}

async function renderTeamSlot() {
  const slot = $('#teamSlot');
  if (!slot || !state.cur) return;
  try {
    const html = await teamBadge(state.cur);
    if (!slot.isConnected) return;
    slot.innerHTML = html || '';
    slot.querySelector('#teamBadge')?.addEventListener('click', () => openCurrentTeam(state.cur));
  } catch (e) { /* 团队不阻塞房间 */ }
}

/* ---------------- 头像条 ---------------- */

async function renderAvatarRow() {
  const box = $('#avatarRow');
  if (!box || !state.cur) return;
  const agents = state.agents || [];
  const byKey = new Map((activity?.agents || []).map(a => [a.key, a]));
  box.innerHTML = agents.map(a => {
    const act = byKey.get(a.key);
    const st = act?.state || 'idle';
    const perm = act?.permission || state.cur.defaultPermission || a.permission || '';
    const title = `${a.name}${perm ? ' · ' + permissionLabel(perm) : ''}${act ? ' · ' + stateLabel(act) : ''}`;
    return `<button class="av-cell" data-key="${esc(a.key)}" title="${esc(title)}">
      ${avatarHTML(a.name, a.color, { size: 34, ring: ringOf(st) })}
      <span class="av-name">${esc(a.name)}</span>
      ${st === 'running' ? '<i class="av-live"></i>' : ''}
    </button>`;
  }).join('');
  box.querySelectorAll('.av-cell').forEach(b => b.addEventListener('click', () => {
    const a = agents.find(x => x.key === b.dataset.key);
    openAgentPanel(a);
  }));
}

async function openAgentPanel(agent) {
  if (!agent) return;
  const m = await import('../js/agentpanel.js');
  m.openAgentPanel(agent, state.cur);
}

/* ---------------- 右面板：房间概览 ---------------- */

async function fillArtifacts(convId) {
  const box = document.querySelector('#artifactList');
  if (!box) return;
  try {
    const list = await api('/v1/library?conversationId=' + encodeURIComponent(convId) + '&limit=8');
    if (!list?.length) {
      box.innerHTML = '<div class="ap-idle">这个群还没有产出文件。角色完成交付后会出现在这里。</div>';
      return;
    }
    box.innerHTML = list.map(a => `
      <button class="ap-art" data-aid="${esc(a.id)}">
        <span class="ap-art-ic">${icon('file-text', 15)}</span>
        <span class="ap-art-body">
          <span class="ap-art-t">${esc(a.title || '未命名')}</span>
          <span class="ap-art-s">${esc((a.agentName || '') + ' · ' + (a.summary || '').slice(0, 28))}</span>
        </span>
      </button>`).join('');
    box.querySelectorAll('[data-aid]').forEach(b => b.addEventListener('click', async () => {
      try {
        const a = await api('/v1/library/' + b.dataset.aid);
        const { openPanel } = await import('../js/shell.js');
        openPanel({
          title: a.title || '产物',
          subtitle: a.agentName || '',
          render: c => { c.innerHTML = `<pre class="art-pre">${esc(a.content || '')}</pre>`; },
        });
      } catch (e) { /* 打不开就算了 */ }
    }));
  } catch (e) {
    box.innerHTML = '';
  }
}

/**
 * 点顶栏右侧面板按钮时打开房间概览（成员 / 本轮 / 产物）。
 * chat.js 的 renderMembers / renderPipeline / addArtifact 直接写这些容器。
 */
export function openRoomPanel() {
  if (!state.cur) return;
  openPanel({
    title: state.cur.title,
    subtitle: '房间概览',
    render: el => {
      const ws = workspacePath(state.cur);
      el.innerHTML = `
        <div class="panel-sec"><h4>工作区</h4>
          <div id="panelWs">${ws
            ? `<div class="pw-line"><code title="${esc(ws)}">${esc(ws)}</code>
                 <span class="pw-perm">${esc(permissionLabel(state.cur.defaultPermission || activity.defaultPermission))}</span></div>
               <button class="btn btn-line btn-sm" id="panelWsBtn" style="width:100%;margin-top:8px">工作区与权限</button>`
            : `<div class="ap-idle">还没有绑定本地工作区。绑定后角色才能读代码、改文件、跑测试。</div>
               <button class="btn btn-primary btn-sm" id="panelWsBtn" style="width:100%;margin-top:10px">选择目录</button>`}
          </div>
        </div>
        ${ws ? `<div class="panel-sec"><h4>文件</h4><div id="fileTree" class="file-tree">
          <div class="ft-loading">读取中…</div></div></div>` : ''}
        <div class="panel-sec"><h4>团队任务</h4>
          <button class="btn btn-line btn-sm" id="panelTeamBtn" style="width:100%">交给团队做</button>
        </div>
        <div class="panel-sec"><h4>角色</h4><div id="memberList"></div></div>
        <div class="panel-sec"><h4>这一轮</h4><div id="pipeline"></div></div>
        <div class="panel-sec"><h4>产物</h4><div id="artifactList"></div></div>`;
      el.querySelector('#panelWsBtn')?.addEventListener('click', () => openWorkspaceDialog(state.cur, {
        onDone: () => { refreshWork(); openRoomPanel(); },
      }));
      el.querySelector('#panelTeamBtn')?.addEventListener('click', () => openTeamTask(state.cur, activity, {
        onBound: () => refreshWork(),
      }));
      if (ws) renderFileTree(el.querySelector('#fileTree'), state.cur.workspace?.id);
      import('../js/chat.js').then(m => {
        m.renderMembers?.();
        m.renderPipeline?.(true);
      });
      // 产物从资料库回填：SSE 事件只在产生那一刻推一次，
      // 重开面板或刷新后必须重新拉，否则这块永远是空的。
      fillArtifacts(state.cur.id);
    },
  });
}

/* ---------------- 工作区文件树 ---------------- */

// 之前工作区里有什么，用户只能靠聊天里的产物 chip 猜。
// 后端 /tree 一直可用，只是没有界面。
const ftOpen = new Set();

async function renderFileTree(box, wsId) {
  if (!box || !wsId) return;
  let data;
  try {
    data = await api(`/v1/workspaces/${encodeURIComponent(wsId)}/tree`);
  } catch (e) {
    box.innerHTML = `<div class="ft-err">${esc(e.message || '读不到目录')}</div>`;
    return;
  }
  box.innerHTML = '';
  const entries = (data?.entries || []).slice().sort((a, b) => {
    // 目录在前，同类按名字
    if (a.isDir !== b.isDir) return a.isDir ? -1 : 1;
    return (a.name || '').localeCompare(b.name || '');
  });
  if (!entries.length) {
    box.innerHTML = '<div class="ft-empty">这个目录是空的</div>';
    return;
  }
  renderLevel(box, entries, data.path || '', wsId, 0);
}

function renderLevel(box, entries, basePath, wsId, depth) {
  for (const e of entries) {
    const rel = basePath ? basePath.replace(/\/$/, '') + '/' + e.name : e.name;
    const row = document.createElement('button');
    row.className = 'ft-row' + (e.isDir ? ' dir' : '');
    row.style.paddingLeft = (10 + depth * 14) + 'px';
    row.innerHTML = `<span class="ft-ic">${icon(e.isDir ? 'folder' : 'file-text', 14)}</span>` +
      `<span class="ft-name">${esc(e.name)}</span>` +
      (e.isDir ? `<span class="ft-chev">${icon('chevron-right', 13)}</span>` : '');
    row.title = rel;
    box.appendChild(row);

    const children = document.createElement('div');
    children.className = 'ft-children';
    box.appendChild(children);

    row.addEventListener('click', async () => {
      if (!e.isDir) {
        const { openArtifact } = await import('../js/exec.js');
        openArtifact(state.cur, { path: rel, name: e.name });
        return;
      }
      const open = children.classList.toggle('on');
      row.classList.toggle('open', open);
      if (open && !children.dataset.loaded) {
        children.dataset.loaded = '1';
        children.innerHTML = '<div class="ft-loading">读取中…</div>';
        try {
          const d = await api(`/v1/workspaces/${encodeURIComponent(wsId)}/tree?path=` + encodeURIComponent(rel));
          children.innerHTML = '';
          const kids = (d?.entries || []).slice().sort((a, b) => {
            if (a.isDir !== b.isDir) return a.isDir ? -1 : 1;
            return (a.name || '').localeCompare(b.name || '');
          });
          if (!kids.length) { children.innerHTML = '<div class="ft-empty">空目录</div>'; return; }
          renderLevel(children, kids, rel, wsId, depth + 1);
        } catch (err) {
          children.innerHTML = `<div class="ft-err">${esc(err.message || '读取失败')}</div>`;
        }
      }
    });
  }
}
