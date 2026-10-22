/* ============================================================
   合奏 Ensemble · 对话弹层
   替代原生 prompt / confirm。
   原生弹窗是浏览器灰色的方块，和产品视觉完全断层。
   ============================================================ */

import { esc } from './core.js';

function shell(html) {
  const mask = document.createElement('div');
  mask.className = 'ui-dlg';
  mask.innerHTML = `<div class="ui-dlg-card">${html}</div>`;
  document.body.appendChild(mask);
  requestAnimationFrame(() => mask.classList.add('on'));
  return mask;
}

function close(mask) {
  mask.classList.remove('on');
  setTimeout(() => mask.remove(), 180);
}

/** 单行输入。返回 Promise<string|null>（取消返回 null） */
export function uiPrompt(title, value = '', opts = {}) {
  return new Promise(resolve => {
    const { placeholder = '', okText = '确定', hint = '', allowEmpty = false } = opts;
    const mask = shell(`
      <h3>${esc(title)}</h3>
      ${hint ? `<p class="ui-dlg-hint">${esc(hint)}</p>` : ''}
      <input class="ui-dlg-input" id="dlgInput" value="${String(value).replace(/"/g, '&quot;')}"
             placeholder="${placeholder}" maxlength="80">
      <div class="ui-dlg-act">
        <button class="ui-dlg-btn" data-x="cancel">取消</button>
        <button class="ui-dlg-btn primary" data-x="ok">${okText}</button>
      </div>`);
    const input = mask.querySelector('#dlgInput');
    input.focus();
    input.select();

    const done = v => { close(mask); document.removeEventListener('keydown', onKey); resolve(v); };
    // allowEmpty：留空也算确认（比如「让他们聊什么」可以不填），
    // 默认留空等同取消，保持旧行为。
    const ok = () => {
      const v = input.value.trim();
      done(v || allowEmpty ? v : null);
    };

    mask.querySelector('[data-x=ok]').onclick = ok;
    mask.querySelector('[data-x=cancel]').onclick = () => done(null);
    mask.addEventListener('click', e => { if (e.target === mask) done(null); });

    function onKey(e) {
      if (e.key === 'Enter') { e.preventDefault(); ok(); }
      if (e.key === 'Escape') { e.preventDefault(); done(null); }
    }
    document.addEventListener('keydown', onKey);
  });
}

/** 多行文本编辑。返回 Promise<string|null>（取消返回 null，空串是有效值） */
export function uiEditText(title, value = '', opts = {}) {
  return new Promise(resolve => {
    const { placeholder = '', okText = '保存', hint = '', maxlength = 4000 } = opts;
    const mask = shell(`
      <h3>${esc(title)}</h3>
      ${hint ? `<p class="ui-dlg-hint">${esc(hint)}</p>` : ''}
      <textarea class="ui-dlg-area" id="dlgArea" placeholder="${placeholder}" maxlength="${maxlength}"></textarea>
      <div class="ui-dlg-act">
        <button class="ui-dlg-btn" data-x="cancel">取消</button>
        <button class="ui-dlg-btn primary" data-x="ok">${okText}</button>
      </div>`);
    const area = mask.querySelector('#dlgArea');
    area.value = value;
    area.focus();
    area.setSelectionRange(value.length, value.length);

    const done = v => { close(mask); document.removeEventListener('keydown', onKey); resolve(v); };
    const ok = () => done(area.value.trim());

    mask.querySelector('[data-x=ok]').onclick = ok;
    mask.querySelector('[data-x=cancel]').onclick = () => done(null);
    mask.addEventListener('click', e => { if (e.target === mask) done(null); });

    function onKey(e) {
      if (e.key === 'Escape') { e.preventDefault(); done(null); }
      if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) { e.preventDefault(); ok(); }
    }
    document.addEventListener('keydown', onKey);
  });
}

/** 编辑角色：人设 + 是否升级。返回 {prompt, upgraded} 或 null（取消） */
export function uiEditAgent(name, prompt = '', upgraded = false) {
  return new Promise(resolve => {
    const mask = shell(`
      <h3>${esc(name)} 的人设</h3>
      <textarea class="ui-dlg-area" id="dlgArea" placeholder="描述身份、语气、关注点" maxlength="4000"></textarea>
      <label class="ui-dlg-up" id="upRow" title="升级后发言走更强的任务模型（处理文件、写代码这类高难度任务）">
        <input type="checkbox" id="dlgUp">
        <span><b>升级角色</b><small>发言用更强的模型（高难度任务全面增强），消耗的额度略高</small></span>
      </label>
      <div class="ui-dlg-act">
        <button class="ui-dlg-btn" data-x="cancel">取消</button>
        <button class="ui-dlg-btn primary" data-x="ok">保存</button>
      </div>`);
    const area = mask.querySelector('#dlgArea');
    const up = mask.querySelector('#dlgUp');
    area.value = prompt; up.checked = !!upgraded;
    area.focus(); area.setSelectionRange(prompt.length, prompt.length);
    const done = v => { close(mask); document.removeEventListener('keydown', onKey); resolve(v); };
    const ok = () => done({ prompt: area.value.trim(), upgraded: up.checked });
    mask.querySelector('[data-x=ok]').onclick = ok;
    mask.querySelector('[data-x=cancel]').onclick = () => done(null);
    mask.addEventListener('click', e => { if (e.target === mask) done(null); });
    function onKey(e) { if (e.key === 'Escape') { e.preventDefault(); done(null); } }
    document.addEventListener('keydown', onKey);
  });
}

/** 确认。返回 Promise<boolean> */
export function uiConfirm(title, desc = '', opts = {}) {
  return new Promise(resolve => {
    const { okText = '确定', danger = false } = opts;
    const mask = shell(`
      <h3>${title}</h3>
      ${desc ? `<p class="ui-dlg-hint">${desc}</p>` : ''}
      <div class="ui-dlg-act">
        <button class="ui-dlg-btn" data-x="cancel">取消</button>
        <button class="ui-dlg-btn ${danger ? 'danger' : 'primary'}" data-x="ok">${okText}</button>
      </div>`);

    const done = v => { close(mask); document.removeEventListener('keydown', onKey); resolve(v); };
    mask.querySelector('[data-x=ok]').onclick = () => done(true);
    mask.querySelector('[data-x=cancel]').onclick = () => done(false);
    mask.addEventListener('click', e => { if (e.target === mask) done(false); });
    const btn = mask.querySelector('[data-x=ok]');
    btn.focus();

    function onKey(e) {
      if (e.key === 'Escape') { e.preventDefault(); done(false); }
      if (e.key === 'Enter') { e.preventDefault(); done(true); }
    }
    document.addEventListener('keydown', onKey);
  });
}
