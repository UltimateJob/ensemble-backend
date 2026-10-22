/* ============================================================
   房间内全宽工作区：绑定目录 / 权限 / 任务 / 时间线 / diff / 回滚
   与旧的 studio 弹层是同一套能力，只是换成整页布局。
   ============================================================ */

import { $, api, state, esc, icon, hydrateIcons, toast } from '../js/core.js';
import { trackRun, onRunEvent } from '../js/runs.js';

let root = null;
let wsId = null;
let runId = null;
let offRun = null;
let convId = '';

export async function mountStudio(host, id) {
  root = host;
  convId = id;
  root.innerHTML = `
    <div class="studio-view">
      <header class="sv-head">
        <div>
          <h2>代码工作室</h2>
          <p class="page-sub">选一个本机目录，派任务；每一步都在时间线上，改动可回滚</p>
        </div>
        <div class="sv-actions">
          <button class="btn btn-line" id="svBack">${icon('arrow-left', 16)}返回聊天</button>
        </div>
      </header>
      <div class="sv-body">
        <aside class="sv-side">
          <label class="field"><span>工作区（本机绝对路径）</span>
            <div class="field-row">
              <input id="wsRoot" placeholder="/Users/you/project" spellcheck="false">
              <button class="btn btn-line" id="wsPick">选择…</button>
            </div>
            <div class="hint">桌面端可用 ⌘O 调原生目录选择器；浏览器里请直接填路径。</div>
          </label>
          <div class="studio-warn hidden" id="wsWarn"></div>

          <label class="field"><span>权限</span>
            <select id="runPerm">
              <option value="readonly">只读（不能改文件、不能跑命令）</option>
              <option value="approval" selected>命令需批准</option>
              <option value="full">完全访问（等同当前系统用户）</option>
            </select>
            <div class="hint">本地运行时不是沙箱。full 下命令能做的事和你本人一样多。</div>
          </label>

          <label class="field"><span>任务</span>
            <textarea id="runTask" rows="5" placeholder="比如：修一下 calc.go 里 Add 的 bug，跑 go test 确认通过"></textarea>
          </label>

          <label class="studio-check">
            <input type="checkbox" id="autoReview" checked>
            <span>执行完自动复审一遍（只读，能跑测试）</span>
          </label>

          <button class="btn btn-primary" id="runStart">开始执行</button>

          <div class="ws-wrap">
            <h2 class="sec-h">已绑定工作区</h2>
            <div id="wsList"></div>
          </div>
        </aside>

        <section class="sv-tl">
          <h2 class="sec-h">执行时间线</h2>
          <div class="timeline" id="runTimeline">
            <div class="tl-empty sv-empty">
              <div class="sv-empty-ic">${icon('terminal', 24)}</div>
              <b>还没有任务</b>
              <span>左边填一个本机目录和一句任务，点「开始执行」。<br>
              每一步的工具调用、输出和 diff 都会按顺序出现在这里。</span>
              <div class="sv-empty-steps">
                <span>① 选目录</span>
                <span>② 写任务</span>
                <span>③ 跑起来</span>
              </div>
            </div>
          </div>
          <h2 class="sec-h" style="margin-top:24px">改动</h2>
          <div class="diff-box" id="runDiff"></div>
        </section>
      </div>
    </div>`;
  hydrateIcons(root);

  // 桌面壳 ⌘O 注入的目录选择回调
  window.__ensemblePickDirectory = path => {
    const inp = $('#wsRoot');
    if (inp) { inp.value = path; toast('已选择：' + path, { type: 'ok' }); }
  };

  $('#svBack').addEventListener('click', () => { location.hash = '#/room/' + encodeURIComponent(convId); });
  $('#wsPick').addEventListener('click', () => {
    if (window.__ensemblePickDirectory) toast('用菜单「文件 → 选择工作区目录」（⌘O）');
    else toast('浏览器里请直接填写本机绝对路径');
  });
  $('#runStart').addEventListener('click', startRun);

  offRun = onRunEvent((rid, ev) => { if (rid === runId) onRunEventUI(ev); });
  await refreshWorkspaces();
}

export function unmountStudio() {
  if (offRun) { offRun(); offRun = null; }
  root = null;
  runId = null;
}

async function refreshWorkspaces() {
  let list = [];
  try { list = await api('/v1/workspaces'); } catch (e) { list = []; }
  const box = $('#wsList');
  if (!box) return;
  if (!list.length) {
    box.innerHTML = '<div class="panel-empty">还没有工作区，填目录后点「开始执行」会先绑定</div>';
    return;
  }
  box.innerHTML = list.map(w => `
    <button class="ws-item${w.id === wsId ? ' on' : ''}" data-id="${esc(w.id)}" data-root="${esc(w.root)}">
      ${icon('folder', 14)}<span title="${esc(w.root)}">${esc(w.root)}</span>
    </button>`).join('');
  box.querySelectorAll('.ws-item').forEach(b => b.addEventListener('click', () => {
    wsId = b.dataset.id;
    $('#wsRoot').value = b.dataset.root;
    box.querySelectorAll('.ws-item').forEach(x => x.classList.toggle('on', x === b));
  }));
  const mine = list.find(w => w.conversationId === convId);
  if (mine) {
    wsId = mine.id;
    $('#wsRoot').value = mine.root;
    box.querySelectorAll('.ws-item').forEach(x => x.classList.toggle('on', x.dataset.id === mine.id));
  }
}

