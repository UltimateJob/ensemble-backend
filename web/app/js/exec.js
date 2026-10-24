/* ============================================================
   执行：@ 派活 → 后端起 Run → 聊天气泡里跟进度
   Run 的真相在服务端（关掉页面也在跑），这里只是它的实时投影；
   刷新后从 /activity 恢复，结束结果由服务端落成群消息。
   ============================================================ */

import { api, state, esc, icon, toast, humanError } from './core.js';
import { avatarHTML } from './avatars.js';
import { trackRun, untrackRun, onRunEvent } from './runs.js';
import { addMsg, addSysNote, scrollBottom, mdRender } from './chat.js';
import { permissionLabel, toolLabel, isExecutable } from './activity.js';

const bubbles = new Map();   // runId -> {el, card, state}
const seeded = new Set();    // 已经从 /runs/{id} 补过历史的 Run，别每 5 秒重拉
let wired = false;

/* ---------------- 发送 ---------------- */

/**
 * 在代码群里 @ 一个或多个角色：走 /execute，只为被 @ 的角色起执行任务。
 * 返回是否真的按执行处理（false 时调用方回落到普通群聊编排）。
 */
export async function sendExecute(conv, text) {
  const btn = document.querySelector('#sendBtn');
  if (btn) btn.disabled = true;
  const sentEl = addMsg({
    name: (state.user && state.user.name) || '你',
    text,
    me: true,
    time: new Date(),
  });
  scrollBottom();
  try {
    // 单聊没有 @ 也只有一个领活的人；多人群必须点名，避免「派活」变成全员乱跑。
    const agents = state.agents || [];
    const named = agents.filter(a =>
      text.includes('@' + a.name) || text.includes('＠' + a.name) ||
      text.includes('@' + a.key) || text.includes('＠' + a.key));
    const agentKeys = named.length ? named.map(a => a.key)
      : (agents.length === 1 ? [agents[0].key] : []);
    const resp = await api('/v1/conversations/' + encodeURIComponent(conv.id) + '/execute', {
      method: 'POST',
      body: { content: text, agentKeys },
    });
    const runs = resp?.runs || [];
    let started = 0;
    for (const r of runs) {
      if (r.error || !r.runId) {
        addSysNote((r.agentName || r.agentKey) + '：' + (r.error || '没能开始'));
        continue;
      }
      started++;
      mountRun(conv, {
        runId: r.runId,
        agentKey: r.agentKey,
        agentName: r.agentName,
        permission: r.permission,
        task: text,
      });
    }
    if (!started) toast('没有角色接下这个任务', { type: 'err' });
    return true;
  } catch (e) {
    sentEl.classList.add('failed');
    sentEl.addEventListener('click', () => {
      if (!sentEl.classList.contains('failed')) return;
      const box = document.querySelector('#ta');
      if (box) {
        box.value = sentEl._raw || text;
        box.dispatchEvent(new Event('input', { bubbles: true }));
      }
      sentEl.remove();
    }, { once: true });
    toast(humanError(e), { type: 'err' });
    return true;
  } finally {
    if (btn) {
      btn.disabled = false;
      document.dispatchEvent(new CustomEvent('round:state'));
    }
  }
}

/* ---------------- 气泡 ---------------- */

