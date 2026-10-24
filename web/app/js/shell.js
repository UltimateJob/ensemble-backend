/* ============================================================
   外壳：左导航 / 右侧工作面板 / 快捷键
   ============================================================ */

import { $, esc, icon, hydrateIcons, state } from './core.js';

export const NAV = [
  { key: 'lobby',      label: '团队大厅', ic: 'users' },
  { key: 'room',       label: '聊天房间', ic: 'message-circle' },
  { key: 'goals',      label: '目标看板', ic: 'trending-up' },
  { key: 'connectors', label: '连接器',   ic: 'share-2' },
  { key: 'library',    label: '资料库',   ic: 'book-open' },
  { key: 'settings',   label: '设置',     ic: 'settings' },
];

let panelRender = null;

export function setActiveNav(name) {
  document.querySelectorAll('#navList .nav-item').forEach(el => {
    el.classList.toggle('on', el.dataset.key === name || (name === 'notfound' && el.dataset.key === 'lobby'));
  });
  document.querySelectorAll('#navRooms .nav-room').forEach(el => {
    el.classList.toggle('on', name === 'room' && el.dataset.id === state.cur?.id);
  });
}

export function renderNav(convs = []) {
  const list = $('#navList');
  if (!list) return;
  list.innerHTML = `
    <button class="nav-search" id="navSearch" title="搜索（⌘K）">
      ${icon('search', 16)}<span>搜索</span><kbd>⌘K</kbd>
    </button>` + NAV.map(n => `
    <a class="nav-item" data-key="${n.key}" href="#/${n.key}">
      ${icon(n.ic, 18)}<span>${n.label}</span>
    </a>`).join('') + `
    <div class="nav-rooms" id="navRooms">
      <div class="nav-rooms-h">房间</div>
      ${convs.length ? convs.map(c => `
        <a class="nav-room" data-id="${esc(c.id)}" href="#/room/${encodeURIComponent(c.id)}" title="${esc(c.title)}">
          <span class="dot"></span><span class="nm">${esc(c.title)}</span>
        </a>`).join('') : '<div class="nav-room-empty">还没有房间</div>'}
    </div>`;
  hydrateIcons(list);
  document.getElementById('navSearch')?.addEventListener('click', () => {
    import('./search.js').then(m => m.openSearch());
  });
  // 房间右键菜单：改名 / 退出 / 删除
  import('./roommenu.js').then(({ bindRoomMenu }) => {
    document.querySelectorAll('#navRooms .nav-room').forEach(el => {
      const conv = convs.find(c => c.id === el.dataset.id);
      if (!conv) return;
      bindRoomMenu(el, conv, (id, title, action) => {
        if (action === 'delete' || action === 'leave') {
          state.convs = (state.convs || []).filter(c => c.id !== id);
          renderNav(state.convs);
          if (state.cur?.id === id) location.hash = '#/lobby';
        } else if (title) {
          const hit = (state.convs || []).find(c => c.id === id);
          if (hit) hit.title = title;
          if (state.cur?.id === id) state.cur.title = title;
          renderNav(state.convs);
        }
      });
    });
  });
  setActiveNav(currentName());
}

const KNOWN = ['lobby', 'room', 'goals', 'connectors', 'library', 'settings'];

