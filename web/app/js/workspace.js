/* ============================================================
   工作区：绑定本地目录、权限档位、解绑
   会话就是工作区的主人：绑在工作区上的执行器按 convId 找工作区。

   工作区是「Agent 能碰哪些文件」的唯一边界。它必须是一个显式的、
   看得见的动作——藏在输入框里没人找得到，而选错目录的后果是 Agent
   在别的地方乱写文件。所以这里提供两种入口：
     · openWorkspaceDialog  完整设置（路径 + 权限 + 解绑）
     · openWorkspacePicker  轻量选择（首次进入 / 换目录）
   ============================================================ */

import { api, state, esc, icon, toast, humanError } from './core.js';
import { openModal, closeModal, confirmModal } from './modal.js';
import { PERMISSIONS } from './activity.js';

// 原生目录选择器由桌面壳注入。浏览器里没有。
const nativePick = () => typeof window.__pickDirectory === 'function';

/**
 * 接上桌面壳的原生目录选择器。
 *
 * 壳选完目录后会回调 window.__ensemblePickDirectory(path)；
 * 这里把路径写回输入框，并触发 input 事件让调用方感知。
 *
 * @returns {boolean} true 表示已交给原生面板（浏览器返回 false）
 */
export function bindNativePicker(input) {
  if (!input) return false;
  if (!window.webkit?.messageHandlers?.pickDirectory) return false;

  window.__ensemblePickDirectory = path => {
    if (!path) return;
    input.value = path;
    input.dispatchEvent(new Event('input', { bubbles: true }));
  };
  window.webkit.messageHandlers.pickDirectory.postMessage({});
  return true;
}

/* ------------------------------------------------------------
   轻量选择面板：首次进入工作室、或想换目录时弹。
   ------------------------------------------------------------ */

/**
 * 打开工作区选择面板。
 *
 * @param {object} opt
 *   convId   绑定的会话（可空）
 *   current  当前工作区对象（可空）
 *   onPick   选完回调 (workspace) => void
 *   onCancel 取消回调
 */
export async function openWorkspacePicker(opt = {}) {
  const { convId = '', current = null, onPick, onCancel } = opt;

  let list = [];
  try {
    list = (await api('/v1/workspaces')) || [];
  } catch (e) {
    list = [];
  }
  if (convId) list = list.filter(w => !w.conversationId || w.conversationId === convId);

  const body = `
    <p class="ws-lead">Agent 只能读写这个目录里的文件，不会碰其他地方。随时可以换。</p>
    <ul class="ws-facts">
      <li>${icon('lock', 15)}目录只留在本机，不上传云端</li>
      <li>${icon('shield-alert', 15)}写文件和执行命令按你选的权限逐项确认</li>
      <li>${icon('refresh-cw', 15)}改动有快照，可以整轮回滚</li>
    </ul>

    ${list.length ? `
      <div class="ws-recent">
        <div class="ws-recent-h">用过的目录</div>
        ${list.map(w => `
          <button class="ws-opt${current && current.id === w.id ? ' on' : ''}" data-id="${esc(w.id)}">
            <span class="ws-opt-ic">${icon('folder', 16)}</span>
            <span class="ws-opt-body">
              <span class="ws-opt-path">${esc(w.root)}</span>
              ${w.label ? `<span class="ws-opt-label">${esc(w.label)}</span>` : ''}
            </span>
            ${current && current.id === w.id ? '<span class="ws-opt-tag">当前</span>' : ''}
          </button>`).join('')}
      </div>` : ''}

    <div class="ws-pick-row">
      ${nativePick()
        ? `<button class="btn btn-primary btn-block" id="wsNative">${icon('folder', 16)}选择目录…</button>
           <span class="hint">也可以按 ⌘O，或从菜单「文件 → 选择工作区目录」</span>`
        : `<label class="field" style="margin:0">
             <span>本机绝对路径</span>
             <input id="wsManual" placeholder="/Users/you/project" spellcheck="false">
           </label>
           <button class="btn btn-primary btn-block" id="wsManualOk">使用这个目录</button>`}
    </div>
  `;

  return openModal({
    title: current ? '更换工作区' : '选择工作区',
    width: 560,
    body,
    actions: [
      { key: 'cancel', label: '取消', kind: 'line', onClick: () => onCancel?.() },
    ],
    onMount: el => {
      // 用过的目录：点一下直接选中
      el.querySelectorAll('.ws-opt').forEach(b => {
        b.addEventListener('click', async () => {
          const w = list.find(x => x.id === b.dataset.id);
          if (!w) return;
          closeModal();
          onPick?.(w);
        });
      });

      const nativeBtn = el.querySelector('#wsNative');
      if (nativeBtn) {
        // 壳选完目录后会调 window.__ensemblePickDirectory(path)
        window.__ensemblePickDirectory = async path => {
          const w = await bindPath(path, convId);
          if (w) { closeModal(); onPick?.(w); }
        };
        nativeBtn.addEventListener('click', () => {
          toast('在弹出的窗口里选目录', { type: 'info' });
          window.webkit.messageHandlers.pickDirectory.postMessage({});
        });
      }

      const manualOk = el.querySelector('#wsManualOk');
      if (manualOk) {
        const input = el.querySelector('#wsManual');
        const go = async () => {
          const path = (input.value || '').trim();
          if (!path) { toast('先填一个目录', { type: 'err' }); return; }
          const w = await bindPath(path, convId);
          if (w) { closeModal(); onPick?.(w); }
        };
        manualOk.addEventListener('click', go);
        input.addEventListener('keydown', e => { if (e.key === 'Enter') { e.preventDefault(); go(); } });
        input.focus();
      }
    },
  });
}

