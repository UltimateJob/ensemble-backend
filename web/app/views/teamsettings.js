/* ============================================================
   团队设置：右滑面板里的重命名、封面、成员、复制与解散
   ============================================================ */

import { api, state, esc, icon, toast } from '../js/core.js';
import { openPanel, closePanel, renderNav } from '../js/shell.js';
import { confirmModal, promptModal, openModal, closeModal } from '../js/modal.js';
import { avatarHTML, coverUrl } from '../js/avatars.js';
import { navigate } from '../js/router.js';

const COVERS = ['bid', 'xhs', 'code', 'anime', 'research', 'companion'];

export function openTeamSettings(conv) {
  if (!conv) return;

  openPanel({
    title: '团队设置',
    subtitle: conv.title,
    render: el => {
      // 用户选过就用选的；没选过按团队类型找默认封面；
      // 都没有才回退首字头像（企业级产品不该一上来就是大色块）。
      const picked = localStorage.getItem('ens_cover_' + conv.id) || '';
      const coverKey = picked || conv.studio || '';
      const coverSrc = coverUrl(coverKey);

      el.innerHTML = `
        <div class="ts-cover">
          ${coverSrc
            ? `<img src="${esc(coverSrc)}" alt="团队封面" style="width:112px;height:63px;object-fit:cover;border-radius:8px">`
            : avatarHTML(conv.title, '#367BED', { size: 56 })}
        </div>
        <button class="set-row" data-act="rename">
          <span class="set-ic">${icon('pencil', 17)}</span><span class="set-t">修改团队名称</span>${icon('chevron-right', 16)}
        </button>
        <button class="set-row" data-act="cover">
          <span class="set-ic">${icon('image', 17)}</span><span class="set-t">更换封面</span>${icon('chevron-right', 16)}
        </button>
        <button class="set-row" data-act="members">
          <span class="set-ic">${icon('users', 17)}</span><span class="set-t">查看全部角色</span>${icon('chevron-right', 16)}
        </button>
        <button class="set-row" data-act="copy">
          <span class="set-ic">${icon('copy', 17)}</span><span class="set-t">复制团队模板</span>${icon('chevron-right', 16)}
        </button>
        <button class="set-row danger" data-act="dissolve">
          <span class="set-ic">${icon('trash', 17)}</span><span class="set-t">解散团队</span>
        </button>`;

      el.querySelector('[data-act=rename]').addEventListener('click', () => rename(conv));
      el.querySelector('[data-act=cover]').addEventListener('click', () => pickCover(conv));
      el.querySelector('[data-act=members]').addEventListener('click', () => showMembers(conv));
      el.querySelector('[data-act=copy]').addEventListener('click', () => copyTemplate(conv));
      el.querySelector('[data-act=dissolve]').addEventListener('click', () => dissolve(conv));
    },
  });
}

async function rename(conv) {
  const title = await promptModal({
    title: '修改团队名称',
    hint: '名字会同步到左栏和房间顶栏。',
    value: conv.title || '',
    placeholder: '团队名称',
    okText: '保存',
  });
  if (!title) return;
  if (title === conv.title) return;
  try {
    await api('/v1/conversations/' + encodeURIComponent(conv.id), { method: 'PATCH', body: { title } });
    conv.title = title;
    state.convs = await api('/v1/conversations').catch(() => state.convs);
    renderNav(state.convs);   // 左栏房间名同步；conv:change 只刷新页面级视图
    // 房间顶栏是 mount 时一次性渲染的，不监听 conv:change，这里直接改掉
    if (state.cur && state.cur.id === conv.id) {
      state.cur.title = title;
      const h = document.querySelector('#chatTitle');
      if (h) h.textContent = title;
    }
    document.dispatchEvent(new CustomEvent('conv:change'));
    // 用刷新后的会话对象重开面板，标题才算真的更新
    const fresh = state.convs.find(c => c.id === conv.id) || conv;
    openTeamSettings(fresh);
    toast('已保存', { type: 'ok' });
  } catch (e) {
    toast('改名失败：' + e.message, { type: 'err' });
  }
}

function pickCover(conv) {
  const current = localStorage.getItem('ens_cover_' + conv.id) || '';
  openModal({
    title: '更换封面',
    width: 560,
    body: `
      <div class="cover-pick">
        ${COVERS.map(k => {
          const url = coverUrl(k);
          return `<button class="cover-opt" data-c="${k}" title="${k}">
            ${url
              ? `<img src="${esc(url)}" alt="${k}">`
              : `<span class="dim" style="display:grid;place-items:center;height:100%">${icon('image', 20)}</span>`}
          </button>`;
        }).join('')}
      </div>
      <div class="hint">封面只影响本机的团队展示，不会上传。当前：${esc(current || '默认')}</div>`,
    actions: [{ key: 'close', label: '关闭', kind: 'line' }],
    onMount: mask => {
      mask.querySelectorAll('.cover-opt').forEach(b => b.addEventListener('click', () => {
        localStorage.setItem('ens_cover_' + conv.id, b.dataset.c);
        closeModal();
        openTeamSettings(conv);   // 面板上的预览同步换掉
        toast('封面已更新（本机）', { type: 'ok' });
      }));
    },
  });
}

function showMembers(conv) {
  const agents = conv.agents?.length
    ? conv.agents
    : (state.scenarios.find(s => s.key === conv.studio)?.agents || []);
  openModal({
    title: '全部角色',
    width: 520,
    body: agents.length
      ? agents.map(a => `
        <div class="nt-row">
          ${avatarHTML(a.name, a.color, { size: 32 })}
          <div class="nt-body">
            <div class="nt-name">${esc(a.name)}</div>
            <div class="nt-title">${esc(a.title || '')}</div>
          </div>
        </div>`).join('')
      : '<div class="panel-empty">这个团队还没有角色</div>',
    actions: [{ key: 'close', label: '关闭', kind: 'line' }],
  });
}

async function copyTemplate(conv) {
  try {
    await api('/v1/studios', {
      method: 'POST',
      body: { fromConvId: conv.id, name: (conv.title || '团队') + '（副本）' },
    });
    toast('已存为模板，可在变体里换人', { type: 'ok' });
  } catch (e) {
    toast('保存模板失败：' + e.message, { type: 'err' });
  }
}

async function dissolve(conv) {
  if (!await confirmModal({
    title: '解散团队',
    text: `「${conv.title || '这个团队'}」的会话与消息会一起删除，不能恢复。`,
    okText: '解散',
    danger: true,
  })) return;
  try {
    await api('/v1/conversations/' + encodeURIComponent(conv.id), { method: 'DELETE' });
    state.convs = await api('/v1/conversations').catch(() => state.convs.filter(c => c.id !== conv.id));
    if (state.cur && state.cur.id === conv.id) state.cur = null;
    renderNav(state.convs);
    document.dispatchEvent(new CustomEvent('conv:change'));
    closePanel();   // 团队没了，面板再留着就是死内容
    navigate('#/lobby');
    toast('已解散', { type: 'ok' });
  } catch (e) {
    toast('解散失败：' + e.message, { type: 'err' });
  }
}
