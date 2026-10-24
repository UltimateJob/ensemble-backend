/* 登录：手机号 / 邮箱；验证码或密码。验证码模式保留原有演示账号直登。 */

import { $, api, state, toast } from '../js/core.js';

/**
 * 把线上令牌同步给本地后端。
 *
 * 浏览器模式（纯本地开发）没有这个端点，404 就静默跳过；
 * 桌面端则由本地后端用它转发模型调用到线上。
 *
 * 令牌同时存一份到 localStorage：本地后端每次启动都是新进程，
 * 不重新注入的话，重开 App 后聊天会直接失败。
 */
export async function syncRemoteToken(token) {
  if (token) localStorage.setItem('ens_remote_token', token);
  else localStorage.removeItem('ens_remote_token');
  try {
    await api('/v1/remote/token', { method: 'POST', body: { token } });
  } catch (e) {
    // 没配线上后端时端点不存在，属正常情况
  }
}

export function mountLogin(host) {
  host.innerHTML = `
  <div class="login-card">
    <img class="login-logo" src="/site/brand/logo-mark.png" alt="纸团">
    <h1 class="login-h1">把一件真事，交给一个纸团。</h1>
    <p class="login-sub">几个角色分工协作，每一步都落成文件，最后由你确认。</p>

    <div class="login-seg" id="loginSeg">
      <button class="on" data-k="phone">手机号</button>
      <button data-k="email">邮箱</button>
    </div>
    <div class="login-seg login-seg--sub" id="modeSeg">
      <button class="on" data-m="code">验证码登录</button>
      <button data-m="password">密码登录</button>
    </div>

    <div id="loginTarget">
      <div id="loginPhone">
        <label class="field"><span>手机号</span>
          <input id="phone" type="tel" placeholder="138 0000 0000" autocomplete="tel"></label>
      </div>
      <div id="loginEmail" class="hidden">
        <label class="field"><span>邮箱</span>
          <input id="email" type="email" placeholder="you@company.com" autocomplete="email"></label>
        <!-- 兼容旧客户端的隐藏字段：邮箱模式的验证码和手机号共用 #code。 -->
        <input id="code2" type="hidden" value="">
      </div>
    </div>

    <div id="modeCode">
      <label class="field"><span>验证码</span>
        <div class="field-row">
          <input id="code" inputmode="numeric" maxlength="6" placeholder="6 位数字">
          <button class="btn btn-line" id="sendCode">获取验证码</button>
        </div>
      </label>
    </div>

    <div id="modePassword" class="hidden">
      <label class="field"><span>密码</span>
        <input id="password" type="password" placeholder="至少 8 位" autocomplete="current-password"></label>
      <div class="login-links"><button class="link-btn" id="forgotBtn">忘记密码？</button></div>
    </div>

    <div class="login-err hidden" id="loginErr"></div>
    <button class="btn btn-primary login-submit" id="doLogin">登录 / 注册</button>
    <p class="login-foot">未注册的账号会自动创建 · 演示账号 13800000000 可直接登录</p>
  </div>`;

  // 浏览器可能在刷新后恢复上次的输入与错误提示。
  // 错误提示必须清掉——用户还没操作就看到「验证码不正确」会以为账号有问题。
  const errBox = host.querySelector('#loginErr');
  if (errBox) { errBox.textContent = ''; errBox.classList.add('hidden'); }

  const firstInput = host.querySelector('#phone');
  if (firstInput) setTimeout(() => firstInput.focus(), 60);

  let kind = 'phone';
  let mode = 'code';

  function syncKind() {
    host.querySelector('#loginPhone').classList.toggle('hidden', kind !== 'phone');
    host.querySelector('#loginEmail').classList.toggle('hidden', kind !== 'email');
    host.querySelector('#sendCode').disabled = mode !== 'code';
  }
  function syncMode() {
    host.querySelector('#modeCode').classList.toggle('hidden', mode !== 'code');
    host.querySelector('#modePassword').classList.toggle('hidden', mode !== 'password');
    host.querySelector('#sendCode').disabled = mode !== 'code';
    host.querySelector('#doLogin').textContent = mode === 'password' ? '登录' : '登录 / 注册';
  }

  host.querySelectorAll('#loginSeg button').forEach(b => b.addEventListener('click', () => {
    host.querySelectorAll('#loginSeg button').forEach(x => x.classList.remove('on'));
    b.classList.add('on');
    kind = b.dataset.k;
    syncKind();
  }));
  host.querySelectorAll('#modeSeg button').forEach(b => b.addEventListener('click', () => {
    host.querySelectorAll('#modeSeg button').forEach(x => x.classList.remove('on'));
    b.classList.add('on');
    mode = b.dataset.m;
    syncMode();
  }));
  syncKind();
  syncMode();

  function showErr(m) {
    const el = $('#loginErr');
    el.textContent = m;
    el.classList.toggle('hidden', !m);
  }

  const targetValue = () => kind === 'phone' ? $('#phone').value.trim() : $('#email').value.trim();

  async function sendCode(btn, { codeId = 'code', targetId = null } = {}) {
    const target = targetId ? $('#' + targetId).value.trim() : targetValue();
    if (!target) { toast('先填手机号或邮箱', { type: 'err' }); return false; }
    btn.disabled = true;
    try {
      const d = await api('/v1/auth/send-code', { method: 'POST', body: { kind, target } });
      if (d.devCode && $(`#${codeId}`)) {
        $('#' + codeId).value = d.devCode;
        toast('验证码已填入（本地模式）', { type: 'ok' });
      } else {
        toast('验证码已发送', { type: 'ok' });
      }
      let n = 60;
      const original = btn.textContent;
      btn.textContent = n + 's';
      const t = setInterval(() => {
        n--;
        btn.textContent = n + 's';
        if (n <= 0) { clearInterval(t); btn.disabled = false; btn.textContent = original; }
      }, 1000);
      return true;
    } catch (e) {
      btn.disabled = false;
      showErr(e.message);
      return false;
    }
  }
  $('#sendCode').addEventListener('click', e => sendCode(e.currentTarget));

  async function login() {
    const target = targetValue();
    if (!target) { showErr('请填写手机号或邮箱'); return; }
    showErr('');
    const btn = $('#doLogin');
    btn.disabled = true;
    btn.textContent = '登录中…';
    try {
      let body;
      if (mode === 'password') {
        body = { kind, target, mode: 'password', password: $('#password').value };
      } else {
        body = { kind, target, code: $('#code').value.trim() };
      }
      const d = await api('/v1/auth/login', { method: 'POST', body });
      state.token = d.token;
      // 两份令牌各管一边：
      //   d.token       本地会话（工作区、执行记录）
      //   d.remoteToken 线上会话（模型调用）
      // 配了线上后端时，登录由本地后端代理到线上，线上的令牌随响应回来；
      // 纯浏览器 / 本地自建账号时没有 remoteToken，跳过即可。
      // Key 在服务器上，客户端不持有——这是「不自带 Key」的关键一步。
      if (d.remoteToken) syncRemoteToken(d.remoteToken);
      state.user = d.user;
      localStorage.setItem('ens_token', d.token);
      host.classList.add('hidden');
      await window.__ens.enterApp();
    } catch (e) {
      if (e.code === 'password_not_set') {
        showErr('这个账号还没设置密码，先用验证码登录，再到「设置 → 账号安全」里设置。');
      } else if (e.code === 'invalid_credentials') {
        showErr('手机号或密码不正确');
      } else {
        showErr(e.message);
      }
      btn.disabled = false;
      btn.textContent = mode === 'password' ? '登录' : '登录 / 注册';
    }
  }
  $('#doLogin').addEventListener('click', login);
  ['#phone', '#email', '#code', '#password'].forEach(s => {
    $(s).addEventListener('keydown', e => { if (e.key === 'Enter') login(); });
  });

  // 找回密码：验证码校验后直接设新密码。
  $('#forgotBtn').addEventListener('click', () => {
    const target = targetValue();
    if (!target) { showErr('先填写手机号或邮箱'); return; }
    openResetFlow(kind, target);
  });
}

