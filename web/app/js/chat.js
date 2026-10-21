/* ============================================================
   合奏 Ensemble · 对话层
   消息渲染 / SSE 流 / 分页 / 会话列表
   ============================================================ */

import {
  $, $$, API, api, state, toast, esc, timeStr, agoStr, makeAv, icon,
} from './core.js';

/* ---------------- 会话 ---------------- */
export async function loadConvs() {
  try {
    state.convs = await api('/v1/conversations');
  } catch (e) {
    state.convs = [];
  }
  renderConvs();
}

export function renderConvs() {
  renderInto($('#convList'));
  renderInto($('#drawerList'));
}

function renderInto(box) {
  if (!box) return;
  if (!state.convs.length) {
    box.innerHTML = `<div style="padding:24px 12px;text-align:center;font-size:12px;color:var(--tx-4)">
      还没有群聊<br><span style="color:var(--tx-4)">点「新建」开一个</span>
    </div>`;
    return;
  }
  box.innerHTML = state.convs.map(c => {
    const sc = state.scenarios.find(s => s.key === c.studio) || {};
    const color = sc.accent || (c.agents && c.agents[0] && c.agents[0].color) || '#4F46E5';
    return `<div class="conv ${state.cur && state.cur.id === c.id ? 'on' : ''}" data-id="${c.id}">
      ${makeAv(c.title, color)}
      <div class="conv-body">
        <div class="conv-name">${esc(c.title)}</div>
        <div class="conv-meta">${esc(sc.name || (c.agents && c.agents.length ? '自定义群' : c.studio) || '自定义')} · ${(c.agents || []).length || (c.members || []).length || (sc.agents || []).length} 个角色</div>
      </div>
      <div class="conv-time">${agoStr(c.updatedAt || c.createdAt).replace('前', '')}</div>
      <button class="conv-more" data-more="${c.id}" title="更多">
        ${icon('ellipsis', 14)}
      </button>
    </div>`;
  }).join('');

  $$('.conv').forEach(el => {
    el.addEventListener('click', e => {
      const more = e.target.closest('[data-more]');
      if (more) {
        e.stopPropagation();
        openCtxMenu(more.dataset.more, more.getBoundingClientRect());
        return;
      }
      openConv(el.dataset.id);
    });
  });
}

let openingConvId = null;

export async function openConv(id) {
  if (state.busy) return toast('上一轮还没结束');
  // 同一个会话正在打开时忽略重复调用（并发会导致消息渲染两遍）
  if (openingConvId === id) return;
  const conv = state.convs.find(c => c.id === id);
  if (!conv) return;

  openingConvId = id;
  state.cur = conv;

  const sc = state.scenarios.find(s => s.key === conv.studio) || { agents: [] };
  // 自定义角色优先
  state.agents = (conv.agents && conv.agents.length)
    ? conv.agents
    : (sc.agents || []);

  renderConvs();
  if (drawer.classList.contains('on')) closeDrawer();
  $('#chatEmpty').style.display = 'none';
  $('#chatHead').style.display = 'flex';
  $('#msgs').style.display = 'block';
  $('#composer').style.display = 'block';
  $('#chatTitle').textContent = conv.title;
  $('#crumbName').textContent = conv.title;
  renderMembers();
  renderPipeline(true);

  try {
    const page = await api('/v1/conversations/' + id + '/messages?page=1&limit=30');
    // 拿到数据后再清空：放在 await 之前的话，并发调用会互相覆盖
    $('#msgWrap').innerHTML = '';
    const msgs = page.messages || [];
    state.oldestID = msgs.length ? msgs[0].id : null;
    state.hasMore = !!page.hasMore;
    artifacts.length = 0;
    msgs.forEach(m => {
      const agent = state.agents.find(a => a.name === m.agent);
      const el = addMsg({
        name: m.role === 'user' ? (state.user && state.user.name) || '你' : (m.agent || 'Agent'),
        title: agent ? agent.title : '',
        color: agent ? agent.color : null,
        text: m.content,
        me: m.role === 'user',
        time: m.createdAt,
      });
      // 历史里若在 @用户，恢复「在问你」标记
      if (m.role === 'assistant' && /@用户|@你/.test(m.content || '')) {
        el.classList.add('asking');
      }
      // 有产物定义的角色，历史里也补一张文件卡
      if (agent && agent.artifact && m.role === 'assistant') {
        addArtifact(agent.name, agent.artifact, m.content);
        appendArtifactCard(agent.artifact, m.content.slice(0, 28));
      }
    });
    if (!msgs.length) {
      addSysNote('群已建好，说第一句话试试');
    } else if (msgs.length > 1) {
      pipeDone = state.agents.length;
      renderPipeline(false);
    }
    renderLoadMore();
  } catch (e) {
    toast(e.message);
  } finally {
    if (openingConvId === id) openingConvId = null;
  }
  scrollBottom();
}

