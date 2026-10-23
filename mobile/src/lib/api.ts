import { deleteItem, getItem, setItem } from './storage';

const KEY_BASE = 'zhituan_base_url';
const KEY_DEVICE_TOKEN = 'zhituan_device_token';
const KEY_ACCOUNT_TOKEN = 'zhituan_account_token';

/**
 * 服务器地址。
 *
 * 手机连的是电脑上的后端，地址由用户在配对页填写/扫码得到。
 * 用变量保存而不是写死：同一台手机可能在不同网络下连不同的电脑。
 */
let baseURL = '';

export function getBaseURL(): string {
  return baseURL;
}

export async function loadBaseURL(): Promise<string> {
  if (baseURL) return baseURL;
  baseURL = (await getItem(KEY_BASE)) ?? '';
  return baseURL;
}

export async function setBaseURL(url: string): Promise<void> {
  baseURL = normalizeBase(url);
  await setItem(KEY_BASE, baseURL);
}

/** 补协议、去尾部斜杠。用户手输 `192.168.1.5:8080` 是很常见的事。 */
export function normalizeBase(url: string): string {
  let u = url.trim();
  if (!u) return '';
  if (!/^https?:\/\//i.test(u)) u = 'http://' + u;
  return u.replace(/\/+$/, '');
}

/* ---------------- 令牌 ---------------- */

/**
 * 两套令牌可以同时存在，这是新模型的核心：
 *
 *   账号令牌  —— 登录后就有。PC 和手机看到同一份会话 / 团队。
 *   设备令牌  —— 在「个人页 → 连接电脑」配对后才有。带它请求才代表
 *                「这台设备被授权操作那台电脑上的工作区」。
 *
 * 请求头只带一个，优先设备令牌：配对了就说明用户想让这台设备去动电脑，
 * 服务端也能据此套用「远程设备需要审批」的策略（DeviceFrom）。
 * 之前两个令牌互相删除，导致配对了就没账号数据、登录了就不能控制电脑。
 */
type TokenKind = 'device' | 'account';

let token: { kind: TokenKind; value: string } | null = null;
/** 分开保存，避免「当前用哪个」和「有没有哪个」混在一起。 */
let deviceToken: string | null = null;
let accountToken: string | null = null;

function pickToken() {
  if (deviceToken) token = { kind: 'device', value: deviceToken };
  else if (accountToken) token = { kind: 'account', value: accountToken };
  else token = null;
}

export async function loadToken(): Promise<void> {
  deviceToken = await getItem(KEY_DEVICE_TOKEN);
  accountToken = await getItem(KEY_ACCOUNT_TOKEN);
  pickToken();
}

export function getToken(): { kind: TokenKind; value: string } | null {
  return token;
}

/** 这台设备是否已经连接过电脑（能不能控制工作区）。 */
export function hasDeviceToken(): boolean {
  return !!deviceToken;
}

/** 是否已登录账号（PC 与手机数据同步的前提）。 */
export function hasAccountToken(): boolean {
  return !!accountToken;
}

export async function saveDeviceToken(t: string): Promise<void> {
  deviceToken = t;
  await setItem(KEY_DEVICE_TOKEN, t);
  pickToken();
}

export async function saveAccountToken(t: string): Promise<void> {
  accountToken = t;
  await setItem(KEY_ACCOUNT_TOKEN, t);
  pickToken();
}

/** 只断开电脑配对，保留账号登录。 */
export async function clearDeviceToken(): Promise<void> {
  deviceToken = null;
  await deleteItem(KEY_DEVICE_TOKEN);
  pickToken();
}

/** 退出登录：两套都清。 */
export async function clearToken(): Promise<void> {
  deviceToken = null;
  accountToken = null;
  token = null;
  await deleteItem(KEY_DEVICE_TOKEN);
  await deleteItem(KEY_ACCOUNT_TOKEN);
}

/* ---------------- 请求 ---------------- */

export class ApiError extends Error {
  code: string;
  status: number;
  constructor(status: number, code: string, message: string) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

export interface Envelope<T> {
  ok: boolean;
  data?: T;
  error?: { code: string; message: string };
}

/** 把网络层的错误翻译成人话。原始信息只该进日志，不该甩给用户。 */
export function humanError(e: unknown): string {
  if (e instanceof ApiError) {
    switch (e.status) {
      case 401:
        return '登录状态过期了，请重新配对。';
      case 403:
        return '这台设备没有权限做这件事。';
      case 404:
        return e.message || '找不到对应的内容。';
      case 429:
        return '操作太频繁了，等几秒再试。';
      default:
        if (e.status >= 500) return '服务端出错了，稍后再试。';
        return e.message || '操作失败。';
    }
  }
  if (e instanceof TypeError) {
    return '连不上电脑。检查手机和电脑是否在同一个网络、后端是否在运行。';
  }
  if (e instanceof Error) return e.message;
  return '出了点问题。';
}

function authHeaders(): Record<string, string> {
  const h: Record<string, string> = { 'Content-Type': 'application/json' };
  if (token) h.Authorization = `Bearer ${token.value}`;
  return h;
}

export async function api<T>(
  path: string,
  init: RequestInit & { timeoutMs?: number } = {},
): Promise<T> {
  const url = `${getBaseURL()}${path}`;
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), init.timeoutMs ?? 20000);

  let res: Response;
  try {
    res = await fetch(url, {
      ...init,
      headers: { ...authHeaders(), ...(init.headers as Record<string, string>) },
      signal: controller.signal,
    });
  } catch (e) {
    if (controller.signal.aborted) throw new ApiError(0, 'timeout', '请求超时');
    throw e;
  } finally {
    clearTimeout(timeout);
  }

  const text = await res.text();
  let body: Envelope<T> | null = null;
  try {
    body = text ? (JSON.parse(text) as Envelope<T>) : null;
  } catch {
    // 非 JSON 响应（比如代理返回的 HTML 错误页）
    throw new ApiError(res.status, 'bad_response', `服务端返回了无法解析的内容（${res.status}）`);
  }
  if (!res.ok || !body?.ok) {
    throw new ApiError(
      res.status,
      body?.error?.code ?? 'http_error',
      body?.error?.message ?? `请求失败（${res.status}）`,
    );
  }
  return body.data as T;
}

