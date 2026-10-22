/* ============================================================
   合奏 Ensemble · 对话层
   消息渲染 / SSE 流 / 分页 / 会话列表
   ============================================================ */

import {
  $, API, api, state, toast, esc, timeStr, fullTimeStr, makeAv, icon, setDocTitle, humanError } from './core.js';
import { uiConfirm, uiEditText } from './dialog.js';
import { loadConvs, renderConvs, setConvOpener } from './convs.js';

/* ---------------- 复制 ---------------- */
// 演示环境可能跑在 http 非 localhost 下，navigator.clipboard 那时不可用，
// 回退到老式的选中+execCommand，两条路都走不通再报错。
export async function copyText(text) {
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(text);
      return true;
    }
  } catch (e) { /* 走回退 */ }
  try {
    const ta = document.createElement('textarea');
    ta.value = text;
    ta.style.cssText = 'position:fixed;opacity:0;pointer-events:none';
    document.body.appendChild(ta);
    ta.select();
    const ok = document.execCommand('copy');
    ta.remove();
    return ok;
  } catch (e) {
    return false;
  }
}

/* ---------------- 轻量 Markdown ---------------- */
// 消息正文大多是模型输出，会带 **粗体**、`代码`、列表这类记号。
// 之前 textContent 直出，满屏星号。这里做一个很小的渲染器：
// 先整体 HTML 转义（防注入），再套一层有限的白名单标记。
// 支持：**粗体**、`行内代码`、```围栏代码块```、无序/有序列表、换行。

function mdInline(s) {
  return s
    .replace(/`([^`\n]+)`/g, '<code>$1</code>')
    .replace(/\*\*([^*\n]+)\*\*/g, '<b>$1</b>');
}

export function mdRender(text) {
  const src = String(text == null ? '' : text);
  const blocks = [];
  // 1. 先抽走围栏代码块，里面的内容保持原样，不被后面的规则碰到
  let s = src.replace(/```([^\n`]*)\n?([\s\S]*?)```/g, (m, lang, code) => {
    blocks.push(`<pre><code>${esc(code.replace(/\n$/, ''))}</code></pre>`);
    return '\u0000B' + (blocks.length - 1) + '\u0000';
  });

  // 2. 逐行处理列表和段落
  const out = [];
  let list = null;   // 'ul' | 'ol'，空行/非列表行时收掉
  const closeList = () => { if (list) { out.push('</' + list + '>'); list = null; } };
  for (const line of s.split('\n')) {
    const t = line.trim();
    let m;
    if ((m = /^[-*•]\s+(.+)$/.exec(t))) {
      if (list !== 'ul') { closeList(); out.push('<ul>'); list = 'ul'; }
      out.push(`<li>${mdInline(esc(m[1]))}</li>`);
    } else if ((m = /^\d+[.、)]\s+(.+)$/.exec(t))) {
      if (list !== 'ol') { closeList(); out.push('<ol>'); list = 'ol'; }
      out.push(`<li>${mdInline(esc(m[1]))}</li>`);
    } else if (/^\u0000B\d+\u0000$/.test(t)) {
      closeList();
      out.push(t);                       // 代码块占位符，独占一行
    } else if (t === '') {
      closeList();
    } else {
      closeList();
      out.push(`<p>${mdInline(esc(t))}</p>`);
    }
  }
  closeList();

  // 3. 把代码块放回去
  return out.join('').replace(/\u0000B(\d+)\u0000/g, (m, i) => blocks[+i]);
}

/* ---------------- 滚动策略 ---------------- */
// 之前每个 delta 都无条件拉到底——用户往上翻历史时会被反复拽下来，
// 看不成任何旧内容。现在只有「本来就在底部附近」才自动跟随；
// 离开底部时改用悬浮按钮提示，流式期间带未读条数。

const NEAR_BOTTOM_PX = 80;
let unreadNew = 0;   // 不在底部期间新到的消息条数

export function scrollBottom() {
  const box = $('#msgs');
  if (box) box.scrollTop = box.scrollHeight;
}

function nearBottom() {
  const box = $('#msgs');
  if (!box) return true;
  return box.scrollHeight - box.scrollTop - box.clientHeight < NEAR_BOTTOM_PX;
}

// 流式输出时用：在底部就跟着走，不在就不动
function autoScroll() {
  if (nearBottom()) scrollBottom();
}

function updateJump() {
  const btn = $('#jumpBtn');
  if (!btn) return;
  btn.classList.toggle('on', !nearBottom());
  const badge = btn.querySelector('.jb-badge');
  if (badge) {
    badge.style.display = unreadNew > 0 ? '' : 'none';
    badge.textContent = unreadNew > 99 ? '99+' : String(unreadNew);
  }
}

// 新消息进面板时统一走这里：记录未读、刷新悬浮按钮
function trackNew() {
  if (!nearBottom()) unreadNew++;
  updateJump();
}

