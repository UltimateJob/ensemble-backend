import { clearToken, get, loadBaseURL, loadToken, post, saveAccountToken, saveDeviceToken, setBaseURL, getToken } from './api';
import { useApp } from './store';
import type { Scenario, User } from './types';

/**
 * 启动引导：恢复地址与令牌，然后验证它还能用。
 *
 * 顺序很重要——先验令牌再拉数据。令牌失效时不要「先渲染主界面再
 * 闪回登录页」，那会让用户以为自己的数据丢了。
 */
export async function boot(): Promise<'ok' | 'need-pair'> {
  const base = await loadBaseURL();
  if (!base) return 'need-pair';
  await loadToken();
  if (!getToken()) return 'need-pair';

  try {
    const me = await get<User>('/v1/me');
    useApp.getState().setUser(me);
  } catch {
    // 401 才清令牌。网络错误（电脑没开机）不该把配对关系抹掉——
    // 那样用户每次换网络都要重新扫码。
    await clearToken();
    return 'need-pair';
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
  useApp.getState().setReady(true);
  return 'ok';
}

/** 扫码/手输配对码后的落地。 */
export async function pairWith(
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
  await boot();
}

/** 账号登录（手机号 + 验证码）。作为配对之外的备用入口。 */
export async function loginWithCode(base: string, target: string, code: string): Promise<void> {
  await setBaseURL(base);
  const res = await post<{ token: string }>('/v1/auth/login', { target, code });
  await saveAccountToken(res.token);
  await boot();
}

export async function sendCode(base: string, target: string): Promise<{ devCode?: string }> {
  await setBaseURL(base);
  return post<{ devCode?: string }>('/v1/auth/send-code', { target });
}

export async function signOut(): Promise<void> {
  await clearToken();
  useApp.getState().setReady(false);
  useApp.getState().setUser(null);
  useApp.getState().setConvs([]);
}