async function bindIfNeeded() {
  const rootPath = $('#wsRoot').value.trim();
  if (!rootPath) throw new Error('先选择或填写一个本机目录');
  const list = await api('/v1/workspaces');
  const hit = (list || []).find(w => w.root === rootPath && (!convId || w.conversationId === convId));
  if (hit) { wsId = hit.id; return wsId; }
  const w = await api('/v1/workspaces', { method: 'POST', body: { root: rootPath, conversationId: convId } });
  wsId = w.id;
  await refreshWorkspaces();
  return wsId;
}

async function startRun() {
  const task = $('#runTask').value.trim();
  if (!task) { toast('先写一句要做什么', { type: 'err' }); return; }
  if (runId) { toast('上一轮还在跑', { type: 'err' }); return; }
  const btn = $('#runStart');
  btn.disabled = true;
  try {
    const id = await bindIfNeeded();
    const permission = $('#runPerm').value;
    const run = await api(`/v1/workspaces/${id}/runs`, {
      method: 'POST',
      body: { task, permission, maxSteps: 20, autoReview: $('#autoReview').checked },
    });
    runId = run.id;
    const tl = $('#runTimeline');
    tl.innerHTML = '';
    $('#runDiff').innerHTML = '';
    add(tl, '调度', `任务已提交 · 权限 <b>${esc(permission)}</b>`, 'info');
    trackRun(run.id);
  } catch (e) {
    toast(e.message, { type: 'err' });
    btn.disabled = false;
  }
}

function onRunEventUI(ev) {
  const tl = $('#runTimeline');
  if (!tl) return;
  const d = ev.data || {};
  switch (ev.type) {
    case 'run.start':
      add(tl, '开始', `工作区 <code>${esc(d.root || '')}</code>`, 'info');
      break;
    case 'step.start':
      add(tl, `第 ${d.step}/${d.maxSteps} 步`, '', 'step');
      break;
    case 'assistant.message':
      add(tl, '说明', esc(d.text || ''), 'say');
      break;
    case 'tool.start':
      add(tl, toolName(d.tool) + ` <span class="tl-badge ${esc(d.risk || '')}">${esc(d.risk || '')}</span>`,
        `<code class="tl-args">${esc(d.args || '')}</code>`, 'tool', d.toolCallId);
      break;
    case 'tool.end':
      endTool(tl, d);
      break;
    case 'diff':
      add(tl, '改动', `${d.files} 个文件，共 ${d.total} 处`, 'info');
      break;
    case 'run.end':
      add(tl, '结束', `状态 <b>${esc(d.status)}</b>${d.error ? ' · ' + esc(d.error) : ''} · 第 ${d.step} 步`, d.status === 'done' ? 'ok' : 'err');
      loadDiff(runId);
      runId = null;
      const btn = $('#runStart');
      if (btn) btn.disabled = false;
      break;
    case 'error':
      add(tl, '错误', esc(d.message || ''), 'err');
      break;
  }
}

function add(box, title, html, kind, id) {
  const el = document.createElement('div');
  el.className = 'tl ' + (kind || '');
  el.innerHTML = `<div class="tl-h">${title}</div>${html ? `<div class="tl-b">${html}</div>` : ''}`;
  if (id) el.dataset.toolCall = id;
  box.appendChild(el);
  box.scrollTop = box.scrollHeight;
}

function endTool(box, d) {
  const el = box.querySelector(`.tl[data-tool-call="${d.toolCallId}"]`);
  if (!el) return;
  el.classList.add(d.status === 'ok' ? 'ok' : (d.status === 'denied' ? 'denied' : 'err'));
  const out = (d.output || '').slice(0, 900);
  const meta = [];
  if (typeof d.exitCode === 'number') meta.push('exit ' + d.exitCode);
  if (d.durationMs != null) meta.push(d.durationMs + 'ms');
  el.querySelector('.tl-b').innerHTML =
    `${meta.length ? `<span class="tl-dim">${meta.join(' · ')}</span>` : ''}<pre class="tl-out"></pre>`;
  el.querySelector('pre').textContent = out;
}

async function loadDiff(id) {
  const box = $('#runDiff');
  if (!box || !id) return;
  try {
    const d = await api(`/v1/runs/${id}/diff`);
    if (!d.files?.length) {
      box.innerHTML = '<div class="panel-empty">没有文件变化</div>';
      return;
    }
    box.innerHTML = d.files.map(f => `
      <div class="df">
        <div class="df-h">
          <span class="df-st ${esc(f.status)}">${esc(f.status)}</span>
          <code>${esc(f.path)}</code>
          <span class="dim">+${f.added}/-${f.removed}</span>
        </div>
        ${f.patch ? `<pre class="df-patch">${esc(f.patch)}</pre>` : ''}
      </div>`).join('') +
      '<button class="btn btn-line" id="revertBtn" style="align-self:flex-start">回滚这次改动</button>';
    box.querySelector('#revertBtn').addEventListener('click', async () => {
      try {
        const r = await api(`/v1/runs/${id}/revert`, { method: 'POST' });
        toast(`${r.reverted} 个文件已恢复`, { type: 'ok' });
        loadDiff(id);
      } catch (e) { toast('回滚失败：' + e.message, { type: 'err' }); }
    });
  } catch (e) {
    box.innerHTML = '';
  }
}

function toolName(t) {
  return ({ read: '读文件', write: '写文件', edit: '改文件', list: '列目录', glob: '找文件', grep: '搜内容', run: '跑命令', docx_create: '生成 Word', xlsx_create: '生成 Excel' })[t] || t;
}