{
  const box = $('#msgs');
  if (box) box.addEventListener('scroll', updateJump, { passive: true });
  const jb = $('#jumpBtn');
  if (jb) jb.addEventListener('click', () => {
    unreadNew = 0;
    scrollBottom();
    updateJump();
  });
}

/* ---------------- 会话 ---------------- */
// 同一个会话正在打开时忽略重复调用（并发会导致消息渲染两遍）
let openingConvId = null;
let openingAt = 0;

export async function openConv(id) {
  // 正在一轮里也允许切换：旧的那轮静默停掉（已生成的消息服务端都落了库），
  // 切回来重新拉历史就行。之前直接拦住，一轮一分钟里整个左栏是死的。
  const conv = state.convs.find(c => c.id === id);
  if (!conv) return;
  if (state.busy && state.cur && state.cur.id !== id) stopRound(true);
  // 同一个会话正在打开时忽略重复调用（并发会导致消息渲染两遍）。
  // 但要给一个时间窗：同步段里万一抛异常，openingConvId 不会被清掉，
  // 之后这个会话就再也打不开了——界面毫无反应，很难查。
  if (openingConvId === id && Date.now() - openingAt < 5000) return;

  openingConvId = id;
  openingAt = Date.now();
  state.cur = conv;
  unreadNew = 0;
  updateJump();
  lastMsgTime = 0;   // 时间分割线的基准跟着换群，否则跨群会「继承」上一个群的时间

  // 立刻清屏并铺骨架。下面取协作者、取消息都是网络请求，
  // 慢的时候要多等一两秒——这期间屏幕上还是【上一个群】的消息，
  // 用户会以为点错了。清空要放在所有 await 之前。
  showMsgSkeleton();

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
  // 移动端抽屉收起。这里不能调 app.js 的 closeDrawer（模块作用域互不可见，
  // 之前这么写抽屉开着时点会话会直接 ReferenceError），直接动 class
  {
    const dw = $('#drawer'), dm = $('#drawerMask');
    if (dw) dw.classList.remove('on');
    if (dm) dm.classList.remove('on');
  }
  $('#chatEmpty').style.display = 'none';
  document.body.classList.remove('no-conv');
  $('#chatHead').style.display = 'flex';
  $('#msgs').style.display = 'block';
  $('#composer').style.display = 'block';
  $('#chatTitle').textContent = conv.title;
  $('#crumbName').textContent = conv.title;
  setChatStatus('');
  setDocTitle(conv.title, false);

  // 工作场景说「派活」，闲聊场景说「随便聊聊」——同一个 placeholder 套所有场景会很怪
  const ta = $('#ta');
  if (ta) {
    const fun = conv.studio === 'companion' || conv.studio === 'anime';
    ta.placeholder = fun ? '随便聊聊，或者说说今天怎么样' : '说点什么，或者直接派活…';
  }
  renderMembers();
  renderPipeline(true);

  try {
    const page = await api('/v1/conversations/' + id + '/messages?page=1&limit=30');
    // 打开期间用户可能又切去了别的群：晚到的响应直接丢掉，
    // 否则会把新群的消息区覆盖成旧群内容
    if (!state.cur || state.cur.id !== id) return;
    // 拿到数据后再清空：放在 await 之前的话，并发调用会互相覆盖
    $('#msgWrap').innerHTML = '';
    lastMsgTime = 0;
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
        id: m.id,
      });
      // 历史里若在 @用户，恢复「在问你」标记
      if (m.role === 'assistant' && /@用户|@你/.test(m.content || '')) {
        el.classList.add('asking');
      }
      // 有产物定义的角色，历史里也补一张文件卡
      if (agent && agent.artifact && m.role === 'assistant') {
        addArtifact(agent.name, agent.artifact, m.content);
        appendArtifactCard(agent.name, agent.artifact, m.content);
      }
    });
    scheduleDedupeTimes();
    if (!msgs.length) {
      addSysNote('群已建好，说第一句话试试');
    } else if (msgs.length > 1) {
      pipeDone = state.agents.length;
      renderPipeline(false);
    }
    renderLoadMore();
    scrollBottom();
    updateJump();
  } catch (e) {
    if (!state.cur || state.cur.id !== id) return;
    hideMsgSkeleton();
    // 聊天区空着会让人以为群坏了：把错误写在面板里，并给一个重试
    const err = humanError(e);
    const box = $('#msgWrap');
    box.innerHTML = `
      <div class="err-card">
        <b>消息没加载出来</b>
        <p>${esc(err)}</p>
        <button class="btn btn-line" id="msgRetry">重试</button>
      </div>`;
    box.querySelector('#msgRetry').addEventListener('click', () => openConv(id));
  } finally {
    if (openingConvId === id) openingConvId = null;
  }
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
        id: m.id,
      });
      frag.appendChild(el.el);
    });
    wrap.insertBefore(frag, wrap.firstChild);
    if (older.length) state.oldestID = older[0].id;

    // 恢复视觉位置，避免跳
    box.scrollTop = box.scrollHeight - prevHeight + prevTop;
  } catch (e) {
    toast(humanError(e));
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
  return fullTimeStr(d);   // 今天 14:30 / 昨天 09:12 / 10月18日 20:05
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
  if (o.id) el.dataset.mid = o.id;   // 搜索结果跳转时靠它定位
  if (o.time) el.dataset.t = String(new Date(o.time).getTime() || 0);
  // 原文挂在元素上：复制按钮和「点击重发」都用它，渲染后的 HTML 不能反推原文
  el._raw = o.text || '';
  el.innerHTML = `
    ${o.me ? '' : makeAv(o.name, o.color)}
    <div class="msg-body">
      <div class="msg-head">
        <span class="msg-name" ${o.color ? `style="color:${esc(o.color)}"` : ''}>${esc(o.name)}</span>
        ${o.title ? `<span class="msg-title">${esc(o.title)}</span>` : ''}
        <span class="msg-time">${o.time ? timeStr(o.time) : ''}</span>
        <button class="msg-copy" title="复制这条消息">${icon('copy', 13)}</button>
      </div>
      <div class="msg-text md"></div>
    </div>`;
  const tx = el.querySelector('.msg-text');
  tx.innerHTML = mdRender(o.text || '');
  el.querySelector('.msg-copy').addEventListener('click', async ev => {
    ev.stopPropagation();
    const ok = await copyText(el._raw);
    toast(ok ? '已复制' : '复制失败，浏览器没给权限');
  });
  return { el, textEl: tx };
}

