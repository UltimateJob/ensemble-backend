/* ============================================================
   合奏 Ensemble · 对话层
   消息渲染 / SSE 流 / 分页 / 会话列表
   ============================================================ */

import {
  $, $$, API, api, state, toast, esc, timeStr, agoStr, makeAv, icon,
} from './core.js';
import { uiPrompt, uiConfirm } from './dialog.js';
import { loadConvs, renderConvs, setConvOpener } from './convs.js';

/* ---------------- 会话 ---------------- */
// 同一个会话正在打开时忽略重复调用（并发会导致消息渲染两遍）
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

  // 协作者的真实姓名和在线状态（renderMembers 要用）
  try {
    state.people = await api('/v1/conversations/' + conv.id + '/collaborators');
  } catch (e) {
    state.people = [];
  }

  renderConvs();
  if (drawer.classList.contains('on')) closeDrawer();
  $('#chatEmpty').style.display = 'none';
  $('#chatHead').style.display = 'flex';
  $('#msgs').style.display = 'block';
  $('#composer').style.display = 'block';
  $('#chatTitle').textContent = conv.title;
  $('#crumbName').textContent = conv.title;
  setChatStatus('');
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
// 上一条消息的时间，用来决定要不要插时间分隔
let lastMsgTime = 0;

function timeLabel(d) {
  const x = new Date(d);
  const now = new Date();
  const sameDay = x.toDateString() === now.toDateString();
  const hm = x.getHours().toString().padStart(2, '0') + ':' + x.getMinutes().toString().padStart(2, '0');
  if (sameDay) return '今天 ' + hm;
  const y = new Date(now);
  y.setDate(y.getDate() - 1);
  if (x.toDateString() === y.toDateString()) return '昨天 ' + hm;
  return (x.getMonth() + 1) + '月' + x.getDate() + '日 ' + hm;
}

/**
 * 时间分隔：间隔超过 10 分钟就插一条。
 * 长对话滚动时，没有这个会分不清「刚才」和「两小时前」。
 */
export function maybeTimeDivider(wrap, when) {
  const t = new Date(when || Date.now()).getTime();
  if (!isNaN(t) && (lastMsgTime === 0 || t - lastMsgTime > 10 * 60 * 1000)) {
    const d = document.createElement('div');
    d.className = 'time-divider';
    d.textContent = timeLabel(t);
    wrap.appendChild(d);
  }
  if (!isNaN(t)) lastMsgTime = t;
}

/**
 * 轮次边界：一轮对话跑完插一条极淡的线。
 * 不加的话，三轮对话连在一起，看不出「这段话是一组的」。
 */
export function markRoundEnd(wrap) {
  const last = wrap.lastElementChild;
  if (!last || last.classList.contains('round-end')) return;
  const d = document.createElement('div');
  d.className = 'round-end';
  wrap.appendChild(d);
}

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
  const wrap = $('#msgWrap');
  maybeTimeDivider(wrap, o.time);
  const { el } = buildMsgEl(o);
  wrap.appendChild(el);
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

/**
 * 顶栏那行小字。
 * 原来写死「在线」+ 绿点——角色是程序，永远「在线」，这话没有信息量。
 * 现在：空闲时说明这个群有几个角色，编排中说谁正在说话。
 */
export function setChatStatus(agentName) {
  const el = $('#chatSub');
  if (!el) return;
  const n = state.agents.length;
  if (agentName) {
    el.innerHTML = `<span class="dot-live busy"></span><span>${esc(agentName)} 正在说…</span>`;
  } else {
    const sc = state.scenarios.find(s => s.key === (state.cur && state.cur.studio));
    const label = sc ? sc.name : '自定义群';
    el.innerHTML = `<span class="dot-live"></span><span>${esc(label)} · ${n} 个角色</span>`;
  }
}

