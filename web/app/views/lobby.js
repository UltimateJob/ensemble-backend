/* ============================================================
   团队大厅：预设团队卡片 + 我的团队列表
   ============================================================ */

import { $, api, state, esc, icon, hydrateIcons, emptyState, toast } from '../js/core.js';
import { navigate } from '../js/router.js';
import { avatarHTML, coverUrl } from '../js/avatars.js';

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

function agentCount(conv) {
  return (conv.agents?.length) || (conv.members?.length) || 0;
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

  root.innerHTML = `
    <div class="page">
      <header class="page-head">
        <div>
          <h1>团队大厅</h1>
          <p class="page-sub">选一组现成的角色，或者自己组一个</p>
        </div>
        <button class="btn btn-primary" id="newTeam">${icon('plus', 16)}新建自定义团队</button>
      </header>

      <section class="sec">
        <h2 class="sec-h">开箱即用预设团队</h2>
        <div class="team-grid">${presets.map(presetCard).join('')}</div>
      </section>

      <section class="sec">
        <h2 class="sec-h">我的自定义团队</h2>
        ${mine.length
          ? `<div class="team-list">${mine.map(teamRow).join('')}</div>`
          : emptyState({
              icon: 'users',
              title: '还没有自定义团队',
              sub: '从上面的预设开始，或者自己组一个',
              action: '<button class="btn btn-primary" id="emptyNew">新建团队</button>',
            })}
      </section>
    </div>`;

  hydrateIcons(root);
  root.querySelectorAll('[data-join]').forEach(b => {
    b.addEventListener('click', e => {
      e.stopPropagation();
      join(b.dataset.join, b);
    });
  });
  root.querySelector('#newTeam')?.addEventListener('click', openNewTeam);
  root.querySelector('#emptyNew')?.addEventListener('click', openNewTeam);
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
  } catch (e) {
    toast('加入失败：' + e.message, { type: 'err' });
    btn.disabled = false;
    btn.textContent = '加入团队';
  }
}
