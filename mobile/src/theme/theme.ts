import { useColorScheme } from 'react-native';
import { create } from 'zustand';
import { dark as darkRaw, light } from './tokens';

/**
 * 明暗双主题 · 纸团 v1.0
 *
 * 浅色：暖灰底 `#F6F6F4` + 白卡，层次靠投影。
 * 深色：真黑底 `#0B0B0C` + 微渐变卡，阴影几乎不可见，改用极淡描边分层。
 *
 * 关键差异（相对上一版）：
 *   - 不再有彩色光晕，底色是平坦的暖灰
 *   - 主按钮是墨黑（浅色）/ 反白（深色），不是蓝紫渐变
 *   - 品牌渐变只给门面页用，群聊内不用
 */

export type ThemeMode = 'light' | 'dark';

export interface Theme {
  mode: ThemeMode;
  dark: boolean;

  /* 表面 */
  canvas: [string, string, string];
  /** 兼容旧组件；新设计不用光晕 */
  orbA: string;
  orbB: string;
  card: string;
  /** 卡片顶部渐变浅端（深色下有值） */
  cardTop: string;
  cardSunken: string;
  cardTint: string;
  /** 悬浮板（成员条 / 托盘 / 输入板） */
  slab: string;
  slabTop: string;
  /** 门面页深色英雄卡底（附录 A2，明暗两套都用这个深色） */
  heroBg: string;
  /** 品牌渐变投影色（附录 A1：0 12px 28px rgba(99,91,240,.38)） */
  brandShadow: string;

  /* 品牌（仅门面页） */
  accent: string;
  accentDeep: string;
  accentText: string;
  gradient: [string, string, string];
  gradientSoft: [string, string];
  accentSoft: string;
  accentFaint: string;

  /* 文字 */
  ink: string;
  ink2: string;
  ink3: string;
  ink4: string;
  faint: string;

  /* 语义 */
  ok: string;
  okSoft: string;
  okDot: string;
  warn: string;
  warnSoft: string;
  bad: string;
  badSoft: string;
  dangerDot: string;
  goldBg: string;
  goldFg: string;
  meBlue: string;

  /* 状态环 */
  ringIdle: string;
  ringWorking: string;
  ringDone: string;
  ringBlocked: string;

  /* 线 / 材质 */
  line: string;
  hairline: string;
  track: string;
  toggleOff: string;
  radioOff: string;
  chipBg: string;
  slate: string;
  scrim: string;
  white: string;
  cardBorder: string;
  barBg: string;
  shadowColor: string;
  /** 主按钮底：浅色墨黑，深色反白 */
  primaryBg: string;
  primaryInk: string;
  primaryGrad: [string, string];
}

export const LIGHT: Theme = {
  mode: 'light',
  dark: false,
  canvas: [light.bg, light.bg, light.bg],
  orbA: 'transparent',
  orbB: 'transparent',
  card: light.surface,
  cardTop: light.surface,
  cardSunken: light.surfaceSoft,
  cardTint: '#FAFAF8',
  slab: 'rgba(255,255,255,0.86)',
  slabTop: '#FFFFFF',
  heroBg: '#141519',
  brandShadow: 'rgba(99,91,240,0.38)',

  accent: light.brandFrom,
  accentDeep: light.brandTo,
  accentText: '#3B5BD6',
  gradient: [light.brandFrom, '#6A6AF2', light.brandTo],
  gradientSoft: ['#EEF2FF', '#F5F0FF'],
  accentSoft: '#EDF2FF',
  accentFaint: '#F7F9FF',

  ink: light.ink,
  ink2: light.text2,
  ink3: light.text3,
  ink4: light.text4,
  faint: light.faint,

  ok: light.ok,
  okSoft: '#EAF4ED',
  okDot: light.okDot,
  warn: light.wait,
  warnSoft: '#FDF3E3',
  bad: light.danger,
  badSoft: light.dangerSoft,
  dangerDot: light.dangerDot,
  goldBg: light.goldBg,
  goldFg: light.goldFg,
  meBlue: light.meBlue,

  ringIdle: light.radioOff,
  ringWorking: light.ink,
  ringDone: light.okDot,
  ringBlocked: light.dangerDot,

  line: light.hairline,
  hairline: light.hairline,
  track: light.track,
  toggleOff: light.toggleOff,
  radioOff: light.radioOff,
  chipBg: light.chipBg,
  slate: light.slate,
  scrim: light.scrim,
  white: light.white,
  cardBorder: 'transparent',
  barBg: 'rgba(255,255,255,0.86)',
  shadowColor: '#101214',
  primaryBg: light.ink,
  primaryInk: '#FFFFFF',
  primaryGrad: [light.inkGrad[0], light.inkGrad[1]],
};

