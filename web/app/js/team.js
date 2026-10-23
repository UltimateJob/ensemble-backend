/* ============================================================
   团队任务：规划 → 分工并行 → 验证 → 返工 → 验收
   编排在后端；这里只负责提交、实时显示阶段与结果。
   ============================================================ */

import { api, state, esc, icon, toast, humanError } from './core.js';
import { openModal, confirmModal } from './modal.js';
import { openPanel, refreshPanel } from './shell.js';
import { avatarHTML } from './avatars.js';
import { addSysNote, scrollBottom } from './chat.js';
import { permissionLabel } from './activity.js';
import { onRunEvent, trackRun, untrackRun } from './runs.js';

const PHASES = [
  { key: 'planning', label: '规划' },
  { key: 'building', label: '并行执行' },
  { key: 'verifying', label: '验证' },
  { key: 'repairing', label: '返工' },
  { key: 'reviewing', label: '验收' },
  { key: 'done', label: '完成' },
];

const PHASE_LABEL = {
  planning: '规划中', building: '并行执行中', verifying: '验证中',
  repairing: '返工中', reviewing: '验收中', done: '已完成',
  failed: '失败', cancelled: '已取消',
};

let live = null;   // {convId, id, team, ctrl, runStates}
let offRun = null;

/* ---------------- 提交 ---------------- */

export async function openTeamTask(conv, activity, { onBound } = {}) {
  const bound = !!conv?.workspace || !!activity?.workspace?.bound;
  if (!bound) {
    const { openWorkspaceDialog } = await import('./workspace.js');
    openWorkspaceDialog(conv, {
      onDone: async () => {
        onBound?.();
        const { loadActivity } = await import('./activity.js');
        openTeamTask(conv, await loadActivity(conv.id));
      },
    });
    toast('先绑定工作区，再交给团队', { type: 'info' });
    return;
  }
  const agents = state.agents || [];
  // 生效权限：角色单独设置 > 群默认 > 角色自带（与后端 ResolveAgentPermission 一致）。
  // 只看 a.permission 会误判：群默认 ask 会压过角色的 approval。
  const effective = a => conv.agentPermissions?.[a.key] || conv.defaultPermission || a.permission || '';
  // 团队流水线无人值守：ask 会在第一个写操作上停住，不能算「可执行」。
  const canTeam = p => p === 'approval' || p === 'full';
  const needPerm = agents.some(a => canTeam(effective(a)));
  openModal({
    title: '交给团队',
    width: 620,
    body: `
      <p class="hint" style="margin-bottom:14px">
        后端会让架构角色先出计划，前后端按分工并行改代码，再跑命令验证；
        不通过会定向返工一轮，最后给出评分。
      </p>
      <label class="field"><span>目标</span>
        <textarea id="teamTask" rows="5" placeholder="例如：给现有项目加一个 /health 接口，返回服务和数据库状态；补一条测试，确保 go test ./... 通过"></textarea>
      </label>
      <div class="team-who">
        ${agents.map(a => `<span class="tw-chip">${avatarHTML(a.name, a.color, { size: 20 })}${esc(a.name)}<em>${esc(permissionLabel(effective(a)))}</em></span>`).join('')}
      </div>
      ${needPerm ? '' : `<div class="studio-warn">团队任务需要至少一个「自动接受编辑 / 完全访问」的执行角色。
        <button class="btn btn-line btn-sm" id="teamEnablePerm" style="margin-top:8px">把群默认权限设为自动接受编辑</button>
      </div>`}
      <label class="field" style="margin-top:14px"><span>每步上限（可选）</span>
        <input id="teamSteps" type="number" min="4" max="40" value="20">
      </label>`,
    actions: [
      { key: 'cancel', label: '取消', kind: 'line' },
      { key: 'go', label: '开始协作', kind: 'primary', onClick: async mask => {
        const task = mask.querySelector('#teamTask').value.trim();
        if (!task) { toast('先写清目标', { type: 'err' }); return false; }
        const hasExec = agents.some(a => ['approval', 'full'].includes(
          conv.agentPermissions?.[a.key] || conv.defaultPermission || a.permission || ''));
        if (!hasExec) { toast('没有可执行的角色', { type: 'err' }); return false; }
        const maxSteps = Math.max(4, Math.min(40, Number(mask.querySelector('#teamSteps').value) || 20));
        try {
          await startTeam(conv, task, maxSteps);
          return true;
        } catch (e) {
          toast(humanError(e), { type: 'err' });
          return false;
        }
      } },
    ],
    onMount: mask => {
      mask.querySelector('#teamTask').focus();
      mask.querySelector('#teamEnablePerm')?.addEventListener('click', async () => {
        try {
          await api('/v1/conversations/' + encodeURIComponent(conv.id), {
            method: 'PATCH', body: { defaultPermission: 'approval' },
          });
          conv.defaultPermission = 'approval';
          const hit = (state.convs || []).find(c => c.id === conv.id);
          if (hit) hit.defaultPermission = 'approval';
          toast('已设为自动接受编辑，再点「开始协作」', { type: 'ok' });
          mask.querySelector('#teamEnablePerm').closest('.studio-warn').remove();
        } catch (e) { toast(humanError(e), { type: 'err' }); }
      });
    },
  });
}