/** 直接按路径绑定（同名目录已存在就直接复用，后端是幂等的）。 */
export async function bindPath(root) {
  const path = String(root || '').trim();
  if (!path) { toast('先选一个目录', { type: 'err' }); return null; }
  try {
    const w = await api('/v1/workspaces', { method: 'POST', body: { root: path } });
    toast('工作区已设为 ' + path, { type: 'ok' });
    return w;
  } catch (e) {
    // 常见原因：目录不存在、不是目录、没权限。后端会给具体文案。
    toast(e.message || humanError(e) || '绑定失败', { type: 'err' });
    return null;
  }
}

/** 取当前会话绑定的工作区（没有就返回 null）。 */
export async function currentWorkspace(convId) {
  try {
    const list = (await api('/v1/workspaces')) || [];
    return list.find(w => w.conversationId === convId) || null;
  } catch (e) {
    return null;
  }
}

/* ------------------------------------------------------------
   完整设置面板：路径 + 权限 + 解绑
   ------------------------------------------------------------ */

export function openWorkspaceDialog(conv, { onDone } = {}) {
  if (!conv) return;
  const current = conv.workspace || {};
  const currentPerm = conv.defaultPermission || 'ask';
  openModal({
    title: current.root ? '工作区设置' : '选择本地工作区',
    width: 560,
    body: `
      <p class="hint" style="margin-bottom:14px">
        角色会在这个目录里读写文件、跑测试。目录只留在本机，不会上传。
      </p>
      <label class="field"><span>目录路径</span>
        <div class="field-row">
          <input id="wsRootInput" placeholder="/Users/you/project" spellcheck="false"
            value="${esc(current.root || '')}">
          <button class="btn btn-line" id="wsPickBtn">选择…</button>
        </div>
        <div class="hint">浏览器里请填写本机绝对路径；桌面端可用「文件 → 选择工作区目录」（⌘O）。</div>
      </label>
      <label class="field"><span>执行权限</span>
        <select id="wsPerm">
          ${PERMISSIONS.filter(p => p.key !== 'readonly').map(p =>
            `<option value="${p.key}"${p.key === currentPerm ? ' selected' : ''}>${p.label} · ${p.hint}</option>`).join('')}
        </select>
      </label>
      <div class="studio-warn hidden" id="wsFullWarn">
        完全访问：命令以你本人的系统权限执行，本地运行不是沙箱。
      </div>
      ${current.root ? `<div class="hint">当前：${esc(current.root)}</div>` : ''}`,
    actions: [
      ...(current.root ? [{ key: 'unbind', label: '解绑', kind: 'line', danger: true, onClick: async () => {
        try {
          await unbindWorkspace(conv);
          toast('已解绑', { type: 'ok' });
          onDone?.();
          return true;
        } catch (e) {
          toast(humanError(e), { type: 'err' });
          return false;
        }
      } }] : []),
      { key: 'cancel', label: '取消', kind: 'line' },
      { key: 'save', label: current.root ? '保存' : '绑定并继续', kind: 'primary', onClick: async mask => {
        const root = mask.querySelector('#wsRootInput');
        const perm = mask.querySelector('#wsPerm');
        const value = root.value.trim();
        if (!value) { toast('先选一个目录', { type: 'err' }); return false; }
        if (perm.value === 'full') {
          const okFull = await confirmModal({
            title: '确认完全访问',
            text: '角色会不经确认就改文件、执行命令。命令以你本人的系统权限运行，请只对信任的目录开启。',
            okText: '我知道，继续',
            danger: true,
          });
          if (!okFull) return false;
        }
        try {
          await bindWorkspace(conv, value, perm.value);
          toast('工作区已绑定', { type: 'ok' });
          onDone?.();
          return true;
        } catch (e) {
          toast(humanError(e), { type: 'err' });
          return false;
        }
      } },
    ],
    onMount: mask => {
      const root = mask.querySelector('#wsRootInput');
      const perm = mask.querySelector('#wsPerm');
      const warn = mask.querySelector('#wsFullWarn');
      const syncWarn = () => warn.classList.toggle('hidden', perm.value !== 'full');
      perm.addEventListener('change', syncWarn);
      syncWarn();
      mask.querySelector('#wsPickBtn').addEventListener('click', () => {
        if (!bindNativePicker(root)) {
          toast('浏览器里请直接填写路径；桌面端用菜单「文件 → 选择工作区目录」（⌘O）', { type: 'info', ms: 4000 });
          root.focus();
        }
      });
      root.focus();
    },
  });
}

