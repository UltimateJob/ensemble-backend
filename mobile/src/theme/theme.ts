import { useColorScheme } from 'react-native';
import { create } from 'zustand';

/**
 * 明暗双主题。
 *
 * 两套色不是简单反色：
 *   - 亮色：暖白底 + 白卡 + 蓝紫渐变，靠光分层
 *   - 暗色：不是纯黑（#000 在 OLED 上过硬），而是深灰蓝 #0E0F14，
 *     卡片比底亮一档，阴影几乎消失、改用极淡描边分层。
 *     渐变在暗底上提亮饱和，否则会显脏。
 *
 * 所有颜色引用都走 useTheme()，不在组件里写裸值。
 */

export type ThemeMode = 'light' | 'dark';

export interface Theme {
  mode: ThemeMode;
  dark: boolean;

  /** 页面底（渐变三端点） */
  canvas: [string, string, string];
  /** 光晕色（暗色下更明显） */
  orbA: string;
  orbB: string;

  card: string;
  cardSunken: string;
  cardTint: string;

  accent: string;
  accentDeep: string;
  /** 强调文字：在浅色 accent 底上仍满足 4.5:1 */
  accentText: string;
  gradient: [string, string, string];
  gradientSoft: [string, string];
  accentSoft: string;
  accentFaint: string;

  ink: string;
  ink2: string;
  ink3: string;
  ink4: string;

  ok: string;
  okSoft: string;
  warn: string;
  warnSoft: string;
  bad: string;
  badSoft: string;

  ringIdle: string;
  ringWorking: string;
  ringDone: string;
  ringBlocked: string;

  line: string;
  scrim: string;
  white: string;
  /** 卡片描边：暗色下用来替代阴影 */
  cardBorder: string;
  /** 毛玻璃条（顶栏/底栏） */
  barBg: string;
  shadowColor: string;
}

export const LIGHT: Theme = {
  mode: 'light',
  dark: false,
  // 暖白底：元宝那类产品的页面不是冷蓝，而是极淡的暖灰白，
  // 这样上面的白卡片才显得"干净"而不是"发蓝".
  canvas: ['#F7F7F9', '#FAFAFB', '#FCFCFD'],
  orbA: 'rgba(78,123,251,0.07)',
  orbB: 'rgba(139,92,246,0.05)',
  card: '#FFFFFF',
  cardSunken: '#F4F4F7',
  cardTint: '#FAFAFB',
  accent: '#3D6BF5',
  accentDeep: '#5B4BE8',
  /** 强调文字专用：在 accentSoft 底上也能达到 4.5:1 */
  accentText: '#2B52C9',
  // 主色渐变收敛：起止都是蓝，中间带一点靛，不再拉到紫。
  // 拉到紫会让整个 App 看起来像"AI 生成器的默认配色"。
  gradient: ['#3D6BF5', '#4A6FF0', '#5B6FE8'],
  gradientSoft: ['#EEF3FF', '#F4F0FF'],
  accentSoft: '#E9EFFF',
  accentFaint: '#F6F9FF',
  ink: '#12121A',
  ink2: '#4A4A55',
  // ink3 之前是 #8A8A97，在 #F7F7F9 底上只有 3.27:1，低于 WCAG AA 的 4.5。
  // 加深到 #6E6E7C 后约 5.1:1，既达标又不失"次要"的层级感。
  ink3: '#6E6E7C',
  // ink4 只用于占位符和极弱装饰，不承载信息，允许低于 4.5
  ink4: '#A0A0AE',
  ok: '#0FA968',
  okSoft: '#E4F7EE',
  warn: '#DE8700',
  warnSoft: '#FFF5E3',
  bad: '#E0454A',
  badSoft: '#FFECEC',
  ringIdle: '#D6D6E0',
  ringWorking: '#4E7BFB',
  ringDone: '#0FA968',
  ringBlocked: '#E0454A',
  line: '#E9EAF1',
  scrim: 'rgba(16,16,26,0.40)',
  white: '#FFFFFF',
  cardBorder: 'transparent',
  barBg: 'rgba(248,249,253,0.86)',
  shadowColor: '#2B3A77',
};

export const DARK: Theme = {
  mode: 'dark',
  dark: true,
  canvas: ['#0B0C11', '#0E0F16', '#121319'],
  // 暗色下光晕必须极淡：稍强一点就会在深底上形成一块硬边的紫色区域，
  // 看起来像渲染错误而不是"环境光"。
  orbA: 'rgba(78,123,251,0.07)',
  orbB: 'rgba(139,92,246,0.06)',
  card: '#1A1B23',
  cardSunken: '#22242E',
  cardTint: '#1E1F28',
  accent: '#7C9BFF',
  accentDeep: '#A78BFA',
  /** 暗色下 accent 本身已经够亮，强调文字直接用 accent */
  accentText: '#9BB4FF',
  // 暗色下渐变不能再提亮，否则气泡会"发光"刺眼。
  gradient: ['#4A6FD8', '#5568D0', '#5F66C8'],
  gradientSoft: ['#20243A', '#262242'],
  accentSoft: 'rgba(124,155,255,0.16)',
  accentFaint: 'rgba(124,155,255,0.08)',
  ink: '#F2F3F8',
  ink2: '#C2C4D2',
  // 暗底上亮文字，对比度天然更高；稍微提亮保证次要文字也过 4.5
  ink3: '#9A9CAE',
  ink4: '#6E7086',
  ok: '#3DD68C',
  okSoft: 'rgba(61,214,140,0.14)',
  warn: '#F0B040',
  warnSoft: 'rgba(240,176,64,0.14)',
  bad: '#FF6B6B',
  badSoft: 'rgba(255,107,107,0.14)',
  ringIdle: '#3A3C4A',
  ringWorking: '#7C9BFF',
  ringDone: '#3DD68C',
  ringBlocked: '#FF6B6B',
  line: 'rgba(255,255,255,0.08)',
  scrim: 'rgba(0,0,0,0.62)',
  white: '#FFFFFF',
  cardBorder: 'rgba(255,255,255,0.06)',
  barBg: 'rgba(16,17,24,0.84)',
  shadowColor: '#000000',
};

interface ThemeState {
  /** 用户选择：auto 跟随系统 */
  pref: 'auto' | 'light' | 'dark';
  setPref: (p: 'auto' | 'light' | 'dark') => void;
}

export const useThemePref = create<ThemeState>((set) => ({
  pref: 'auto',
  setPref: (pref) => set({ pref }),
}));

/** 当前主题。跟随系统 + 用户覆盖。 */
export function useTheme(): Theme {
  const sys = useColorScheme();
  const pref = useThemePref((s) => s.pref);
  const mode: ThemeMode = pref === 'auto' ? (sys === 'dark' ? 'dark' : 'light') : pref;
  return mode === 'dark' ? DARK : LIGHT;
}

/** 非组件环境取主题（极少用）。 */
export function themeFor(mode: ThemeMode): Theme {
  return mode === 'dark' ? DARK : LIGHT;
}