/* 顶部「加载更早」 */
function renderLoadMore() {
  let bar = document.getElementById('loadMore');
  if (!state.hasMore) {
    if (bar) bar.remove();
    return;
  }
  if (!bar) {
    bar = document.createElement('div');
    bar.id = 'loadMore';
    bar.className = 'load-more';
    bar.innerHTML = '<span>加载更早的消息</span>';
    bar.addEventListener('click', loadEarlier);
    const wrap = $('#msgWrap');
    wrap.insertBefore(bar, wrap.firstChild);
  }
}

export async function loadEarlier() {
  if (state.loadingMore || !state.hasMore || !state.oldestID || !state.cur) return;
  state.loadingMore = true;
  const bar = document.getElementById('loadMore');
  if (bar) bar.textContent = '加载中…';

  const box = $('#msgs');
  const prevHeight = box.scrollHeight;
  const prevTop = box.scrollTop;

  try {
    const page = await api(
      '/v1/conversations/' + state.cur.id + '/messages?page=1&limit=30&before=' + encodeURIComponent(state.oldestID)
    );
    const older = page.messages || [];
    state.hasMore = !!page.hasMore;

    // 把更早的消息插到最前面（保持原顺序）
    const wrap = $('#msgWrap');
    const frag = document.createDocumentFragment();
    older.forEach(m => {
      const agent = state.agents.find(a => a.name === m.agent);
      const el = buildMsgEl({
        name: m.role === 'user' ? (state.user && state.user.name) || '你' : (m.agent || 'Agent'),
        title: agent ? agent.title : '',
        color: agent ? agent.color : null,
        text: m.content,
        me: m.role === 'user',
        time: m.createdAt,
      });
      frag.appendChild(el.el);
    });
    wrap.insertBefore(frag, wrap.firstChild);
    if (older.length) state.oldestID = older[0].id;

    // 恢复视觉位置，避免跳
    box.scrollTop = box.scrollHeight - prevHeight + prevTop;
  } catch (e) {
    toast(e.message);
  } finally {
    state.loadingMore = false;
    if (bar) bar.remove();
    renderLoadMore();
  }
}

/* ---------------- 渲染 ---------------- */
export function buildMsgEl(o) {
  const el = document.createElement('div');
  el.className = 'msg' + (o.me ? ' me' : '');
  el.innerHTML = `
    ${o.me ? '' : makeAv(o.name, o.color)}
    <div class="msg-body">
      <div class="msg-head">
        <span class="msg-name" ${o.color ? `style="color:${o.color}"` : ''}>${esc(o.name)}</span>
        ${o.title ? `<span class="msg-title">${esc(o.title)}</span>` : ''}
        <span class="msg-time">${o.time ? timeStr(o.time) : ''}</span>
      </div>
      <div class="msg-text"></div>
    </div>`;
  el.querySelector('.msg-text').textContent = o.text || '';
  return { el, textEl: el.querySelector('.msg-text') };
}

export function addMsg(o) {
  const { el } = buildMsgEl(o);
  $('#msgWrap').appendChild(el);
  return el;
}

export function addSysNote(text) {
  const el = document.createElement('div');
  el.className = 'sys-note';
  el.textContent = text;
  $('#msgWrap').appendChild(el);
  return el;
}

export function scrollBottom() {
  const box = $('#msgs');
  if (box) box.scrollTop = box.scrollHeight;
}

