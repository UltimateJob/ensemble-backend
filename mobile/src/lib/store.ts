import { create } from 'zustand';
import { getItem, setItem } from './storage';
import type { Conversation, Scenario, User } from './types';

/**
 * 全局状态。
 *
 * 除常规数据外，这里还担着两件设计规范要求的事：
 *   - B1 首次进入落到官方群
 *   - B2 冷启动恢复上次停留的页面与群
 */

interface AppState {
  /** 已配对/已登录。false 时路由停在配对页。 */
  ready: boolean;
  user: User | null;
  scenarios: Scenario[];
  convs: Conversation[];

  /** 当前打开的群（对话页里正在看的那一个） */
  currentConv: string | null;
  /** 官方群 ID（纸团新手村） */
  officialConv: string | null;
  /** 官方群的引导 chips */
  officialHints: string[];

  setReady: (v: boolean) => void;
  setUser: (u: User | null) => void;
  setScenarios: (s: Scenario[]) => void;
  setConvs: (c: Conversation[]) => void;
  upsertConv: (c: Conversation) => void;
  removeConv: (id: string) => void;
  setCurrentConv: (id: string | null) => void;
  setOfficial: (id: string, hints: string[]) => void;
}

/** 当前群的持久化 key（附录 B2：冷启动恢复） */
const KEY_CURRENT = 'zhituan_current_conv';

export const useApp = create<AppState>((set, get) => ({
  ready: false,
  user: null,
  scenarios: [],
  convs: [],
  currentConv: null,
  officialConv: null,
  officialHints: [],

  setReady: (ready) => set({ ready }),
  setUser: (user) => set({ user }),
  setScenarios: (scenarios) => set({ scenarios }),
  setConvs: (convs) => set({ convs }),
  upsertConv: (c) =>
    set((s) => {
      const i = s.convs.findIndex((x) => x.id === c.id);
      if (i < 0) return { convs: [c, ...s.convs] };
      const next = [...s.convs];
      next[i] = c;
      return { convs: next };
    }),
  removeConv: (id) =>
    set((s) => ({
      convs: s.convs.filter((c) => c.id !== id),
      currentConv: s.currentConv === id ? null : s.currentConv,
    })),
  setCurrentConv: (id) => {
    set({ currentConv: id });
    // 不 await：写盘失败不影响当前操作
    void setItem(KEY_CURRENT, id ?? '');
  },
  setOfficial: (id, hints) => set({ officialConv: id, officialHints: hints }),
}));

/** 冷启动时恢复上次停留的群。 */
export async function restoreCurrentConv(): Promise<void> {
  const id = await getItem(KEY_CURRENT);
  if (id) useApp.setState({ currentConv: id });
}
