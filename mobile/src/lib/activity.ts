import type { AgentActivity, AgentState } from './types';

/**
 * /v1/conversations/{id}/activity 现在返回
 * { workspace, defaultPermission, agents, runs }。
 * 早期版本返回数组，这里两种都接住，并把服务端状态词
 * （running / waiting）翻成 UI 的状态环词（working / blocked）。
 */
export function normalizeActivity(raw: unknown): AgentActivity[] {
  const list: any[] = Array.isArray(raw)
    ? raw
    : Array.isArray((raw as any)?.agents)
      ? (raw as any).agents
      : [];
  return list.map((a) => ({
    ...a,
    state: mapState(a?.state, a),
  }));
}

function mapState(state: string | undefined, a: any): AgentState {
  if (state === 'running' || state === 'working') return 'working';
  if (state === 'waiting' || state === 'waiting_approval' || state === 'blocked') return 'blocked';
  if (state === 'done') return 'done';
  return a?.lastRun ? 'done' : 'idle';
}

/** 活动对象里的工作区信息（旧数组响应没有）。 */
export function activityWorkspace(raw: unknown) {
  const w = (raw as any)?.workspace;
  return w && typeof w === 'object' ? w : null;
}
