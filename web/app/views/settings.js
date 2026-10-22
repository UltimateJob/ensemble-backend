/* ============================================================
   设置 / 个人中心：资料、通知偏好与账号操作
   ============================================================ */

import { api, state, esc, icon, toast } from '../js/core.js';
import { avatarHTML } from '../js/avatars.js';
import { confirmModal, promptModal } from '../js/modal.js';

let root = null;

export function mount(host) {
  root = host;
  render();
  // 验证码登录后直接进 app，boot 不会再拉 /v1/me；这里补一次，额度才是真的。
  if (!state.me) {
    api('/v1/me').then(d => {
      if (!root) return;
      state.me = d;
      state.user = d.user || state.user;
      render();
    }).catch(() => { /* 拉不到就按已有信息渲染 */ });
  }
}

export function unmount() {
  root = null;
}

// preferences 挂在 user 上；state.me 是 /v1/me 的整个响应，两处都兼容。
function prefs() {
  return state.me?.preferences || state.user?.preferences || {};
}

function render() {
  if (!root) return;
  const u = state.user || {};
  // 本机选的头像优先；没选过才回落到账号自带的。
  const localAvatar = localStorage.getItem('ens_avatar') || u.avatar || '';
  const notifyOn = prefs().notify !== 'false';   // 没设置过就当开启

  root.innerHTML = `
    <div class="page">
      <header class="page-head">
        <div><h1>设置</h1><p class="page-sub">账号、通知与偏好</p></div>
      </header>

      <section class="profile-card">
        <div class="pf-av">${localAvatar
          ? `<img src="${esc(localAvatar)}" alt="头像">`
          : avatarHTML(u.name || '我', '#367BED', { size: 64 })}</div>
        <div class="pf-body">
          <div class="pf-name">${esc(u.name || '未命名')}</div>
          <div class="pf-id">${esc(u.phone || u.email || '未绑定手机号或邮箱')} · ${esc(u.plan || 'free')}</div>
          <div class="pf-usage">剩余额度 ${esc(state.me?.remaining ?? '-')} 次</div>
        </div>
        <label class="btn btn-line">更换头像
          <input type="file" id="avatarUp" accept="image/*" hidden>
        </label>
      </section>

      <section class="settings-list">
        ${row('user-round', '个人资料', '修改昵称', 'editName')}
        ${row('bell', '消息通知', '审批与任务完成提醒', 'toggleNotify', notifyOn)}
        ${row('shield-alert', '账号安全', '登录设备与验证方式', 'security')}
        ${row('lock', '隐私设置', '数据与可见范围', 'privacy')}
        ${row('log-out', '退出登录', '', 'logout', null, true)}
      </section>
    </div>`;

  bind();
}

function bind() {
  root.querySelector('#avatarUp')?.addEventListener('change', e => {
    const f = e.target.files?.[0];
    e.target.value = '';   // 允许再次选同一张图
    if (!f) return;
    if (f.size > 400 * 1024) { toast('图片别超过 400KB', { type: 'err' }); return; }
    const reader = new FileReader();
    reader.onload = () => {
      localStorage.setItem('ens_avatar', String(reader.result));
      render();
      toast('头像已更新', { type: 'ok' });
    };
    reader.onerror = () => toast('图片读取失败，换一张试试', { type: 'err' });
    reader.readAsDataURL(f);
  });

  root.querySelector('[data-act=editName]')?.addEventListener('click', async () => {
    const name = await promptModal({
      title: '修改昵称',
      value: state.user?.name || '',
      placeholder: '你的昵称',
      okText: '保存',
    });
    if (!name) return;
    try {
      const d = await api('/v1/me', { method: 'PATCH', body: { name } });
      // handleUpdateMe 直接返回用户对象；带 user 包裹时也兼容。
      const user = d?.user || d;
      state.user = user;
      if (state.me) state.me.user = user;
      render();
      toast('已保存', { type: 'ok' });
    } catch (e) {
      toast('保存失败：' + e.message, { type: 'err' });
    }
  });

  root.querySelector('[data-act=toggleNotify]')?.addEventListener('click', async () => {
    const next = prefs().notify === 'false' ? 'true' : 'false';
    applyNotify(next);   // 先动开关，点击反馈不等网络
    render();
    try {
      const d = await api('/v1/me', { method: 'PATCH', body: { preferences: { notify: next } } });
      // 接口返回的是用户对象，偏好以其为准，避免本地和服务端漂移。
      const user = d?.user || d;
      if (user) {
        state.user = user;
        if (state.me) {
          state.me.user = user;
          state.me.preferences = user.preferences || state.me.preferences;
        }
      }
      toast(next === 'true' ? '已开启通知' : '已关闭通知', { type: 'ok' });
    } catch (e) {
      applyNotify(next === 'true' ? 'false' : 'true');   // 失败回滚
      render();
      toast('设置失败：' + e.message, { type: 'err' });
    }
  });

  root.querySelector('[data-act=security]')?.addEventListener('click', () => {
    toast('当前使用手机号 / 邮箱验证码登录');
  });
  root.querySelector('[data-act=privacy]')?.addEventListener('click', () => {
    toast('工作区文件只存在本机，不会上传');
  });
  root.querySelector('[data-act=logout]')?.addEventListener('click', async () => {
    if (!await confirmModal({
      title: '退出登录',
      text: '退出后需要重新验证才能进入。',
      okText: '退出',
      danger: true,
    })) return;
    localStorage.removeItem('ens_token');
    state.token = '';
    location.reload();
  });
}

// 本地先合入，接口失败再翻回来；后端暂时不落 preferences。
function applyNotify(value) {
  const merged = { ...prefs(), notify: value };
  if (state.me) state.me.preferences = merged;
  if (state.user) state.user.preferences = merged;
}

function row(ic, title, sub, act, on = null, danger = false) {
  return `<button class="set-row${danger ? ' danger' : ''}" data-act="${esc(act)}">
    <span class="set-ic">${icon(ic, 18)}</span>
    <span class="set-body">
      <span class="set-t">${esc(title)}</span>
      ${sub ? `<span class="set-s">${esc(sub)}</span>` : ''}
    </span>
    ${on !== null ? `<span class="switch ${on ? 'on' : ''}"></span>` : icon('chevron-right', 16)}
  </button>`;
}