// 本地乐观消息的临时 id：后端落库后 id 不同，但至少让「刚发的这条」
// 有个可寻址的 data-mid，搜索跳转找不到服务端 id 时还有文本兜底。
let localSeq = 0;

export function addMsg(o) {
  const wrap = $('#msgWrap');
  maybeTimeDivider(wrap, o.time);
  if (!o.id) o.id = 'local-' + Date.now().toString(36) + '-' + (localSeq++);
  const { el } = buildMsgEl(o);
  trackNew();          // 先记未读再插入：判断的是插入前的滚动位置
  wrap.appendChild(el);
  scheduleDedupeTimes();
  return el;
}

// 同一分钟里连发五条，每条都标一个 23:06 是纯噪音。
// 只保留「和上一条相差 5 分钟以上」的那条时间戳——
// 时间信息没丢，但不再是满屏重复数字。
const TIME_GAP_MS = 5 * 60 * 1000;
let dedupePending = false;

function scheduleDedupeTimes() {
  if (dedupePending) return;
  dedupePending = true;
  requestAnimationFrame(() => {
    dedupePending = false;
    dedupeMsgTimes();
  });
}

export function dedupeMsgTimes() {
  const wrap = $('#msgWrap');
  if (!wrap) return;
  let lastShown = 0;
  wrap.querySelectorAll('.msg').forEach(el => {
    const t = el.querySelector('.msg-time');
    if (!t) return;
    if (!t.textContent.trim()) return;
    const ms = Number(el.dataset.t || 0);
    if (ms && lastShown && ms - lastShown < TIME_GAP_MS) {
      t.textContent = '';
    } else if (ms) {
      lastShown = ms;
    }
  });
}

// 加载历史时的骨架屏。
// 之前这段是纯空白：慢网络下打开一个群，要盯着白屏好几秒，
// 看起来像页面挂了。骨架屏至少告诉用户「在加载」。
export function showMsgSkeleton() {
  const wrap = $('#msgWrap');
  if (!wrap) return;
  const rows = [88, 62, 74].map(w => `
    <div class="sk-msg">
      <div class="sk-av"></div>
      <div class="sk-body">
        <div class="sk-line" style="width:${w}px"></div>
        <div class="sk-line sk-wide"></div>
        <div class="sk-line" style="width:60%"></div>
      </div>
    </div>`).join('');
  wrap.innerHTML = `<div class="sk-wrap">${rows}</div>`;
}

export function hideMsgSkeleton() {
  const box = $('#msgWrap .sk-wrap');
  if (box) box.remove();
}

export function addSysNote(text, into) {
  const el = document.createElement('div');
  el.className = 'sys-note';
  el.textContent = text;
  // 一轮之内的事件（额度、出错）追加到本轮的容器里：
  // 用户中途切去别的群时容器已脱离文档，不会污染新群的消息区
  (into || $('#msgWrap')).appendChild(el);
  return el;
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
  // 右栏成员列表同步高亮——只在消息里标名字的话，
  // 五个人你一句我一句时，「现在是谁」要靠眼睛在两边对
  markSpeakingMember(agentName);
}

