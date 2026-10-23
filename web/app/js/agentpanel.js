/* ============================================================
   Agent 工作面板（右侧常驻）
   当前任务 / 最近产出 / 单独发指令
   ============================================================ */

import { $, api, esc, icon, state, toast } from './core.js';
import { openPanel } from './shell.js';
import { avatarHTML } from './avatars.js';
import { trackRun, onRunEvent } from './runs.js';
import { loadActivity, permissionLabel, stateLabel, ringOf, toolLabel } from './activity.js';
import { workspacePath, openWorkspaceDialog } from './workspace.js';

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
            <div class="ap-hero-perm" id="apHeroPerm"></div>
          </div>
        </div>

        <section class="panel-sec" id="apPermSec" hidden>
          <h4>执行权限</h4>
          <select class="select" id="apPermSel">
            <option value="">跟随群默认</option>
            <option value="ask">逐项批准</option>
            <option value="approval">自动接受编辑</option>
            <option value="full">完全访问</option>
            <option value="readonly">只读</option>
          </select>
          <div class="hint ap-hint">只影响下一次派活；正在跑的任务不改变。</div>
        </section>

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
      // 群主可以单独调这个角色的权限；协作者只读。
      const owner = state.user && conv.userId === state.user.id;
      const sec = el.querySelector('#apPermSec');
      const sel = el.querySelector('#apPermSel');
      if (owner && sec && sel) {
        sec.hidden = false;
        sel.value = conv.agentPermissions?.[agent.key] || '';
        sel.addEventListener('change', async () => {
          try {
            const next = { ...(conv.agentPermissions || {}) };
            if (sel.value) next[agent.key] = sel.value; else delete next[agent.key];
            await api('/v1/conversations/' + encodeURIComponent(conv.id), {
              method: 'PATCH', body: { agentPermissions: next },
            });
            conv.agentPermissions = next;
            toast('已更新，下一次派活生效', { type: 'ok' });
            loadCurrent(agent, conv, el);
          } catch (e) { toast(e.message, { type: 'err' }); }
        });
      }
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
    const act = await loadActivity(conv.id);
    const a = (act.agents || []).find(x => x.key === agent.key);
    const hero = el.querySelector('#apHeroState');
    const permEl = el.querySelector('#apHeroPerm');
    const st = a?.state || 'idle';
    if (hero) {
      hero.textContent = a ? stateLabel(a) : '空闲中';
      hero.className = 'ap-hero-state ' + st;
    }
    if (permEl) {
      const perm = a?.permission || conv.defaultPermission || agent.permission || '';
      permEl.innerHTML = perm
        ? `<span class="perm-badge p-${esc(perm)}">${esc(permissionLabel(perm))}</span>`
        : '<span class="hint">未设置执行权限</span>';
    }
    if (!a || !a.runId) {
      box.innerHTML = `<div class="ap-idle">${st === 'running'
        ? '他正在群里发言。发言结束后如果有执行任务，会在这里显示进度和工具调用。'
        : '现在没有在跑的任务。点输入框旁的「派活」派一句指令，或者让他在群里接话。'}</div>`;
      return;
    }
    trackRun(a.runId);
    const run = await api('/v1/runs/' + a.runId);
    const calls = (run.toolCalls || []).slice(-6);
    box.innerHTML = `
      <div class="ap-run">
        <div style="display:flex;align-items:center;gap:8px;margin-bottom:6px">
          ${avatarHTML(agent.name, agent.color, { size: 22, ring: ringOf(a.state) })}
          <b style="font-size:13px">${esc(run.task || '执行中')}</b>
        </div>
        <div class="hint">第 ${run.step}/${run.maxSteps} 步 · ${esc(statusLabel(run.status))}
          ${run.artifacts?.length ? ' · ' + run.artifacts.length + ' 个产物' : ''}</div>
        ${a.pendingApproval ? `<div class="ap-approval">等你审批：${esc(a.pendingApproval.summary || a.pendingApproval.command || '')}</div>` : ''}
        <div class="timeline" style="margin-top:10px">
          ${calls.map(t => `
            <div class="tl ${t.status === 'error' || t.status === 'denied' ? 'err' : ''}">
              <div class="tl-h">${esc(toolLabel(t.tool))}<span class="tl-badge ${esc(t.risk || '')}">${esc(t.risk || '')}</span></div>
            </div>`).join('') || '<div class="tl-dim">还没有工具调用</div>'}
        </div>
        <div style="display:flex;gap:8px;margin-top:10px">
          <button class="btn btn-line btn-sm" id="apOpen" style="flex:1">完整时间线</button>
          <button class="btn btn-line btn-sm danger" id="apStop">停止</button>
        </div>
      </div>`;
    box.querySelector('#apOpen')?.addEventListener('click', () => {
      location.hash = '#/room/' + encodeURIComponent(conv.id) + '?view=studio';
    });
    box.querySelector('#apStop')?.addEventListener('click', async () => {
      try {
        await api('/v1/runs/' + encodeURIComponent(a.runId) + '/abort', { method: 'POST' });
        toast('已请求停止', { type: 'info' });
      } catch (e) { toast(e.message, { type: 'err' }); }
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
  if (!workspacePath(conv)) {
    openWorkspaceDialog(conv, {});
    toast('先绑定工作区，才能单独派活', { type: 'info' });
    return;
  }
  const btn = el.querySelector('#apSend');
  btn.disabled = true;
  try {
    const resp = await api('/v1/conversations/' + encodeURIComponent(conv.id) + '/execute', {
      method: 'POST',
      body: { content: text, agentKeys: [agent.key] },
    });
    const r = (resp.runs || [])[0];
    if (!r || r.error) throw new Error(r?.error || '没有创建执行任务');
    const { mountRun } = await import('./exec.js');
    mountRun(conv, {
      runId: r.runId, agentKey: agent.key, agentName: agent.name, permission: r.permission, task: text,
    });
    toast('已派给 ' + agent.name, { type: 'ok' });
    ta.value = '';
    loadCurrent(agent, conv, el);
  } catch (e) {
    toast(e.message, { type: 'err' });
  } finally {
    btn.disabled = false;
  }
}

function statusLabel(s) {
  return ({ running: '执行中', waiting: '等审批', waiting_approval: '等审批', done: '已完成', failed: '失败', aborted: '已中止', cancelled: '已取消' })[s] || s;
}
