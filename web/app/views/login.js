/* 登录：手机号 / 邮箱 + 验证码。保留原有免验证码演示账号。 */

import { $, api, state, toast } from '../js/core.js';

export function mountLogin(host) {
  host.innerHTML = `
  <div class="login-card">
    <img class="login-logo" src="/site/brand/logo.svg" alt="智团">
    <h1 class="login-h1">把一件真事，交给一个智团。</h1>
    <p class="login-sub">几个角色分工协作，每一步都落成文件，最后由你确认。</p>

    <div class="login-seg" id="loginSeg">
      <button class="on" data-k="phone">手机号</button>
      <button data-k="email">邮箱</button>
    </div>

    <div id="loginPhone">
      <label class="field"><span>手机号</span>
        <input id="phone" type="tel" placeholder="138 0000 0000" autocomplete="tel"></label>
      <label class="field"><span>验证码</span>
        <div class="field-row">
          <input id="code" inputmode="numeric" maxlength="6" placeholder="6 位数字">
          <button class="btn btn-line" id="sendCode">获取验证码</button>
        </div>
      </label>
    </div>

    <div id="loginEmail" class="hidden">
      <label class="field"><span>邮箱</span>
        <input id="email" type="email" placeholder="you@company.com" autocomplete="email"></label>
      <label class="field"><span>验证码</span>
        <div class="field-row">
          <input id="code2" inputmode="numeric" maxlength="6" placeholder="6 位数字">
          <button class="btn btn-line" id="sendCode2">获取验证码</button>
        </div>
      </label>
    </div>

    <div class="login-err hidden" id="loginErr"></div>
    <button class="btn btn-primary login-submit" id="doLogin">登录 / 注册</button>
    <p class="login-foot">未注册的账号会自动创建 · 演示账号 13800000000 可直接登录</p>
  </div>`;

  let kind = 'phone';
  host.querySelectorAll('#loginSeg button').forEach(b => b.addEventListener('click', () => {
    host.querySelectorAll('#loginSeg button').forEach(x => x.classList.remove('on'));
    b.classList.add('on');
    kind = b.dataset.k;
    $('#loginPhone').classList.toggle('hidden', kind !== 'phone');
    $('#loginEmail').classList.toggle('hidden', kind !== 'email');
  }));

  function showErr(m) {
    const el = $('#loginErr');
    el.textContent = m;
    el.classList.toggle('hidden', !m);
  }

  async function sendCode(btn, targetId, codeId) {
    const target = $('#' + targetId).value.trim();
    if (!target) { toast('先填手机号或邮箱', { type: 'err' }); return; }
    btn.disabled = true;
    try {
      const d = await api('/v1/auth/send-code', { method: 'POST', body: { kind, target } });
      if (d.devCode) {
        $('#' + codeId).value = d.devCode;
        toast('验证码已填入（本地模式）', { type: 'ok' });
      } else {
        toast('验证码已发送', { type: 'ok' });
      }
      let n = 60;
      btn.textContent = n + 's';
      const t = setInterval(() => {
        n--;
        btn.textContent = n + 's';
        if (n <= 0) { clearInterval(t); btn.disabled = false; btn.textContent = '获取验证码'; }
      }, 1000);
    } catch (e) {
      btn.disabled = false;
      showErr(e.message);
    }
  }
  $('#sendCode').addEventListener('click', e => sendCode(e.currentTarget, 'phone', 'code'));
  $('#sendCode2').addEventListener('click', e => sendCode(e.currentTarget, 'email', 'code2'));

  async function login() {
    const target = kind === 'phone' ? $('#phone').value.trim() : $('#email').value.trim();
    const code = kind === 'phone' ? $('#code').value.trim() : $('#code2').value.trim();
    if (!target) { showErr('请填写手机号或邮箱'); return; }
    showErr('');
    const btn = $('#doLogin');
    btn.disabled = true;
    btn.textContent = '登录中…';
    try {
      const d = await api('/v1/auth/login', { method: 'POST', body: { kind, target, code } });
      state.token = d.token;
      state.user = d.user;
      localStorage.setItem('ens_token', d.token);
      host.classList.add('hidden');
      await window.__ens.enterApp();
    } catch (e) {
      showErr(e.message);
      btn.disabled = false;
      btn.textContent = '登录 / 注册';
    }
  }
  $('#doLogin').addEventListener('click', login);
  ['#phone', '#code', '#email', '#code2'].forEach(s => {
    $(s).addEventListener('keydown', e => { if (e.key === 'Enter') login(); });
  });
}