export const DARK: Theme = {
  mode: 'dark',
  dark: true,
  canvas: [darkRaw.bg, darkRaw.bg, darkRaw.bg],
  orbA: 'transparent',
  orbB: 'transparent',
  card: darkRaw.surface,
  cardTop: darkRaw.surfaceTop,
  cardSunken: '#1E1E22',
  cardTint: '#202024',
  slab: 'rgba(38,38,43,0.92)',
  slabTop: darkRaw.slabTop,
  heroBg: '#141519',
  brandShadow: 'rgba(99,91,240,0.38)',

  accent: '#7C9BFF',
  accentDeep: '#A78BFA',
  accentText: '#9BB4FF',
  gradient: ['#4E7BFB', '#6A6AF2', '#8B5CF6'],
  gradientSoft: ['#20243A', '#262242'],
  accentSoft: 'rgba(124,155,255,0.16)',
  accentFaint: 'rgba(124,155,255,0.08)',

  ink: darkRaw.text,
  ink2: darkRaw.text2,
  ink3: darkRaw.text3,
  ink4: '#8A8A92',
  faint: darkRaw.faint,

  ok: darkRaw.ok,
  okSoft: 'rgba(76,195,138,0.14)',
  okDot: darkRaw.ok,
  warn: '#E8A33D',
  warnSoft: 'rgba(232,163,61,0.14)',
  bad: darkRaw.danger,
  badSoft: darkRaw.dangerSoft,
  dangerDot: darkRaw.danger,
  goldBg: 'rgba(150,118,47,0.22)',
  goldFg: '#D6B269',
  meBlue: '#3E8FE0',

  ringIdle: '#3A3C4A',
  ringWorking: '#F2F2F2',
  ringDone: darkRaw.ok,
  ringBlocked: darkRaw.danger,

  line: darkRaw.hairline,
  hairline: darkRaw.hairline,
  track: darkRaw.track,
  toggleOff: '#3A3C42',
  radioOff: '#4A4C56',
  chipBg: darkRaw.chipBg,
  slate: '#C9C9CE',
  scrim: 'rgba(0,0,0,0.58)',
  white: '#FFFFFF',
  cardBorder: 'rgba(255,255,255,0.06)',
  barBg: 'rgba(30,30,34,0.86)',
  shadowColor: '#000000',
  primaryBg: darkRaw.primary,
  primaryInk: darkRaw.primaryInk,
  primaryGrad: [darkRaw.primary, darkRaw.primary],
};

/** 字体大小档位。只分三档：再细用户也感知不到，反而难选。 */
export type FontScale = 'small' | 'normal' | 'large';

/** 相对标准档的倍率。±10% 是移动端不破坏排版的舒适上限。 */
export const FONT_SCALE: Record<FontScale, number> = {
  small: 0.9,
  normal: 1,
  large: 1.12,
};

interface ThemeState {
  pref: 'auto' | 'light' | 'dark';
  setPref: (p: 'auto' | 'light' | 'dark') => void;
  /** 全局字体缩放。文字组件读它，不是每个页面自己算。 */
  fontScale: FontScale;
  setFontScale: (f: FontScale) => void;
}

export const useThemePref = create<ThemeState>((set) => ({
  pref: 'auto',
  setPref: (pref) => set({ pref }),
  fontScale: 'normal',
  setFontScale: (fontScale) => set({ fontScale }),
}));

/** 当前字号倍率。非文字组件（图标、间距）不要用，排版会被撑歪。 */
export function useFontScaleValue(): number {
  return FONT_SCALE[useThemePref((s) => s.fontScale)];
}

export function useTheme(): Theme {
  const sys = useColorScheme();
  const pref = useThemePref((s) => s.pref);
  const mode: ThemeMode = pref === 'auto' ? (sys === 'dark' ? 'dark' : 'light') : pref;
  return mode === 'dark' ? DARK : LIGHT;
}

export function themeFor(mode: ThemeMode): Theme {
  return mode === 'dark' ? DARK : LIGHT;
}