export async function startTeam(conv, task, maxSteps) {
  const resp = await api('/v1/conversations/' + encodeURIComponent(conv.id) + '/team', {
    method: 'POST',
    body: { task, maxSteps },
  });
  const team = resp?.team;
  if (!team?.id) throw new Error('团队任务没有返回 id');
  // 后端已经落了一条用户消息；本地补一条同样内容的，避免要刷新才看得到。
  const { addMsg } = await import('./chat.js');
  const el = addMsg({
    name: (state.user && state.user.name) || '你',
    text: task, me: true, time: new Date(), id: resp.message?.id,
  });
  if (el) el.dataset.team = team.id;
  scrollBottom();
  live = { convId: conv.id, id: team.id, team, runStates: new Map() };
  showTeamPanel(conv);
  attachTeam(team.id);
  return team;
}

/* ---------------- 面板 ---------------- */

export function showTeamPanel(conv, team) {
  if (team && (!live || live.id !== team.id)) {
    live = { convId: conv.id, id: team.id, team, runStates: new Map() };
    attachTeam(team.id);
  }
  if (!live) return;
  openPanel({
    title: '团队任务',
    subtitle: clip(live.team.task, 40),
    render: el => {
      el.innerHTML = teamPanelHTML(live.team);
      wirePanel(el, conv);
    },
  });
}

function teamPanelHTML(t) {
  const status = t.status || t.phase || 'planning';
  const phaseIdx = Math.max(0, PHASES.findIndex(p => p.key === (t.phase || status)));
  const assignments = t.assignments || t.plan?.assignments || [];
  const checks = t.checks || [];
  const issues = t.issues || [];
  const done = isFinal(status);
  return `
    <div class="tp-head">
      <span class="tp-status ${esc(status)}">${esc(PHASE_LABEL[status] || status)}</span>
      ${t.score ? `<span class="tp-score" title="自动验收评分">${t.score}<em>分</em></span>` : ''}
    </div>
    <div class="tp-phases">
      ${PHASES.map((p, i) => `<span class="tp-phase${i < phaseIdx ? ' past' : ''}${p.key === (t.phase || status) ? ' on' : ''}">
        <i></i>${p.label}</span>`).join('')}
    </div>
    ${t.plan?.objective ? `<div class="tp-objective">${esc(t.plan.objective)}</div>` : ''}

    <section class="panel-sec"><h4>分工</h4>
      <div id="tpAssign">
        ${assignments.length ? assignments.map(a => `
          <div class="tp-assign" data-agent="${esc(a.agentKey)}">
            ${avatarHTML(a.agentName || a.agentKey, '', { size: 24 })}
            <div class="ta-body">
              <div class="ta-name">${esc(a.agentName || a.agentKey)}</div>
              <div class="ta-task">${esc(a.task || '')}</div>
              ${(a.scope || []).length ? `<div class="ta-scope">${a.scope.map(s => `<code>${esc(s)}</code>`).join(' ')}</div>` : ''}
            </div>
            <span class="ta-state" data-state>${done ? '结束' : '已派活'}</span>
          </div>`).join('') : '<div class="ap-idle">架构角色正在拆解任务…</div>'}
      </div>
    </section>

    <section class="panel-sec"><h4>验证</h4>
      <div id="tpChecks">
        ${checks.length ? checks.map(c => checkHTML(c)).join('') : '<div class="ap-idle">执行完成后会自动跑验证命令。</div>'}
      </div>
    </section>

    ${issues.length ? `<section class="panel-sec"><h4>待修问题</h4>
      <div class="tp-issues">${issues.map(i => `<div class="tp-issue">${esc(i)}</div>`).join('')}</div>
    </section>` : ''}

    ${t.review ? `<section class="panel-sec"><h4>验收</h4>
      <div class="tp-review ${esc(t.review.verdict || '')}">
        <b>${t.review.verdict === 'pass' ? '通过' : t.review.verdict === 'issues' ? '有问题' : '没给结论'}</b>
        <span>${esc(t.review.summary || '')}</span>
      </div>
    </section>` : ''}

    ${t.error ? `<div class="studio-warn">${esc(t.error)}</div>` : ''}

    <div class="tp-foot">
      ${done
        ? `<button class="btn btn-line btn-sm" data-act="again">再派一个任务</button>`
        : `<button class="btn btn-line btn-sm danger" data-act="stop">停止团队任务</button>`}
      <span class="hint">${t.repairs ? `返工 ${t.repairs} 轮 · ` : ''}${t.tokensIn || t.tokensOut ? `tokens ${(t.tokensIn || 0) + (t.tokensOut || 0)}` : ''}</span>
    </div>`;
}