export function mountRun(conv, info) {
  if (!info?.runId || bubbles.has(info.runId)) return;
  const wrap = document.querySelector('#msgWrap');
  if (!wrap) return;
  const agent = (state.agents || []).find(a => a.key === info.agentKey || a.name === info.agentName);
  const name = info.agentName || agent?.name || '角色';
  const el = document.createElement('div');
  el.className = 'msg run-msg';
  el.dataset.run = info.runId;
  el.innerHTML = `
    ${avatarHTML(name, agent?.color, { size: 32 })}
    <div class="msg-body">
      <div class="msg-head">
        <span class="msg-name">${esc(name)}</span>
        <span class="msg-title">执行任务</span>
        <span class="run-pill" data-pill>准备中</span>
      </div>
      <div class="run-card">
        <div class="run-line">
          <span class="run-step" data-step>第 0 步</span>
          <span class="run-perm">${esc(permissionLabel(info.permission))}</span>
        </div>
        <div class="run-now" data-now>正在排队…</div>
        <div class="run-tl" data-tl hidden></div>
        <div class="run-foot">
          <button class="btn btn-ghost btn-sm" data-act="toggle">${icon('chevron-down', 13)}过程</button>
          <button class="btn btn-ghost btn-sm" data-act="open">工作区</button>
          <button class="btn btn-ghost btn-sm" data-act="stop">${icon('square', 12)}停止</button>
        </div>
        <div class="run-arts" data-arts></div>
      </div>
    </div>`;
  wrap.appendChild(el);

  const rec = {
    el,
    conv,
    agent,
    name,
    pill: el.querySelector('[data-pill]'),
    step: el.querySelector('[data-step]'),
    now: el.querySelector('[data-now]'),
    tl: el.querySelector('[data-tl]'),
    arts: el.querySelector('[data-arts]'),
    maxSteps: 0,
    done: false,
  };
  bubbles.set(info.runId, rec);

  el.querySelector('[data-act=toggle]').addEventListener('click', () => {
    rec.tl.hidden = !rec.tl.hidden;
    el.querySelector('[data-act=toggle]').classList.toggle('on', !rec.tl.hidden);
  });
  el.querySelector('[data-act=open]').addEventListener('click', () => {
    location.hash = '#/room/' + encodeURIComponent(conv.id) + '?view=studio&run=' + encodeURIComponent(info.runId);
  });
  el.querySelector('[data-act=stop]').addEventListener('click', async () => {
    try {
      await api('/v1/runs/' + encodeURIComponent(info.runId) + '/abort', { method: 'POST' });
      toast('已请求停止', { type: 'info' });
    } catch (e) { toast(humanError(e), { type: 'err' }); }
  });

  if (!wired) {
    wired = true;
    onRunEvent((runId, ev) => {
      const r = bubbles.get(runId);
      if (r) applyRunEvent(r, ev);
    });
  }
  trackRun(info.runId);
  scrollBottom();
  return rec;
}

function setPill(rec, text, kind) {
  rec.pill.textContent = text;
  rec.pill.className = 'run-pill' + (kind ? ' ' + kind : '');
}

function applyRunEvent(rec, ev) {
  const d = ev.data || {};
  switch (ev.type) {
    // 断流与重连要显式告诉用户：之前断了只 console.warn，
    // 气泡停在「执行中」，用户不知道是在跑还是已经挂了。
    case 'stream.reconnecting':
      setPill(rec, '连接中断，重连中', 'warn');
      break;
    case 'stream.reconnected':
      setPill(rec, '执行中', 'on');
      rec.now.textContent = '已重新连上，继续接收进度';
      break;
    case 'stream.lost':
      setPill(rec, '连接已断开', 'bad');
      rec.now.textContent = '实时进度断了（' + (d.reason || '') + '）。任务可能还在后端跑，刷新页面能看到最新状态。';
      break;
    case 'run.start':
      rec.maxSteps = d.maxSteps || 0;
      rec.step.textContent = '第 0/' + rec.maxSteps + ' 步';
      setPill(rec, '执行中', 'on');
      rec.now.textContent = '任务已接手，正在读工作区';
      break;
    case 'step.start':
      rec.step.textContent = `第 ${d.step}/${d.maxSteps || rec.maxSteps} 步`;
      break;
    case 'assistant.message':
      rec.now.textContent = clipText(d.text || '', 90);
      addTl(rec, '说明', esc(clipText(d.text || '', 160)), 'say');
      break;
    case 'tool.start':
      setPill(rec, '执行中', 'on');
      rec.now.textContent = `${toolLabel(d.tool)} ${d.summary || ''}`.trim();
      addTl(rec, toolLabel(d.tool),
        `<code>${esc(clipText(d.args || d.path || '', 120))}</code>`,
        'tool', d.toolCallId, d.risk);
      break;
    case 'tool.end':
      endTl(rec, d);
      break;
    case 'approval.required':
      setPill(rec, '等你审批', 'warn');
      rec.now.textContent = d.summary || '有一个操作在等你确认';
      break;
    case 'diff':
      addTl(rec, '改动', `${d.files || 0} 个文件`, 'info');
      if (d.artifacts?.length) renderArts(rec, d.artifacts);
      break;
    case 'artifact':
      renderArts(rec, [d]);
      break;
    case 'error':
      rec.now.textContent = d.message || '执行出错';
      break;
    case 'run.end':
      rec.done = true;
      const ok = d.status === 'done';
      setPill(rec, ok ? '已完成' : (d.status === 'aborted' ? '已停止' : '未完成'), ok ? 'ok' : 'bad');
      rec.step.textContent = `共 ${d.step || 0} 步 · ${(d.tokens?.in || 0) + (d.tokens?.out || 0)} tokens`;
      rec.now.innerHTML = d.summary
        ? `<div class="run-sum md">${mdRender(d.summary)}</div>`
        : (d.error || '任务结束');
      if (d.artifacts?.length) renderArts(rec, d.artifacts);
      rec.el.querySelector('[data-act=stop]')?.remove();
      untrackRun(rec.el.dataset.run);
      bubbles.delete(rec.el.dataset.run);
      document.dispatchEvent(new CustomEvent('run:ended', { detail: { runId: rec.el.dataset.run } }));
      break;
  }
}

