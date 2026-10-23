/**
 * 设计 token · 纸团 v1.0
 *
 * 严格对应《纸团 App 设计规范 v1.0》。三条铁律：
 *
 *   1. **黑白灰打底，颜色只做语义。** 界面 95% 是黑白灰。
 *      彩色只出现在：成员头像（大地色身份色）、文件类型图标（低饱和）、
 *      状态语义（绿=完成、红=等审批、琥珀=等待）。
 *   2. **一个界面只有一个主操作。** 主按钮是墨黑实心胶囊。
 *   3. **深度靠投影，不靠边框染色。** 每个表面 = 顶部 1px 高光
 *      + 纵向微渐变 + 柔和投影，三层缺一不可。
 *
 * 例外：附录 A 允许门面页使用品牌渐变。群聊内不用。
 */

/* ============================================================
   浅色
   ============================================================ */
export const light = {
  /* 表面 */
  bg: '#F6F6F4',
  surface: '#FFFFFF',
  surfaceSoft: '#F4F4F1',
  chipBg: '#F4F4F1',

  /* 文字 */
  ink: '#17181C',
  text2: '#5C5C66',
  text3: '#8A8F98',
  text4: '#9AA0A8',
  faint: '#A6ABB5',

  /* 主按钮/用户气泡的纵向微渐变 */
  inkGrad: ['#2B2C31', '#141518'] as const,

  /* 线 */
  hairline: 'rgba(20,24,40,.05)',
  hairlineStrong: 'rgba(20,24,40,.07)',
  track: '#ECEEF2',
  toggleOff: '#E3E5E9',
  radioOff: '#C9CDD4',

  /* 语义（全 App 唯一允许的彩色） */
  wait: '#E8A33D',
  danger: '#C93A2E',
  dangerDot: '#D9534A',
  dangerSoft: '#FDECEA',
  ok: '#3E7C4F',
  okDot: '#4C9A6C',
  goldBg: '#F7F1E3',
  goldFg: '#96762F',
  meBlue: '#2B7CD3',

  /* 品牌渐变（附录 A，仅门面页） */
  brandFrom: '#4E7BFB',
  brandTo: '#8B5CF6',

  /* 其他 */
  scrim: 'rgba(16,18,20,.38)',
  white: '#FFFFFF',
  slate: '#5A6B7D',
} as const;

/* ============================================================
   深色
   ============================================================ */
export const dark = {
  bg: '#0B0B0C',
  surface: '#242428',
  surfaceTop: '#2B2B2F',
  slabTop: '#26262B',
  chipBg: '#2A2A2F',

  text: '#F2F2F2',
  text2: '#C9C9CE',
  text3: '#9A9AA0',
  faint: '#63636A',

  hairline: 'rgba(255,255,255,.07)',
  track: 'rgba(255,255,255,.14)',

  ok: '#4CC38A',
  danger: '#FF7166',
  dangerSoft: 'rgba(217,83,74,.20)',

  primary: '#F4F4F2',
  primaryInk: '#141518',

  scrim: 'rgba(0,0,0,.58)',
} as const;

/* ============================================================
   成员身份色（大地色系，按成员固定分配，不用 hash）
   ============================================================ */
export const MEMBER_COLORS = [
  '#A98A68', // 赭
  '#B98A5E', // 陶
  '#64766B', // 松绿
  '#8A6F5C', // 褐
  '#6B7A8A', // 岩蓝灰
  '#7A6B8A', // 紫灰
  '#5C7A72', // 青
  '#8A7A5C', // 卡其
] as const;

/* ============================================================
   文件/应用图标底色（低饱和）
   ============================================================ */
export const FILE_COLORS = {
  doc: '#7C93A8',
  image: '#7A9A7E',
  artifact: '#8B7EC8',
  goal: '#5F9E72',
  connector: '#D96C5F',
} as const;

/* ============================================================
   字体与字阶
   ============================================================ */
export const type = {
  /** 页面大标题 27/750 */
  display: 27,
  /** 页标题 20/700 */
  page: 20,
  /** 栏标题 16.5/650 */
  title: 16.5,
  /** 卡片标题 */
  card: 15.5,
  /** 正文 */
  body: 15.5,
  /** 次要正文 */
  sub: 14.5,
  /** 辅助 */
  caption: 12.5,
  /** 微型 */
  micro: 11,

  /* 兼容旧键名 */
  headline: 16.5,
  cardTitle: 15.5,
} as const;

export const lead = {
  display: 34,
  page: 26,
  title: 21,
  card: 21,
  body: 24.5,
  sub: 22,
  caption: 19,
  micro: 15,
} as const;

