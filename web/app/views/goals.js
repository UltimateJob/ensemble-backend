/* ============================================================
   目标看板：看板 / 列表两种视图，拖拽改状态，右面板看详情
   ============================================================ */

import { api, state, esc, icon, hydrateIcons, emptyState, toast } from '../js/core.js';
import { openPanel, closePanel, panelOpen } from '../js/shell.js';
import { confirmModal } from '../js/modal.js';
import { avatarHTML } from '../js/avatars.js';
import { stagger } from '../js/motion.js';

const COLS = [
  { key: 'todo', label: '待规划' },
  { key: 'doing', label: '进行中' },
  { key: 'done', label: '已完成' },
];

let root = null;
let view = 'kanban';
let filter = 'active';
let goals = [];

export async function mount(host) {
  root = host;
  await load();
}

export function unmount() {
  root = null;
  goals = [];
}

async function load() {
  try {
    goals = await api('/v1/goals');
  } catch (e) {
    goals = [];
    toast('目标加载失败：' + e.message, { type: 'err' });
  }
  render();
}

function visible() {
  return filter === 'active' ? goals.filter(g => g.status !== 'done') : goals;
}

// 进度来自服务端派生，但写进内联样式前仍夹到 0-100，避免脏数据撑破卡片。
function pct(g) {
  const n = Number(g.progress);
  return Number.isFinite(n) ? Math.max(0, Math.min(100, Math.round(n))) : 0;
}

function card(g) {
  const conv = (state.convs || []).find(c => c.id === g.conversationId);
  const agents = (conv?.agents || []).filter(a => (g.assignees || []).includes(a.key));
  return `<article class="goal-card" draggable="true" tabindex="0" data-id="${esc(g.id)}">
    <div class="gc-title">${esc(g.title)}</div>
    ${g.description ? `<div class="gc-desc">${esc(g.description)}</div>` : ''}
    <div class="gc-meta">
      <div class="gc-avs">${agents.map(a => avatarHTML(a.name, a.color, { size: 22 })).join('')}</div>
      <span class="gc-pct">${pct(g)}%</span>
    </div>
    <div class="gc-bar"><i style="width:${pct(g)}%"></i></div>
  </article>`;
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
    <div class="goal-row" tabindex="0" data-id="${esc(g.id)}">
      <span class="gr-status ${esc(g.status)}"></span>
      <div class="gr-body">
        <div class="gr-title">${esc(g.title)}</div>
        <div class="gr-sub">${esc(g.description || '没有描述')}</div>
      </div>
      <span class="gr-pct">${pct(g)}%</span>
    </div>`).join('')}</div>`;
}

function render() {
  if (!root) return;
  const list = visible();
  root.innerHTML = `
    <div class="page">
      <header class="page-head">
        <div>
          <h1>目标看板</h1>
          <p class="page-sub">进度由关联任务派生，不靠手填</p>
        </div>
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
      ${list.length
        ? (view === 'kanban' ? kanban(list) : listView(list))
        : emptyState({
            icon: 'trending-up',
            title: '暂无进行中的团队目标',
            sub: '建一个目标，把角色和任务挂上去',
            action: '<button class="btn btn-primary" id="emptyGoal">新建团队目标</button>',
          })}
    </div>`;
  hydrateIcons(root);
  root.querySelectorAll('.kb-body').forEach(b => stagger(b, 35, 220));
  bind();
}

function bind() {
  root.querySelectorAll('#goalFilter button').forEach(b => {
    b.addEventListener('click', () => { filter = b.dataset.f; render(); });
  });
  root.querySelectorAll('#goalView button').forEach(b => {
    b.addEventListener('click', () => { view = b.dataset.v; render(); });
  });
  root.querySelector('#newGoal')?.addEventListener('click', newGoal);
  root.querySelector('#emptyGoal')?.addEventListener('click', newGoal);

  root.querySelectorAll('.goal-card,.goal-row').forEach(el => {
    el.addEventListener('click', () => openDetail(el.dataset.id));
    el.addEventListener('keydown', e => {
      if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); openDetail(el.dataset.id); }
    });
  });

  // 拖拽只在看板视图存在；列表行没有 draggable。
  root.querySelectorAll('.goal-card').forEach(el => {
    el.addEventListener('dragstart', e => {
      e.dataTransfer.setData('text/plain', el.dataset.id);
      e.dataTransfer.effectAllowed = 'move';
      el.classList.add('dragging');
    });
    el.addEventListener('dragend', () => el.classList.remove('dragging'));
  });

  root.querySelectorAll('.kb-col').forEach(col => {
    col.addEventListener('dragover', e => {
      e.preventDefault();
      col.classList.add('drag-over');
    });
    // relatedTarget 还在列里就不撤高亮，否则划过子元素会闪。
    col.addEventListener('dragleave', e => {
      if (!col.contains(e.relatedTarget)) col.classList.remove('drag-over');
    });
    col.addEventListener('drop', async e => {
      e.preventDefault();
      col.classList.remove('drag-over');
      const id = e.dataTransfer.getData('text/plain');
      const g = goals.find(x => x.id === id);
      if (!g || g.status === col.dataset.col) return;
      await patch(id, { status: col.dataset.col });
    });
  });
}

