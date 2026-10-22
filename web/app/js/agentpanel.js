/* ============================================================
   Agent 工作面板（右侧常驻）
   当前任务 / 最近产出 / 单独发指令
   ============================================================ */

import { $, api, esc, icon, state, toast } from './core.js';
import { openPanel } from './shell.js';
import { avatarHTML } from './avatars.js';
import { trackRun, onRunEvent } from './runs.js';

let offRun = null;

export function openAgentPanel(agent, conv) {
  if (!agent || !conv) return;
  if (offRun) { offRun(); offRun = null; }

  openPanel({
    title: agent.name,
    subtitle: agent.title || '角色工作面板',
    render: el => {
      el.innerHTML = `
        <div style="display:flex;justify-content:center;padding:4px 0 18px">
          ${avatarHTML(agent.name, agent.color, { size: 56 })}
        </div>
        <section class="panel-sec">
          <h4>当前任务</h4>
          <div id="apTask"><div class="panel-empty">还没有在跑的任务</div></div>
        </section>
        <section class="panel-sec">
          <h4>最近产出</h4>
          <div id="apArts"><div class="panel-empty">暂无产出</div></div>
        </section>
        <section class="panel-sec">
          <h4>单独发指令</h4>
          <textarea id="apCmd" class="ap-cmd" rows="3" placeholder="只发给 ${esc(agent.name)}…"></textarea>
          <button class="btn btn-primary" id="apSend" style="width:100%;margin-top:8px">发送</button>
          <div class="hint" style="margin-top:8px">房间绑了工作区时，会以这个角色的身份起一次执行任务；否则作为一条群消息发出。</div>
        </section>`;
      loadCurrent(agent, conv, el);
      el.querySelector('#apSend').addEventListener('click', () => sendDirect(agent, conv, el));
    },
  });

  // Run 事件到达时刷新「当前任务」
  offRun = onRunEvent(() => loadCurrent(agent, conv, $('#panelContent')));
}

async function loadCurrent(agent, conv, el) {
  const box = el?.querySelector('#apTask');
  if (!box) return;
  try {
    const acts = await api('/v1/conversations/' + conv.id + '/activity');
    const a = (acts || []).find(x => x.key === agent.key);
    if (!a || !a.runId) {
      box.innerHTML = `<div class="panel-empty">${stateLabel(a?.state)}</div>`;
      return;
    }
    trackRun(a.runId);
    const run = await api('/v1/runs/' + a.runId);
    const calls = (run.toolCalls || []).slice(-6);
    box.innerHTML = `
      <div class="ap-run">
        <div style="display:flex;align-items:center;gap:8px;margin-bottom:6px">
          ${avatarHTML(agent.name, agent.color, { size: 22, ring: a.state })}
          <b style="font-size:13px">${esc(run.task || '执行中')}</b>
        </div>
        <div class="hint">第 ${run.step}/${run.maxSteps} 步 · ${esc(statusLabel(run.status))}</div>
        <div class="timeline" style="margin-top:10px">
          ${calls.map(t => `
            <div class="tl ${t.status === 'error' || t.status === 'denied' ? 'err' : ''}">
              <div class="tl-h">${esc(toolName(t.tool))}<span class="tl-badge ${esc(t.risk || '')}">${esc(t.risk || '')}</span></div>
            </div>`).join('') || '<div class="tl-dim">还没有工具调用</div>'}
        </div>
        <button class="btn btn-line" id="apOpen" style="width:100%;margin-top:10px">展开完整时间线</button>
      </div>`;
    box.querySelector('#apOpen')?.addEventListener('click', () => {
      location.hash = '#/room/' + encodeURIComponent(conv.id) + '?view=studio';
    });
  } catch (e) {
    box.innerHTML = '<div class="panel-empty">读取执行状态失败</div>';
  }

  try {
    const arts = await api('/v1/library?convId=' + encodeURIComponent(conv.id) + '&limit=10');
    const list = (arts || []).filter(a => a.agentKey === agent.key || a.agentName === agent.name);
    const ab = el.querySelector('#apArts');
    if (!ab) return;
    ab.innerHTML = list.length
      ? list.map(a => `<button class="art-item" data-id="${esc(a.id)}">${icon('file-text', 14)}<span class="art-n">${esc(a.title)}</span></button>`).join('')
      : '<div class="panel-empty">这个角色还没有产出</div>';
    ab.querySelectorAll('[data-id]').forEach(b => b.addEventListener('click', async () => {
      try {
        const a = await api('/v1/library/' + b.dataset.id);
        const { openPanel: op } = await import('./shell.js');
        op({
          title: a.title || '产物',
          subtitle: a.agentName || '',
          render: c => { c.innerHTML = `<pre class="art-pre">${esc(a.content || '')}</pre>`; },
        });
      } catch (e) { toast('打不开：' + e.message, { type: 'err' }); }
    }));
  } catch (e) { /* 资料库未启用 */ }
}

async function sendDirect(agent, conv, el) {
  const ta = el.querySelector('#apCmd');
  const text = ta.value.trim();
  if (!text) { toast('先写一句指令', { type: 'err' }); return; }
  const btn = el.querySelector('#apSend');
  btn.disabled = true;
  try {
    const list = await api('/v1/workspaces');
    const ws = (list || []).find(w => w.conversationId === conv.id);
    if (!ws) {
      await api(`/v1/conversations/${conv.id}/messages`, { method: 'POST', body: { content: `@${agent.name} ${text}` } });
      toast('已作为群消息发出（这个房间还没绑工作区）', { type: 'info' });
      ta.value = '';
      return;
    }
    const run = await api(`/v1/workspaces/${ws.id}/runs`, {
      method: 'POST',
      body: { task: text, operatorKey: agent.key, permission: 'approval', maxSteps: 12, autoReview: true },
    });
    trackRun(run.id);
    toast('已派给 ' + agent.name, { type: 'ok' });
    ta.value = '';
    loadCurrent(agent, conv, el);
  } catch (e) {
    toast(e.message, { type: 'err' });
  } finally {
    btn.disabled = false;
  }
}

function stateLabel(s) {
  return ({ working: '正在执行任务', done: '刚完成一次任务', blocked: '等你审批', idle: '空闲中' })[s] || '空闲中';
}

function statusLabel(s) {
  return ({ running: '执行中', waiting_approval: '等审批', done: '已完成', failed: '失败', aborted: '已中止', cancelled: '已取消' })[s] || s;
}

function toolName(t) {
  return ({ read: '读文件', write: '写文件', edit: '改文件', list: '列目录', glob: '找文件', grep: '搜内容', run: '跑命令', docx_create: '生成 Word', xlsx_create: '生成 Excel' })[t] || t;
}
