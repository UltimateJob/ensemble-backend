import { stream } from './api';
import type { Run } from './types';

/**
 * Run 事件流订阅。
 *
 * 与群聊 SSE 共用同一个 stream()，只是事件类型不同。
 * approval.required 事件里的 data 就是后端的 agent.Approval 结构。
 */
export interface RunEvent {
  seq: number;
  type:
    | 'run.start'
    | 'step.start'
    | 'assistant.message'
    | 'tool.start'
    | 'tool.end'
    | 'approval.required'
    | 'diff'
    | 'review.started'
    | 'run.end'
    | 'error'
    | '__error'
    | '__closed';
  at: string;
  data?: any;
}

export interface Approval {
  id: string;
  runId: string;
  toolCallId?: string;
  agentKey?: string;
  tool: string;
  summary: string;
  risk: string;
  /** 待执行的完整命令（run 工具才有） */
  command?: string;
  /** 写入类操作会带 diff 预览 */
  diff?: string;
  status: 'pending' | 'approved' | 'rejected' | 'expired';
  reason?: string;
}

export function subscribeRun(
  runId: string,
  onEvent: (e: RunEvent) => void,
): () => void {
  // GET 事件流也要走 stream()，所以这里手写一个 GET 版本。
  // 复用 POST 的 stream 会把 body 带上，虽然后端会忽略，但语义不对。
  const controller = new AbortController();
  let closed = false;

  (async () => {
    // 动态 import 会让打包更复杂，这里直接内联实现。
    const { getBaseURL, getToken } = await import('./api');
    const token = getToken();
    const url = `${getBaseURL()}/v1/runs/${runId}/events`;
    try {
      const res = await fetch(url, {
        headers: {
          Accept: 'text/event-stream',
          ...(token ? { Authorization: `Bearer ${token.value}` } : {}),
        },
        signal: controller.signal,
      });
      if (!res.ok || !res.body) {
        onEvent({ seq: 0, type: '__error', at: new Date().toISOString(), data: `订阅失败（${res.status}）` });
        return;
      }
      const reader = res.body.getReader();
      const dec = new TextDecoder();
      let buf = '';
      for (;;) {
        const { done, value } = await reader.read();
        if (done) break;
        buf += dec.decode(value, { stream: true });
        let idx: number;
        while ((idx = buf.search(/\r?\n\r?\n/)) >= 0) {
          const frame = buf.slice(0, idx);
          buf = buf.slice(idx + (buf.startsWith('\r\n\r\n', idx) ? 4 : 2));
          for (const line of frame.split(/\r?\n/)) {
            if (!line.startsWith('data:')) continue;
            const raw = line.slice(5).trim();
            if (!raw || raw === '[DONE]') continue;
            try {
              onEvent(JSON.parse(raw) as RunEvent);
            } catch {
              /* 单帧坏掉不中断 */
            }
          }
        }
      }
    } catch (e) {
      if (!controller.signal.aborted) {
        onEvent({
          seq: 0,
          type: '__error',
          at: new Date().toISOString(),
          data: e instanceof Error ? e.message : '连接中断',
        });
      }
    } finally {
      if (!closed) onEvent({ seq: 0, type: '__closed', at: new Date().toISOString() });
    }
  })();

  return () => {
    closed = true;
    controller.abort();
  };
}

/** 订阅某个工作区里所有活跃 Run 的审批事件，供聊天页统一弹窗。 */
export function watchApprovals(
  workspaceId: string,
  runs: Run[],
  onApproval: (a: Approval, runId: string) => void,
): () => void {
  const stops = runs
    .filter((r) => r.status === 'waiting_approval' || r.status === 'running')
    .map((r) =>
      subscribeRun(r.id, (e) => {
        if (e.type === 'approval.required' && e.data) {
          onApproval(e.data as Approval, r.id);
        }
      }),
    );
  return () => stops.forEach((s) => s());
}