function openResetFlow(kind, target) {
  const mask = document.createElement('div');
  mask.className = 'modal-mask';
  mask.innerHTML = `<div class="modal-card" style="max-width:460px" role="dialog" aria-modal="true">
    <div class="modal-head"><h3>找回密码</h3>
      <button class="icon-btn" data-x="close" title="关闭">×</button></div>
    <div class="modal-body">
      <p class="hint" style="margin-bottom:14px">验证码会发到 ${target}。验证通过后可以直接设置新密码。</p>
      <label class="field"><span>验证码</span>
        <div class="field-row">
          <input id="rpCode" inputmode="numeric" maxlength="6" placeholder="6 位数字">
          <button class="btn btn-line" id="rpSend">获取验证码</button>
        </div>
      </label>
      <label class="field"><span>新密码</span>
        <input id="rpPass" type="password" placeholder="8-72 个字符" autocomplete="new-password"></label>
      <div class="login-err hidden" id="rpErr"></div>
    </div>
    <div class="modal-foot">
      <button class="btn btn-line" data-x="cancel">取消</button>
      <button class="btn btn-primary" id="rpGo">重设密码</button>
    </div>
  </div>`;
  document.getElementById('modalRoot').appendChild(mask);
  requestAnimationFrame(() => mask.classList.add('on'));
  const close = () => { mask.classList.remove('on'); setTimeout(() => mask.remove(), 160); document.removeEventListener('keydown', onKey); };
  mask.querySelector('[data-x=close]').onclick = close;
  mask.querySelector('[data-x=cancel]').onclick = close;
  mask.addEventListener('click', e => { if (e.target === mask) close(); });
  function onKey(e) { if (e.key === 'Escape') { e.preventDefault(); close(); } }
  document.addEventListener('keydown', onKey);

  const err = m => {
    const el = mask.querySelector('#rpErr');
    el.textContent = m;
    el.classList.toggle('hidden', !m);
  };
  mask.querySelector('#rpSend').addEventListener('click', async e => {
    const btn = e.currentTarget;
    btn.disabled = true;
    try {
      const d = await api('/v1/auth/send-code', { method: 'POST', body: { kind, target } });
      if (d.devCode) { mask.querySelector('#rpCode').value = d.devCode; toast('验证码已填入（本地模式）', { type: 'ok' }); }
      else toast('验证码已发送', { type: 'ok' });
      let n = 60;
      btn.textContent = n + 's';
      const t = setInterval(() => {
        n--; btn.textContent = n + 's';
        if (n <= 0) { clearInterval(t); btn.disabled = false; btn.textContent = '获取验证码'; }
      }, 1000);
    } catch (e2) {
      btn.disabled = false;
      err(e2.message);
    }
  });
  mask.querySelector('#rpGo').addEventListener('click', async e => {
    const btn = e.currentTarget;
    btn.disabled = true;
    try {
      await api('/v1/auth/reset-password', {
        method: 'POST',
        body: {
          kind, target,
          code: mask.querySelector('#rpCode').value.trim(),
          newPassword: mask.querySelector('#rpPass').value,
        },
      });
      toast('密码已重设，可以用新密码登录了', { type: 'ok' });
      close();
    } catch (e2) {
      err(e2.message === '密码长度需在 8 到 72 个字符之间' ? '密码长度需在 8-72 个字符之间' : e2.message);
      btn.disabled = false;
    }
  });
  mask.querySelector('#rpCode').focus();
}