/** 字重：只用这六档，不用 800+ */
export const weight = {
  regular: '400',
  medium: '550',
  semibold: '600',
  strong: '650',
  bold: '700',
  heavy: '750',
} as const;

/* ============================================================
   圆角
   ============================================================ */
export const radius = {
  /** 卡片 */
  card: 24,
  /** 气泡 */
  bubble: 15,
  /** 卡片消息 */
  message: 17,
  /** 审批托盘 */
  tray: 30,
  /** 图标底 */
  icon: 13,
  /** 标签 */
  tag: 7,
  pill: 999,

  /* 兼容旧键名 */
  md: 20,
  sm: 13,
  control: 20,
  avatar: 16,
} as const;

/* ============================================================
   间距（4 的倍数）
   ============================================================ */
export const gap = {
  xxs: 4,
  xs: 8,
  sm: 10,
  md: 12,
  lg: 16,
  xl: 20,
  xxl: 28,
  xxxl: 40,
} as const;

/** 页面左右边距 */
export const PAGE_X = 20;
/** 卡片页的左右边距 */
export const CARD_X = 12;
/** 卡片内边距 */
export const CARD_PAD = 17;
/** 卡片间距 */
export const CARD_GAP = 10;

/* ============================================================
   投影（统一两层）
   ============================================================ */
export const shadow = {
  card: {
    shadowColor: '#101214',
    shadowOpacity: 0.045,
    shadowRadius: 26,
    shadowOffset: { width: 0, height: 12 },
    elevation: 3,
  },
  slab: {
    shadowColor: '#101214',
    shadowOpacity: 0.07,
    shadowRadius: 34,
    shadowOffset: { width: 0, height: 16 },
    elevation: 6,
  },
  tray: {
    shadowColor: '#101214',
    shadowOpacity: 0.11,
    shadowRadius: 40,
    shadowOffset: { width: 0, height: 20 },
    elevation: 8,
  },
  primary: {
    shadowColor: '#101214',
    shadowOpacity: 0.22,
    shadowRadius: 22,
    shadowOffset: { width: 0, height: 10 },
    elevation: 6,
  },
} as const;

/* ============================================================
   动效
   ============================================================ */
export const motion = {
  panel: 380,
  avatarGrow: 320,
  avatarShrink: 240,
  tray: 300,
  trayContent: 120,
  toggle: 200,
  press: 120,
  message: 220,
  breathe: 1600,
  segment: 300,
} as const;

/** 头像尺寸档 */
export const AVATAR = {
  bar: 30,
  header: 36,
  message: 40,
  grid: 46,
  me: 58,
  detail: 72,
} as const;

export const HIT = 44;

/* ============================================================
   兼容别名：旧组件仍在引用 color / colors / font / space / weights。
   逐组件重写时删除。
   ============================================================ */
export const color = {
  bg: light.bg,
  canvasTop: light.bg,
  canvasMid: light.bg,
  canvasBottom: light.bg,
  card: light.surface,
  cardSunken: light.surfaceSoft,
  cardTint: '#FAFAF8',
  accent: light.brandFrom,
  accentDeep: light.brandTo,
  accentText: '#3B5BD6',
  gradient: [light.brandFrom, '#6A6AF2', light.brandTo] as const,
  gradientSoft: ['#EEF2FF', '#F5F0FF'] as const,
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
  scrim: light.scrim,
  white: light.white,
  slate: light.slate,
  primaryBg: light.ink,
  primaryInk: '#FFFFFF',
  primaryGrad: light.inkGrad,
};

export const colors = {
  ...color,
  text: color.ink,
  text2: color.ink2,
  text3: color.ink3,
  text4: color.ink4,
  line2: light.hairlineStrong,
  input: light.surfaceSoft,
  accentFaint: color.accentFaint,
  okSoft: color.okSoft,
  warnSoft: color.warnSoft,
  badSoft: color.badSoft,
  ringIdle: color.ringIdle,
  ringWorking: color.ringWorking,
  ringDone: color.ringDone,
  ringBlocked: color.ringBlocked,
};
export const font = {
  h1: type.display,
  h2: type.page,
  h3: type.title,
  body: type.body,
  small: type.sub,
  tiny: type.caption,
};
export const space = gap;
export const weights = weight;
export const icon = { sm: 16, md: 20, lg: 24 } as const;

/* ============================================================
   兼容别名（旧组件仍在用；逐组件重写后删除）
   ============================================================ */
export const leading = lead;
/** 旧 type 键名 → 新键名 */
export const typeCompat = type;
