/* ============================================================
   合奏 Ensemble · 基础层
   工具函数 / 头像生成 / 全局状态 / API 封装
   ============================================================ */

export const $ = s => document.querySelector(s);
export const $$ = s => [...document.querySelectorAll(s)];
export const API = '';

// icons.js 是普通脚本，挂在 window 上；这里转成模块导出，
// 让其他模块统一从 core 取，不用记两套。
export const icon = (...a) => window.icon(...a);

/* ---------------- 窗口标题 ---------------- */
// 浏览器标签页/任务栏是用户随时在看的地方。
// 永远显示「合奏 Ensemble」的话，开了几个标签页就分不清哪个是哪个。
export function setDocTitle(convTitle, busy) {
  const base = '合奏 Ensemble';
  if (!convTitle) {
    document.title = base;
    return;
  }
  const dot = busy ? '● ' : '';
  document.title = `${dot}${convTitle} · 合奏`;
}

/* ---------------- 网络状态 ---------------- */
// 断网时界面必须说话。什么都不提示的话，
// 用户点了发送没反应，会以为是产品坏了。
let onlineHandlers = [];
export function onNetChange(fn) { onlineHandlers.push(fn); }
export function isOnline() { return navigator.onLine !== false; }

function emitNet() {
  const on = isOnline();
  onlineHandlers.forEach(fn => {
    try { fn(on); } catch (e) { /* 单个回调出错不影响其他 */ }
  });
}
window.addEventListener('online', emitNet);
window.addEventListener('offline', emitNet);
export const hydrateIcons = (...a) => window.hydrateIcons(...a);

const urlToken = new URLSearchParams(location.search).get('token');
if (urlToken) localStorage.setItem('ens_token', urlToken);

export const state = {
  token: urlToken || localStorage.getItem('ens_token') || '',
  user: null,
  scenarios: [],
  convs: [],
  cur: null,
  agents: [],
  people: [],        // 群成员（含协作者的真实姓名与在线状态）
  busy: false,
  pick: null,
  oldestID: null,
  hasMore: false,
  loadingMore: false,
  me: null,           // /v1/me 的完整响应（含 demo / upstream）
  library: [],        // 角色库
  picked: [],         // 建群时勾选的角色库 ID
};

/* ---------------- 工具 ---------------- */
export function toast(msg, ms) {
  const t = $('#toast');
  t.textContent = msg;
  t.classList.add('on');
  clearTimeout(t._t);
  t._t = setTimeout(() => t.classList.remove('on'), ms || 1800);
}

// 把各种错误翻成用户能看懂、知道下一步做什么的话。
// 原生 fetch 抛的是「Failed to fetch」，对用户没有任何信息量。
export function humanError(e) {
  const msg = String((e && e.message) || '');
  const status = (e && e.status) || 0;
  const code = (e && e.code) || '';

  if (/Failed to fetch|NetworkError|load failed|network error/i.test(msg)) {
    return '网络断了，没连上服务器。检查一下网络再试。';
  }
  if (status === 401 || code === 'unauthorized') return '登录状态过期了，刷新页面重新登录。';
  if (status === 403) return '没有权限做这件事。';
  if (status === 404) return '东西已经不在了，可能被删掉了。';
  if (status === 429) return '操作太频繁了，等几秒再试。';
  if (status >= 500) return '服务端出错了，稍后再试。';
  if (/timeout|timed out/i.test(msg)) return '服务器没响应，稍等一下再试。';
  // 业务错误本身就是人话（「消息不能为空」「额度用完了」），原样放出去
  return msg || '出了点问题，再试一次。';
}

// 引号也要转义：esc 的结果会进 title="…"、data-x="…" 这类属性上下文，
// 只转 & < > 的话，一个 " 就能把属性掐断（颜色、文件名都来自外部数据）。
export function esc(s) {
  return String(s == null ? '' : s)
    .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;').replace(/'/g, '&#39;');
}

export function timeStr(d) {
  const x = new Date(d);
  if (isNaN(x)) return '';
  return x.getHours().toString().padStart(2, '0') + ':' + x.getMinutes().toString().padStart(2, '0');
}

