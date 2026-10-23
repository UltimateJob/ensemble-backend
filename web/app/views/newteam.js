/* ============================================================
   新建自定义团队：选一组角色，直接开一个群
   ============================================================ */

import { api, state, esc, icon, toast } from '../js/core.js';
import { openModal } from '../js/modal.js';
import { navigate } from '../js/router.js';
import { renderNav } from '../js/shell.js';
import { avatarHTML } from '../js/avatars.js';

let picked = [];
// 角色库单独缓存：state.library 被旧代码当作「我的角色」（SavedAgent）用，
// 两种形状混在一起会互相踩，不能共用。
let charCache = [];

export function openNewTeam() {
  picked = [];
  openModal({
    title: '新建自定义团队',
    width: 640,
    body: `
      <label class="field"><span>团队名称</span>
        <input id="ntName" maxlength="20" placeholder="比如：增长小组"></label>
      <label class="field"><span>团队简介</span>
        <input id="ntDesc" maxlength="60" placeholder="一句话说明这组角色负责什么"></label>
      <div class="nt-roles">
        <div class="nt-h">角色</div>
        <div id="ntList"></div>
        <button class="btn btn-line" id="ntAdd">${icon('plus', 15)}添加角色</button>
      </div>`,
    actions: [
      { key: 'cancel', label: '取消', kind: 'line' },
      { key: 'save', label: '保存并进入', kind: 'primary', onClick: save },
    ],
    onMount: mask => {
      renderPicked(mask);
      mask.querySelector('#ntAdd').addEventListener('click', () => togglePicker(mask));
      mask.querySelector('#ntName').focus();
    },
  });
}

function renderPicked(mask) {
  const box = mask.querySelector('#ntList');
  box.innerHTML = picked.map((a, i) => `
    <div class="nt-row">
      ${avatarHTML(a.name, a.color, { size: 32 })}
      <div class="nt-body">
        <div class="nt-name">${esc(a.name)}</div>
        <div class="nt-title">${esc(a.title || '')}</div>
      </div>
      <button class="icon-btn" data-del="${i}" title="移除">${icon('x', 15)}</button>
    </div>`).join('') || '<div class="panel-empty">还没有角色，至少加一个</div>';

  box.querySelectorAll('[data-del]').forEach(b => b.addEventListener('click', () => {
    picked.splice(Number(b.dataset.del), 1);
    renderPicked(mask);
  }));
}

async function togglePicker(mask) {
  const existing = mask.querySelector('#ntPicker');
  if (existing) {   // 再点一次收起，别叠出好几个搜索框
    existing.remove();
    return;
  }
  const host = mask.querySelector('.nt-roles');
  host.insertAdjacentHTML('beforeend', `
    <div class="nt-picker" id="ntPicker">
      <input id="ntSearch" placeholder="搜名字、作品或性格" autocomplete="off">
      <div class="nt-results" id="ntResults"></div>
    </div>`);
  const input = mask.querySelector('#ntSearch');
  const results = mask.querySelector('#ntResults');

  const draw = () => {
    const q = input.value.trim();
    const list = (charCache || []).filter(c =>
      !q || (c.name + (c.work || '') + (c.tags || []).join('')).includes(q)
    ).slice(0, 30);
    results.innerHTML = list.length ? list.map(c => {
      const on = picked.some(p => p.name === c.name);
      return `<button class="nt-res" data-n="${esc(c.name)}"${on ? ' disabled' : ''}>
        ${avatarHTML(c.name, '', { size: 24 })}
        <span>${esc(c.name)}</span>
        <em>${esc(on ? '已添加' : (c.work || ''))}</em>
      </button>`;
    }).join('') : '<div class="panel-empty">没找到，换个关键词试试</div>';
    results.querySelectorAll('.nt-res').forEach(b => b.addEventListener('click', () => {
      const c = charCache.find(x => x.name === b.dataset.n);
      if (!c || picked.some(p => p.name === c.name)) return;
      picked.push({
        key: 'lib_' + c.name,
        characterId: 'lib:' + c.name,
        name: c.name,
        title: (c.tags || []).join(' / '),
        emoji: [...c.name][0],
        color: '',
        prompt: c.tone || '',
      });
      renderPicked(mask);
      draw();   // 立即把这一项标成「已添加」
      input.focus();
    }));
  };

  input.addEventListener('input', draw);
  draw();
  input.focus();

  if (!charCache.length) {
    try {
      charCache = (await api('/v1/characters?q=')) || [];
      draw();
    } catch (e) {
      results.innerHTML = '<div class="panel-empty">角色库加载失败，稍后再试</div>';
    }
  }
}

async function save(mask) {
  const name = mask.querySelector('#ntName').value.trim();
  if (!name) { toast('先写团队名称', { type: 'err' }); return false; }
  if (!picked.length) { toast('至少加一个角色', { type: 'err' }); return false; }
  // 简介目前只做界面（会话表没有简介字段），不随请求发送。

  const btn = mask.querySelector('[data-x=save]');
  if (btn) { btn.disabled = true; btn.textContent = '创建中…'; }
  try {
    const conv = await api('/v1/conversations', {
      method: 'POST',
      body: { title: name, studio: 'custom', agents: picked },
    });
    state.convs = await api('/v1/conversations').catch(() => state.convs);
    renderNav(state.convs);   // 新房间立刻出现在左栏
    document.dispatchEvent(new CustomEvent('conv:change'));
    toast('团队已创建', { type: 'ok' });
    navigate('#/room/' + encodeURIComponent(conv.id));
    // 与大厅一致：新群进来就先聊起来
    setTimeout(async () => {
      if (state.cur?.id !== conv.id) return;
      const { kickoff } = await import('../js/chat.js');
      kickoff(conv.id);
    }, 600);
    return true;
  } catch (e) {
    toast('创建失败：' + e.message, { type: 'err' });
    if (btn) { btn.disabled = false; btn.textContent = '保存并进入'; }
    return false;
  }
}