function checkHTML(c) {
  const cls = c.skipped ? 'skip' : (c.passed ? 'ok' : 'bad');
  const label = c.skipped ? '跳过' : (c.passed ? '通过' : '失败');
  return `<div class="tp-check ${cls}">
    <span class="tc-dot"></span>
    <div class="tc-body">
      <div class="tc-name">${esc(c.name)}${c.argv?.length ? ` · <code>${esc(c.argv.join(' '))}</code>` : ''}</div>
      <div class="tc-meta">${label}${typeof c.exitCode === 'number' && !c.passed ? ` · exit ${c.exitCode}` : ''}${c.ms ? ` · ${c.ms}ms` : ''}${c.reason ? ' · ' + esc(c.reason) : ''}</div>
      ${c.output && !c.passed ? `<pre class="tc-out">${esc(c.output.slice(0, 1200))}</pre>` : ''}
    </div>
  </div>`;
}

function wirePanel(el, conv) {
  el.querySelector('[data-act=stop]')?.addEventListener('click', async () => {
    if (!await confirmModal({
      title: '停止团队任务',
      text: '正在执行的子任务会一起中止。已经写下的文件会保留。',
      okText: '停止', danger: true,
    })) return;
    try {
      await api('/v1/teams/' + encodeURIComponent(live.id) + '/cancel', { method: 'POST' });
      toast('已请求停止', { type: 'info' });
    } catch (e) { toast(humanError(e), { type: 'err' }); }
  });
  el.querySelector('[data-act=again]')?.addEventListener('click', async () => {
    const { openTeamTask } = await import('./team.js');
    const { loadActivity } = await import('./activity.js');
    openTeamTask(conv, await loadActivity(conv.id));
  });
}

/* ---------------- 实时事件 ---------------- */

async function attachTeam(teamId, force) {
  if (!live || live.id !== teamId) return;
  if (live.ctrl && !force) return;
  live.ctrl?.abort();
  const ctrl = new AbortController();
  live.ctrl = ctrl;
  live.attached = true;
  try {
    const res = await fetch('/v1/teams/' + encodeURIComponent(teamId) + '/events', {
      headers: { Authorization: 'Bearer ' + state.token },
      signal: ctrl.signal,
    });
    if (!res.ok || !res.body) throw new Error('HTTP ' + res.status);
    const reader = res.body.getReader();
    const dec = new TextDecoder();
    let buf = '';
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      buf += dec.decode(value, { stream: true });
      const parts = buf.split('\n\n');
      buf = parts.pop();
      for (const part of parts) {
        const line = part.trim();
        if (!line.startsWith('data:')) continue;
        let ev;
        try { ev = JSON.parse(line.slice(5).trim()); } catch { continue; }
        applyTeamEvent(ev);
      }
    }
  } catch (e) {
    if (e.name !== 'AbortError') console.warn('team stream', teamId, e.message);
  }
}