function clipText(s, n) {
  const t = String(s || '').replace(/\s+/g, ' ').trim();
  return t.length > n ? t.slice(0, n) + '…' : t;
}

function addTl(rec, title, html, kind, id, risk) {
  rec.tl.hidden = false;
  const row = document.createElement('div');
  row.className = 'rtl rtl-' + (kind || '');
  row.innerHTML = `<span class="rtl-t">${title}</span>` +
    `${risk ? `<span class="rtl-risk ${esc(risk)}">${esc(risk)}</span>` : ''}` +
    `<span class="rtl-b">${html}</span>`;
  if (id) row.dataset.tc = id;
  rec.tl.appendChild(row);
  while (rec.tl.children.length > 40) rec.tl.firstChild.remove();
}

function endTl(rec, d) {
  if (!d.toolCallId) return;
  const row = rec.tl.querySelector(`[data-tc="${CSS.escape(d.toolCallId)}"]`);
  if (!row) return;
  const cls = d.status === 'ok' ? 'ok' : (d.status === 'denied' ? 'denied' : 'err');
  row.classList.add(cls);
  if (d.output) row.querySelector('.rtl-b').title = String(d.output).slice(0, 900);
}

/* ---------------- 产物 ---------------- */

function renderArts(rec, arts) {
  rec.arts.innerHTML = '';
  (arts || []).forEach(a => {
    const b = document.createElement('button');
    b.className = 'att-chip';
    b.innerHTML = `${icon('file-text', 13)}<span>${esc(a.name || a.path)}</span>` +
      `<em>${esc(a.agentName || rec.name)}</em>`;
    b.addEventListener('click', () => openArtifact(rec.conv, a));
    rec.arts.appendChild(b);
  });
}

// 二进制产物：按文本读会得到乱码，直接给下载入口。
// docx/xlsx 是执行器最常产出的两类文件，也是「每一步都落成文件」
// 这个卖点最直接的兑现物，不能只给一屏乱码。
const BINARY_EXT = /\.(docx|xlsx|pptx|pdf|zip|png|jpe?g|gif|webp|mp4|mp3)$/i;