export const get = <T,>(p: string) => api<T>(p);
export const post = <T,>(p: string, body?: unknown) =>
  api<T>(p, { method: 'POST', body: body === undefined ? undefined : JSON.stringify(body) });
export const patch = <T,>(p: string, body?: unknown) =>
  api<T>(p, { method: 'PATCH', body: body === undefined ? undefined : JSON.stringify(body) });
export const del = <T,>(p: string) => api<T>(p, { method: 'DELETE' });

/* ---------------- SSE ---------------- */

export type SSEHandler = (event: any) => void;

/**
 * 流式读取。
 *
 * 用手动分帧而不是 EventSource：EventSource 在 React Native 里不可用，
 * 且它不支持自定义 Authorization 头。
 *
 * 返回中止函数。调用后不再回调。
 */
export async function stream(
  path: string,
  body: unknown,
  onEvent: SSEHandler,
  opts: { timeoutMs?: number } = {},
): Promise<() => void> {
  const controller = new AbortController();
  const url = `${getBaseURL()}${path}`;

  let res: Response;
  try {
    res = await fetch(url, {
      method: 'POST',
      headers: {
        ...authHeaders(),
        Accept: 'text/event-stream',
      },
      body: JSON.stringify(body),
      signal: controller.signal,
    });
  } catch (e) {
    if (controller.signal.aborted) return () => {};
    throw e;
  }

  if (!res.ok) {
    const text = await res.text().catch(() => '');
    let msg = `请求失败（${res.status}）`;
    try {
      const j = JSON.parse(text);
      if (j?.error?.message) msg = j.error.message;
    } catch {
      /* 非 JSON，用默认文案 */
    }
    throw new ApiError(res.status, 'stream_error', msg);
  }

  // 读循环不能 await 在返回之前——调用方要拿到中止函数。
  void (async () => {
    const reader = res.body?.getReader();
    if (!reader) return;
    const decoder = new TextDecoder();
    let buf = '';

    // 空闲超时：后端每 15 秒发一次 `: ping` 注释，
    // 超过 60 秒没有任何字节就认为连接已死。
    let lastByte = Date.now();
    const idle = setInterval(() => {
      if (Date.now() - lastByte > 60000) {
        controller.abort();
        clearInterval(idle);
      }
    }, 5000);

    try {
      for (;;) {
        const { done, value } = await reader.read();
        if (done) break;
        lastByte = Date.now();
        buf += decoder.decode(value, { stream: true });

        // SSE 以空行分帧。\r\n 也要处理——有些代理会改写换行符。
        let idx: number;
        while ((idx = findFrameEnd(buf)) >= 0) {
          const frame = buf.slice(0, idx);
          buf = buf.slice(idx + frameSepLen(buf, idx));
          for (const line of frame.split(/\r?\n/)) {
            // `:` 开头是注释（心跳），没有 data 就跳过
            if (!line.startsWith('data:')) continue;
            const payload = line.slice(5).trimStart();
            if (!payload || payload === '[DONE]') continue;
            try {
              onEvent(JSON.parse(payload));
            } catch {
              // 单帧坏掉不该中断整条流
            }
          }
        }
      }
    } catch (e) {
      if (!controller.signal.aborted) {
        onEvent({ type: '__error', message: humanError(e) });
      }
    } finally {
      clearInterval(idle);
      onEvent({ type: '__closed' });
    }
  })();

  return () => controller.abort();
}

function findFrameEnd(s: string): number {
  const a = s.indexOf('\n\n');
  const b = s.indexOf('\r\n\r\n');
  if (a < 0) return b;
  if (b < 0) return a;
  return Math.min(a, b);
}

function frameSepLen(s: string, idx: number): number {
  return s.startsWith('\r\n\r\n', idx) ? 4 : 2;
}
