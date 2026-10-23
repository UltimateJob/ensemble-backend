/**
 * 设计 token · iOS 灵动版
 *
 * 参照元宝这类国内一线 AI App 的视觉语言，核心是四点：
 *
 *   1. **暖调底 + 浮起白卡**：整个 App 不是纯白，而是一层极淡的暖灰蓝渐变底；
 *      卡片是纯白，靠真实阴影"浮"在底上。层次靠光，不靠色阶。
 *   2. **活力渐变**：主色不是单色，是蓝→靛紫的渐变；关键动作、图标底、
 *      进度条都用它。渐变是这套语言的情绪来源，不是装饰。
 *   3. **大圆角 + 柔和阴影**：卡片 24，按钮/输入 18，图标底 14。
 *      阴影是"东西浮起来了"，不是描边。
 *   4. **弹性微交互**：按压回弹、入场错帧、状态环呼吸——
 *      动效让界面是"活的"，这是和静态截图最大的区别。
 *
 * 所有页面只引用这里的 token，不写裸值。
 */

export const color = {
  /* ---- 表面 ---- */
  /** 页面底：纯白，覆盖在上面的渐变由页面自己铺 */
  bg: '#FFFFFF',
  /** 底色渐变的三个端点（极淡暖灰蓝，几乎看不出颜色但让白卡浮起来） */
  canvasTop: '#F4F6FC',
  canvasMid: '#F7F8FC',
  canvasBottom: '#FBFBFD',

  /** 浮起的卡片 */
  card: '#FFFFFF',
  /** 卡片内部的浅层块（嵌套内容） */
  cardSunken: '#F6F7FB',
  /** 更浅的分区底 */
  cardTint: '#FAFBFE',

  /* ---- 主色（活力渐变） ---- */
  accent: '#3D6DF6',
  accentDeep: '#5B4BE8',
  /** 渐变：从左到右 */
  gradient: ['#4E7BFB', '#6A5BF0', '#8B5CF6'] as const,
  /** 用于小面积强调的柔和渐变（正反向） */
  gradientSoft: ['#EEF3FF', '#F4F0FF'] as const,
  /** 主色浅底 */
  accentSoft: '#EDF2FF',
  accentFaint: '#F7F9FF',

  /* ---- 文字 ---- */
  ink: '#15151C',
  ink2: '#5A5A68',
  ink3: '#8E8E9C',
  ink4: '#B9B9C6',

  /* ---- 语义 ---- */
  ok: '#12A56A',
  okSoft: '#E6F7EF',
  warn: '#E08A00',
  warnSoft: '#FFF6E5',
  bad: '#E5484D',
  badSoft: '#FFEEEE',

  /* ---- 状态环 ---- */
  ringIdle: '#D7D7E0',
  ringWorking: '#4E7BFB',
  ringDone: '#12A56A',
  ringBlocked: '#E5484D',

  /* ---- 其他 ---- */
  line: '#EDEDF3',
  white: '#FFFFFF',
  scrim: 'rgba(18,18,28,0.42)',
};

/** 字号阶梯：相邻至少差 3px，层级才拉得开。 */
/**
 * 字号阶梯：只有 6 档，全部是整数。
 *
 * 小数（15.5 / 13.5）在 Web 上会被舍入成 15.5px 但在某些渲染路径下
 * 取整成 16，导致同一档字号在不同元素上看起来不一样。
 * 整数是唯一能让全站严格一致的做法。
 */
export const type = {
  /** 页面级标题，一屏最多一个 */
  display: 30,
  /** 区块标题 */
  title: 20,
  /** 卡片标题 / 导航栏 */
  headline: 16,
  /** 正文 */
  body: 15,
  /** 次要说明 */
  sub: 13,
  /** 元信息 */
  caption: 11,
} as const;

export const leading = {
  display: 38,
  title: 27,
  headline: 22,
  body: 23,
  sub: 19,
  caption: 15,
} as const;

