/* ============================================================
   智团 · 基础层
   工具函数 / 全局状态 / API 封装 / Toast / 骨架 / 空状态
   ============================================================ */

export const $ = s => document.querySelector(s);
export const $$ = s => [...document.querySelectorAll(s)];
export const API = '';

// icons.js 是普通脚本，挂在 window 上；这里转成模块导出。
export const icon = (...a) => window.icon(...a);
export const hydrateIcons = (...a) => window.hydrateIcons(...a);

/* ---------------- 窗口标题 ---------------- */
export function setDocTitle(convTitle, busy) {
  const base = '智团';
  if (!convTitle) { document.title = base; return; }
  document.title = `${busy ? '● ' : ''}${convTitle} · 智团`;
}

/* ---------------- 网络状态 ---------------- */
let onlineHandlers = [];
export function onNetChange(fn) { onlineHandlers.push(fn); }
export function isOnline() { return navigator.onLine !== false; }
function emitNet() {
  const on = isOnline();
  onlineHandlers.forEach(fn => { try { fn(on); } catch (e) { /* 单个回调出错不影响其他 */ } });
}
window.addEventListener('online', emitNet);
window.addEventListener('offline', emitNet);

const urlToken = new URLSearchParams(location.search).get('token');
if (urlToken) localStorage.setItem('ens_token', urlToken);

export const state = {
  token: urlToken || localStorage.getItem('ens_token') || '',
  user: null,
  me: null,
  scenarios: [],
  convs: [],
  cur: null,
  agents: [],
  people: [],
  models: [],
  route: { name: 'lobby', params: {} },
  busy: false,
  oldestID: null,
  hasMore: false,
  loadingMore: false,
  library: [],
  picked: [],
};

/* ---------------- 工具 ---------------- */
export function toast(msg, opt = {}) {
  const { type = 'info', ms = 2200 } = typeof opt === 'number' ? { ms: opt } : opt;
  const t = $('#toast');
  if (!t) return;
  t.className = 'toast toast--' + type + ' on';
  t.innerHTML = `${icon(type === 'ok' ? 'check' : type === 'err' ? 'x' : 'info', 16)}<span>${esc(msg)}</span>`;
  clearTimeout(t._t);
  t._t = setTimeout(() => t.classList.remove('on'), ms);
}

export function humanError(e) {
  const msg = String((e && e.message) || '');
  const status = (e && e.status) || 0;
  const code = (e && e.code) || '';
  if (/Failed to fetch|NetworkError|load failed|network error/i.test(msg)) return '网络断了，没连上服务器。检查一下网络再试。';
  if (status === 401 || code === 'unauthorized') return '登录状态过期了，刷新页面重新登录。';
  if (status === 403) return '没有权限做这件事。';
  if (status === 404) return '东西已经不在了，可能被删掉了。';
  if (status === 429) return '操作太频繁了，等几秒再试。';
  if (status >= 500) return '服务端出错了，稍后再试。';
  if (/timeout|timed out/i.test(msg)) return '服务器没响应，稍等一下再试。';
  return msg || '出了点问题，再试一次。';
}

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

/* ---------------- 颜色 ---------------- */
export function shift(hex, amt) {
  const m = /^#?([a-f\d]{2})([a-f\d]{2})([a-f\d]{2})$/i.exec(hex || '');
  if (!m) return hex;
  const c = [1, 2, 3].map(i => Math.max(0, Math.min(255, parseInt(m[i], 16) + amt)));
  return '#' + c.map(v => v.toString(16).padStart(2, '0')).join('');
}

export function hexOf(c) {
  if (/^#[0-9a-fA-F]{6}$/.test(c || '')) return c;
  const m = /hsl\((\d+)/.exec(c || '');
  if (!m) return '#367BED';
  const h = +m[1] / 360;
  const s = 0.46, l = 0.58;
  const q = l < 0.5 ? l * (1 + s) : l + s - l * s;
  const p = 2 * l - q;
  const hue2rgb = t => {
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
  return `<span class="av ${cls || ''}" style="background:linear-gradient(140deg,${base},${shift(base, -18)})"><b>${esc((name || '?').trim().slice(0, 1))}</b></span>`;
}

export function autoColor(name) {
  let h = 0;
  for (let i = 0; i < name.length; i++) h = (h * 31 + name.charCodeAt(i)) >>> 0;
  return `hsl(${h % 360} 46% 58%)`;
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
  try { json = await res.json(); } catch (e) { /* 空响应 */ }
  if (!res.ok || json.ok === false) {
    const msg = (json.error && json.error.message) || ('HTTP ' + res.status);
    const err = new Error(msg);
    err.status = res.status;
    err.code = json.error && json.error.code;
    throw err;
  }
  return json.data;
}

/* ---------------- 骨架 / 空状态 ---------------- */
export function skeleton(kind = 'card', n = 3) {
  return `<div class="sk sk--${kind}">${Array.from({ length: n }, () =>
    '<div class="sk-row"><div class="sk-b"></div><div class="sk-b short"></div></div>').join('')}</div>`;
}

export function emptyState({ icon: ic = 'file', title, sub = '', action = '' }) {
  return `<div class="empty-state">
    <div class="es-ic">${icon(ic, 22)}</div>
    <div class="es-t">${esc(title)}</div>
    ${sub ? `<div class="es-d">${esc(sub)}</div>` : ''}
    ${action}
  </div>`;
}
