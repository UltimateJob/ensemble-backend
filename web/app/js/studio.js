/* ============================================================
   合奏 Ensemble · 代码工作室
   工作区绑定 / 任务执行 / 工具时间线 / 审批 / diff / 回滚
   ============================================================ */

import { $, api, state, toast, esc, icon, hydrateIcons } from './core.js';

let wsId = null;        // 当前绑定的工作区
let runId = null;       // 当前运行
let abort = null;       // 中止事件流的函数
const runs = new Map(); // runId -> { toolCalls, diffs }

/** 桌面端注入的原生目录选择回调；浏览器里不存在。 */
window.__ensemblePickDirectory = path => {
  const inp = $('#wsRoot');
  if (inp) {
    inp.value = path;
    toast('已选择：' + path);
  }
};

export function openStudio() {
  $('#studio').classList.add('on');
  refreshWorkspaces();
  hydrateIcons($('#studio'));
}

export function closeStudio() {
  $('#studio').classList.remove('on');
  if (abort) { abort(); abort = null; }
}

async function refreshWorkspaces() {
  try {
    const list = await api('/v1/workspaces');
    renderWorkspaceList(list || []);
    if (!wsId && list && list.length) {
      wsId = list[0].id;
      $('#wsRoot').value = list[0].root;
      renderRunHistory(list[0].id);
    }
  } catch (e) {
    toast('读取工作区失败：' + e.message);
  }
}

function renderWorkspaceList(list) {
  const box = $('#wsList');
  if (!box) return;
  if (!list.length) {
    box.innerHTML = '<div class="ws-empty">还没有工作区。填一个本机目录，点「开始执行」会先绑定。</div>';
    return;
  }
  box.innerHTML = list.map(w => `
    <button class="ws-item${w.id === wsId ? ' on' : ''}" data-id="${esc(w.id)}" data-root="${esc(w.root)}">
      <i data-ic="folder" data-size="14"></i>
      <span class="ws-path">${esc(w.root)}</span>
      <span class="ws-label">${esc(w.label || '')}</span>
    </button>`).join('');
  box.querySelectorAll('.ws-item').forEach(b => {
    b.addEventListener('click', () => {
      wsId = b.dataset.id;
      $('#wsRoot').value = b.dataset.root;
      box.querySelectorAll('.ws-item').forEach(x => x.classList.toggle('on', x === b));
      renderRunHistory(wsId);
    });
  });
  hydrateIcons(box);
}

async function bindIfNeeded() {
  const root = $('#wsRoot').value.trim();
  if (!root) throw new Error('先选择或填写一个本机目录');
  if (wsId) {
    const cur = await api('/v1/workspaces');
    const hit = (cur || []).find(w => w.id === wsId);
    if (hit && hit.root === root) return wsId;
  }
  const w = await api('/v1/workspaces', { method: 'POST', body: { root } });
  wsId = w.id;
  refreshWorkspaces();
  return wsId;
}

export async function startRun() {
  const task = $('#runTask').value.trim();
  if (!task) { toast('先写一句要做什么'); return; }
  if (runId) { toast('上一轮还在跑'); return; }
  const btn = $('#runStart');
  btn.disabled = true;
  try {
    const id = await bindIfNeeded();
    const permission = $('#runPerm').value;
    const run = await api(`/v1/workspaces/${id}/runs`, {
      method: 'POST',
      body: { task, permission, maxSteps: 20 },
    });
    runId = run.id;
    runs.set(run.id, { toolCalls: [], diffs: [] });
    $('#runTimeline').innerHTML = '';
    addTimeline('调度', `任务已提交 · 权限 <b>${esc(permission)}</b>`, 'info');
    subscribe(run.id);
  } catch (e) {
    addTimeline('失败', esc(e.message), 'err');
    btn.disabled = false;
  }
}

function subscribe(id) {
  const ctrl = new AbortController();
  abort = () => ctrl.abort();
  (async () => {
    const res = await fetch(`/v1/runs/${id}/events`, {
      headers: { Authorization: 'Bearer ' + state.token },
      signal: ctrl.signal,
    });
    if (!res.ok || !res.body) throw new Error('HTTP ' + res.status);
    const reader = res.body.getReader();
    const decoder = new TextDecoder();
    let buf = '';
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      buf += decoder.decode(value, { stream: true });
      const parts = buf.split('\n\n');
      buf = parts.pop();
      for (const part of parts) {
        const line = part.trim();
        if (!line.startsWith('data:')) continue;
        let ev;
        try { ev = JSON.parse(line.slice(5).trim()); } catch (e) { continue; }
        handleEvent(ev);
      }
    }
  })().catch(e => {
    if (e.name !== 'AbortError') addTimeline('事件流中断', esc(e.message), 'err');
    finishUi();
  });
}

function handleEvent(ev) {
  const d = ev.data || {};
  switch (ev.type) {
    case 'run.start':
      addTimeline('开始', `工作区 <code>${esc(d.root || '')}</code> · 权限 ${esc(d.permission || '')}<br><span class="dim">${esc(d.note || '')}</span>`, 'info');
      break;
    case 'step.start':
      addTimeline(`第 ${d.step}/${d.maxSteps} 步`, '', 'step');
      break;
    case 'assistant.message':
      addTimeline('说明', esc(d.text || ''), 'say');
      break;
    case 'tool.start':
      addTool(d.toolCallId, d.tool, d.args, d.risk);
      break;
    case 'tool.end':
      endTool(d);
      break;
    case 'approval.required':
      addApproval(d);
      break;
    case 'diff':
      addTimeline('改动', `${d.files} 个文件，共 ${d.total} 处`, 'info');
      break;
    case 'run.end':
      addTimeline('结束', `状态 <b>${esc(d.status)}</b>${d.error ? ' · ' + esc(d.error) : ''} · 第 ${d.step} 步 · token ${d.tokens ? d.tokens.in + '/' + d.tokens.out : '-'}`, d.status === 'done' ? 'ok' : 'err');
      loadDiff(runId);
      finishUi();
      break;
    case 'error':
      addTimeline('错误', esc(d.message || JSON.stringify(d)), 'err');
      break;
  }
}