export async function openArtifact(conv, ref) {
  const wsId = conv?.workspace?.id;
  if (!wsId || !ref?.path) { toast('这个产物没有可预览的路径', { type: 'info' }); return; }
  const isBinary = BINARY_EXT.test(ref.path || '');

  // 下载必须走 fetch 带 Authorization：
  // 裸 <a href> 不会带请求头，后端只认 Bearer，结果必然是 401。
  const download = async () => {
    try {
      const res = await fetch(
        `/v1/workspaces/${encodeURIComponent(wsId)}/raw?path=` + encodeURIComponent(ref.path),
        { headers: { Authorization: 'Bearer ' + (state.token || '') } });
      if (!res.ok) throw new Error('下载失败（HTTP ' + res.status + '）');
      const blob = await res.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = ref.name || ref.path.split('/').pop() || 'file';
      document.body.appendChild(a);
      a.click();
      a.remove();
      setTimeout(() => URL.revokeObjectURL(url), 4000);
    } catch (e) {
      toast(e.message || '下载失败', { type: 'err' });
    }
  };

  if (isBinary) {
    const { openPanel } = await import('./shell.js');
    openPanel({
      title: ref.name || ref.path,
      subtitle: (ref.agentName || '') + ' · ' + ref.path,
      render: el => {
        el.innerHTML = `<div class="art-binary">
          <div class="art-binary-ic">${icon('file-text', 28)}</div>
          <b>${esc(ref.name || ref.path)}</b>
          <span>这类文件不能当文本预览，下载后用对应软件打开</span>
          <button class="btn btn-primary" data-act="dl">${icon('download', 14)}下载</button>
        </div>`;
        el.querySelector('[data-act=dl]').addEventListener('click', download);
      },
    });
    return;
  }

  try {
    const chunk = await api(`/v1/workspaces/${encodeURIComponent(wsId)}/file?path=` +
      encodeURIComponent(ref.path) + '&limit=400');
    const { openPanel } = await import('./shell.js');
    const { confirmModal } = await import('./modal.js');
    openPanel({
      title: ref.name || ref.path,
      subtitle: (ref.agentName || '') + ' · ' + ref.path,
      render: el => {
        el.innerHTML = `<div class="art-dl-row">
            <button class="btn btn-line btn-sm" data-act="dl">${icon('download', 13)}下载</button>
            <button class="btn btn-line btn-sm" data-act="rev">${icon('git-compare', 13)}看改动</button>
          </div>
          <pre class="art-pre">${esc(chunk?.content || '(空文件)')}</pre>`;
        el.querySelector('[data-act=dl]').addEventListener('click', download);
        el.querySelector('[data-act=rev]').addEventListener('click', async () => {
          if (!ref.runId) return;
          try {
            const diff = await api('/v1/runs/' + encodeURIComponent(ref.runId) + '/diff');
            const f = (diff.files || []).find(x => x.path === ref.path);
            await confirmModal({
              title: ref.path,
              text: f?.patch || '没有差异记录（可能已回滚或改动被合并）',
              okText: '好',
              cancelText: '关闭',
            });
          } catch (e) { toast(humanError(e), { type: 'err' }); }
        });
      },
    });
  } catch (e) {
    toast('打不开：' + humanError(e), { type: 'err' });
  }
}

/* ---------------- 断线恢复 ---------------- */

/** 刷新页面后：把服务端还在跑的 Run 重新投影成气泡。 */
export async function resumeRuns(conv, activity) {
  const running = (activity?.runs || []).filter(r =>
    r.status === 'running' || r.status === 'waiting' || r.status === 'waiting_approval');
  for (const r of running) {
    if (bubbles.has(r.id)) continue;
    const rec = mountRun(conv, {
      runId: r.id,
      agentKey: r.agentKey,
      agentName: r.agentName,
      permission: r.permission,
      task: r.task,
    });
    if (!rec || seeded.has(r.id)) continue;
    seeded.add(r.id);
    try {
      const full = await api('/v1/runs/' + encodeURIComponent(r.id));
      rec.maxSteps = full.maxSteps || rec.maxSteps;
      rec.step.textContent = `第 ${full.step || 0}/${full.maxSteps || '?'} 步`;
      setPill(rec, full.status === 'waiting_approval' || full.status === 'waiting' ? '等你审批' : '执行中',
        full.status === 'waiting_approval' || full.status === 'waiting' ? 'warn' : 'on');
      (full.toolCalls || []).slice(-12).forEach(t => {
        addTl(rec, toolLabel(t.tool), `<code>${esc(clipText(t.args || '', 120))}</code>`,
          'tool', t.id, t.risk);
        if (t.status) endTl(rec, { toolCallId: t.id, status: t.status, output: t.output });
      });
      if (!rec.tl.hidden) rec.tl.hidden = true;
      rec.now.textContent = '重连到这个任务；下面的过程是它已经走过的步骤。';
    } catch (e) { /* 读不到详情时保留空气泡，事件流会继续更新 */ }
  }
}

export function clearRunBubbles() {
  [...bubbles.keys()].forEach(id => { untrackRun(id); bubbles.delete(id); });
}

export function hasLiveRuns() { return bubbles.size > 0; }

export function executableAgents(conv) {
  return (state.agents || []).filter(a => isExecutable(a.permission) ||
    isExecutable(conv?.defaultPermission));
}