export const weight = {
  regular: '400',
  medium: '500',
  semibold: '600',
  bold: '700',
  heavy: '800',
} as const;

/** 4pt 间距系统。 */
export const gap = {
  xxs: 4,
  xs: 8,
  sm: 12,
  md: 16,
  lg: 20,
  xl: 24,
  xxl: 32,
  xxxl: 44,
} as const;

export const radius = {
  /** 大卡片 / 弹层 */
  card: 24,
  /** 次级块 */
  md: 18,
  /** 按钮、输入框 */
  control: 16,
  /** 图标底、小标签 */
  sm: 14,
  /** 头像 */
  avatar: 16,
  pill: 999,
} as const;

/**
 * 阴影：这是新版最重要的变化。
 *
 * iOS 上"浮起来"全靠阴影。这里给三档，越靠近用户的层越深。
 * 颜色用蓝紫色而不是纯黑——纯黑在浅色底上会显脏。
 */
export const shadow = {
  /** 卡片：几乎无感，只负责让边缘离开背景 */
  card: {
    shadowColor: '#2B3A77',
    shadowOpacity: 0.06,
    shadowRadius: 14,
    shadowOffset: { width: 0, height: 4 },
    elevation: 3,
  },
  /** 浮层卡片 / 强调卡：明显浮起 */
  lift: {
    shadowColor: '#2B3A77',
    shadowOpacity: 0.11,
    shadowRadius: 26,
    shadowOffset: { width: 0, height: 10 },
    elevation: 8,
  },
  /** 弹层、侧滑面板 */
  float: {
    shadowColor: '#14163A',
    shadowOpacity: 0.18,
    shadowRadius: 40,
    shadowOffset: { width: 0, height: 18 },
    elevation: 16,
  },
  /** 主色按钮：带颜色投影，让它像发光的实体 */
  accent: {
    shadowColor: '#4E7BFB',
    shadowOpacity: 0.32,
    shadowRadius: 18,
    shadowOffset: { width: 0, height: 8 },
    elevation: 8,
  },
  /** 兼容旧名 */
  sheet: {
    shadowColor: '#14163A',
    shadowOpacity: 0.18,
    shadowRadius: 40,
    shadowOffset: { width: 0, height: 18 },
    elevation: 16,
  },
} as const;

export const icon = { sm: 16, md: 20, lg: 24 } as const;
export const HIT = 44;
export const PAGE_X = 20;

/** 动效时长。iOS 的默认体感：快进慢出。 */
export const motion = {
  fast: 160,
  base: 240,
  slow: 380,
  spring: { damping: 16, stiffness: 220, mass: 0.7 },
} as const;

/* ============================================================
   兼容别名：旧页面还在用 colors / font / space / weights
   ============================================================ */
export const colors = {
  bg: color.bg,
  card: color.card,
  accent: color.accent,
  accentSoft: color.accentSoft,
  accentFaint: color.accentFaint,
  text: color.ink,
  text2: color.ink2,
  text3: color.ink3,
  text4: color.ink4,
  line: color.line,
  line2: color.line,
  ringIdle: color.ringIdle,
  ringWorking: color.ringWorking,
  ringDone: color.ringDone,
  ringBlocked: color.ringBlocked,
  ok: color.ok,
  okSoft: color.okSoft,
  warn: color.warn,
  warnSoft: color.warnSoft,
  bad: color.bad,
  badSoft: color.badSoft,
  input: color.cardSunken,
  scrim: color.scrim,
};
export const font = {
  h1: type.display,
  h2: type.title,
  h3: type.headline,
  body: type.body,
  small: type.sub,
  tiny: type.caption,
};
export const space = gap;
export const weights = weight;

/**
 * 卡片内边距。全站统一 —— 之前每页 11/12/13/14/15 混用，
 * 导致同样的卡片在不同页面高矮不一。
 */
export const CARD_PAD = 14;
/** 卡片之间的垂直间距。比内边距大，卡片才是"分开的组"而不是"一张长表"。 */
export const CARD_GAP = 10;
