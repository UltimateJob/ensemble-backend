/* ============================================================
   长期记忆：群记忆只属于这个群；角色记忆按「角色 × 用户」跨群保留。
   新壳里显示在右侧工作面板，不再用独立弹层。
   ============================================================ */

import { api, state, toast, esc } from './core.js';
import { openPanel } from './shell.js';
import { confirmModal } from './modal.js';

const KIND = {
  episode: '事件',
  fact: '记得',
  promise: '答应过',
  open_thread: '没聊完',
  relation: '关系',
};

function dateStr(s) {
  const d = new Date(s);
  if (isNaN(d)) return '';
  return `${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
}

function item(m) {
  return `<div class="mem-item">
    <span class="mem-date">${dateStr(m.occurredAt)}</span>
    <span class="mem-kind">${KIND[m.kind] || '记忆'}</span>
    <span>${esc(m.text)}</span>
  </div>`;
}

function bodyHTML(data) {
  const conv = data.conversation || [];
  const agents = data.agents || [];

  if (!conv.length && !agents.some(a => (a.memories || []).length || a.relation)) {
    return '<div class="ap-idle">还没有长期记忆。聊够一段之后，他们才会开始记住；群记忆不会带到别的群。</div>';
  }

  let html = '';
  if (conv.length) {
    html += `<div class="panel-sec">
      <h4>这个群 · ${conv.length} 条
        <button class="btn btn-line" data-clear-conv style="float:right;height:24px;font-size:11px;padding:0 8px">清空</button>
      </h4>
      ${conv.map(item).join('')}
    </div>`;
  }
  for (const a of agents) {
    const ms = a.memories || [];
    if (!ms.length && !a.relation) continue;
    html += `<div class="panel-sec">
      <h4>${esc(a.characterName || a.characterId)} · ${ms.length + (a.relation ? 1 : 0)} 条
        <button class="btn btn-line" data-clear-agent="${esc(a.characterId || '')}" style="float:right;height:24px;font-size:11px;padding:0 8px">清空</button>
      </h4>
      ${a.relation ? `<div class="mem-rel">${esc(a.relation.text)}</div>` : ''}
      ${ms.map(item).join('')}
    </div>`;
  }
  return html;
}

async function refresh(el) {
  if (!state.cur) return;
  el.innerHTML = '<div class="ap-idle">读取中…</div>';
  try {
    const data = await api('/v1/memories?conversationId=' + encodeURIComponent(state.cur.id));
    el.innerHTML = bodyHTML(data);
  } catch (e) {
    el.innerHTML = `<div class="ap-idle">${esc(e.message)}</div>`;
  }
}

export async function openMemory() {
  if (!state.cur) { toast('先选一个群', { type: 'info' }); return; }
  openPanel({
    title: '他们记得',
    subtitle: '群记忆只属于这个群；角色记得你的事，换群也保留',
    render: el => {
      refresh(el);
      el.addEventListener('click', async e => {
        const convBtn = e.target.closest('[data-clear-conv]');
        const agentBtn = e.target.closest('[data-clear-agent]');
        if (!convBtn && !agentBtn) return;

        if (convBtn) {
          const yes = await confirmModal({
            title: '清空这个群的记忆？',
            text: '只清空群记忆；角色和你的记忆会保留。',
            okText: '清空', danger: true,
          });
          if (!yes) return;
          try {
            await api('/v1/memories?conversationId=' + encodeURIComponent(state.cur.id), { method: 'DELETE' });
            toast('群记忆已清空', { type: 'ok' });
            refresh(el);
          } catch (err) { toast(err.message, { type: 'err' }); }
          return;
        }

        const id = agentBtn.dataset.clearAgent;
        if (!id) return;
        const name = agentBtn.closest('.panel-sec')?.querySelector('h4')?.textContent?.trim().split(/\s|·/)[0] || 'TA';
        const yes = await confirmModal({
          title: `清空 ${name} 的记忆？`,
          text: '会清空 TA 和你之间的全部记忆（跨群），不可撤销。',
          okText: '清空', danger: true,
        });
        if (!yes) return;
        try {
          await api('/v1/memories?characterId=' + encodeURIComponent(id), { method: 'DELETE' });
          toast('角色记忆已清空', { type: 'ok' });
          refresh(el);
        } catch (err) { toast(err.message, { type: 'err' }); }
      });
    },
  });
}

export function initMemory() {
  // 兼容旧入口：新壳的房间顶栏直接绑 openMemory。
  window.__openMemory = openMemory;
}