function addTimeline(title, html, kind) {
  const box = $('#runTimeline');
  const el = document.createElement('div');
  el.className = 'tl ' + (kind || '');
  el.innerHTML = `<div class="tl-h"><span class="tl-t">${title}</span></div>${html ? `<div class="tl-b">${html}</div>` : ''}`;
  box.appendChild(el);
  box.scrollTop = box.scrollHeight;
  return el;
}

function addTool(id, tool, args, risk) {
  const el = addTimeline(toolName(tool) + ` <span class="tl-badge ${esc(risk)}">${esc(risk || '')}</span>`, `<code class="tl-args">${esc(args || '')}</code>`, 'tool');
  el.dataset.toolCall = id;
}

function endTool(d) {
  const el = document.querySelector(`.tl[data-tool-call="${d.toolCallId}"]`);
  if (!el) return;
  const body = el.querySelector('.tl-b');
  const status = d.status === 'ok' ? 'ok' : (d.status === 'denied' ? 'denied' : 'err');
  el.classList.add(status);
  let out = d.output || '';
  if (out.length > 900) out = out.slice(0, 900) + '…';
  const extra = [];
  if (typeof d.exitCode === 'number') extra.push('exit ' + d.exitCode);
  if (d.durationMs != null) extra.push(d.durationMs + 'ms');
  body.innerHTML = `${extra.length ? `<span class="tl-dim">${extra.join(' · ')}</span>` : ''}<pre class="tl-out">${esc(out)}</pre>`;
  body.querySelectorAll('pre').forEach(p => { p.textContent = out; });
}

function addApproval(a) {
  const el = addTimeline('需要批准', `
    <div class="apv">
      <div class="apv-sum">${esc(a.summary || a.tool)}</div>
      ${a.command ? `<code class="apv-cmd">${esc(a.command)}</code>` : ''}
      <div class="apv-risk">风险：${esc(a.risk || '')}</div>
      <div class="apv-btns">
        <button class="btn btn-grad" data-act="yes">允许一次</button>
        <button class="btn btn-line" data-act="run">本任务都允许</button>
        <button class="btn btn-line" data-act="no">拒绝</button>
      </div>
    </div>`, 'approval');
  el.querySelectorAll('button').forEach(b => {
    b.addEventListener('click', async () => {
      try {
        await api(`/v1/runs/${runId}/approve`, {
          method: 'POST',
          body: { approvalId: a.id, approve: b.dataset.act !== 'no', scope: b.dataset.act === 'run' ? 'run' : 'once' },
        });
        el.querySelectorAll('button').forEach(x => { x.disabled = true; });
        el.classList.add(b.dataset.act === 'no' ? 'err' : 'ok');
      } catch (e) {
        toast('审批失败：' + e.message);
      }
    });
  });
}

function toolName(t) {
  return ({ read: '读文件', write: '写文件', edit: '改文件', list: '列目录', glob: '找文件', grep: '搜内容', run: '跑命令' })[t] || t;
}

async function loadDiff(id) {
  if (!id) return;
  try {
    const d = await api(`/v1/runs/${id}/diff`);
    runs.get(id).diffs = d.files || [];
    renderDiff(d);
  } catch (e) { /* 没改动时可能 404，忽略 */ }
}

function renderDiff(d) {
  if (!d.files || !d.files.length) {
    addTimeline('改动', '没有文件变化', 'info');
    return;
  }
  const el = addTimeline('改动明细', d.files.map(f => `
    <div class="df">
      <div class="df-h"><span class="df-st ${esc(f.status)}">${esc(f.status)}</span> <code>${esc(f.path)}</code> <span class="tl-dim">+${f.added}/-${f.removed}</span></div>
      ${f.patch ? `<pre class="df-patch">${esc(f.patch)}</pre>` : ''}
    </div>`).join('') + `<div style="margin-top:8px"><button class="btn btn-line" id="revertBtn">回滚这次改动</button></div>`, 'diff');
  const rv = el.querySelector('#revertBtn');
  if (rv) rv.addEventListener('click', async () => {
    rv.disabled = true;
    try {
      const r = await api(`/v1/runs/${id}/revert`, { method: 'POST' });
      addTimeline('已回滚', `${r.reverted} 个文件恢复到执行前`, 'ok');
    } catch (e) { toast('回滚失败：' + e.message); }
  });
}

function finishUi() {
  runId = null;
  abort = null;
  const b = $('#runStart');
  if (b) b.disabled = false;
}

async function renderRunHistory(id) {
  const box = $('#runHistory');
  if (!box) return;
  box.innerHTML = '<div class="tl-dim">最近任务</div>';
}

export function initStudio() {
  const panel = $('#studio');
  if (!panel) return;
  $('#studioClose').addEventListener('click', closeStudio);
  $('#wsPick')?.addEventListener('click', () => {
    if (window.__ensemblePickDirectory) {
      toast('用菜单「文件 → 选择工作区目录」（⌘O）');
    } else {
      toast('浏览器里请直接填写本机绝对路径');
    }
  });
  $('#runStart').addEventListener('click', startRun);
  panel.addEventListener('click', e => {
    if (e.target === panel) closeStudio();
  });
  document.addEventListener('keydown', e => {
    if (e.key === 'Escape' && panel.classList.contains('on')) closeStudio();
  });
}