export function renderMembers() {
  const box = $('#memberList');
  const conv = state.cur;
  const iAmOwner = !!(conv && state.user && conv.userId === state.user.id);

  const agentRows = state.agents.map(a => `
    <div class="member">
      ${makeAv(a.name, a.color, 'av-sm')}
      <div>
        <div class="mn">${esc(a.name)}</div>
        <div class="mt">${esc(a.title)}</div>
      </div>
    </div>`).join('');

  // 协作者：用 /collaborators 返回的真实姓名和在线状态。
  // 之前显示 uid 末六位（谁也不知道那是谁），绿点也是写死的。
  const people = state.people || [];
  const others = people.filter(p => p.role === 'member');
  const collabRows = others.map(p => `
    <div class="member">
      ${makeAv(p.name || '人', '#4F46E5', 'av-sm')}
      <div>
        <div class="mn">${esc(p.name || p.id.slice(-6))}</div>
        <div class="mt">${p.online ? '在线' : '离线'}</div>
      </div>
      ${p.online ? '<span class="st"></span>' : '<span class="st off"></span>'}
      ${iAmOwner ? `<button class="collab-x" data-rm="${p.id}" title="移除协作者">×</button>` : ''}
    </div>`).join('');

  box.innerHTML = agentRows + (collabRows
    ? `<div class="member-sep">协作者 · ${others.length}</div>${collabRows}`
    : '');

  $('#memberCount').textContent = state.agents.length + others.length;
  $('#topAvatars').innerHTML = state.agents.slice(0, 5)
    .map(a => makeAv(a.name, a.color, 'av-sm')).join('');

  box.querySelectorAll('[data-rm]').forEach(b => {
    b.addEventListener('click', async () => {
      const uid = b.dataset.rm;
      const yes = await uiConfirm('移除这个协作者？', '对方将不能再看到这个群里的对话。', {
        okText: '移除', danger: true,
      });
      if (!yes) return;
      try {
        await api(`/v1/conversations/${conv.id}/collaborators/${uid}`, { method: 'DELETE' });
        conv.collaborators = conv.collaborators.filter(x => x !== uid);
        renderMembers();
        toast('已移除');
      } catch (e) { toast(e.message); }
    });
  });
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

/* ---------------- 发送（核心） ---------------- */
/**
 * 跑一轮编排，把 SSE 事件画到界面上。
 *
 * send 和 kickoff 之前各有一份几乎逐行相同的解析代码，
 * 改一处漏一处（顶栏状态就是这么漏的）。现在只有这一份。
 *
 * 返回本轮的收尾信息，调用方自己决定怎么复位输入框。
 */
export async function runRound(path, body, opts = {}) {
  const container = document.createElement('div');
  $('#msgWrap').appendChild(container);
  renderPipeline(true);

  let curEl = null;
  let curText = '';

  function handleEvent(ev) {
    if (ev.type === 'start') {
      setChatStatus(ev.name);
      curText = '';
      container.querySelectorAll('.msg.speaking').forEach(m => m.classList.remove('speaking'));
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

    if (ev.type === 'end') {
      if (curEl) {
        curEl.classList.remove('speaking');
        curEl.querySelector('.msg-text').classList.remove('caret');
        curEl.querySelector('.msg-time').textContent = timeStr(new Date());
      }
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
      scrollBottom();
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

    if (ev.type === 'quota') {
      addSysNote('本月额度用完了 — 这一轮没人能接话，去「账户设置」看看能不能加额度');
      return;
    }

    if (ev.type === 'error') {
      addSysNote(ev.name + ' 出错了：' + ev.content);
      return;
    }
  }

  try {
    const res = await fetch(API + path, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Authorization': 'Bearer ' + state.token,
      },
      body: JSON.stringify(body || {}),
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
    if (/额度/.test(e.message)) {
      addSysNote('本月额度用完了 — 这句话先留着，加完额度再点「继续聊」');
    } else {
      addSysNote('出错了：' + e.message);
    }
  } finally {
    state.busy = false;
    setChatStatus('');
    if (curEl) curEl.querySelector('.msg-text').classList.remove('caret');
    markRoundEnd($('#msgWrap'));
    scrollBottom();
    loadConvs();
    if (opts.onDone) opts.onDone();
  }
}

export async function send() {
  // 上一轮还没结束就静默 return 的话，用户点了发送却毫无反应，会以为卡死了
  if (state.busy) { toast('还在说，等这一轮结束'); return; }
  if (!state.cur) { toast('先选一个群'); return; }
  const ta = $('#ta');
  const text = ta.value.trim();
  if (!text) return;

  ta.value = '';
  ta.style.height = 'auto';
  $('#sendBtn').disabled = true;
  state.busy = true;

  addMsg({ name: (state.user && state.user.name) || '你', text, me: true, time: new Date() });
  scrollBottom();

  await runRound('/v1/conversations/' + state.cur.id + '/orchestrate', { content: text }, {
    onDone: () => {
      const box = $('#ta');
      $('#sendBtn').disabled = !box || !box.value.trim();
    },
  });
}

/* ---------------- 自主开场 ---------------- */
export async function kickoff(convId) {
  if (state.busy) return;
  state.busy = true;
  $('#sendBtn').disabled = true;

  await runRound('/v1/conversations/' + convId + '/kickoff', {}, {
    onDone: () => {
      const box = $('#ta');
      $('#sendBtn').disabled = !box || !box.value.trim();
      const nb = $('#nudgeBtn');
      if (nb) nb.disabled = false;
    },
  });
}
