/* ============================================================
   启动装配：登录 → 拉数据 → 渲染导航 → 起路由
   ============================================================ */

import { $, api, state } from './core.js';
import { defineViews, startRouter, navigate } from './router.js';
import { initShell, renderNav, renderNavFoot } from './shell.js';
import { mountLogin } from '../views/login.js';
import { maybeStartOnboarding } from './onboarding.js';
import { onRunEvent } from './runs.js';

import * as lobby from '../views/lobby.js';
import * as room from '../views/room.js';
import * as goals from '../views/goals.js';
import * as connectors from '../views/connectors.js';
import * as library from '../views/library.js';
import * as settings from '../views/settings.js';
import * as notfound from '../views/notfound.js';

defineViews({ lobby, room, goals, connectors, library, settings, notfound });
initShell();

// Run 事件总线：让各页面共用一条通道（detail: {runId, ev}）
onRunEvent(() => {});

function showLogin(msg = '') {
  const host = $('#login');
  $('#app').classList.add('hidden');
  host.classList.remove('hidden');
  mountLogin(host);
  if (msg) {
    setTimeout(() => {
      const el = $('#loginErr');
      if (el) { el.textContent = msg; el.classList.remove('hidden'); }
    }, 0);
  }
}

export async function enterApp() {
  $('#login').classList.add('hidden');
  $('#app').classList.remove('hidden');
  const [convs, scenarios] = await Promise.all([
    api('/v1/conversations').catch(() => []),
    api('/v1/scenarios').catch(() => []),
  ]);
  state.convs = convs || [];
  state.scenarios = scenarios || [];
  renderNav(state.convs);
  renderNavFoot(state.user);
  startRouter();
  maybeStartOnboarding();
}

export async function refreshConvs() {
  state.convs = await api('/v1/conversations').catch(() => state.convs);
  renderNav(state.convs);
  document.dispatchEvent(new CustomEvent('conv:change'));
}

async function boot() {
  if (!state.token) { showLogin(); return; }
  try {
    const d = await api('/v1/me');
    state.user = d.user;
    state.me = d;
    await enterApp();
  } catch (e) {
    if (e.status === 401 || e.code === 'unauthorized') {
      localStorage.removeItem('ens_token');
      state.token = '';
      showLogin();
      return;
    }
    showLogin('网络不通，或者服务正在重启。检查一下网络再试。');
  }
}

// 供其他模块调用
window.__ens = { navigate, enterApp, refreshConvs, showLogin, boot };

document.addEventListener('login:show', e => showLogin(e.detail?.msg || ''));
boot();