export function renderMembers() {
  const box = $('#memberList');
  box.innerHTML = state.agents.map(a => `
    <div class="member">
      ${makeAv(a.name, a.color, 'av-sm')}
      <div>
        <div class="mn">${esc(a.name)}</div>
        <div class="mt">${esc(a.title)}</div>
      </div>
      <span class="st"></span>
    </div>`).join('');
  $('#memberCount').textContent = state.agents.length;
  $('#topAvatars').innerHTML = state.agents.slice(0, 5)
    .map(a => makeAv(a.name, a.color, 'av-sm')).join('');
}

let pipeDone = 0;
export function renderPipeline(reset) {
  if (reset) pipeDone = 0;
  const box = $('#pipeline');
  if (!box) return;
  box.innerHTML = state.agents.map((a, i) => {
    const cls = i < pipeDone ? 'done' : (i === pipeDone ? 'live' : '');
    return `<div class="pipe-step ${cls}">
      <span class="n">${i + 1}</span>
      <span>${esc(a.name)}</span>
      <span class="r">${i < pipeDone ? '✓' : ''}</span>
    </div>`;
  }).join('');
  $('#pipeCount').textContent = pipeDone + '/' + state.agents.length;
}

const artifacts = [];

function buildArtifactCard(title, detail) {
  const card = document.createElement('div');
  card.className = 'artifact';
  card.innerHTML = `
    <span class="fi">${icon('file-text', 16)}</span>
    <div>
      <div class="an">${esc(title)}</div>
      <div class="ad">${esc(detail || '')}</div>
    </div>
    <span class="go">${icon('chevron-right', 16)}</span>`;
  card.addEventListener('click', () => toast('打开 ' + title));
  return card;
}

export function appendArtifactCard(title, detail) {
  $('#msgWrap').appendChild(buildArtifactCard(title, detail));
}

export function addArtifact(name, title, detail) {
  artifacts.push({ name, title, detail });
  $('#artifactCount').textContent = artifacts.length;
  const box = $('#artifactList');
  box.innerHTML = artifacts.map(a => `
    <div style="display:flex;align-items:center;gap:8px;padding:5px 0;color:var(--tx-2)">
      <span style="color:var(--pri);display:inline-flex">${icon('file-text', 13)}</span>
      <span style="font-size:12px">${esc(a.title)}</span>
    </div>`).join('');
}

/* ---------------- 会话右键菜单 ---------------- */
let ctxConvId = null;

function openCtxMenu(id, rect) {
  ctxConvId = id;
  const menu = $('#ctxMenu');
  const mask = $('#ctxMask');
  menu.classList.add('on');
  mask.classList.add('on');
  // 贴着按钮右侧展开，超出视口就翻到左边
  const w = 150;
  let left = rect.right + 4;
  if (left + w > window.innerWidth - 8) left = rect.left - w - 4;
  const top = Math.min(rect.top, window.innerHeight - 120);
  menu.style.left = Math.max(8, left) + 'px';
  menu.style.top = Math.max(8, top) + 'px';
}

function closeCtxMenu() {
  $('#ctxMenu').classList.remove('on');
  $('#ctxMask').classList.remove('on');
  ctxConvId = null;
}

$('#ctxMask').addEventListener('click', closeCtxMenu);
document.addEventListener('keydown', e => { if (e.key === 'Escape') closeCtxMenu(); });

$$('#ctxMenu button').forEach(btn => {
  btn.addEventListener('click', async () => {
    const act = btn.dataset.act;
    const id = ctxConvId;
    closeCtxMenu();
    if (!id) return;
    const conv = state.convs.find(c => c.id === id);
    if (!conv) return;

    if (act === 'rename') {
      const name = prompt('新的群名', conv.title);
      if (!name || name === conv.title) return;
      try {
        await api('/v1/conversations/' + id, { method: 'PATCH', body: { title: name } });
        await loadConvs();
        if (state.cur && state.cur.id === id) {
          state.cur.title = name;
          $('#chatTitle').textContent = name;
          $('#crumbName').textContent = name;
        }
        toast('已重命名');
      } catch (e) { toast(e.message); }
      return;
    }

    if (act === 'invite') {
      const target = prompt('对方的手机号或邮箱（需要先注册过）', '');
      if (!target) return;
      try {
        const r = await api('/v1/conversations/' + id + '/collaborators', {
          method: 'POST', body: { target: target.trim() },
        });
        toast('已邀请 ' + (r.invitee ? r.invitee.name : target));
        await loadConvs();
      } catch (e) { toast(e.message); }
      return;
    }

    if (act === 'delete') {
      UI_confirm_delete(conv);
    }
  });
});