function markSpeakingMember(agentName) {
  const box = $('#memberList');
  if (!box) return;
  box.querySelectorAll('.member').forEach(m => {
    const isMe = agentName && m.dataset.agent === agentName;
    m.classList.toggle('talking', !!isMe);
  });
}

export function renderMembers() {
  const box = $('#memberList');
  const conv = state.cur;
  const iAmOwner = !!(conv && state.user && conv.userId === state.user.id);

  // 场景页说「创建后可以随时换人、改设定」——这里就是兑现的地方（群主可见）
  const agentRows = state.agents.map((a, i) => `
    <div class="member" data-agent="${esc(a.name)}">
      ${makeAv(a.name, a.color, 'av-sm')}
      <div class="mm">
        <div class="mn">${esc(a.name)}</div>
        <div class="mt">${esc(a.title)}</div>
      </div>
      ${iAmOwner ? `
      <span class="m-acts">
        <button class="m-edit" data-i="${i}" title="改设定">${icon('pencil', 12)}</button>
        <button class="m-del" data-i="${i}" title="移除角色">${icon('x', 12)}</button>
      </span>` : ''}
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

  // 模型选择：群主可换（PATCH {model}，下一轮发言生效）。
  // 模型列表来自运维台——那里增删/启停模型，这里的选择器跟着变。
  const models = state.models || [];
  let modelRows = '';
  if (models.length > 0) {
    const cur = (conv && conv.model) || '';
    const curInfo = models.find(m => m.key === cur);
    const opts = (cur && !curInfo ? `<option value="${esc(cur)}" selected>${esc(cur)}（当前）</option>` : '')
      + models.map(m => `<option value="${esc(m.key)}"${m.key === cur ? ' selected' : ''}>${esc(m.key)}${m.note ? ' · ' + esc(m.note) : ''}</option>`).join('');
    modelRows = `
      <div class="member-sep">模型</div>
      <div class="member">
        <div class="mm">
          <div class="mn">${cur ? esc(cur) : '自动'}</div>
          <div class="mt">${curInfo ? esc(curInfo.note || '') : '按权重自动分配'}</div>
        </div>
        ${iAmOwner ? `<select class="model-sel" id="convModel" title="切换后从下一轮发言开始生效">${opts}</select>` : ''}
      </div>`;
  } else {
    modelRows = '';
  }

  box.innerHTML = agentRows
    + (iAmOwner ? `
      <button class="member-add" id="memberAdd">${icon('plus', 13)} 从角色库添加</button>` : '')
    + (collabRows
      ? `<div class="member-sep">协作者 · ${others.length}</div>${collabRows}`
      : '')
    + modelRows;

  // 换模型：立即 PATCH，成功后刷新本行显示
  const modelSel = $('#convModel');
  if (modelSel) {
    modelSel.addEventListener('change', async () => {
      const model = modelSel.value;
      try {
        await api(`/v1/conversations/${conv.id}`, { method: 'PATCH', body: { model } });
        conv.model = model;
        toast('模型已切换，下一轮发言生效');
        renderMembers();
      } catch (e) {
        toast(humanError(e));
        renderMembers();
      }
    });
  }

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
      } catch (e) { toast(humanError(e)); }
    });
  });

  // 改人设：PATCH 整个 agents 数组替换（后端契约）
  box.querySelectorAll('.m-edit').forEach(b => {
    b.addEventListener('click', async () => {
      const i = +b.dataset.i;
      const a = state.agents[i];
      if (!a) return;
      const v = await uiEditText(`${a.name} 的人设`, a.prompt || '', {
        okText: '保存', hint: '描述身份、语气、关注点。保存后从下一轮开始生效。',
      });
      if (v === null) return;
      const next = state.agents.map((x, k) => k === i ? { ...x, prompt: v } : x);
      try {
        await patchAgents(next);
        state.agents = next;
        toast(`${a.name} 的设定已更新`);
      } catch (e) {
        toast(e && e.status === 403 ? '只有群主能改角色设定' : humanError(e));
      }
    });
  });

  // 移除角色：DELETE，后端按 key 删
  box.querySelectorAll('.m-del').forEach(b => {
    b.addEventListener('click', async () => {
      const i = +b.dataset.i;
      const a = state.agents[i];
      if (!a) return;
      const yes = await uiConfirm(`移除「${a.name}」？`,
        '这个角色会退出群聊，之后的新消息不再有他。历史消息保留。', {
          okText: '移除', danger: true,
        });
      if (!yes) return;
      try {
        await api(`/v1/conversations/${conv.id}/agents/${encodeURIComponent(a.key || a.name)}`, { method: 'DELETE' });
        state.agents.splice(i, 1);
        if (conv.agents) conv.agents = state.agents.slice();
        pipeDone = 0;
        renderMembers();
        renderPipeline(true);
        setChatStatus('');
        toast(`已移除 ${a.name}`);
      } catch (e) {
        toast(e && e.status === 403 ? '只有群主能移除角色' : humanError(e));
      }
    });
  });

  const addBtn = box.querySelector('#memberAdd');
  if (addBtn) addBtn.addEventListener('click', () => pickLibAgent());
}

/* 角色设定整体替换。后端按数组全量替换，前端把当前生效的数组原样传回 */
async function patchAgents(agents) {
  const conv = state.cur;
  const fresh = await api('/v1/conversations/' + conv.id, {
    method: 'PATCH',
    body: { agents },
  });
  // 后端可能返回更新后的会话，同步到本地
  if (fresh && fresh.agents) conv.agents = fresh.agents;
  return fresh;
}

/* 从角色库挑一个人拉进当前群 */
function pickLibAgent() {
  const conv = state.cur;
  if (!conv) return;
  const inGroup = new Set(state.agents.map(a => a.name));
  const cands = (state.library || []).filter(a => !inGroup.has(a.name));
  const mask = document.createElement('div');
  mask.className = 'ui-dlg';
  mask.innerHTML = `<div class="ui-dlg-card">
    <h3>从角色库添加</h3>
    ${!state.library.length
      ? `<p class="ui-dlg-hint">角色库还是空的。先去场景页「添加角色」造几个，存入角色库后就能拉进任何群。</p>`
      : !cands.length
        ? `<p class="ui-dlg-hint">角色库里的人都已经在群里了。</p>`
        : `<div class="lib-pick-list">
            ${cands.map(a => `
              <button class="lp-item" data-id="${esc(a.id)}">
                ${makeAv(a.name, a.color, 'av-sm')}
                <span class="lp-n">${esc(a.name)}</span>
                <span class="lp-t">${esc(a.title || '')}</span>
              </button>`).join('')}
           </div>`}
    <div class="ui-dlg-act"><button class="ui-dlg-btn" data-x="cancel">关闭</button></div>
  </div>`;
  document.body.appendChild(mask);
  requestAnimationFrame(() => mask.classList.add('on'));
  const close = () => { mask.classList.remove('on'); setTimeout(() => mask.remove(), 180); document.removeEventListener('keydown', onKey); };
  mask.querySelector('[data-x=cancel]').onclick = close;
  mask.addEventListener('click', e => { if (e.target === mask) close(); });
  function onKey(e) { if (e.key === 'Escape') { e.preventDefault(); close(); } }
  document.addEventListener('keydown', onKey);

  mask.querySelectorAll('.lp-item').forEach(b => {
    b.addEventListener('click', async () => {
      b.disabled = true;
      try {
        await api(`/v1/agents/${b.dataset.id}/to/${conv.id}`, { method: 'POST' });
        close();
        await refreshCurConv();
        toast('已加入群聊');
      } catch (e) {
        b.disabled = false;
        toast(humanError(e));
      }
    });
  });
}

/* 成员变动后刷新本地会话数据（不重拉消息，只更新右侧面板） */
async function refreshCurConv() {
  try {
    await loadConvs();
  } catch (e) { /* 列表刷新失败不拦主流程 */ }
  const conv = state.convs.find(c => c.id === state.cur.id);
  if (!conv) return;
  state.cur = conv;
  if (conv.agents && conv.agents.length) state.agents = conv.agents;
  renderMembers();
  renderPipeline(true);
  setChatStatus('');
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

/* 产物正文来源：SSE 的 artifact 事件只带 28 字摘要（后端 summarize），
   完整内容在产生它的那条消息里。点开时按角色名取最近一条消息全文。 */
function artifactFullText(agentName) {
  if (!agentName) return '';
  const els = [...$('#msgWrap').querySelectorAll('.msg')];
  for (let i = els.length - 1; i >= 0; i--) {
    const n = els[i].querySelector('.msg-name');
    if (n && n.textContent === agentName && els[i]._raw) return els[i]._raw;
  }
  return '';
}

/* 产物查看器：自绘弹层，显示全文 + 复制 + 下载 .md */
function openArtifactView(title, detail, agentName) {
  const full = artifactFullText(agentName) || detail || '';
  const mask = document.createElement('div');
  mask.className = 'ui-dlg art-view';
  mask.innerHTML = `<div class="ui-dlg-card art-view-card">
    <div class="art-view-head">
      <span class="art-view-ic">${icon('file-text', 16)}</span>
      <b>${esc(title)}</b>
      <button class="icon-btn" data-x="close" title="关闭">${icon('x', 16)}</button>
    </div>
    ${agentName ? `<div class="art-view-by">来自 ${esc(agentName)}</div>` : ''}
    <pre class="art-view-body">${esc(full)}</pre>
    <div class="ui-dlg-act">
      <button class="ui-dlg-btn" data-x="copy">${icon('copy', 13)} 复制全文</button>
      <button class="ui-dlg-btn primary" data-x="dl">${icon('download', 13)} 下载 .md</button>
    </div>
  </div>`;
  document.body.appendChild(mask);
  requestAnimationFrame(() => mask.classList.add('on'));
  const close = () => { mask.classList.remove('on'); setTimeout(() => mask.remove(), 180); document.removeEventListener('keydown', onKey); };
  mask.querySelector('[data-x=close]').onclick = close;
  mask.addEventListener('click', e => { if (e.target === mask) close(); });
  function onKey(e) { if (e.key === 'Escape') { e.preventDefault(); close(); } }
  document.addEventListener('keydown', onKey);

  mask.querySelector('[data-x=copy]').onclick = async () => {
    const ok = await copyText(full);
    toast(ok ? '已复制' : '复制失败，浏览器没给权限');
  };
  mask.querySelector('[data-x=dl]').onclick = () => {
    const blob = new Blob([full], { type: 'text/markdown;charset=utf-8' });
    const a = document.createElement('a');
    a.href = URL.createObjectURL(blob);
    a.download = (title || '产物').replace(/[\\/:*?"<>|]/g, '_') + '.md';
    a.click();
    setTimeout(() => URL.revokeObjectURL(a.href), 4000);
  };
}

function buildArtifactCard(agentName, title, detail) {
  const card = document.createElement('div');
  card.className = 'artifact';
  card.innerHTML = `
    <span class="fi">${icon('file-text', 16)}</span>
    <div class="art-txt">
      <div class="an">${esc(title)}</div>
      <div class="ad">${esc(detail || '')}</div>
    </div>
    <span class="go">${icon('chevron-right', 16)}</span>`;
  card.addEventListener('click', () => openArtifactView(title, detail, agentName));
  return card;
}

export function appendArtifactCard(agentName, title, detail) {
  $('#msgWrap').appendChild(buildArtifactCard(agentName, title, detail));
}

export function addArtifact(name, title, detail) {
  // 同一个角色 + 同一个产物名，只保留一份（内容更新为最新的）。
  // 不加这个的话，六轮对话下来右栏会列十几个「合规点对表」——
  // 那是历史记录，不是产物清单。
  const key = name + '/' + title;
  const idx = artifacts.findIndex(a => a.key === key);
  const item = { key, name, title, detail, at: Date.now() };
  if (idx >= 0) {
    artifacts[idx] = item;
  } else {
    artifacts.push(item);
  }

  $('#artifactCount').textContent = artifacts.length;
  const box = $('#artifactList');
  box.innerHTML = artifacts.map((a, i) => `
    <button class="art-item" data-art="${i}" title="${esc((a.detail || '').slice(0, 80))}">
      <span class="art-ic">${icon('file-text', 13)}</span>
      <span class="art-n">${esc(a.title)}</span>
    </button>`).join('');
  box.querySelectorAll('[data-art]').forEach(b => {
    b.addEventListener('click', () => {
      const a = artifacts[+b.dataset.art];
      if (a) openArtifactView(a.title, a.detail, a.name);
    });
  });
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

// 一轮的取消句柄和序号：
// - 句柄给「停止」按钮用（AbortController 掐断 fetch）；
// - 序号防竞态：中途切群会 abort 旧轮，旧轮的 finally 不能
//   把新轮刚设好的 busy 状态又改回去。
let abortCtl = null;
let roundSeq = 0;

export function stopRound(silent) {
  if (abortCtl) {
    abortCtl.abort();
    abortCtl = null;
  }
  if (!silent) toast('已停止这一轮，说出来的都保留着');
}

/* 发送按钮在「发送 / 停止」之间切换。
   之前一轮里按钮整个禁用，中途说错了话只能干等一分钟。 */
export function setBusyUI(on) {
  const btn = $('#sendBtn');
  if (!btn) return;
  const ta = $('#ta');
  if (on) {
    btn.classList.add('stop');
    btn.disabled = false;
    btn.title = '停止这一轮';
    btn.innerHTML = icon('square', 14);
  } else {
    btn.classList.remove('stop');
    btn.title = '发送';
    btn.innerHTML = icon('arrow-up', 16);
    btn.disabled = !ta || ta.disabled || !ta.value.trim();
  }
}

export async function runRound(path, body, opts = {}) {
  const mySeq = ++roundSeq;
  const ac = new AbortController();
  abortCtl = ac;
  // 本轮消息都挂在这个容器下；中途切群后容器脱离文档，
  // 迟到的 SSE 事件写进来也不会污染新群
  const container = document.createElement('div');
  $('#msgWrap').appendChild(container);
  renderPipeline(true);

  let curEl = null;
  let curText = '';
  // 后端每轮结束都会发一个 done。如果流在 done 之前就断了，
  // 前端原本完全分不出来——看起来和正常结束一模一样，
  // 用户只会觉得「怎么没人接着说了」。
  let sawDone = false;
  const aborted = () => ac.signal.aborted;

  function handleEvent(ev) {
    if (ev.type === 'done') { sawDone = true; return; }
    if (ev.type === 'start') {
      // 上一个角色如果只留下一个空的思考占位（上游失败或前端没收到内容），
      // 先清掉——不然连着几个 start 会堆出好几个空气泡
      if (curEl && !curEl.querySelector('.msg-text').textContent.trim()) {
        curEl.remove();
      }
      setChatStatus(ev.name);
      setDocTitle(state.cur && state.cur.title, true);
      curText = '';
      container.querySelectorAll('.msg.speaking').forEach(m => m.classList.remove('speaking'));
      curEl = document.createElement('div');
      curEl.className = 'msg';
      curEl.innerHTML = `
        ${makeAv(ev.name, ev.color)}
        <div class="msg-body">
          <div class="msg-head">
            <span class="msg-name" style="color:${esc(ev.color)}">${esc(ev.name)}</span>
            <span class="msg-time"></span>
            <button class="msg-copy" title="复制这条消息">${icon('copy', 13)}</button>
          </div>
          <div class="thinking"><i></i><i></i><i></i></div>
          <div class="msg-text md" style="display:none"></div>
        </div>`;
      curEl._raw = '';
      curEl.querySelector('.msg-copy').addEventListener('click', async e => {
        e.stopPropagation();
        const ok = await copyText(curEl._raw);
        toast(ok ? '已复制' : '复制失败，浏览器没给权限');
      });
      container.appendChild(curEl);
      curEl.classList.add('speaking');
      trackNew();
      scrollBottom();   // 新角色开口，跟着走到他
      return;
    }

    if (ev.type === 'delta' && curEl) {
      curEl.querySelector('.thinking').style.display = 'none';
      const tx = curEl.querySelector('.msg-text');
      tx.style.display = '';
      curText += ev.content;
      curEl._raw = curText;
      tx.innerHTML = mdRender(curText);
      tx.classList.add('caret');
      autoScroll();   // 只有本来就在底部才跟随，翻历史时不打扰
      return;
    }

    if (ev.type === 'end') {
      if (curEl) {
        curEl.classList.remove('speaking');
        const tx = curEl.querySelector('.msg-text');
        tx.classList.remove('caret');
        // 后端在流结束后会清理掉说话人前缀（「工头：…」这类）。
        // 流式显示的是原文，这里用清理后的内容覆盖一次，
        // 保证界面和落库的一致。
        if (typeof ev.content === 'string' && ev.content !== curText) {
          curText = ev.content;
          curEl._raw = curText;
          tx.innerHTML = mdRender(curText);
        }
        curEl.querySelector('.msg-time').textContent = timeStr(new Date());
      }
      pipeDone++;
      renderPipeline(false);
      autoScroll();
      return;
    }

    if (ev.type === 'artifact') {
      addArtifact(ev.name, ev.title, ev.detail);
      container.appendChild(buildArtifactCard(ev.name, ev.title, ev.detail));
      trackNew();
      autoScroll();
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
      autoScroll();
      return;
    }

    if (ev.type === 'ask') {
      if (curEl) curEl.classList.add('asking');
      return;
    }

    if (ev.type === 'busy') {
      addSysNote('群里正在说话，这条先放着 — 稍后点右上角「继续聊」让他们接上', container);
      return;
    }

    if (ev.type === 'quota') {
      const u = state.user || {};
      const left = (typeof u.quota === 'number' && typeof u.used === 'number')
        ? Math.max(0, u.quota - u.used) : null;
      addSysNote(left === null
        ? '本月额度用完了 — 联系管理员重置额度后再试'
        : `本月额度用完了（剩余 ${left} 次）— 联系管理员重置额度后再试`, container);
      return;
    }

    if (ev.type === 'error') {
      // 上游挂了：把还没说完的那个空气泡清掉，别让用户对着
      // 一个只有思考三点、永远不会有内容的占位发呆
      if (curEl && !curEl.querySelector('.msg-text').textContent.trim()) {
        curEl.remove();
        curEl = null;
      }
      addSysNote(ev.name === '系统' ? ev.content : ev.name + ' 出错了：' + ev.content, container);
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
      signal: ac.signal,
    });

    if (!res.ok) {
      const j = await res.json().catch(() => ({}));
      // 组装出和 api() 一样的错误对象，humanError 才能按状态码翻译
      const err = new Error((j.error && j.error.message) || ('HTTP ' + res.status));
      err.status = res.status;
      err.code = j.error && j.error.code;
      throw err;
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

    if (aborted()) return;
    // 流正常关闭但没收到 done：连接被中途掐断了。
    // 已经说出来的话都在（服务端逐条落库了），只是这一轮没跑完。
    if (!sawDone && opts.expectDone !== false) {
      addSysNote('连接断了，这一轮没说完 — 已经说出来的都保留着', container);
    }
  } catch (e) {
    // 主动停止 / 切群：不算错误，已经显示的消息保留
    if (aborted() || e.name === 'AbortError') {
      addSysNote('这一轮已停止 — 已经说出来的都保留着', container);
      return;
    }
    if ((e && e.status === 429) || /额度/.test(e.message)) {
      addSysNote('本月额度用完了 — 这句话先留着，额度重置后再点「继续聊」', container);
    } else if (/Failed to fetch|NetworkError|load failed|network error/i.test(e.message)) {
      // 网络断了：这条消息根本没到服务器。界面上那条「我发出的」是乐观渲染，
      // 刷新就没了。必须把原文还给用户，否则他要重新打一遍。
      addSysNote('网络断了，这句话没发出去 — 已经放回输入框，网络好了再发一次', container);
      if (opts.onFail) opts.onFail(e, 'network');
    } else {
      addSysNote(humanError(e), container);
      if (opts.onFail) opts.onFail(e, 'other');
    }
    return;
  } finally {
    if (abortCtl === ac) abortCtl = null;
    // 只有「最新一轮」才有资格复位全局状态：
    // 切群后旧轮的收尾不该动新一轮的 busy / 按钮状态
    if (mySeq === roundSeq) {
      state.busy = false;
      setBusyUI(false);
      setChatStatus('');
      setDocTitle(state.cur && state.cur.title, false);
      const nb = $('#nudgeBtn');
      if (nb) nb.disabled = false;
      loadConvs();
      // 一轮结束就把焦点还给输入框：用户看完最想做的事是说话，
      // 让他再点一次输入框是多此一举
      const box = $('#ta');
      if (box && !box.disabled && document.activeElement === document.body) box.focus();
    }
    if (curEl) curEl.querySelector('.msg-text').classList.remove('caret');
    // 轮次分隔线画在本轮容器里：切群后 msgWrap 已属于新群，别把线画过去
    markRoundEnd(container);
    updateJump();
    if (opts.onDone) opts.onDone();
  }
}

export async function send() {
  // 上一轮还没结束就静默 return 的话，用户点了发送却毫无反应，会以为卡死了
  if (state.busy) { toast('还在说，点发送键（红色方块）可以先停掉这一轮'); return; }
  if (!state.cur) { toast('先选一个群'); return; }
  const ta = $('#ta');
  const text = ta.value.trim();
  if (!text) return;
  // 后端上限 4000 字。前端先拦一道，省得用户打完才被拒。
  if ([...text].length > 4000) {
    toast('太长了，最多 4000 字。拆成几条发更好');
    return;
  }

  ta.value = '';
  ta.style.height = 'auto';
  state.busy = true;
  setBusyUI(true);

  const sentEl = addMsg({ name: (state.user && state.user.name) || '你', text, me: true, time: new Date() });
  scrollBottom();

  await runRound('/v1/conversations/' + state.cur.id + '/orchestrate', { content: text }, {
    onFail: (err, kind) => {
      // 消息没到服务器。把它标成「没发出去」，并把原文放回输入框。
      if (sentEl) sentEl.classList.add('failed');
      const box = $('#ta');
      if (box && !box.value.trim()) {
        box.value = text;
        box.dispatchEvent(new Event('input', { bubbles: true }));
        if (kind === 'network') box.focus();
      }
    },
  });

  // 点红色的失败气泡 = 重发这条原文（输入框里已有的内容会被覆盖，
  // 但失败场景下输入框通常就是这句原文）
  if (sentEl) sentEl.addEventListener('click', () => {
    if (!sentEl.classList.contains('failed')) return;
    if (state.busy) return toast('还在说，等这一轮结束');
    const box = $('#ta');
    if (box && box.value.trim() && box.value.trim() !== sentEl._raw) {
      // 输入框里已经打了别的：别覆盖，先放回去让用户自己发
      toast('输入框里有新内容，先把这句放回去了');
    }
    if (box) {
      box.value = sentEl._raw;
      box.dispatchEvent(new Event('input', { bubbles: true }));
    }
    sentEl.remove();
    send();
  });
}

/* ---------------- 自主开场 ---------------- */
export async function kickoff(convId, topic) {
  if (state.busy) return;
  state.busy = true;
  setBusyUI(true);
  const nb = $('#nudgeBtn');
  if (nb) nb.disabled = true;

  await runRound('/v1/conversations/' + convId + '/kickoff', topic ? { topic } : {});
}
