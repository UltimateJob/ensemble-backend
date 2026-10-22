/* ============================================================
   Run 订阅总线
   任何页面都能订阅 Run 事件；审批弹窗由这里统一触发，
   不依赖用户当前停留在哪个页面。
   ============================================================ */

import { state } from './core.js';
import { approvalModal } from './modal.js';

const subs = new Map();       // runId -> AbortController
const handlers = new Set();   // (runId, ev) => void
const pendingApprovals = new Set();

export function onRunEvent(fn) {
  handlers.add(fn);
  return () => handlers.delete(fn);
}

export function activeRuns() { return [...subs.keys()]; }

function emit(runId, ev) {
  handlers.forEach(fn => { try { fn(runId, ev); } catch (e) { console.error(e); } });
  document.dispatchEvent(new CustomEvent('run:event', { detail: { runId, ev } }));
}

function handle(runId, ev) {
  if (ev.type === 'approval.required') {
    const ap = ev.data || {};
    // 同一个审批只弹一次：SSE 重放（刷新页面）会重发历史事件。
    const key = ap.id || `${runId}:${ev.seq}`;
    if (!pendingApprovals.has(key)) {
      pendingApprovals.add(key);
      approvalModal({ approval: ap, runId });
      notifyNative('需要你的审批', ap.summary || ap.command || '有一个操作在等你确认');
    }
  }
  if (ev.type === 'run.end') subs.delete(runId);
  emit(runId, ev);
}

export function trackRun(runId) {
  if (!runId || subs.has(runId)) return;
  const ctrl = new AbortController();
  subs.set(runId, ctrl);
  (async () => {
    try {
      const res = await fetch(`/v1/runs/${runId}/events`, {
        headers: { Authorization: 'Bearer ' + state.token },
        signal: ctrl.signal,
      });
      if (!res.ok || !res.body) throw new Error('HTTP ' + res.status);
      const reader = res.body.getReader();
      const dec = new TextDecoder();
      let buf = '';
      for (;;) {
        const { done, value } = await reader.read();
        if (done) break;
        buf += dec.decode(value, { stream: true });
        const parts = buf.split('\n\n');
        buf = parts.pop();
        for (const p of parts) {
          const line = p.trim();
          if (!line.startsWith('data:')) continue;
          let ev;
          try { ev = JSON.parse(line.slice(5).trim()); } catch (e) { continue; }
          handle(runId, ev);
        }
      }
    } catch (e) {
      if (e.name !== 'AbortError') console.warn('run stream', runId, e.message);
    } finally {
      subs.delete(runId);
    }
  })();
}

export function untrackRun(runId) {
  const c = subs.get(runId);
  if (c) { c.abort(); subs.delete(runId); }
}

function notifyNative(title, body) {
  try { window.webkit?.messageHandlers?.notify?.postMessage({ title, body }); } catch (e) { /* 浏览器忽略 */ }
}
