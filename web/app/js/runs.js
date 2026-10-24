/* ============================================================
   Run 订阅总线
   任何页面都能订阅 Run 事件；审批弹窗由这里统一触发，
   不依赖用户当前停留在哪个页面。
   ============================================================ */

import { state } from './core.js';
import { enqueueApproval } from './modal.js';

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
      // 排队而不是直接弹：两个 Run 同时要审批时直接弹会互相覆盖
      enqueueApproval({ approval: ap, runId });
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
  pump(runId, ctrl, 0);
}

// pump 拉一次事件流；中途断了就按退避重连。
//
// 之前断线只 console.warn 就结束：气泡停在「执行中」，团队面板停在
// 「验证中」，用户不知道该等还是该刷新。而后端本来就支持历史重放
// （订阅时先补发已发生的事件），所以重连是安全的，不会丢事件。
const RECONNECT_DELAYS = [1000, 2000, 4000, 8000];

async function pump(runId, ctrl, attempt) {
  (async () => {
    try {
      const res = await fetch(`/v1/runs/${runId}/events`, {
        headers: { Authorization: 'Bearer ' + state.token },
        signal: ctrl.signal,
      });
      if (!res.ok || !res.body) throw new Error('HTTP ' + res.status);
      if (attempt > 0) emit(runId, { type: 'stream.reconnected' });
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
      if (e.name === 'AbortError' || ctrl.signal.aborted) return;
      // 断了：告诉界面上「正在重连」，然后退避重试。
      // 重试用尽才真正放弃，并把失败原因交给界面。
      if (attempt < RECONNECT_DELAYS.length) {
        emit(runId, { type: 'stream.reconnecting', attempt: attempt + 1 });
        const delay = RECONNECT_DELAYS[attempt];
        await new Promise(r => setTimeout(r, delay));
        if (ctrl.signal.aborted) return;
        subs.delete(runId);
        trackRunWithCtrl(runId, ctrl, attempt + 1);
        return;
      }
      console.warn('run stream 重连失败', runId, e.message);
      emit(runId, { type: 'stream.lost', reason: e.message || '连接中断' });
    } finally {
      if (!ctrl.signal.aborted) subs.delete(runId);
    }
  })();
}

// trackRunWithCtrl 用已有的 AbortController 继续拉流（重连时不换 controller，
// 否则用户的「停止」按钮会指向一个已经废弃的 controller）。
function trackRunWithCtrl(runId, ctrl, attempt) {
  if (subs.has(runId)) return;
  subs.set(runId, ctrl);
  pump(runId, ctrl, attempt);
}

export function untrackRun(runId) {
  const c = subs.get(runId);
  if (c) { c.abort(); subs.delete(runId); }
}

function notifyNative(title, body) {
  try { window.webkit?.messageHandlers?.notify?.postMessage({ title, body }); } catch (e) { /* 浏览器忽略 */ }
}
