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
  busy: false,
  pick: null,
  oldestID: null,
  hasMore: false,
  loadingMore: false,
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

export function esc(s) {
  return String(s == null ? '' : s)
    .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}

export function timeStr(d) {
  const x = new Date(d);
  if (isNaN(x)) return '';
  return x.getHours().toString().padStart(2, '0') + ':' + x.getMinutes().toString().padStart(2, '0');
}

export function agoStr(d) {
  const diff = (Date.now() - new Date(d).getTime()) / 1000;
  if (diff < 60) return '刚刚';
  if (diff < 3600) return Math.floor(diff / 60) + ' 分钟前';
  if (diff < 86400) return Math.floor(diff / 3600) + ' 小时前';
  return Math.floor(diff / 86400) + ' 天前';
}

// 由名字生成稳定的渐变头像
export function avatarStyle(seed, color) {
  let h = 0;
  for (let i = 0; i < seed.length; i++) h = (h * 31 + seed.charCodeAt(i)) >>> 0;
  const hue = h % 360;
  if (color) {
    return `background:linear-gradient(140deg, ${color}, ${shift(color, -22)})`;
  }
  return `background:linear-gradient(140deg, hsl(${hue} 62% 58%), hsl(${(hue + 40) % 360} 62% 46%))`;
}

export function shift(hex, amt) {
  const m = /^#?([a-f\d]{2})([a-f\d]{2})([a-f\d]{2})$/i.exec(hex || '');
  if (!m) return hex;
  const c = [1, 2, 3].map(i => Math.max(0, Math.min(255, parseInt(m[i], 16) + amt)));
  return '#' + c.map(v => v.toString(16).padStart(2, '0')).join('');
}

/* 由名字生成稳定的几何头像：背景 + 2~3 个图形。
   思路参考 Boring Avatars / DiceBear，但不依赖任何外部资源。 */
export function hashStr(str) {
  let h = 2166136261;
  for (let i = 0; i < str.length; i++) {
    h ^= str.charCodeAt(i);
    h = Math.imul(h, 16777619);
  }
  return h >>> 0;
}

export function rng(seed) {
  let s = seed;
  return function () {
    s ^= s << 13; s >>>= 0;
    s ^= s >> 17;
    s ^= s << 5; s >>>= 0;
    return s / 4294967296;
  };
}

export function geoAvatar(name, color) {
  const seed = hashStr(name || 'x');
  const r = rng(seed);
  const pick = arr => arr[Math.floor(r() * arr.length)];

  // 基色：角色色优先，否则由名字推导
  let base = color;
  if (!base) {
    const hue = seed % 360;
    base = `hsl(${hue} 58% 54%)`;
  }
  const tints = [0, 18, -22, 40];
  const t = tints[Math.floor(r() * tints.length)];
  const bg = base;
  const fg = `rgba(255,255,255,.85)`;
  const fg2 = `rgba(255,255,255,.42)`;

  const variant = Math.floor(r() * 4);
  let art = '';

  // 角落装饰，不影响首字可读性
  if (variant === 0) {
    art = `<circle cx="34" cy="6" r="12" fill="rgba(255,255,255,.16)"/>`;
  } else if (variant === 1) {
    art = `<path d="M0 40 L40 14 L40 40 Z" fill="rgba(255,255,255,.16)"/>`;
  } else if (variant === 2) {
    art = `<circle cx="6" cy="34" r="11" fill="rgba(255,255,255,.14)"/>`;
  } else {
    art = `<rect x="26" y="-6" width="22" height="22" rx="6" fill="rgba(255,255,255,.15)" transform="rotate(20 37 5)"/>`;
  }
  const ch = (name || '?').trim().slice(0, 1);
  art += `<text x="20" y="21" text-anchor="middle" dominant-baseline="central"
    font-family="-apple-system,BlinkMacSystemFont,PingFang SC,sans-serif"
    font-size="17" font-weight="600" fill="rgba(255,255,255,.96)">${ch.replace(/[<>&]/g, '')}</text>`;

  return `<svg viewBox="0 0 40 40" xmlns="http://www.w3.org/2000/svg" stroke="none" style="display:block;width:100%;height:100%">
    <defs><linearGradient id="g${seed}" x1="0" y1="0" x2="1" y2="1">
      <stop offset="0%" stop-color="${bg}"/>
      <stop offset="100%" stop-color="${shift(hexOf(bg), -26) || bg}"/>
    </linearGradient></defs>
    <rect width="40" height="40" fill="url(#g${seed})"/>
    ${art}
  </svg>`;
}

// 把 hsl 或 hex 统一成 hex，供 shift 使用
export function hexOf(c) {
  if (/^#/.test(c)) return c;
  const m = /hsl\((\d+)/.exec(c);
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

