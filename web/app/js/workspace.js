/* ============================================================
   工作区：绑定本地目录、权限档位、解绑
   会话就是工作区的主人：绑在工作区上的执行器按 convId 找工作区。
   ============================================================ */

import { api, state, esc, icon, toast, humanError } from './core.js';
import { openModal, confirmModal } from './modal.js';
import { PERMISSIONS } from './activity.js';

export function bindNativePicker(input) {
  if (!input) return false;
  if (window.webkit?.messageHandlers?.pickWorkspace) {
    // 桌面壳：⌘O 和这个按钮走同一个原生面板。
    window.__ensemblePickDirectory = path => {
      if (path) {
        input.value = path;
        input.dispatchEvent(new Event('input', { bubbles: true }));
      }
    };
    window.webkit.messageHandlers.pickWorkspace.postMessage({ requestId: 'ws' });
    return true;
  }
  return false;
}

export function openWorkspaceDialog(conv, { onDone } = {}) {
  if (!conv) return;
  const current = conv.workspace || {};
  const currentPerm = conv.defaultPermission || 'ask';
  openModal({
    title: current.bound ? '工作区设置' : '选择本地工作区',
    width: 560,
    body: `
      <p class="hint" style="margin-bottom:14px">
        角色会在这个目录里读写文件、跑测试。目录只留在本机，不会上传。
      </p>
      <div class="ws-kind">
        <div class="ws-kind-card disabled">
          <div class="wk-t">云端工作区</div>
          <div class="wk-d">即将支持</div>
        </div>
        <div class="ws-kind-card on">
          <div class="wk-t">${icon('folder', 15)} 本地文件夹</div>
          <div class="wk-d">桌面端可直接选目录</div>
        </div>
      </div>
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
      ${current.bound ? `<div class="hint">当前：${esc(current.root)}</div>` : ''}`,
    actions: [
      ...(current.bound ? [{ key: 'unbind', label: '解绑', kind: 'line', danger: true, onClick: async mask => {
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
      { key: 'save', label: current.bound ? '保存' : '绑定并继续', kind: 'primary', onClick: async mask => {
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
          toast('浏览器里请直接填写路径；桌面端用菜单「文件 → 选择工作区目录」', { type: 'info', ms: 4000 });
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
