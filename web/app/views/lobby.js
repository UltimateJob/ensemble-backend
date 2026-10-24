/* ============================================================
   团队大厅：预设团队卡片 + 我的团队列表
   ============================================================ */

import { $, api, state, esc, icon, hydrateIcons, emptyState, toast } from '../js/core.js';
import { navigate } from '../js/router.js';
import { avatarHTML, coverUrl } from '../js/avatars.js';
import { stagger } from '../js/motion.js';
import { openModal, closeModal, confirmModal } from '../js/modal.js';
import { renderNav } from '../js/shell.js';

// 大厅展示的四个预设：三个工作组 + 草帽团。
// 「二次元茶话会」是随机抽人的场景，放进大厅会让人以为角色是固定的；
// 研报、日常陪聊同理，都留给「新建」入口。
const PRESET = ['bid', 'xhs', 'code', 'onepiece'];

let root = null;
let onConvChange = null;

export function mount(host) {
  root = host;
  render();
  onConvChange = () => render();
  document.addEventListener('conv:change', onConvChange);
}

export function unmount() {
  if (onConvChange) document.removeEventListener('conv:change', onConvChange);
  onConvChange = null;
  root = null;
}

function coverHTML(sc) {
  const url = coverUrl(sc.key);
  if (url) return `<div class="team-cover"><img src="${url}" alt="" loading="lazy"></div>`;
  return `<div class="team-cover team-cover--empty">${icon(sc.icon || 'users', 28)}</div>`;
}

function presetCard(sc) {
  const agents = (sc.agents || []).slice(0, 5);
  return `<article class="team-card" data-key="${esc(sc.key)}">
    ${coverHTML(sc)}
    <div class="team-body">
      <h3>${esc(sc.name)}</h3>
      <p>${esc(sc.tagline || '')}</p>
      <div class="team-agents">
        ${agents.map(a => avatarHTML(a.name, a.color, { size: 26 })).join('')}
        ${sc.random ? '<span class="team-rand">开局随机</span>' : ''}
      </div>
    </div>
    <div class="team-foot">
      <button class="btn btn-primary" data-join="${esc(sc.key)}">加入团队</button>
      <span class="tf-hint">${agents.length || 0} 个角色</span>
    </div>
  </article>`;
}

// 用户保存的模板卡：外观和预设一致，但不是场景，用模板 id 建群。
function templateCard(t) {
  const agents = (t.agents || []).slice(0, 5);
  // 和预设卡保持同一套骨架（封面 + 正文 + 底栏），
  // 否则两块并排时高度参差，看着像半成品。
  return `<article class="team-card tpl-card" data-tpl="${esc(t.id)}">
    <div class="tpl-cover">${icon('bookmark', 26)}</div>
    <div class="team-body">
      <h3>${esc(t.name)}</h3>
      <p>${esc(t.tagline || '自定义模板')}</p>
      <div class="team-agents">
        ${agents.map(a => avatarHTML(a.name, a.color, { size: 26 })).join('')}
      </div>
    </div>
    <div class="team-foot">
      <button class="btn btn-primary" data-usetpl="${esc(t.id)}">用这个模板</button>
      <button class="icon-btn tpl-del" data-deltpl="${esc(t.id)}" title="删除模板">${icon('trash', 14)}</button>
      <span class="tf-hint">${agents.length || 0} 个角色</span>
    </div>
  </article>`;
}

function scName(conv) {
  const sc = state.scenarios.find(s => s.key === conv.studio);
  return sc ? sc.name : '自定义';
}

function scColor(conv) {
  const sc = state.scenarios.find(s => s.key === conv.studio);
  return sc?.accent || conv.agents?.[0]?.color || '#367BED';
}

// 预设场景的群不把角色写进会话：角色来自场景定义。
// 只看 agents/members 会显示「0 个角色」，和实际不符。
function agentCount(conv) {
  const own = (conv.agents?.length) || (conv.members?.length) || 0;
  if (own) return own;
  const sc = state.scenarios.find(x => x.key === conv.studio);
  return sc?.agents?.length || 0;
}

function teamRow(conv) {
  return `<div class="team-row">
    ${avatarHTML(conv.title, scColor(conv), { size: 36 })}
    <div class="tr-body">
      <div class="tr-name">${esc(conv.title)}</div>
      <div class="tr-meta">${agentCount(conv)} 个角色 · ${esc(scName(conv))}</div>
    </div>
    <a class="btn btn-line" href="#/room/${encodeURIComponent(conv.id)}">进入房间</a>
  </div>`;
}