// 完整时间：今天/昨天带时分，更早带日期。
// 消息、搜索结果都用这一份，避免一边「今天 14:30」一边「2026-10-22 14:30」。
export function fullTimeStr(d) {
  const x = new Date(d);
  if (isNaN(x)) return '';
  const hm = x.getHours().toString().padStart(2, '0') + ':' + x.getMinutes().toString().padStart(2, '0');
  const now = new Date();
  if (x.toDateString() === now.toDateString()) return '今天 ' + hm;
  const y = new Date(now);
  y.setDate(y.getDate() - 1);
  if (x.toDateString() === y.toDateString()) return '昨天 ' + hm;
  return (x.getMonth() + 1) + '月' + x.getDate() + '日 ' + hm;
}

export function agoStr(d) {
  const diff = (Date.now() - new Date(d).getTime()) / 1000;
  if (diff < 60) return '刚刚';
  if (diff < 3600) return Math.floor(diff / 60) + ' 分钟前';
  if (diff < 86400) return Math.floor(diff / 3600) + ' 小时前';
  return Math.floor(diff / 86400) + ' 天前';
}

// 颜色微调：把 hex 明度增减 amt，供头像/场景图标的渐变拼接
export function shift(hex, amt) {
  const m = /^#?([a-f\d]{2})([a-f\d]{2})([a-f\d]{2})$/i.exec(hex || '');
  if (!m) return hex;
  const c = [1, 2, 3].map(i => Math.max(0, Math.min(255, parseInt(m[i], 16) + amt)));
  return '#' + c.map(v => v.toString(16).padStart(2, '0')).join('');
}

// 把 hsl 或 hex 统一成合法的 6 位 hex，供 shift / 渐变拼接使用。
// 严格校验：颜色可能来自 LLM 生成的角色设定，任何不是
// #RRGGBB / hsl(...) 的值都落到默认色，避免被拼进 style 里。
export function hexOf(c) {
  if (/^#[0-9a-fA-F]{6}$/.test(c || '')) return c;
  const m = /hsl\((\d+)/.exec(c || '');
  if (!m) return '#4F46E5';
  const h = +m[1] / 360;
  const s = 0.58, l = 0.54;
  const q = l < 0.5 ? l * (1 + s) : l + s - l * s;
  const p = 2 * l - q;
  const hue2rgb = (t) => {
    if (t < 0) t += 1;
    if (t > 1) t -= 1;
    if (t < 1 / 6) return p + (q - p) * 6 * t;
    if (t < 1 / 2) return q;
    if (t < 2 / 3) return p + (q - p) * (2 / 3 - t) * 6;
    return p;
  };
  const R = Math.round(hue2rgb(h + 1 / 3) * 255);
  const G = Math.round(hue2rgb(h) * 255);
  const B = Math.round(hue2rgb(h - 1 / 3) * 255);
  return '#' + [R, G, B].map(v => v.toString(16).padStart(2, '0')).join('');
}

export function makeAv(name, color, cls) {
  const c = color || autoColor(name || '');
  const base = hexOf(c);
  return `<span class="av ${cls || ''}" style="background:linear-gradient(140deg,${base},${shift(base, -30)})"><b>${esc((name || '?').trim().slice(0, 1))}</b></span>`;
}

// 没有角色色时，由名字推导一个稳定色
export function autoColor(name) {
  let h = 0;
  for (let i = 0; i < name.length; i++) h = (h * 31 + name.charCodeAt(i)) >>> 0;
  return `hsl(${h % 360} 56% 52%)`;
}

/* ---------------- API ---------------- */
export async function api(path, opt) {
  opt = opt || {};
  const headers = { 'Content-Type': 'application/json' };
  if (state.token) headers['Authorization'] = 'Bearer ' + state.token;
  const res = await fetch(API + path, {
    method: opt.method || 'GET',
    headers,
    body: opt.body ? JSON.stringify(opt.body) : undefined,
  });
  let json = {};
  try { json = await res.json(); } catch (e) { /* ignore */ }
  if (!res.ok || json.ok === false) {
    const msg = (json.error && json.error.message) || ('HTTP ' + res.status);
    const err = new Error(msg);
    err.status = res.status;
    err.code = json.error && json.error.code;
    throw err;
  }
  return json.data;
}