function applyTeamEvent(ev) {
  if (!live) return;
  const t = live.team;
  const d = ev.data || {};
  switch (ev.type) {
    case 'team.phase':
      t.phase = d.phase || t.phase;
      if (d.phase !== 'planning') t.status = d.phase;
      break;
    case 'team.plan':
      t.plan = d.plan || t.plan;
      t.assignments = d.assignments || t.plan?.assignments || t.assignments;
      break;
    case 'team.run': {
      const rec = live.runStates.get(d.runId) || {};
      rec.agentKey = d.agentKey;
      rec.phase = d.phase;
      live.runStates.set(d.runId, rec);
      trackRun(d.runId);
      if (offRun == null) {
        offRun = onRunEvent((runId, e2) => {
          const r = live?.runStates.get(runId);
          if (!r) return;
          if (e2.type === 'step.start') r.step = e2.data?.step;
          if (e2.type === 'tool.start') r.tool = e2.data?.tool;
          if (e2.type === 'run.end') r.done = true;
          updateAssignStates();
        });
      }
      break;
    }
    case 'team.check':
      if (d.check) t.checks = [...(t.checks || []), d.check];
      break;
    case 'team.issue':
      if (d.issue) t.issues = [...(t.issues || []), d.issue];
      else if (d.issues) t.issues = [...(t.issues || []), ...d.issues];
      break;
    case 'team.review':
      t.review = d.review || t.review;
      break;
    case 'team.end':
      t.status = d.status || 'done';
      t.phase = d.status;
      t.score = d.score || 0;
      t.repairs = d.repairs || 0;
      if (d.error) t.error = d.error;
      onTeamDone();
      return;
  }
  if (['team.phase', 'team.plan', 'team.check', 'team.issue', 'team.review'].includes(ev.type)) {
    refreshPanel();
    updateAssignStates();
  }
}

function updateAssignStates() {
  const box = document.querySelector('#tpAssign');
  if (!box || !live) return;
  const states = [...live.runStates.values()];
  box.querySelectorAll('[data-agent]').forEach(row => {
    const key = row.dataset.agent;
    const mine = states.filter(s => s.agentKey === key);
    const el = row.querySelector('[data-state]');
    if (!el) return;
    if (!mine.length) return;
    const running = mine.find(s => !s.done);
    if (!running) { el.textContent = '完成'; el.className = 'ta-state ok'; return; }
    el.textContent = running.step ? `第 ${running.step} 步` : '执行中';
    el.className = 'ta-state on';
  });
}

function onTeamDone() {
  const t = live?.team;
  if (!t) return;
  refreshPanel();
  [...(live.runStates?.keys() || [])].forEach(id => untrackRun(id));
  if (offRun) { offRun(); offRun = null; }
  const score = t.score ? ` · 评分 ${t.score}` : '';
  addSysNote(`团队任务结束：${PHASE_LABEL[t.status] || t.status}${score}`);
  scrollBottom();
  live.ctrl?.abort();
}

function isFinal(s) {
  return s === 'done' || s === 'failed' || s === 'partial' || s === 'cancelled';
}

function clip(s, n) {
  const t = String(s || '').replace(/\s+/g, ' ').trim();
  return t.length > n ? t.slice(0, n) + '…' : t;
}

/* ---------------- 进入房间时恢复最近一次 ---------------- */

export async function loadLatestTeam(conv) {
  try {
    const list = await api('/v1/conversations/' + encodeURIComponent(conv.id) + '/teams?limit=5');
    if (!list?.length) return null;
    if (!live || live.id !== list[0].id) {
      live = { convId: conv.id, id: list[0].id, team: list[0], runStates: new Map() };
      if (!isFinal(list[0].status)) attachTeam(list[0].id);
    } else {
      live.team = list[0];
    }
    return list;
  } catch (e) {
    return null;
  }
}

export function currentTeam() { return live?.team || null; }

/** 房间顶栏的小徽标：最近一次团队任务的状态与分数。 */
export async function teamBadge(conv) {
  const t = await loadLatestTeam(conv);
  const top = t?.[0];
  if (!top) return '';
  const running = !isFinal(top.status);
  return `<span class="team-badge${running ? ' on' : ''}" id="teamBadge" title="${esc(top.task || '')}">
    ${icon('users', 12)}团队 ${esc(PHASE_LABEL[top.status] || top.status)}${top.score ? ' · ' + top.score : ''}
  </span>`;
}

export function openCurrentTeam(conv) {
  if (!live) {
    toast('这个群还没有团队任务', { type: 'info' });
    return;
  }
  showTeamPanel(conv);
}
