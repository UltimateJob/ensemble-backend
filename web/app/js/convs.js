/* ============================================================
   合奏 Ensemble · 会话列表
   左栏的渲染、右键菜单、重命名/邀请/删除/离开。
   和「群里的对话」是两件事，所以分开。
   ============================================================ */

import {
  $, $$, api, state, toast, esc, makeAv, agoStr, setDocTitle, humanError } from './core.js';
import { uiPrompt, uiConfirm } from './dialog.js';

// 点会话时要打开它——但「打开」的逻辑在 chat.js。
// 用回调注入，避免两个模块互相 import 成环。
let openConvFn = id => { toast('打不开：' + id); };
export function setConvOpener(fn) { openConvFn = fn; }

export async function loadConvs() {
  try {
    state.convs = await api('/v1/conversations');
  } catch (e) {
    // 刷新失败不要把列表清空。之前是 state.convs = []，
    // 网络抖一下整个侧栏就变成「还没有群聊 / 开第一个群」——
    // 用户会以为自己的群全没了。保留上一次的列表，只是不更新。
    if (!state.convs || !state.convs.length) state.convs = [];
    renderConvs();
    if (window.__refreshOnboard) window.__refreshOnboard();
    return;
  }
  renderConvs();
  // 会话数变化会影响上手清单
  if (window.__refreshOnboard) window.__refreshOnboard();
}

export function renderConvs() {
  renderInto($('#convList'));
  renderInto($('#drawerList'));
}

function renderInto(box) {
  if (!box) return;
  if (!state.convs.length) {
    // 只说「点新建」不够——得给一个能点的东西
    box.innerHTML = `<div class="conv-empty">
      <div class="ce-t">还没有群聊</div>
      <div class="ce-d">选一个场景，平台会配好一组角色</div>
      <button class="ce-btn" id="ceNew">开第一个群</button>
    </div>`;
    const b = box.querySelector('#ceNew');
    if (b) b.addEventListener('click', () => { if (window.__openScenes) window.__openScenes(); });
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
      openConvFn(el.dataset.id);
    });
  });
}



let ctxConvId = null;

export function openCtxMenu(id, rect) {
  // 权限不同，能做的操作也不同
  const conv = state.convs.find(c => c.id === id);
  const iAmOwner = !!(conv && state.user && conv.userId === state.user.id);
  document.querySelectorAll('#ctxMenu [data-act]').forEach(b => {
    const act = b.dataset.act;
    // 改群名和删除、存模板都是群主的操作；
    // 协作者能看到的是邀请和离开
    if (act === 'delete' || act === 'saveStudio' || act === 'rename') {
      b.style.display = iAmOwner ? '' : 'none';
    }
    if (act === 'leave') b.style.display = iAmOwner ? 'none' : '';
  });
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

export function closeCtxMenu() {
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
      const name = await uiPrompt('改个名字', conv.title, { okText: '改名' });
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
      } catch (e) { toast(humanError(e)); }
      return;
    }

    if (act === 'saveStudio') {
      const name = await uiPrompt('存为工作室', conv.title, {
        okText: '保存',
        hint: '之后它会像内置场景一样出现在场景页里，点一下就能开同样的群。',
      });
      if (!name) return;
      try {
        const st = await api('/v1/studios', {
          method: 'POST',
          body: { fromConvId: conv.id, name: name.trim() },
        });
        toast('已存为工作室「' + st.name + '」');
        // 场景列表要重新拉，不然场景页里看不到新的
        try {
          state.scenarios = await api('/v1/scenarios');
        } catch (e) { /* 忽略 */ }
        if (window.__refreshOnboard) window.__refreshOnboard();
      } catch (e) { toast(humanError(e)); }
      return;
    }

    if (act === 'invite') {
      const target = await uiPrompt('邀请协作者', '', {
        placeholder: '手机号或邮箱',
        okText: '邀请',
        hint: '对方需要先注册过。邀请后可以一起看这个群、一起说话。',
      });
      if (!target) return;
      try {
        const r = await api('/v1/conversations/' + id + '/collaborators', {
          method: 'POST', body: { target: target.trim() },
        });
        toast('已邀请 ' + (r.invitee ? r.invitee.name : target));
        await loadConvs();
      } catch (e) { toast(humanError(e)); }
      return;
    }

    if (act === 'delete') {
      UI_confirm_delete(conv);
    }
  });
});

async function UI_confirm_delete(conv) {
  const yes = await uiConfirm(`删除「${conv.title}」？`,
    '群里的消息会一并清掉，不能恢复。', { okText: '删除', danger: true });
  if (!yes) return;
  api('/v1/conversations/' + conv.id, { method: 'DELETE' })
    .then(async () => {
      if (state.cur && state.cur.id === conv.id) {
        state.cur = null;
        $('#msgWrap').innerHTML = '';
        $('#msgs').style.display = 'none';
        $('#composer').style.display = 'none';
        $('#chatHead').style.display = 'none';
        $('#chatEmpty').style.display = 'flex';
        document.body.classList.add('no-conv');
        $('#crumbName').textContent = '—';
        state.agents = [];
        setDocTitle('', false);
        renderMembers();
      }
      await loadConvs();
      toast('已删除');
    })
    .catch(e => toast(humanError(e)));
}



