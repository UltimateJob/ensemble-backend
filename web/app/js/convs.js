/* ============================================================
   会话列表：新壳里房间列表挂在左侧导航内。
   这里只保留 chat.js 依赖的两个入口，避免循环 import。
   ============================================================ */

import { api, state } from './core.js';
import { renderNav } from './shell.js';

// chat.js 里通过 setConvOpener 注入「打开会话」；新路由下用不到，
// 保留函数签名避免改调用方。
let openConvFn = () => {};
export function setConvOpener(fn) { openConvFn = fn; }

export async function loadConvs() {
  try {
    const list = await api('/v1/conversations');
    if (list) state.convs = list;
  } catch (e) {
    // 刷新失败不要清空：网络抖一下整个列表消失，用户会以为群全没了。
  }
  renderConvs();
  // 额度跟着每次发言变，左栏那个「剩 N 次」不能只在启动时算一次——
  // 之前用户聊了一下午，数字还停在登录时的值。
  syncQuota();
}

let quotaBusy = false;
async function syncQuota() {
  if (quotaBusy) return;
  quotaBusy = true;
  try {
    const d = await api('/v1/me');
    if (d?.user) {
      state.user = d.user;
      if (state.me) state.me.user = d.user;
      const { renderNavFoot } = await import('./shell.js');
      renderNavFoot(state.user);
    }
  } catch (e) {
    // 拿不到就保持旧值，别把数字清零吓用户
  } finally {
    quotaBusy = false;
  }
}

export { syncQuota };

export function renderConvs() {
  renderNav(state.convs || []);
}

export { openConvFn };
