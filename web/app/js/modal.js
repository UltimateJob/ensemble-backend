/* ============================================================
   弹层：通用模态 / 审批 / 确认 / 权限拒绝 / 输入
   ============================================================ */

import { api, esc, icon, toast } from './core.js';

let current = null;

export function openModal({ title, body, actions = [], width = 520, onMount, onClose }) {
  closeModal();
  const host = document.getElementById('modalRoot');
  if (!host) return null;
  const mask = document.createElement('div');
  mask.className = 'modal-mask';
  mask.innerHTML = `<div class="modal-card" style="max-width:${width}px" role="dialog" aria-modal="true">
    <div class="modal-head"><h3>${esc(title)}</h3>
      <button class="icon-btn" data-x="close" title="关闭">${icon('x', 16)}</button></div>
    <div class="modal-body">${body}</div>
    ${actions.length ? `<div class="modal-foot">${actions.map(a =>
      `<button class="btn ${a.kind === 'primary' ? 'btn-primary' : 'btn-line'}${a.danger ? ' danger' : ''}" data-x="${a.key}">${esc(a.label)}</button>`).join('')}</div>` : ''}
  </div>`;
  host.appendChild(mask);
  requestAnimationFrame(() => mask.classList.add('on'));
  current = { mask, onClose };

  mask.addEventListener('click', e => { if (e.target === mask) closeModal(); });
  mask.querySelector('[data-x=close]').addEventListener('click', () => closeModal());
  actions.forEach(a => {
    const btn = mask.querySelector(`[data-x="${a.key}"]`);
    if (!btn) return;
    btn.addEventListener('click', async () => {
      if (a.onClick) {
        const keep = await a.onClick(mask);
        if (keep === false) return;
      }
      closeModal();
    });
  });
  document.addEventListener('keydown', escClose);
  onMount?.(mask);
  return mask;
}

function escClose(e) {
  if (e.key === 'Escape') { e.preventDefault(); closeModal(); }
}

export function closeModal() {
  if (!current) return;
  const { mask, onClose } = current;
  current = null;
  document.removeEventListener('keydown', escClose);
  mask.classList.remove('on');
  setTimeout(() => mask.remove(), 160);
  onClose?.();
}

export function modalOpen() { return !!current; }

/* ---------------- 审批 ---------------- */

// 内部风险枚举值不该直接给用户看。
const RISK_LABEL = {
  read: '只读',
  write: '会修改文件',
  network: '会访问网络',
  delete: '会删除文件',
  privilege: '会提权',
  'git-push': '会推送或重置仓库',
  critical: '不可逆的高危操作',
};

export function approvalModal({ approval, runId, onDone }) {
  const risk = RISK_LABEL[approval.risk] || approval.risk || '未知';
  const danger = approval.risk === 'critical';
  // 每个出口都要通知队列继续：三条动作按钮 + 关闭 + Esc
  const done = fn => async (...args) => { try { return await fn(...args); } finally { onDone?.(); } };
  return openModal({
    title: '需要你的审批',
    width: 580,
    onClose: () => onDone?.(),
    body: `
      <div class="apv-sum">${esc(approval.summary || approval.tool || '一次操作')}</div>
      ${approval.command ? `<pre class="apv-cmd">${esc(approval.command)}</pre>` : ''}
      ${approval.diff ? `<pre class="apv-diff">${esc(approval.diff)}</pre>` : ''}
      <div class="apv-risk${danger ? ' danger' : ''}">
        ${icon('shield-alert', 15)}
        <span><b>${esc(risk)}</b> · 本地运行时，命令以你的用户权限执行</span>
      </div>`,
    actions: [
      { key: 'run', label: '本任务都允许', kind: 'line', onClick: done(() => decide(runId, approval.id, true, 'run')) },
      { key: 'no', label: '拒绝', kind: 'line', danger: true, onClick: done(() => decide(runId, approval.id, false, 'once')) },
      { key: 'yes', label: '允许本次执行', kind: 'primary', onClick: done(() => decide(runId, approval.id, true, 'once')) },
    ],
  });
}

async function decide(runId, approvalId, approve, scope) {
  try {
    await api(`/v1/runs/${runId}/approve`, { method: 'POST', body: { approvalId, approve, scope } });
    toast(approve ? '已允许' : '已拒绝', { type: approve ? 'ok' : 'info' });
  } catch (e) {
    toast('审批失败：' + e.message, { type: 'err' });
  }
  return true;
}

/* ---------------- 审批队列 ---------------- */

// 同一时刻只显示一个审批弹窗。
//
// openModal 会先关掉已有的弹窗，所以两个 Run 同时要审批时，后弹的会
// 顶掉前一个；前一个的 id 已经进了 pendingApprovals，之后不再重放，
// 用户永远看不到它——10 分钟后后端以「未在时限内批准」失败。
//
// 改成排队：一次只问一个，处理完自动弹下一个。
const apvQueue = [];
let apvOpen = false;

export function enqueueApproval({ approval, runId }) {
  apvQueue.push({ approval, runId });
  pumpApproval();
}

function pumpApproval() {
  if (apvOpen || apvQueue.length === 0) return;
  const next = apvQueue.shift();
  apvOpen = true;
  approvalModal({
    approval: next.approval,
    runId: next.runId,
    onDone: () => {
      apvOpen = false;
      // 让上一个弹窗的关闭动画走完再弹下一个，避免闪一下
      setTimeout(pumpApproval, 180);
    },
  });
}

/* ---------------- 确认 / 提示 ---------------- */

export function confirmModal({ title, text, okText = '确定', cancelText = '取消', danger = false }) {
  return new Promise(resolve => {
    openModal({
      title,
      body: `<p class="modal-text">${esc(text)}</p>`,
      actions: [
        { key: 'no', label: cancelText, kind: 'line', onClick: () => { resolve(false); } },
        { key: 'yes', label: okText, kind: 'primary', danger, onClick: () => { resolve(true); } },
      ],
      onClose: () => resolve(false),
    });
  });
}

export function denyModal(text = '暂无该操作权限') {
  return openModal({
    title: '无法继续',
    body: `<p class="modal-text">${esc(text)}</p>`,
    actions: [{ key: 'ok', label: '知道了', kind: 'primary' }],
  });
}

/** 单行输入。返回 Promise<string|null>。 */
export function promptModal({ title, value = '', placeholder = '', okText = '确定', hint = '' }) {
  return new Promise(resolve => {
    let settled = false;
    const done = v => { if (!settled) { settled = true; resolve(v); } };
    openModal({
      title,
      body: `${hint ? `<p class="hint" style="margin-bottom:10px">${esc(hint)}</p>` : ''}
        <label class="field"><input id="pmInput" maxlength="80" placeholder="${esc(placeholder)}"></label>`,
      actions: [
        { key: 'no', label: '取消', kind: 'line', onClick: () => { done(null); } },
        { key: 'yes', label: okText, kind: 'primary', onClick: mask => {
          done(mask.querySelector('#pmInput').value.trim() || null);
        } },
      ],
      onMount: mask => {
        const input = mask.querySelector('#pmInput');
        input.value = value;
        input.focus();
        input.select();
        input.addEventListener('keydown', e => {
          if (e.key === 'Enter') { e.preventDefault(); done(input.value.trim() || null); closeModal(); }
        });
      },
      onClose: () => done(null),
    });
  });
}