async function patch(id, body) {
  try {
    await api('/v1/goals/' + encodeURIComponent(id), { method: 'PATCH', body });
    await load();
    return true;
  } catch (e) {
    toast('更新失败：' + e.message, { type: 'err' });
    return false;
  }
}

function newGoal() {
  openPanel({
    title: '新建目标',
    subtitle: '创建后落在「待规划」列',
    render: el => {
      el.innerHTML = `
        <label class="field"><span>标题</span>
          <input id="gTitle" maxlength="100" placeholder="比如：园区改造标书交付">
        </label>
        <label class="field"><span>描述</span>
          <textarea id="gDesc" rows="3" placeholder="可选，写清交付物和验收标准"></textarea>
        </label>
        <label class="field"><span>关联房间</span>
          <select id="gConv">
            <option value="">不关联</option>
            ${(state.convs || []).map(c => `<option value="${esc(c.id)}">${esc(c.title)}</option>`).join('')}
          </select>
        </label>
        <button class="btn btn-primary" id="gSave" style="width:100%">创建目标</button>`;

      const save = el.querySelector('#gSave');
      save.addEventListener('click', async () => {
        const title = el.querySelector('#gTitle').value.trim();
        if (!title) { toast('标题必填', { type: 'err' }); return; }
        save.disabled = true;
        try {
          await api('/v1/goals', {
            method: 'POST',
            body: {
              title,
              description: el.querySelector('#gDesc').value.trim(),
              conversationId: el.querySelector('#gConv').value,
            },
          });
          toast('已创建', { type: 'ok' });
          closePanel();
          await load();
        } catch (e) {
          toast('创建失败：' + e.message, { type: 'err' });
          save.disabled = false;
        }
      });
    },
  });
}

function openDetail(id) {
  const g = goals.find(x => x.id === id);
  if (!g) return;
  const conv = (state.convs || []).find(c => c.id === g.conversationId);
  openPanel({
    title: g.title,
    subtitle: `${pct(g)}% · ${g.progressSource === 'manual' ? '手动进度' : '按任务派生'}`,
    render: el => {
      el.innerHTML = `
        ${conv ? `<div class="panel-sec"><h4>关联房间</h4><div class="gd-run">${esc(conv.title)}</div></div>` : ''}
        <div class="panel-sec"><h4>描述</h4>
          <div class="gd-desc">${esc(g.description || '没有描述')}</div>
        </div>
        <div class="panel-sec"><h4>状态</h4>
          <div class="seg" id="gdStatus">${COLS.map(c =>
            `<button class="${g.status === c.key ? 'on' : ''}" data-s="${c.key}">${c.label}</button>`).join('')}</div>
        </div>
        <div class="panel-sec"><h4>关联任务</h4>
          ${(g.runIds || []).length
            ? (g.runIds || []).map(r => `<div class="gd-run"><code>${esc(r)}</code></div>`).join('')
            : '<div class="panel-empty">还没有关联任务</div>'}
        </div>
        <button class="btn btn-line danger" id="gdDel">删除目标</button>`;

      el.querySelectorAll('#gdStatus button').forEach(b => {
        b.addEventListener('click', async () => {
          if (b.classList.contains('on')) return;
          // 状态换完重开面板，让标题、进度和按钮高亮一起刷新。
          if (await patch(g.id, { status: b.dataset.s }) && panelOpen()) openDetail(g.id);
        });
      });

      el.querySelector('#gdDel').addEventListener('click', async () => {
        const yes = await confirmModal({
          title: '删除目标',
          text: '删除后不能恢复，关联的执行任务本身不受影响。',
          okText: '删除',
          danger: true,
        });
        if (!yes) return;
        try {
          await api('/v1/goals/' + encodeURIComponent(g.id), { method: 'DELETE' });
          closePanel();
          toast('已删除', { type: 'ok' });
          await load();
        } catch (e) {
          toast('删除失败：' + e.message, { type: 'err' });
        }
      });
    },
  });
}
