import { useEffect, useRef, useState } from 'react';
import { getBaseURL, getToken } from './api';

/**
 * 连接状态。
 *
 * 手机端最难受的失败不是报错，是「点了没反应」——后端在电脑上，
 * 电脑休眠、Wi-Fi 切换、后端重启都会让请求静静超时。用户看到的
 * 只是列表不刷新，不知道发生了什么。
 *
 * 所以这里做一件很小的事：定期探一次 /healthz，断的时候在顶部
 * 挂一条可点击重试的提示。不引第三方依赖（NetInfo 只能判断手机
 * 有没有网，判断不了「那台电脑还在不在」）。
 */

export type ConnState = 'ok' | 'down' | 'unknown';

const PROBE_MS = 15000;

let listeners = new Set<(s: ConnState) => void>();
let state: ConnState = 'unknown';
let timer: ReturnType<typeof setInterval> | null = null;
let probing = false;

function emit(next: ConnState) {
  if (state === next) return;
  state = next;
  listeners.forEach((fn) => fn(next));
}

async function probe(): Promise<void> {
  const base = getBaseURL();
  if (!base || probing) return;
  probing = true;
  const ctrl = new AbortController();
  const t = setTimeout(() => ctrl.abort(), 6000);
  try {
    // /healthz 不需要登录，用它只判断「服务端在不在」，
    // 不会因为令牌过期就误报断网。
    const res = await fetch(`${base}/healthz`, { signal: ctrl.signal });
    emit(res.ok ? 'ok' : 'down');
  } catch {
    emit('down');
  } finally {
    clearTimeout(t);
    probing = false;
  }
}

/** 登录态变化或页面回到前台时立刻重探一次。 */
export function probeNow() {
  void probe();
}

/** 全局单例：第一个订阅者启动轮询，最后一个离开时停掉。 */
export function useConnection(): ConnState {
  const [s, setS] = useState<ConnState>(state);
  const started = useRef(false);

  useEffect(() => {
    listeners.add(setS);
    if (!started.current) {
      started.current = true;
      if (getToken()) void probe();
      if (!timer) timer = setInterval(() => {
        // 没登录时不探：入口页本来就要用户先配对/登录
        if (getToken()) void probe();
      }, PROBE_MS);
    }
    return () => {
      listeners.delete(setS);
    };
  }, []);

  return s;
}

/** 测试或登出时重置。 */
export function resetConnection() {
  if (timer) {
    clearInterval(timer);
    timer = null;
  }
  emit('unknown');
}