function render() {
  if (!root) return;
  const presets = PRESET
    .map(k => state.scenarios.find(s => s.key === k))
    .filter(Boolean);
  const mine = state.convs || [];
  // 用户自己存的模板。之前存完就找不到了——大厅只渲染固定四个预设，
  // toast 说「可在变体里换人」，但没有任何地方能看到它。
  const templates = state.studios || [];

  root.innerHTML = `
    <div class="page">
      <header class="page-head">
        <div>
          <h1>团队大厅</h1>
          <p class="page-sub">选一组现成的角色，或者自己组一个</p>
        </div>
        <button class="btn btn-line" id="soloBtn">${icon('message-circle', 16)}和一位角色单聊</button>
        <button class="btn btn-primary" id="newTeam">${icon('plus', 16)}新建自定义团队</button>
      </header>

      ${mine.length ? `
      <section class="sec">
        <h2 class="sec-h">我的团队</h2>
        <div class="team-list">${mine.map(teamRow).join('')}</div>
      </section>` : ''}

      ${templates.length ? `
      <section class="sec">
        <h2 class="sec-h">我的模板</h2>
        <div class="team-grid">${templates.map(templateCard).join('')}</div>
      </section>` : ''}

      <section class="sec">
        <h2 class="sec-h">开箱即用预设团队</h2>
        <div class="team-grid">${presets.map(presetCard).join('')}</div>
      </section>

      ${mine.length ? '' : `
      <section class="sec">
        <h2 class="sec-h">我的团队</h2>
        ${emptyState({
          icon: 'users',
          title: '还没有自定义团队',
          sub: '从上面的预设开始，或者自己组一个',
          action: '<button class="btn btn-primary" id="emptyNew">新建团队</button>',
        })}
      </section>`}
    </div>`;

  hydrateIcons(root);
  // 卡片依次浮现，而不是四张同时出现
  stagger(root.querySelector('.team-grid'));
  stagger(root.querySelector('.team-list'));
  root.querySelectorAll('[data-join]').forEach(b => {
    b.addEventListener('click', e => {
      e.stopPropagation();
      join(b.dataset.join, b);
    });
  });
  root.querySelectorAll('[data-usetpl]').forEach(b => {
    b.addEventListener('click', async e => {
      e.stopPropagation();
      const tpl = (state.studios || []).find(t => t.id === b.dataset.usetpl);
      if (!tpl) return;
      b.disabled = true;
      try {
        const conv = await api('/v1/conversations', {
          method: 'POST',
          body: { title: tpl.name, studio: 'custom', agents: tpl.agents || [], tagline: tpl.tagline || '' },
        });
        const list = await api('/v1/conversations').catch(() => []);
        state.convs = list || state.convs;
        renderNav(state.convs);
        location.hash = '#/room/' + encodeURIComponent(conv.id);
      } catch (err) {
        toast('创建失败：' + (err.message || ''), { type: 'err' });
        b.disabled = false;
      }
    });
  });
  root.querySelectorAll('[data-deltpl]').forEach(b => {
    b.addEventListener('click', async e => {
      e.stopPropagation();
      const yes = await confirmModal({
        title: '删除这个模板？',
        text: '只是模板，用它建的群不受影响。',
        okText: '删除', danger: true,
      });
      if (!yes) return;
      try {
        await api('/v1/studios/' + encodeURIComponent(b.dataset.deltpl), { method: 'DELETE' });
        state.studios = (state.studios || []).filter(t => t.id !== b.dataset.deltpl);
        render();
      } catch (err) {
        toast('删除失败：' + (err.message || ''), 'err');
      }
    });
  });
  root.querySelector('#newTeam')?.addEventListener('click', openNewTeam);
  root.querySelector('#emptyNew')?.addEventListener('click', openNewTeam);
  root.querySelector('#soloBtn')?.addEventListener('click', openSolo);
}

/* ---------------- 单聊：一个房间只有你和一位角色 ---------------- */