export async function bindWorkspace(conv, root, permission) {
  const fresh = await api('/v1/conversations/' + encodeURIComponent(conv.id) + '/workspace', {
    method: 'PUT',
    body: { kind: 'local', root, label: root.split('/').filter(Boolean).pop() || '' },
  });
  // 绑定接口只存路径；权限是会话字段，单独 PATCH，语义更清楚。
  const updated = await api('/v1/conversations/' + encodeURIComponent(conv.id), {
    method: 'PATCH',
    body: { defaultPermission: permission },
  });
  applyConv(conv, updated?.workspace ? updated : fresh, permission);
  return updated || fresh;
}

export async function unbindWorkspace(conv) {
  await api('/v1/conversations/' + encodeURIComponent(conv.id) + '/workspace', { method: 'DELETE' });
  conv.workspace = null;
  state.cur = conv;
  const hit = (state.convs || []).find(c => c.id === conv.id);
  if (hit) {
    hit.workspace = null;
    hit.defaultPermission = '';
  }
  document.dispatchEvent(new CustomEvent('conv:change'));
}

function applyConv(local, fresh, permission) {
  const next = fresh && typeof fresh === 'object' ? fresh : {
    ...local,
    workspace: { root: local.workspace?.root, label: local.workspace?.label, bound: true },
  };
  if (permission) next.defaultPermission = permission;
  Object.assign(local, next);
  state.cur = local;
  const hit = (state.convs || []).find(c => c.id === local.id);
  if (hit) Object.assign(hit, next);
  document.dispatchEvent(new CustomEvent('conv:change'));
}

export function workspacePath(conv) {
  return conv?.workspace?.root || '';
}

/** 聊天顶部的绑定提示卡；已绑定返回空字符串。 */
export function workspaceBannerHTML(conv) {
  if (!conv || workspacePath(conv)) return '';
  return `<div class="ws-banner" id="wsBanner">
    <span class="ws-b-icon">${icon('folder', 17)}</span>
    <div class="ws-b-body">
      <b>先选一个本地工作区</b>
      <span>选好目录后，@ 一个角色就能让它读代码、改文件、跑测试。</span>
    </div>
    <button class="btn btn-primary btn-sm" id="wsBindBtn">选择目录</button>
  </div>`;
}
