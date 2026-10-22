/* ============================================================
   智团 TeamWise · 长期记忆
   群记忆只属于当前群；角色记忆按「角色 × 用户」跨群保留。
   ============================================================ */

import { $, api, state, toast, esc } from './core.js';
import { uiConfirm } from './dialog.js';

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

function render(data) {
  const conv = data.conversation || [];
  const agents = data.agents || [];
  const totalAgent = agents.reduce((n, a) => n + (a.memories || []).length + (a.relation ? 1 : 0), 0);

  if (!conv.length && !agents.length) {
    $('#memoryBody').innerHTML = `<div class="mem-empty">
      还没有长期记忆。<br>聊够一段之后，他们才会开始记住；群记忆不会带到别的群。
    </div>`;
    return;
  }

  let html = '';
  if (conv.length) {
    html += `<div class="mem-sec">
      <div class="mem-sec-h">
        这个群
        <span class="mem-count">${conv.length} 条</span>
        <button class="mem-act" data-clear-conv>清空群记忆</button>
      </div>
      ${conv.map(item).join('')}
    </div>`;
  }
  for (const a of agents) {
    const ms = a.memories || [];
    html += `<div class="mem-sec">
      <div class="mem-sec-h">
        ${a.characterName || a.characterId}
        <span class="mem-count">${ms.length + (a.relation ? 1 : 0)} 条</span>
        <button class="mem-act" data-clear-agent="${esc(a.characterId || '')}">清空 TA 的记忆</button>
      </div>
      ${a.relation ? `<div class="mem-rel">${esc(a.relation.text)}</div>` : ''}
      ${ms.map(item).join('')}
    </div>`;
  }
  if (!conv.length && !agents.length) {
    html += `<div class="mem-empty">还没有长期记忆。</div>`;
  }
  $('#memoryBody').innerHTML = html;
}

function close() {
  $('#memory').classList.remove('on');
}

async function refresh() {
  if (!state.cur) return;
  const body = $('#memoryBody');
  body.innerHTML = `<div class="mem-empty">读取中…</div>`;
  try {
    const data = await api('/v1/memories?conversationId=' + encodeURIComponent(state.cur.id));
    render(data);
  } catch (e) {
    body.innerHTML = `<div class="mem-empty">${esc(e.message)}</div>`;
  }
}

export async function openMemory() {
  if (!state.cur) return toast('先选一个群');
  $('#memory').classList.add('on');
  await refresh();
}

export function initMemory() {
  $('#memoryBtn')?.addEventListener('click', openMemory);
  $('#memoryClose')?.addEventListener('click', close);
  $('#memory')?.addEventListener('click', e => {
    if (e.target.id === 'memory') close();
  });

  $('#memoryBody')?.addEventListener('click', async e => {
    const convBtn = e.target.closest('[data-clear-conv]');
    const agentBtn = e.target.closest('[data-clear-agent]');
    if (!convBtn && !agentBtn) return;

    if (convBtn) {
      if (!(await uiConfirm('清空这个群的记忆？', '只清空群记忆；角色和你的记忆会保留。', { danger: true, okText: '清空' }))) return;
      try {
        await api('/v1/memories?conversationId=' + encodeURIComponent(state.cur.id), { method: 'DELETE' });
        toast('群记忆已清空');
        await refresh();
      } catch (err) {
        toast(err.message);
      }
      return;
    }

    const id = agentBtn.dataset.clearAgent;
    if (!id) return;
    const name = agentBtn.closest('.mem-sec')?.querySelector('.mem-sec-h')?.textContent?.trim().split(/\s/)[0] || 'TA';
    if (!(await uiConfirm(`清空 ${name} 的记忆？`, '会清空 TA 和你之间的全部记忆（跨群），不可撤销。', { danger: true, okText: '清空' }))) return;
    try {
      await api('/v1/memories?characterId=' + encodeURIComponent(id), { method: 'DELETE' });
      toast('角色记忆已清空');
      await refresh();
    } catch (err) {
      toast(err.message);
    }
  });
}
