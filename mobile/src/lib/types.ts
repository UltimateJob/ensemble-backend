/** 与后端 internal/domain 对齐的类型。字段名保持 JSON 原名。 */

export type Role = 'system' | 'user' | 'assistant';

export interface User {
  id: string;
  phone?: string;
  email?: string;
  name: string;
  avatar?: string;
  plan: string;
  quota: number;
  used: number;
  role: string;
  status: string;
  createdAt: string;
  lastSeen: string;
}

export interface ConvAgent {
  key: string;
  characterId?: string;
  name: string;
  title: string;
  prompt?: string;
  emoji: string;
  color: string;
  upgraded?: boolean;
  artifact?: string;
  permission?: 'readonly' | 'ask' | 'approval' | 'full' | 'audit';
  scope?: string[];
}

export interface ConvWorkspace {
  id?: string;
  kind?: string;
  root: string;
  label?: string;
  boundAt?: string;
}

export interface Conversation {
  id: string;
  userId: string;
  title: string;
  studio: string;
  model?: string;
  members: string[];
  agents?: ConvAgent[];
  collaborators: string[];
  /** 会话绑定的本地工作区（角色执行任务的根目录）。 */
  workspace?: ConvWorkspace | null;
  /** ask | approval | full | readonly；空表示跟随角色设置。 */
  defaultPermission?: string;
  /** 按角色 key 覆盖权限。 */
  agentPermissions?: Record<string, string>;
  createdAt: string;
  updatedAt: string;
}

/** 消息里附带的真实产物（来自一次 Run 的改动文件）。 */
export interface ArtifactRef {
  id: string;
  kind: string;
  name: string;
  path?: string;
  size?: number;
  agentKey?: string;
  agentName?: string;
  runId?: string;
  createdAt?: string;
}

export interface Message {
  id: string;
  conversationId: string;
  role: Role;
  agent?: string;
  content: string;
  artifacts?: string[];
  attachments?: ArtifactRef[];
  kind?: string;
  ref?: string;
  mentions?: string[];
  tokensIn: number;
  tokensOut: number;
  model?: string;
  latencyMs: number;
  createdAt: string;
}

export interface ScenarioAgent {
  key: string;
  characterId?: string;
  name: string;
  title: string;
  emoji: string;
  color: string;
  upgraded?: boolean;
  artifact?: string;
  permission?: string;
  scope?: string[];
}

export interface Scenario {
  key: string;
  name: string;
  tagline: string;
  category: 'work' | 'fun';
  icon: string;
  accent: string;
  /** 随机场景（random: true）为 null——角色开群时才挑 */
  agents: ScenarioAgent[] | null;
  opening?: string;
  round: number;
  random?: boolean;
}

export interface ModelInfo {
  key: string;
  provider: string;
  taskOnly?: boolean;
  upstream?: string;
  enabled: boolean;
  weight: number;
  costPer1k?: number;
  note?: string;
}

/* ---------------- 群聊 SSE 事件 ---------------- */

export type ChatEventType =
  | 'start'
  | 'delta'
  | 'end'
  | 'artifact'
  | 'pass'
  | 'ask'
  | 'error'
  | 'done'
  | 'busy'
  | 'quota'
  | '__error'
  | '__closed';

export interface ChatEvent {
  type: ChatEventType;
  agent?: string;
  name?: string;
  color?: string;
  content?: string;
  title?: string;
  detail?: string;
  tokens?: number;
  latency?: number;
  message?: string;
}

/* ---------------- 状态环 ---------------- */

export type AgentState = 'idle' | 'working' | 'done' | 'blocked';

export interface AgentActivity {
  key: string;
  name: string;
  state: AgentState;
  detail?: string;
  runId?: string;
  updatedAt?: string;
  /** 生效的执行权限（ask / approval / full / readonly）。 */
  permission?: string;
  step?: number;
  maxSteps?: number;
  elapsedMs?: number;
  currentTool?: { tool: string; summary?: string; startedAt?: string };
  lastRun?: { status?: string; summary?: string; endedAt?: string };
  tokens?: { in?: number; out?: number };
}

/* ---------------- 角色库 ---------------- */

