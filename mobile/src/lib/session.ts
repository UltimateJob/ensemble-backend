import {
  clearDeviceToken,
  clearToken,
  get,
  getBaseURL,
  hasDeviceToken,
  loadBaseURL,
  loadToken,
  post,
  saveAccountToken,
  saveDeviceToken,
  setBaseURL,
  getToken,
} from './api';
import { restoreCurrentConv, useApp } from './store';
import type { Scenario, User } from './types';

/**
 * 启动引导：恢复地址与令牌，然后验证它还能用。
 *
 * 顺序很重要——先验令牌再拉数据。令牌失效时不要「先渲染主界面再
 * 闪回登录页」，那会让用户以为自己的数据丢了。
 */
export async function boot(): Promise<'ok' | 'need-login'> {
  const base = await loadBaseURL();
  if (!base) return 'need-login';
  await loadToken();
  if (!getToken()) return 'need-login';

  try {
    // /v1/me 返回的是「用户 + 套餐 + 钱包」的组合对象，不是用户本身。
    // 直接把整个 data 当用户存，user.id 会是 undefined，
    // 群主判断（解散群聊 / 权限编辑）就会全部失效。
    const me = await get<User & { user?: User }>('/v1/me');
    useApp.getState().setUser((me as any)?.user ?? me);
  } catch {
    // 401 才清令牌。网络错误（服务器没开机）不该把登录状态抹掉——
    // 那样用户每次换网络都要重新登录。
    await clearToken();
    return 'need-login';
  }

  // 并发拉基础数据，任一失败不影响已登录状态
  const [sc, cv] = await Promise.allSettled([
    get<{ scenarios: Scenario[] }>('/v1/scenarios'),
    get<any[]>('/v1/conversations'),
  ]);
  if (sc.status === 'fulfilled') {
    // 后端返回 { scenarios: [...], mine: [...] } 或直接数组，容错处理
    const raw: any = sc.value;
    const list: Scenario[] = Array.isArray(raw) ? raw : raw?.scenarios ?? [];
    useApp.getState().setScenarios(list);
  }
  if (cv.status === 'fulfilled') {
    useApp.getState().setConvs(cv.value ?? []);
  }

  // 官方群（附录 B1）：确保存在，并作为首次进入的落点。
  // 失败不阻塞登录——没有官方群照样能用。
  try {
    const off = await get<{ conversation: any; hints: string[] }>('/v1/official');
    if (off?.conversation?.id) {
      useApp.getState().setOfficial(off.conversation.id, off.hints ?? []);
      useApp.getState().upsertConv(off.conversation);
    }
  } catch {
    /* 官方群可选 */
  }

  // 冷启动恢复上次停留的群（附录 B2）；没有就落到官方群
  await restoreCurrentConv();
  const st = useApp.getState();
  if (!st.currentConv) {
    st.setCurrentConv(st.officialConv ?? st.convs[0]?.id ?? null);
  }

  useApp.getState().setReady(true);
  return 'ok';
}

/* ---------------- 账号登录（默认路径） ---------------- */

function kindOf(target: string): 'phone' | 'email' {
  return target.includes('@') ? 'email' : 'phone';
}

export async function loginWithCode(base: string, target: string, code: string): Promise<void> {
  await setBaseURL(base);
  const res = await post<{ token: string }>('/v1/auth/login', { kind: kindOf(target), target, code });
  await saveAccountToken(res.token);
  await boot();
}

export async function loginWithPassword(base: string, target: string, password: string): Promise<void> {
  await setBaseURL(base);
  const res = await post<{ token: string }>('/v1/auth/login', {
    kind: kindOf(target), target, mode: 'password', password,
  });
  await saveAccountToken(res.token);
  await boot();
}

export async function sendCode(base: string, target: string): Promise<{ devCode?: string }> {
  await setBaseURL(base);
  return post<{ devCode?: string }>('/v1/auth/send-code', { target });
}

/* ---------------- 连接电脑（登录后的可选能力） ---------------- */

/**
 * 用 6 位码把「这台设备」绑定到一台电脑。
 *
 * 只加设备令牌，**不动账号令牌**：登录态和电脑授权是两件事。
 * 之前这里会顺手删掉账号令牌，等于连了电脑就看不到 PC 上的会话。
 */
export async function connectComputer(
  base: string,
  code: string,
  name: string,
  platform: 'ios' | 'android',
): Promise<void> {
  await setBaseURL(base);
  const res = await post<{ device: { id: string }; token: string }>('/v1/devices/pair', {
    code,
    name,
    platform,
  });
  await saveDeviceToken(res.token);
  // 重新引导一次：设备令牌会改变服务端的审批策略（远程设备需审批），
  // 也让工作区能力立刻生效。
  await boot();
}

/** 断开这台设备与电脑的绑定，保留账号登录。 */
export async function disconnectComputer(): Promise<void> {
  await clearDeviceToken();
  await boot();
}

/** 当前这台设备是否已经连接过电脑。 */
export function computerConnected(): boolean {
  return hasDeviceToken();
}

/** 登录页要展示的服务器地址（空表示还没配过）。 */
export function currentBase(): string {
  return getBaseURL();
}

export async function signOut(): Promise<void> {
  await clearToken();
  useApp.getState().setReady(false);
  useApp.getState().setUser(null);
  useApp.getState().setConvs([]);
}


/** 兼容旧调用名：pairWith = connectComputer。 */
export const pairWith = connectComputer;
