/* ============================================================
   房间右键菜单：改名 / 退出 / 删除。

   之前这些操作只能「进房间 → 齿轮 → 解散团队」，而解散是不可逆的。
   房间一多，列表就没法打理了。

   权限边界由后端决定（只有群主能删/改名），这里按 conv 判断只是为了
   不展示点了必然失败的菜单项——不是安全边界。
   ============================================================ */

import { api, esc, icon, state } from './core.js';
import { confirmModal, promptModal } from './modal.js';

/**
 * 给房间条目绑定右键 / 长按菜单。
 * @param {HTMLElement} el 房间条目
 * @param {object} conv 会话对象
 * @param {function} onChanged 改动后回调（通常是刷新列表）
 */
export function bindRoomMenu(el, conv, onChanged) {
  el.addEventListener('contextmenu', e => {
    e.preventDefault();
    openRoomMenu(e.clientX, e.clientY, conv, onChanged);
  });
  // 触屏没有右键：长按 500ms 打开
  let timer = null;
  el.addEventListener('touchstart', e => {
    timer = setTimeout(() => {
      const t = e.touches[0];
      openRoomMenu(t.clientX, t.clientY, conv, onChanged);
    }, 500);
  }, { passive: true });
  const cancel = () => { if (timer) { clearTimeout(timer); timer = null; } };
  el.addEventListener('touchend', cancel);
  el.addEventListener('touchmove', cancel);
}

function openRoomMenu(x, y, conv, onChanged) {
  document.querySelector('.room-menu')?.remove();

  const me = state.user?.id;
  const isOwner = !conv.userId || conv.userId === me;
  const isCollaborator = (conv.collaborators || []).includes(me);

  const menu = document.createElement('div');
  menu.className = 'room-menu';
  menu.innerHTML = `
    ${isOwner ? `<button class="room-menu-i" data-act="rename">${icon('pencil', 14)}改名</button>` : ''}
    ${!isOwner && isCollaborator ? `<button class="room-menu-i" data-act="leave">${icon('log-out', 14)}退出该群</button>` : ''}
    ${isOwner ? `<button class="room-menu-i danger" data-act="del">${icon('trash', 14)}删除</button>` : ''}
  `;

  // 贴近视口边缘时翻转，避免菜单被裁掉
  menu.style.position = 'fixed';
  menu.style.left = Math.min(x, window.innerWidth - 180) + 'px';
  menu.style.top = Math.min(y, window.innerHeight - 140) + 'px';
  menu.style.zIndex = '9000';
  document.body.appendChild(menu);

  const close = () => {
    menu.remove();
    document.removeEventListener('mousedown', onDoc, true);
    document.removeEventListener('keydown', onEsc);
  };
  const onDoc = e => { if (!menu.contains(e.target)) close(); };
  const onEsc = e => { if (e.key === 'Escape') close(); };
  setTimeout(() => {
    document.addEventListener('mousedown', onDoc, true);
    document.addEventListener('keydown', onEsc);
  }, 0);

  menu.querySelector('[data-act=rename]')?.addEventListener('click', async () => {
    close();
    const name = await promptModal({
      title: '改个名字',
      value: conv.title || '',
      placeholder: '群名',
      okText: '保存',
    });
    if (!name || name === conv.title) return;
    try {
      await api('/v1/conversations/' + encodeURIComponent(conv.id), {
        method: 'PATCH', body: { title: name },
      });
      onChanged?.(conv.id, name);
    } catch (e) {
      toastErr(e, '改名失败');
    }
  });

  menu.querySelector('[data-act=leave]')?.addEventListener('click', async () => {
    close();
    const ok = await confirmModal({
      title: '退出这个群？',
      text: '退出后你不再看到这个群，历史记录仍归群主。',
      okText: '退出', danger: true,
    });
    if (!ok) return;
    try {
      await api('/v1/conversations/' + encodeURIComponent(conv.id) + '/leave', { method: 'POST' });
      onChanged?.(conv.id, null, 'leave');
    } catch (e) {
      toastErr(e, '退出失败');
    }
  });

  menu.querySelector('[data-act=del]')?.addEventListener('click', async () => {
    close();
    const ok = await confirmModal({
      title: '删除「' + (conv.title || '这个群') + '」？',
      text: '群和里面的消息都会删掉，不可恢复。想留着就先别删。',
      okText: '删除', danger: true,
    });
    if (!ok) return;
    try {
      await api('/v1/conversations/' + encodeURIComponent(conv.id), { method: 'DELETE' });
      onChanged?.(conv.id, null, 'delete');
    } catch (e) {
      toastErr(e, '删除失败');
    }
  });
}

async function toastErr(e, fallback) {
  const { toast } = await import('./core.js');
  const msg = e?.status === 403 ? '只有群主能做这个操作' : (e?.message || fallback);
  toast(msg, { type: 'err' });
}