export interface Character {
  name: string;
  work: string;
  tags: string[];
  tone: string;
  emoji?: string;
}

/* ---------------- 目标 ---------------- */

export type GoalStatus = 'todo' | 'doing' | 'done';

export interface Goal {
  id: string;
  userId: string;
  conversationId?: string;
  title: string;
  description?: string;
  status: GoalStatus;
  assignees?: string[];
  runIds?: string[];
  manualProgress: number;
  progress: number;
  progressSource: 'manual' | 'runs' | 'status';
  createdAt: string;
  updatedAt: string;
  completedAt?: string;
}

/* ---------------- 连接器 ---------------- */

export interface Connector {
  id: string;
  userId: string;
  scope: 'team' | 'private';
  kind: string;
  name: string;
  enabled: boolean;
  config?: Record<string, string>;
  secretKeys?: string[];
  status: 'ok' | 'error' | 'unset';
  lastError?: string;
  checkedAt?: string;
  createdAt: string;
  updatedAt: string;
}

export interface ConnectorsResponse {
  items: Connector[];
  kinds: Record<string, string>;
}

/* ---------------- 资料库 ---------------- */

export interface Artifact {
  id: string;
  userId: string;
  conversationId?: string;
  conversationTitle?: string;
  messageId?: string;
  runId?: string;
  agentKey?: string;
  agentName?: string;
  title: string;
  kind: 'doc' | 'code' | 'sheet' | 'chart' | 'note';
  content?: string;
  summary?: string;
  createdAt: string;
}

/* ---------------- 设备 ---------------- */

export interface Device {
  id: string;
  userId: string;
  name: string;
  platform: string;
  lastSeenIp?: string;
  createdAt: string;
  lastSeen: string;
  revokedAt?: string;
  requireApproval: boolean;
}

/* ---------------- 工作区 / Run ---------------- */

export interface Workspace {
  id: string;
  userId: string;
  conversationId: string;
  root: string;
  label?: string;
  createdAt: string;
}

export type RunStatus =
  | 'running'
  | 'waiting'
  | 'waiting_approval'
  | 'done'
  | 'failed'
  | 'cancelled'
  | 'aborted';

export interface Run {
  id: string;
  workspaceId: string;
  conversationId: string;
  userId: string;
  operatorKey: string;
  permission: 'readonly' | 'ask' | 'approval' | 'full' | 'audit';
  scope?: string[];
  task: string;
  model?: string;
  status: RunStatus;
  step: number;
  maxSteps: number;
  tokensIn: number;
  tokensOut: number;
  costUsd: number;
  startedAt: string;
  endedAt: string;
  summary?: string;
  error?: string;
  forceApproval?: boolean;
  origin?: string;
  toolCalls?: ToolCall[];
  diffs?: { path: string; kind: string; added?: number; removed?: number }[];
}

export interface ToolCall {
  id?: string;
  name: string;
  arg?: { path?: string; cmd?: string; [k: string]: unknown };
  output?: string;
  exitCode?: number;
  ok?: boolean;
}

/**
 * 设备是否在线。
 *
 * 后端在设备令牌通过鉴权时每 10 分钟最多落一次 lastSeen，
 * 所以 25 分钟内有过活动就算在线。不要用「是否配过对」判断——
 * 电脑关机后那个条件永远为真，界面会一直亮着「在线」。
 */
export function isDeviceOnline(d?: Device | null): boolean {
  if (!d || d.revokedAt) return false;
  const t = new Date(d.lastSeen).getTime();
  if (!Number.isFinite(t)) return false;
  return Date.now() - t < 25 * 60 * 1000;
}

/**
 * 纸团号：从用户 ID 推出来的稳定四位展示号。
 *
 * 同一个用户在任何页面都必须看到同一个号——之前「我」页取 ID 后四位、
 * 设置页另做一次哈希，两处对不上，看起来像两个账号。
 * 只用于展示，不参与鉴权。
 */
export function paperNo(id?: string | null): string {
  if (!id) return 'zt-1024';
  let h = 0;
  for (let i = 0; i < id.length; i++) h = (h * 31 + id.charCodeAt(i)) >>> 0;
  return `zt-${1000 + (h % 9000)}`;
}
