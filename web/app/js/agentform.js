/* ============================================================
   新建角色

   角色库之前只能读不能写：界面上没有任何创建入口，用户只能靠
   预置角色。而「给角色配头像」「给角色开生图能力」这些需求
   都挂在这张表单上。

   头像走平台固定提示词：用户只填名字和简介，风格由我们定，
   这样一排头像放在群里才像一套。
   ============================================================ */

import { api, state, esc, icon, toast, humanError } from './core.js';
import { openModal, closeModal } from './modal.js';

const COLORS = ['#2F6BFF', '#DB2777', '#047857', '#B45309', '#7C3AED', '#0891B2'];

export function openAgentForm({ onSaved } = {}) {
  let avatarUrl = '';
  let genBusy = false;

  openModal({
    title: '新建角色',
    width: 620,
    body: `
      <div class="af-top">
        <div class="af-av" id="afAv">
          <span class="af-av-ph">${icon('user-round', 24)}</span>
        </div>
        <div class="af-av-act">
          <button class="btn btn-line btn-sm" id="afGen">${icon('sparkles', 13)}生成头像</button>
          <div class="hint" id="afGenHint">按名字和简介生成，风格和官方角色一致</div>
        </div>
      </div>

      <label class="field"><span>名字</span>
        <input id="afName" maxlength="20" placeholder="比如：林深" autocomplete="off">
      </label>
      <label class="field"><span>一句话定位</span>
        <input id="afTitle" maxlength="24" placeholder="比如：产品设计" autocomplete="off">
      </label>
      <label class="field"><span>人设</span>
        <textarea id="afPrompt" rows="6" maxlength="2000"
          placeholder="描述这个角色怎么说话、关注什么、擅长什么。至少一句能指导对话的话。"></textarea>
      </label>

      <div class="field"><span>颜色</span>
        <div class="af-colors">
          ${COLORS.map((c, i) => `<button class="af-color${i === 0 ? ' on' : ''}" data-c="${c}" style="background:${c}"></button>`).join('')}
        </div>
      </div>

      <label class="studio-check">
        <input type="checkbox" id="afImg">
        <span><b>能生成图片</b> <span class="hint">派活时可以用 image_generate 真的出图。只给需要画图的角色开</span></span>
      </label>`,
    actions: [
      { key: 'cancel', label: '取消', kind: 'line' },
      { key: 'ok', label: '创建', kind: 'primary', onClick: async mask => {
        const name = mask.querySelector('#afName').value.trim();
        const prompt = mask.querySelector('#afPrompt').value.trim();
        if (!name) { toast('先起个名字', { type: 'err' }); return false; }
        if ([...prompt].length < 10) { toast('人设太短，至少写一句能指导对话的话', { type: 'err' }); return false; }
        const color = mask.querySelector('.af-color.on')?.dataset.c || COLORS[0];
        try {
          await api('/v1/agents', {
            method: 'POST',
            body: {
              name,
              title: mask.querySelector('#afTitle').value.trim(),
              prompt,
              color,
              avatar: avatarUrl,   // 空表示用首字回退
              capabilities: mask.querySelector('#afImg').checked ? ['image'] : [],
            },
          });
          toast('角色已创建', { type: 'ok' });
          // 刷新本地角色库，否则再打开「从角色库添加」还是旧的
          state.library = await api('/v1/characters').catch(() => state.library);
          const mine = await api('/v1/agents').catch(() => null);
          if (mine) state.library = mine;
          onSaved?.();
          return true;
        } catch (e) {
          toast(humanError(e) || '创建失败', { type: 'err' });
          return false;
        }
      } },
    ],
    onMount: mask => {
      mask.querySelectorAll('.af-color').forEach(b => {
        b.addEventListener('click', () => {
          mask.querySelectorAll('.af-color').forEach(x => x.classList.remove('on'));
          b.classList.add('on');
        });
      });
      mask.querySelector('#afGen').addEventListener('click', () => generateAvatar(mask));
      mask.querySelector('#afName').focus();
    },
  });

  async function generateAvatar(mask) {
    if (genBusy) return;
    const name = mask.querySelector('#afName').value.trim();
    if (!name) { toast('先填名字，生成的头像才对得上', { type: 'err' }); return; }
    genBusy = true;
    const btn = mask.querySelector('#afGen');
    const hint = mask.querySelector('#afGenHint');
    const av = mask.querySelector('#afAv');
    btn.disabled = true;
    hint.textContent = '生成中，大约十几秒…';
    av.innerHTML = '<span class="spinner"></span>';
    try {
      const d = await api('/v1/agents/avatar/generate', {
        method: 'POST',
        body: {
          name,
          title: mask.querySelector('#afTitle').value.trim(),
          prompt: mask.querySelector('#afPrompt').value.trim(),
        },
      });
      avatarUrl = d.avatar;
      av.innerHTML = `<img src="${esc(d.avatar)}" alt="">`;
      hint.textContent = '不满意就再生成一次，每次都不一样';
    } catch (e) {
      av.innerHTML = `<span class="af-av-ph">${icon('user-round', 24)}</span>`;
      hint.textContent = e.status === 503
        ? '还没有配置生图渠道，头像会先用首字'
        : (e.message || '生成失败，可以重试');
    } finally {
      genBusy = false;
      btn.disabled = false;
    }
  }
}
