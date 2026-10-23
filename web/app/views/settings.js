/* ============================================================
   设置 / 个人中心：资料、通知偏好与账号操作
   ============================================================ */

import { api, state, esc, icon, toast } from '../js/core.js';
import { avatarHTML } from '../js/avatars.js';
import { confirmModal, promptModal, openModal } from '../js/modal.js';

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
// 内部枚举值不该直接给用户看。
function planLabel(plan) {
  return ({ free: '免费版', pro: '专业版', team: '团队版' })[plan] || '免费版';
}

function prefs() {
  return state.me?.preferences || state.user?.preferences || {};
}

function render() {
  if (!root) return;
  const u = state.user || {};
  // 本机选的头像优先；没选过才回落到账号自带的。
  const localAvatar = localStorage.getItem('ens_avatar') || u.avatar || '';
  const notifyOn = prefs().notify !== 'false';   // 没设置过就当开启
  const quota = typeof u.quota === 'number' ? u.quota : null;
  const used = typeof u.used === 'number' ? u.used : 0;
  const remaining = quota != null ? Math.max(0, quota - used) : null;
  const pct = quota ? Math.min(100, Math.round(used / quota * 100)) : 0;
  const hasPassword = !!state.me?.hasPassword;

  root.innerHTML = `
    <div class="page">
      <header class="page-head">
        <div><h1>设置</h1><p class="page-sub">账号、通知与偏好</p></div>
      </header>

      <section class="profile-card">
        <div class="pf-av">${localAvatar
          ? `<img src="${esc(localAvatar)}" alt="头像">`
          : avatarHTML(u.name || '我', '#8B8377', { size: 64 })}</div>
        <div class="pf-body">
          <div class="pf-name">${esc(u.name || '未命名')}</div>
          <div class="pf-id">${esc(u.phone || u.email || '未绑定手机号或邮箱')} · ${esc(planLabel(u.plan))}</div>
          <div class="pf-usage">剩余额度 ${remaining != null ? esc(remaining) : '-'} 次</div>
        </div>
        <label class="btn btn-line">更换头像
          <input type="file" id="avatarUp" accept="image/*" hidden>
        </label>
      </section>

      <section class="quota-card">
        <div class="qc-head">
          <div>
            <div class="qc-t">本月额度</div>
            <div class="qc-sub">每轮群聊发言、每次执行任务都会计入用量</div>
          </div>
          <div class="qc-num">${remaining != null ? `${esc(remaining)}<em> / ${esc(quota)}</em>` : '—'}</div>
        </div>
        <div class="qc-bar"><i style="width:${pct}%"></i></div>
        <div class="qc-foot">${quota != null
          ? `已用 ${used} 次 · ${pct}%`
          : '额度信息暂不可用'}</div>
      </section>

      <section class="settings-list">
        ${row('user-round', '个人资料', '修改昵称', 'editName')}
        ${row('bell', '消息通知', '审批与任务完成提醒', 'toggleNotify', notifyOn)}
        ${row('shield-alert', '账号安全', hasPassword ? '已设置密码 · 可修改' : '尚未设置密码（仅验证码登录）', 'security')}
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

  root.querySelector('[data-act=security]')?.addEventListener('click', () => openPasswordDialog());
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

/* ---------------- 账号安全：设置 / 修改密码 ---------------- */

function openPasswordDialog() {
  const has = !!state.me?.hasPassword;
  openModal({
    title: has ? '修改密码' : '设置密码',
    width: 480,
    body: `
      <p class="hint" style="margin-bottom:14px">
        ${has
          ? '修改后，旧的密码立即失效。验证码登录不受影响。'
          : '设置密码后，可以用手机号 / 邮箱 + 密码登录，也可以继续用验证码。'}
      </p>
      ${has ? `<label class="field"><span>原密码</span>
        <input id="oldPass" type="password" autocomplete="current-password"></label>` : ''}
      <label class="field"><span>新密码</span>
        <input id="newPass" type="password" placeholder="8-72 个字符" autocomplete="new-password"></label>
      <label class="field"><span>确认新密码</span>
        <input id="newPass2" type="password" autocomplete="new-password"></label>
      <div class="login-err hidden" id="pwErr"></div>`,
    actions: [
      { key: 'cancel', label: '取消', kind: 'line' },
      { key: 'save', label: has ? '修改密码' : '设置密码', kind: 'primary', onClick: async mask => {
        const oldPass = mask.querySelector('#oldPass')?.value || '';
        const np = mask.querySelector('#newPass').value;
        const np2 = mask.querySelector('#newPass2').value;
        const err = mask.querySelector('#pwErr');
        const fail = m => { err.textContent = m; err.classList.remove('hidden'); return false; };
        if ([...np].length < 8 || [...np].length > 72) return fail('密码长度需在 8-72 个字符之间');
        if (np !== np2) return fail('两次输入的新密码不一致');
        try {
          await api('/v1/me/password', { method: 'POST', body: { oldPassword: oldPass, newPassword: np } });
          if (state.me) state.me.hasPassword = true;
          toast(has ? '密码已修改' : '密码已设置', { type: 'ok' });
          render();
          return true;
        } catch (e) {
          if (e.code === 'old_password_incorrect') return fail('原密码不正确');
          if (e.code === 'weak_password') return fail('密码长度需在 8-72 个字符之间');
          return fail(e.message);
        }
      } },
    ],
    onMount: mask => mask.querySelector(has ? '#oldPass' : '#newPass')?.focus(),
  });
}
