/* ============================================================
   头像：优先查预生成的黏土头像，未命中回退 CSS 首字头像。
   状态环四色由 tokens.css 定义。
   ============================================================ */

import { esc, hexOf, shift, autoColor } from './core.js';

let manifest = { characters: {}, covers: {} };
let loading = null;

export function loadAvatars() {
  if (!loading) {
    loading = fetch('avatars/manifest.json')
      .then(r => (r.ok ? r.json() : null))
      .then(m => { if (m && m.characters) manifest = m; })
      .catch(() => { /* 没有素材时静默回退 */ });
  }
  return loading;
}
loadAvatars();

export function avatarsReady() { return loading; }

export function avatarUrl(name) {
  const hit = manifest.characters?.[name];
  return hit ? 'avatars/' + hit : null;
}

export function coverUrl(key) {
  const hit = manifest.covers?.[key];
  return hit ? 'avatars/' + hit : null;
}

const RING = { idle: 'ring-idle', working: 'ring-working', done: 'ring-done', blocked: 'ring-blocked' };

/**
 * 生成头像 HTML。
 * @param {string} name 角色名（查 manifest 用）
 * @param {string} color 回退头像的底色
 * @param {{size?:number, ring?:string, cls?:string}} opt
 */
export function avatarHTML(name, color, opt = {}) {
  const { size = 32, ring = '', cls = '' } = opt;
  const url = avatarUrl(name);
  const classes = ['av', ring ? RING[ring] || '' : '', cls].filter(Boolean).join(' ');
  const style = `width:${size}px;height:${size}px`;
  if (url) {
    return `<span class="${classes}" style="${style}"><img src="${url}" alt="${esc(name)}" loading="lazy"></span>`;
  }
  const base = hexOf(color || autoColor(name || ''));
  return `<span class="${classes} av--fallback" style="${style};background:linear-gradient(140deg,${base},${shift(base, -18)})">` +
    `<b style="font-size:${Math.max(10, Math.round(size * 0.4))}px">${esc((name || '?').trim().slice(0, 1))}</b></span>`;
}

/** 从会话里取角色头像（供大厅 / 导航复用）。 */
export function agentAvatar(agent, size = 28, ring = '') {
  return avatarHTML(agent?.name || '?', agent?.color, { size, ring });
}
