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
        <div class="ap-hero">
          ${avatarHTML(agent.name, agent.color, { size: 64 })}
          <div class="ap-hero-body">
            <div class="ap-hero-name">${esc(agent.name)}</div>
            <div class="ap-hero-title">${esc(agent.title || '角色')}</div>
            <div class="ap-hero-state" id="apHeroState">空闲中</div>
          </div>
        </div>

        <section class="panel-sec">
          <h4>当前任务</h4>
          <div id="apTask"><div class="ap-idle">现在没有在跑的任务。派一句指令给他，或者让他在群里接话。</div></div>
        </section>

        <section class="panel-sec">
          <h4>最近产出</h4>
          <div id="apArts"><div class="ap-idle">这个角色还没有产出文件。标书、文案、代码这类交付会出现在这里。</div></div>
        </section>

        <section class="panel-sec">
          <h4>单独发指令</h4>
          <div class="ap-compose">
            <textarea id="apCmd" rows="3" placeholder="只发给 ${esc(agent.name)}，群里其他人不会接这句话…"></textarea>
            <button class="btn btn-primary" id="apSend">发送给 ${esc(agent.name)}</button>
          </div>
          <div class="hint ap-hint">房间绑了工作区时，会以这个角色的身份起一次执行任务；否则作为一条群消息发出。</div>
        </section>`;
      loadCurrent(agent, conv, el);
      el.querySelector('#apSend').addEventListener('click', () => sendDirect(agent, conv, el));
      // Cmd+Enter 直接发
      el.querySelector('#apCmd').addEventListener('keydown', e => {
        if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
          e.preventDefault();
          sendDirect(agent, conv, el);
        }
      });
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
    const hero = el.querySelector('#apHeroState');
    const st = a?.state || 'idle';
    if (hero) {
      // 后端会区分「正在发言」和「正在处理任务」，直接用它的 detail，
      // 不要用前端的状态词覆盖——否则两个状态看起来一样。
      hero.textContent = a?.detail || stateLabel(st);
      hero.className = 'ap-hero-state ' + st;
    }
    // 只有「发言中」而没有 Run 时，任务区应该说明原因，而不是矛盾地说「没有在跑的任务」。
    if (!a || !a.runId) {
      const speaking = st === 'working';
      box.innerHTML = `<div class="ap-idle">${speaking
        ? '他正在群里发言。发言结束后如果有执行任务，会在这里显示进度和工具调用。'
        : '现在没有在跑的任务。派一句指令给他，或者让他在群里接话。'}</div>`;
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
      ? list.map(a => `<button class="ap-art" data-id="${esc(a.id)}">
          <span class="ap-art-ic">${icon('file-text', 15)}</span>
          <span class="ap-art-body">
            <span class="ap-art-t">${esc(a.title)}</span>
            <span class="ap-art-s">${esc((a.summary || '').slice(0, 40))}</span>
          </span>
        </button>`).join('')
      : '<div class="ap-idle">这个角色还没有产出文件。标书、文案、代码这类交付会出现在这里。</div>';
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