function UI_confirm_delete(conv) {
  // 复用一个简易确认弹层
  if (!confirm(`删除「${conv.title}」？群里的消息会一并清掉，不可恢复。`)) return;
  api('/v1/conversations/' + conv.id, { method: 'DELETE' })
    .then(async () => {
      if (state.cur && state.cur.id === conv.id) {
        state.cur = null;
        $('#msgWrap').innerHTML = '';
        $('#msgs').style.display = 'none';
        $('#composer').style.display = 'none';
        $('#chatHead').style.display = 'none';
        $('#chatEmpty').style.display = 'flex';
        $('#crumbName').textContent = '—';
        state.agents = [];
        renderMembers();
      }
      await loadConvs();
      toast('已删除');
    })
    .catch(e => toast(e.message));
}


/* ---------------- 发送（核心） ---------------- */
export async function send() {
  if (state.busy || !state.cur) return;
  const ta = $('#ta');
  const text = ta.value.trim();
  if (!text) return;

  ta.value = '';
  ta.style.height = 'auto';
  $('#sendBtn').disabled = true;
  state.busy = true;

  addMsg({ name: (state.user && state.user.name) || '你', text, me: true, time: new Date() });
  scrollBottom();

  renderPipeline(true);
  const container = document.createElement('div');
  $('#msgWrap').appendChild(container);

  let curAgent = null;
  let curEl = null;
  let curText = '';

  try {
    const res = await fetch(API + '/v1/conversations/' + state.cur.id + '/orchestrate', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Authorization': 'Bearer ' + state.token,
      },
      body: JSON.stringify({ content: text }),
    });

    if (!res.ok) {
      const j = await res.json().catch(() => ({}));
      throw new Error((j.error && j.error.message) || 'HTTP ' + res.status);
    }

    const reader = res.body.getReader();
    const decoder = new TextDecoder();
    let buf = '';

    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      buf += decoder.decode(value, { stream: true });

      const parts = buf.split('\n\n');
      buf = parts.pop();

      for (const part of parts) {
        const line = part.trim();
        if (!line.startsWith('data:')) continue;
        let ev;
        try { ev = JSON.parse(line.slice(5).trim()); } catch (e) { continue; }
        handleEvent(ev);
      }
    }
  } catch (e) {
    addSysNote('出错了：' + e.message);
  } finally {
    state.busy = false;
    if (curEl) curEl.querySelector('.msg-text').classList.remove('caret');
    scrollBottom();
    loadConvs();
  }

  function handleEvent(ev) {
    if (ev.type === 'start') {
      curAgent = ev;
      curText = '';
      container.querySelectorAll('.msg.speaking').forEach(m => m.classList.remove('speaking'));
      // 思考占位
      curEl = document.createElement('div');
      curEl.className = 'msg';
      curEl.innerHTML = `
        ${makeAv(ev.name, ev.color)}
        <div class="msg-body">
          <div class="msg-head">
            <span class="msg-name" style="color:${ev.color}">${esc(ev.name)}</span>
            <span class="msg-time"></span>
          </div>
          <div class="thinking"><i></i><i></i><i></i></div>
          <div class="msg-text" style="display:none"></div>
        </div>`;
      container.appendChild(curEl);
      curEl.classList.add('speaking');
      scrollBottom();
      return;
    }

    if (ev.type === 'delta' && curEl) {
      curEl.querySelector('.thinking').style.display = 'none';
      const tx = curEl.querySelector('.msg-text');
      tx.style.display = '';
      curText += ev.content;
      tx.textContent = curText;
      tx.classList.add('caret');
      scrollBottom();
      return;
    }

    if (ev.type === 'end' && curEl) {
      curEl.classList.remove('speaking');
      const tx = curEl.querySelector('.msg-text');
      tx.classList.remove('caret');
      curEl.querySelector('.msg-time').textContent = timeStr(new Date());
      pipeDone++;
      renderPipeline(false);
      return;
    }

    if (ev.type === 'artifact') {
      addArtifact(ev.name, ev.title, ev.detail);
      container.appendChild(buildArtifactCard(ev.title, ev.detail));
      scrollBottom();
      return;
    }

    if (ev.type === 'pass') {
      // 有人选择不说话：把之前建的思考占位清掉，只留一条极轻的痕迹
      if (curEl && !curEl.querySelector('.msg-text').textContent.trim()) {
        curEl.remove();
        curEl = null;
      }
      const n = document.createElement('div');
      n.className = 'pass-note';
      n.textContent = ev.name + ' 没接话';
      container.appendChild(n);
      return;
    }

    if (ev.type === 'ask') {
      if (curEl) curEl.classList.add('asking');
      return;
    }

    if (ev.type === 'busy') {
      addSysNote('群里正在说话，这条先放着 — 稍后点右下角「继续聊」让他们接上');
      return;
    }

    if (ev.type === 'error') {
      addSysNote(ev.name + ' 出错了：' + ev.content);
      return;
    }
  }
}

