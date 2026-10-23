/* ============================================================
   hash 路由
   无服务端改动；旧 ?c=<id> 链接自动迁移到 #/room/<id>。
   ============================================================ */

import { closePanel } from './shell.js';
import { pressFeedback } from './motion.js';

let views = {};
let active = null;
let activeName = '';

/** 解析当前 hash。 */
export function parse(hash) {
  const raw = (hash ?? location.hash ?? '').replace(/^#/, '');
  const [pathPart, qs] = raw.split('?');
  const parts = pathPart.split('/').filter(Boolean);
  const q = new URLSearchParams(qs || '');
  if (!parts.length || parts[0] === 'lobby') return { name: 'lobby', params: {}, query: q };
  const [head, id] = parts;
  if (head === 'room') return { name: 'room', params: { id: id || q.get('c') || '' }, query: q };
  if (head === 'c' && id) return { name: 'room', params: { id }, query: q };
  if (['goals', 'connectors', 'library', 'settings'].includes(head)) {
    return { name: head, params: {}, query: q };
  }
  return { name: 'notfound', params: {}, query: q };
}

export function currentRoute() { return parse(); }

export function navigate(path, { replace = false } = {}) {
  const target = path.startsWith('#') ? path : '#' + path;
  if (replace) {
    history.replaceState(null, '', target);
    render();
    return;
  }
  if (location.hash === target) { render(); return; }
  location.hash = target;
}

export function routeHref(name, params = {}) {
  if (name === 'room') return '#/room/' + encodeURIComponent(params.id || '');
  return '#/' + name;
}

export function defineViews(v) { views = v; }

export function render() {
  const r = parse();
  const host = document.getElementById('main');
  if (!host) return;
  if (active && active.unmount) {
    try { active.unmount(); } catch (e) { console.error('unmount failed', e); }
  }
  // 右面板属于上一个视图的内容，切页时收起，避免显示陈旧数据。
  closePanel();
  active = null;
  activeName = r.name;
  host.innerHTML = '';
  const view = views[r.name] || views.notfound;
  if (!view) return;
  active = view;
  try {
    view.mount(host, r.params, r.query);
  } catch (e) {
    console.error('mount failed', e);
    host.innerHTML = `<div class="page page--center"><div class="empty-state">
      <div class="es-t">这个页面出错了</div>
      <div class="es-d">${String(e.message || e)}</div>
      <button class="btn btn-primary" onclick="location.reload()">重新载入</button>
    </div></div>`;
  }
  // 新视图挂载后统一绑定按压反馈（各视图不必各自处理）
  pressFeedback(host);
  document.dispatchEvent(new CustomEvent('route:change', { detail: r }));
}

export function startRouter() {
  const qc = new URLSearchParams(location.search).get('c');
  if (qc && !location.hash) {
    history.replaceState(null, '', '#/room/' + encodeURIComponent(qc));
  }
  if (!location.hash) history.replaceState(null, '', '#/lobby');
  window.addEventListener('hashchange', render);
  render();
}

export function activeView() { return activeName; }
