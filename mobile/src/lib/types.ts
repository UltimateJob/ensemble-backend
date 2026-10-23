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
  permission?: 'readonly' | 'approval' | 'full';
  scope?: string[];
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
  createdAt: string;
  updatedAt: string;
}

export interface Message {
  id: string;
  conversationId: string;
  role: Role;
  agent?: string;
  content: string;
  artifacts?: string[];
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
  permission: 'readonly' | 'approval' | 'full' | 'audit';
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
