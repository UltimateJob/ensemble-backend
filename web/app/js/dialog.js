/* ============================================================
   旧弹层 API 的兼容层。
   chat.js 仍在用 uiPrompt / uiEditText / uiEditAgent / uiConfirm，
   这里全部转调 modal.js，保证视觉与新弹窗一致。
   新代码请直接用 modal.js。
   ============================================================ */

import { esc } from './core.js';
import { openModal, confirmModal } from './modal.js';

/** 单行输入。返回 Promise<string|null>（取消返回 null） */
export function uiPrompt(title, value = '', opts = {}) {
  const { placeholder = '', okText = '确定', hint = '', allowEmpty = false } = opts;
  return new Promise(resolve => {
    let settled = false;
    const done = v => { if (!settled) { settled = true; resolve(v); } };
    openModal({
      title,
      body: `${hint ? `<p class="hint" style="margin-bottom:10px">${esc(hint)}</p>` : ''}
        <label class="field"><input id="dlgInput" maxlength="80" placeholder="${esc(placeholder)}"></label>`,
      actions: [
        { key: 'cancel', label: '取消', kind: 'line', onClick: () => done(null) },
        { key: 'ok', label: okText, kind: 'primary', onClick: mask => {
          const v = mask.querySelector('#dlgInput').value.trim();
          done(v || allowEmpty ? v : null);
        } },
      ],
      onMount: mask => {
        const input = mask.querySelector('#dlgInput');
        input.value = value;
        input.focus();
        input.select();
        input.addEventListener('keydown', e => {
          if (e.key === 'Enter') {
            e.preventDefault();
            const v = input.value.trim();
            done(v || allowEmpty ? v : null);
            mask.querySelector('[data-x=ok]').click();
          }
        });
      },
      onClose: () => done(null),
    });
  });
}

/** 多行文本编辑。返回 Promise<string|null>（取消返回 null，空串是有效值） */
export function uiEditText(title, value = '', opts = {}) {
  const { placeholder = '', okText = '保存', hint = '', maxlength = 4000 } = opts;
  return new Promise(resolve => {
    let settled = false;
    const done = v => { if (!settled) { settled = true; resolve(v); } };
    openModal({
      title,
      width: 620,
      body: `${hint ? `<p class="hint" style="margin-bottom:10px">${esc(hint)}</p>` : ''}
        <label class="field"><textarea id="dlgArea" rows="10" maxlength="${maxlength}" placeholder="${esc(placeholder)}"></textarea></label>`,
      actions: [
        { key: 'cancel', label: '取消', kind: 'line', onClick: () => done(null) },
        { key: 'ok', label: okText, kind: 'primary', onClick: mask => {
          done(mask.querySelector('#dlgArea').value.trim());
        } },
      ],
      onMount: mask => {
        const area = mask.querySelector('#dlgArea');
        area.value = value;
        area.focus();
        area.setSelectionRange(value.length, value.length);
      },
      onClose: () => done(null),
    });
  });
}

/** 编辑角色：人设 + 是否升级。返回 {prompt, upgraded} 或 null（取消） */
export function uiEditAgent(name, prompt = '', upgraded = false, caps = []) {
  return new Promise(resolve => {
    let settled = false;
    const done = v => { if (!settled) { settled = true; resolve(v); } };
    const canImage = (caps || []).includes('image');
    openModal({
      title: `${name} 的人设`,
      width: 620,
      body: `
        <label class="field"><textarea id="dlgArea" rows="10" maxlength="4000" placeholder="描述身份、语气、关注点"></textarea></label>
        <label class="studio-check" style="margin-top:4px">
          <input type="checkbox" id="dlgUp">
          <span><b>升级角色</b> <span class="hint">发言用更强的模型（高难度任务全面增强），消耗的额度略高</span></span>
        </label>
        <label class="studio-check" style="margin-top:8px">
          <input type="checkbox" id="dlgImg" ${canImage ? 'checked' : ''}>
          <span><b>能生成图片</b> <span class="hint">派活时可以用 image_generate 真的出图。只给需要画图的角色开，生图较慢也按次计费</span></span>
        </label>`,
      actions: [
        { key: 'cancel', label: '取消', kind: 'line', onClick: () => done(null) },
        { key: 'ok', label: '保存', kind: 'primary', onClick: mask => {
          done({
            prompt: mask.querySelector('#dlgArea').value.trim(),
            upgraded: mask.querySelector('#dlgUp').checked,
            capabilities: mask.querySelector('#dlgImg').checked ? ['image'] : [],
          });
        } },
      ],
      onMount: mask => {
        const area = mask.querySelector('#dlgArea');
        area.value = prompt;
        mask.querySelector('#dlgUp').checked = !!upgraded;
        area.focus();
        area.setSelectionRange(prompt.length, prompt.length);
      },
      onClose: () => done(null),
    });
  });
}

/** 确认。返回 Promise<boolean> */
export function uiConfirm(title, desc = '', opts = {}) {
  const { okText = '确定', danger = false } = opts;
  return confirmModal({ title, text: desc, okText, danger });
}
