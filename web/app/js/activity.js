/* ============================================================
   会话活动：工作区、每个角色的执行状态、最近 Run
   服务端 /activity 返回 {workspace, defaultPermission, agents, runs}。
   ============================================================ */

import { api } from './core.js';

export const PERMISSIONS = [
  { key: 'ask', label: '逐项批准', hint: '每次改文件、跑命令都要你点头' },
  { key: 'approval', label: '自动接受编辑', hint: '文件自动改，命令仍需批准' },
  { key: 'full', label: '完全访问', hint: '文件和命令都自动执行' },
  { key: 'readonly', label: '只读', hint: '只能读、不能改' },
];

const PERM_LABEL = {
  ask: '逐项批准',
  approval: '自动接受编辑',
  full: '完全访问',
  readonly: '只读',
  audit: '审查',
};

export function permissionLabel(p) {
  return PERM_LABEL[p] || '未设置';
}

export function isExecutable(p) {
  return p === 'ask' || p === 'approval' || p === 'full';
}

/** 状态 → 头像环。服务端用 running/waiting，CSS 用 working/blocked。 */
export function ringOf(state) {
  if (state === 'running' || state === 'working') return 'working';
  if (state === 'waiting' || state === 'waiting_approval') return 'blocked';
  if (state === 'done') return 'done';
  return 'idle';
}

export function stateLabel(a) {
  switch (a?.state) {
    case 'running': return a.currentTool
      ? `${toolLabel(a.currentTool.tool)}${a.currentTool.summary ? ' · ' + a.currentTool.summary : ''}`
      : '正在执行';
    case 'waiting':
    case 'waiting_approval': return '等你审批';
    default: return a?.lastRun ? '刚完成' : '空闲';
  }
}

export function toolLabel(t) {
  return ({
    read: '读文件', write: '写文件', edit: '改文件', list: '列目录',
    glob: '找文件', grep: '搜内容', run: '跑命令',
    docx_create: '生成 Word', xlsx_create: '生成 Excel',
  })[t] || t || '执行';
}

export async function loadActivity(convId) {
  if (!convId) return { agents: [], runs: [], workspace: { bound: false } };
  try {
    const d = await api('/v1/conversations/' + encodeURIComponent(convId) + '/activity');
    return {
      workspace: d.workspace || { bound: false },
      defaultPermission: d.defaultPermission || '',
      agents: d.agents || [],
      runs: d.runs || [],
    };
  } catch (e) {
    return { agents: [], runs: [], workspace: { bound: false }, error: e };
  }
}

export function agentActivity(activity, key) {
  return (activity?.agents || []).find(a => a.key === key) || null;
}

export function activeRuns(activity) {
  return (activity?.runs || []).filter(r =>
    r.status === 'running' || r.status === 'waiting' || r.status === 'waiting_approval');
}

/** 群里有工作区、且是代码/工作类群时，界面才需要露出执行入口。 */
export function isWorkRoom(conv) {
  if (!conv) return false;
  if (conv.workspace) return true;
  return conv.studio === 'code';
}