// 未知路由一律当作 notfound：否则 #/nope 这类地址会让导航失去高亮，
// 用户会以为自己「不在任何页面里」。
function currentName() {
  const h = (location.hash || '#/lobby').replace(/^#\/?/, '');
  const head = h.split('/')[0].split('?')[0] || 'lobby';
  return KNOWN.includes(head) ? head : 'notfound';
}

// 左栏头像：跟账号走（颜色 / emoji / 首字）。
// 之前这里写死渐变底 + 首字母，用户在设置里换的头像永远不生效。
function navAvatar(user, name) {
  const a = user?.avatar || '';
  if (a && !a.startsWith('#')) {
    return `<span class="av av--fallback nav-av" style="background:var(--paper-2)">
      <span style="font-size:15px;line-height:1">${esc(a)}</span></span>`;
  }
  const bg = a.startsWith('#') ? a : 'linear-gradient(150deg,#DCD6CB,#CDC5B7)';
  const fg = a.startsWith('#') ? '#fff' : '#5A5145';
  return `<span class="av av--fallback nav-av" style="background:${bg}">
    <b style="font-size:13px;color:${fg}">${esc([...name][0] || '?')}</b></span>`;
}

export function renderNavFoot(user) {
  const box = $('#navFoot');
  if (!box) return;
  const name = user?.name || '未登录';
  const plan = user?.plan === 'pro' ? 'Pro' : user?.plan === 'team' ? 'Team' : '免费版';
  const left = user?.quota != null && user?.used != null ? user.quota - user.used : null;
  // 显示额度而不是「free」：free 是内部枚举值，对用户没有信息量。
  const sub = left != null ? `${plan} · 剩 ${left} 次` : plan;
  box.innerHTML = `
    <button class="nav-user" id="navUser" title="${esc(name)}">
      ${navAvatar(user, name)}
      <span class="nav-user-body">
        <span class="nav-user-name">${esc(name)}</span>
        <span class="nav-user-sub">${esc(sub)}</span>
      </span>
    </button>`;
  box.querySelector('#navUser').addEventListener('click', () => { location.hash = '#/settings'; });
}

/* ---------------- 右面板 ---------------- */

export function openPanel({ title = '', subtitle = '', render }) {
  const panel = $('#panel');
  const body = $('#panelBody');
  if (!panel || !body) return;
  body.innerHTML = `
    <div class="panel-head">
      <div style="min-width:0;flex:1">
        <div class="panel-title">${esc(title)}</div>
        ${subtitle ? `<div class="panel-sub">${esc(subtitle)}</div>` : ''}
      </div>
      <button class="icon-btn panel-x" id="panelClose" title="收起">${icon('x', 16)}</button>
    </div>
    <div class="panel-content" id="panelContent"></div>`;
  panel.classList.add('on');
  panel.setAttribute('aria-hidden', 'false');
  $('#app')?.classList.add('panel-open');
  body.querySelector('#panelClose').addEventListener('click', closePanel);
  const content = body.querySelector('#panelContent');
  panelRender = render || null;
  if (render) render(content);
}

/** 重新渲染当前面板（数据更新后调用）。 */
export function refreshPanel() {
  const content = $('#panelContent');
  if (content && panelRender) {
    content.innerHTML = '';
    panelRender(content);
  }
}

export function closePanel() {
  const panel = $('#panel');
  if (!panel) return;
  panel.classList.remove('on');
  panel.setAttribute('aria-hidden', 'true');
  $('#app')?.classList.remove('panel-open');
  panelRender = null;
}

export function panelOpen() {
  return !!$('#panel')?.classList.contains('on');
}

export function togglePanel() {
  if (panelOpen()) { closePanel(); return; }
  openPanel({
    title: '工作面板',
    subtitle: '点开一个角色或产物看详情',
    render: el => { el.innerHTML = '<div class="panel-empty">从房间头像、目标卡片或资料库里选一项。</div>'; },
  });
}

export function initShell() {
  $('#panelHandle')?.addEventListener('click', togglePanel);
  document.addEventListener('keydown', e => {
    const mod = e.metaKey || e.ctrlKey;
    if (!mod) return;
    // ⌘K 全局搜索：README 一直在宣传，但三栏改版时界面被删了
    if (e.key === 'k' || e.key === 'K') {
      e.preventDefault();
      import('./search.js').then(m => m.openSearch());
      return;
    }
    if (e.key === '0') { e.preventDefault(); togglePanel(); return; }
    const n = Number(e.key);
    if (n >= 1 && n <= NAV.length) { e.preventDefault(); location.hash = '#/' + NAV[n - 1].key; }
  });
  document.addEventListener('route:change', () => setActiveNav(currentName()));
}
