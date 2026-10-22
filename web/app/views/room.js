/* ============================================================
   聊天房间：顶栏头像条 + 消息流 + 输入条
   消息渲染与 SSE 复用 chat.js，这里只负责房间骨架与交互接线。
   ============================================================ */

import { $, api, state, esc, icon, hydrateIcons, skeleton } from '../js/core.js';
import { openConv, send, kickoff, stopRound, setBusyUI } from '../js/chat.js';
import { openPanel } from '../js/shell.js';
import { avatarHTML } from '../js/avatars.js';

let root = null;
let actTimer = null;
let convId = '';

export async function mount(host, params, query) {
  root = host;
  convId = params.id;
  const conv = (state.convs || []).find(c => c.id === convId);
  if (!conv) {
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
          <button class="icon-btn" id="nudgeBtn" title="让他们继续聊">${icon('sparkles', 17)}</button>
          <button class="icon-btn" id="roomMemory" title="他们记得">${icon('brain', 17)}</button>
          <button class="icon-btn" id="roomSettings" title="团队设置">${icon('settings', 17)}</button>
          <button class="icon-btn" id="roomPanel" title="工作面板">${icon('panel-right', 17)}</button>
        </div>
      </header>

      <div class="msg-stream" id="msgStream">
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
        <div class="composer-hint"><span>回车发送，Shift + 回车换行</span></div>
      </div>
    </div>`;

  hydrateIcons(root);
  wireComposer();

  // chat.js 会读 state.agents / 渲染到 #msgWrap
  await openConv(conv.id);
  renderAvatarRow();
  renderEmptyState();
  actTimer = setInterval(renderAvatarRow, 4000);

  if (query?.get('view') === 'studio') {
    const { mountStudio } = await import('./studio.js');
    mountStudio(host, conv.id);
  }
}

export function unmount() {
  if (actTimer) clearInterval(actTimer);
  actTimer = null;
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
      if (ta.value.trim()) send();
    }
  });
  btn?.addEventListener('click', () => {
    if (state.busy) { stopRound(); sync(); return; }
    send();
  });

  // 发送/停止后按钮状态由 chat.js 的 setBusyUI 通知，这里再补一次
  document.addEventListener('round:state', sync);
  sync();

  $('#nudgeBtn')?.addEventListener('click', () => kickoff(state.cur.id));
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

/* ---------------- 头像条 ---------------- */

async function renderAvatarRow() {
  const box = $('#avatarRow');
  if (!box || !state.cur) return;
  const agents = state.agents || [];
  let acts = [];
  try { acts = await api('/v1/conversations/' + state.cur.id + '/activity'); } catch (e) { acts = []; }
  const byKey = new Map((acts || []).map(a => [a.key, a]));
  box.innerHTML = agents.map(a => {
    const act = byKey.get(a.key);
    const st = act?.state || 'idle';
    const title = st === 'working' ? `${a.name} · 正在执行` :
                  st === 'blocked' ? `${a.name} · 等你审批` :
                  st === 'done' ? `${a.name} · 刚完成` : a.name;
    return `<button class="av-cell" data-key="${esc(a.key)}" title="${esc(title)}">
      ${avatarHTML(a.name, a.color, { size: 34, ring: st })}
      <span class="av-name">${esc(a.name)}</span>
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
    const list = await api('/v1/library?convId=' + encodeURIComponent(convId) + '&limit=8');
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
      el.innerHTML = `
        <div class="panel-sec"><h4>角色</h4><div id="memberList"></div></div>
        <div class="panel-sec"><h4>这一轮</h4><div id="pipeline"></div></div>
        <div class="panel-sec"><h4>产物</h4><div id="artifactList"></div></div>`;
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
