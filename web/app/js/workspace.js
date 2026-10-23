/* ============================================================
   工作区选择
   桌面端第一次进代码工作室、或想换目录时，弹出这个面板。

   为什么值得单独做：工作区是「Agent 能碰哪些文件」的唯一边界，
   藏在一个输入框里没人找得到。选错目录的后果是 Agent 在别的地方
   乱写文件——这个动作必须显式、可见、可撤销。
   ============================================================ */

import { api, esc, icon, toast } from './core.js';
import { openModal, closeModal } from './modal.js';

// 原生目录选择器由桌面壳注入。浏览器里没有。
const nativePick = () => typeof window.__pickDirectory === 'function';

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
        ? `<button class="btn btn-primary" id="wsNative">${icon('folder', 16)}选择目录…</button>
           <span class="hint">也可以用菜单「文件 → 选择工作区目录」（⌘O）</span>`
        : `<label class="field" style="margin:0">
             <span>本机绝对路径</span>
             <input id="wsManual" placeholder="/Users/you/project" spellcheck="false">
           </label>
           <button class="btn btn-primary" id="wsManualOk">使用这个目录</button>`}
    </div>
  `;

  const mask = openModal({
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
        // 原生选择器是异步回调的：壳选完目录后调
        // window.__ensemblePickDirectory(path)，见 desktop/EnsembleDesktop.swift
        window.__ensemblePickDirectory = async path => {
          const w = await bindWorkspace(path, convId);
          if (w) { closeModal(); onPick?.(w); }
        };
        nativeBtn.addEventListener('click', () => {
          toast('在弹出的窗口里选目录', { type: 'info' });
          // 壳侧监听这个信号再弹 NSOpenPanel
          window.webkit?.messageHandlers?.pickDirectory?.postMessage({});
        });
      }

      const manualOk = el.querySelector('#wsManualOk');
      if (manualOk) {
        const input = el.querySelector('#wsManual');
        const go = async () => {
          const path = (input.value || '').trim();
          if (!path) { toast('先填一个目录', { type: 'err' }); return; }
          const w = await bindWorkspace(path, convId);
          if (w) { closeModal(); onPick?.(w); }
        };
        manualOk.addEventListener('click', go);
        input.addEventListener('keydown', e => { if (e.key === 'Enter') { e.preventDefault(); go(); } });
        input.focus();
      }
    },
  });
  return mask;
}

/** 绑定一个目录，失败时给出人能看懂的原因。 */
async function bindWorkspace(root, convId) {
  try {
    const body = { root };
    if (convId) body.conversationId = convId;
    const w = await api('/v1/workspaces', { method: 'POST', body });
    toast('工作区已设为 ' + root, { type: 'ok' });
    return w;
  } catch (e) {
    // 常见原因：目录不存在、不是目录、没权限。后端会给具体文案。
    toast(e.message || '绑定失败', { type: 'err' });
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
