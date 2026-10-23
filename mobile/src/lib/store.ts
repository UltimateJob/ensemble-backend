import { create } from 'zustand';
import type { Conversation, Scenario, User } from './types';

interface AppState {
  /** 已配对/已登录。false 时路由停在配对页。 */
  ready: boolean;
  user: User | null;
  scenarios: Scenario[];
  convs: Conversation[];

  setReady: (v: boolean) => void;
  setUser: (u: User | null) => void;
  setScenarios: (s: Scenario[]) => void;
  setConvs: (c: Conversation[]) => void;
  upsertConv: (c: Conversation) => void;
  removeConv: (id: string) => void;
}

export const useApp = create<AppState>((set) => ({
  ready: false,
  user: null,
  scenarios: [],
  convs: [],

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
  removeConv: (id) => set((s) => ({ convs: s.convs.filter((c) => c.id !== id) })),
}));