async function openSolo() {
  const code = state.scenarios.find(s => s.key === 'code');
  const quick = code?.agents || [];
  const mask = openModal({
    title: '和一位角色单聊',
    width: 620,
    body: `
      <p class="hint" style="margin-bottom:14px">
        只有你和这一位角色。适合简单任务和快速迭代；复杂任务再交给团队。
        绑定本地工作区后，可以直接让它读代码、改文件、跑测试。
      </p>
      <div class="solo-h">代码角色</div>
      <div class="solo-grid" id="soloQuick">
        ${quick.map(a => `<button class="solo-card" data-key="${esc(a.key)}">
          ${avatarHTML(a.name, a.color, { size: 38 })}
          <span class="sc-body"><b>${esc(a.name)}</b><em>${esc(a.title || '')}</em></span>
          ${icon('chevron-right', 14)}
        </button>`).join('')}
      </div>
      <div class="solo-h" style="margin-top:18px">从角色库选</div>
      <div class="solo-grid" id="soloLib"><div class="ap-idle">正在加载角色库…</div></div>`,
    actions: [{ key: 'close', label: '关闭', kind: 'line' }],
  });
  mask?.querySelectorAll('#soloQuick [data-key]').forEach(b => {
    b.addEventListener('click', () => {
      const a = quick.find(x => x.key === b.dataset.key);
      closeModal();
      createSolo({ studio: 'code', member: a.key, name: a.name });
    });
  });
  try {
    const list = await api('/v1/characters?q=');
    const box = mask?.querySelector('#soloLib');
    if (!box) return;
    const picks = (list || []).slice(0, 12);
    box.innerHTML = picks.length ? picks.map(c => `
      <button class="solo-card" data-name="${esc(c.name)}">
        ${avatarHTML(c.name, '', { size: 38 })}
        <span class="sc-body"><b>${esc(c.name)}</b><em>${esc(c.work || '')}</em></span>
        ${icon('chevron-right', 14)}
      </button>`).join('') : '<div class="ap-idle">角色库还是空的。可以先用上面的代码角色。</div>';
    box.querySelectorAll('[data-name]').forEach(b => b.addEventListener('click', () => {
      const c = picks.find(x => x.name === b.dataset.name);
      if (!c) return;
      closeModal();
      createSolo({
        studio: 'custom',
        agent: {
          key: 'lib_' + c.name, characterId: 'lib:' + c.name,
          name: c.name, title: (c.tags || []).join(' / '), emoji: [...c.name][0],
          color: '', prompt: c.tone || '',
        },
      });
    }));
  } catch (e) {
    const box = mask?.querySelector('#soloLib');
    if (box) box.innerHTML = '<div class="ap-idle">角色库加载失败，不影响上面的代码角色。</div>';
  }
}

async function createSolo({ studio, member, name, agent }) {
  try {
    const body = {
      title: `和「${name || agent?.name}」单聊`,
      studio,
      // 默认最安全的逐项批准；需要更顺滑时在房间顶栏切。
      defaultPermission: 'ask',
    };
    if (member) body.members = [member];
    if (agent) body.agents = [agent];
    const conv = await api('/v1/conversations', { method: 'POST', body });
    state.convs = await api('/v1/conversations').catch(() => state.convs);
    document.dispatchEvent(new CustomEvent('conv:change'));
    navigate('#/room/' + encodeURIComponent(conv.id));
  } catch (e) {
    toast('创建失败：' + e.message, { type: 'err' });
  }
}

async function openNewTeam() {
  try {
    const m = await import('./newteam.js');
    m.openNewTeam();
  } catch (e) {
    toast('新建团队面板还没准备好：' + e.message, { type: 'err' });
  }
}

async function join(key, btn) {
  const sc = state.scenarios.find(s => s.key === key);
  if (!sc) return;
  btn.disabled = true;
  btn.textContent = '创建中…';
  try {
    const conv = await api('/v1/conversations', { method: 'POST', body: { title: sc.name, studio: key } });
    state.convs = await api('/v1/conversations');
    document.dispatchEvent(new CustomEvent('conv:change'));
    navigate('#/room/' + encodeURIComponent(conv.id));
    // 建完就让角色自己聊起来：进来时群里是活的，而不是一间静音的空房。
    // 延迟到房间挂载完成后再开场，否则 kickoff 找不到会话状态。
    setTimeout(async () => {
      if (state.cur?.id !== conv.id) return;
      const { kickoff } = await import('../js/chat.js');
      kickoff(conv.id);
    }, 600);
  } catch (e) {
    toast('加入失败：' + e.message, { type: 'err' });
    btn.disabled = false;
    btn.textContent = '加入团队';
  }
}