/* ---------------- 自主开场 ---------------- */
export async function kickoff(convId) {
  if (state.busy) return;
  state.busy = true;
  $('#sendBtn').disabled = true;
  const container = document.createElement('div');
  $('#msgWrap').appendChild(container);
  renderPipeline(true);

  let curEl = null, curText = '';

  try {
    const res = await fetch(API + '/v1/conversations/' + convId + '/kickoff', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'Authorization': 'Bearer ' + state.token },
      body: JSON.stringify({}),
    });
    if (!res.ok) return;
    const reader = res.body.getReader();
    const dec = new TextDecoder();
    let buf = '';
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      buf += dec.decode(value, { stream: true });
      const parts = buf.split('\n\n');
      buf = parts.pop();
      for (const part of parts) {
        const line = part.trim();
        if (!line.startsWith('data:')) continue;
        let ev; try { ev = JSON.parse(line.slice(5).trim()); } catch (e) { continue; }
        if (ev.type === 'start') {
          curText = '';
          curEl = document.createElement('div');
          curEl.className = 'msg';
          curEl.innerHTML = `
            ${makeAv(ev.name, ev.color)}
            <div class="msg-body">
              <div class="msg-head">
                <span class="msg-name" style="color:${ev.color}">${esc(ev.name)}</span>
                <span class="msg-time"></span>
              </div>
              <div class="thinking"><i></i><i></i><i></i></div>
              <div class="msg-text" style="display:none"></div>
            </div>`;
          container.appendChild(curEl);
          curEl.classList.add('speaking');
          scrollBottom();
        } else if (ev.type === 'delta' && curEl) {
          curEl.querySelector('.thinking').style.display = 'none';
          const tx = curEl.querySelector('.msg-text');
          tx.style.display = '';
          curText += ev.content;
          tx.textContent = curText;
          tx.classList.add('caret');
          scrollBottom();
        } else if (ev.type === 'end' && curEl) {
          curEl.classList.remove('speaking');
          curEl.querySelector('.msg-text').classList.remove('caret');
          curEl.querySelector('.msg-time').textContent = timeStr(new Date());
          pipeDone++;
          renderPipeline(false);
        } else if (ev.type === 'pass') {
          const n = document.createElement('div');
          n.className = 'pass-note';
          n.textContent = ev.name + ' 没接话';
          container.appendChild(n);
          scrollBottom();
        } else if (ev.type === 'busy') {
          const n = document.createElement('div');
          n.className = 'pass-note';
          n.textContent = '群里正在说话，等一下再试';
          container.appendChild(n);
          scrollBottom();
        }
      }
    }
  } catch (e) {
    console.warn('开场失败：' + (e && e.message));
  } finally {
    state.busy = false;
    const box = $('#ta');
    $('#sendBtn').disabled = !box || !box.value.trim();
    const nb = $('#nudgeBtn');
    if (nb) nb.disabled = false;
    scrollBottom();
    loadConvs();
  }
}

