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
  /* 表面 —— 全部是同一个暖纸色系，色相统一在 40° 左右。
     之前底色是暖灰（#F6F6F4）但文字和发丝线是冷灰（#17181C / rgba(20,24,40)），
     暖冷混用会让整个界面显得脏。 */
  bg: '#F7F6F3',
  surface: '#FFFFFF',
  surfaceSoft: '#F2F1ED',
  chipBg: '#F2F1ED',

  /* 文字 —— 用同一色相的深暖灰，不是纯黑也不是冷灰 */
  ink: '#191712',
  text2: '#57534B',
  text3: '#8A857A',
  text4: '#A39E94',
  faint: '#B8B3A8',

  /* 主按钮/用户气泡的纵向微渐变 */
  inkGrad: ['#2A2721', '#17150F'] as const,

  /* 线 —— 用暖灰而不是蓝灰 */
  hairline: 'rgba(38,33,24,.06)',
  hairlineStrong: 'rgba(38,33,24,.09)',
  track: '#EAE8E2',
  toggleOff: '#E3E0D8',
  radioOff: '#C6C1B6',

  /* 语义（全 App 唯一允许的彩色）—— 全部降饱和，
     让它们和暖纸底融合，而不是跳出来喊。 */
  wait: '#B8863C',
  danger: '#B4433A',
  dangerDot: '#C2554B',
  dangerSoft: '#F8EBE8',
  ok: '#4A7355',
  okDot: '#5C8A68',
  goldBg: '#F4EEE0',
  goldFg: '#8A6B32',
  meBlue: '#8A6A55',

  /* 品牌强调色 —— 单一颜色，不再用紫蓝渐变。
     赭石红在暖纸底上有足够对比度，又不像蓝紫那样是 AI 设计的默认指纹。 */
  brand: '#8C4A3C',
  brandDeep: '#6E3529',

  /* 其他 */
  scrim: 'rgba(24,20,14,.42)',
  white: '#FFFFFF',
  slate: '#5E5A51',
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
  '#A8734F', // 赭
  '#7A8B6F', // 苔绿
  '#6E7F92', // 岩蓝
  '#9B6B6B', // 陶红
  '#8A7A5E', // 卡其
  '#6F7F7A', // 松青
  '#8C7361', // 褐
  '#7D7286', // 紫灰
] as const;

/* ============================================================
   文件/应用图标底色（低饱和）
   ============================================================ */
export const FILE_COLORS = {
  doc: '#6E7F92',
  image: '#7A8B6F',
  artifact: '#7D7286',
  goal: '#5F7A64',
  connector: '#9B6B5E',
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
  cardTint: '#FBFAF7',
  accent: light.brand,
  accentDeep: light.brandDeep,
  accentText: light.brand,
  gradient: [light.brand, light.brand, light.brand] as const,
  gradientSoft: ['#F6EFEC', '#F2E9E5'] as const,
  accentSoft: '#F4ECE9',
  accentFaint: '#FAF5F3',
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
