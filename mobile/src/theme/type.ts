import { Platform } from 'react-native';

/**
 * 排版系统。
 *
 * 之前全站散落着 11 / 11.5 / 12 / 13 / 13.5 / 14 / 15 / 15.5 / 16 / 18 / 22 / 26 / 30 / 32 / 38
 * ——十几档字号，看起来"每个地方都随便调了一下"。专业排版只有 6 档，
 * 每档有明确的职责，跨档至少差 24%。
 *
 * 字重同理：只有 4 档。之前 40 处用 700，等于所有标题一样重，
 * 层级反而消失了。
 */

export const TYPE = {
  /** 页面级标题（一屏最多一个） */
  display: 30,
  /** 区块标题 */
  title: 20,
  /** 卡片标题 / 导航栏标题 */
  headline: 16,
  /** 正文 */
  body: 15,
  /** 次要说明 */
  sub: 13,
  /** 元信息（时间、计数） */
  caption: 11,
} as const;

export const LEAD = {
  display: 38,
  title: 27,
  headline: 22,
  body: 23,
  sub: 19,
  caption: 15,
} as const;

/**
 * 字重。规则：
 *   - 标题用 600（不是 700，700 在手机上会糊）
 *   - 正文 400
 *   - 只有极少数强调（数字、按钮）用 700
 */
export const WEIGHT = {
  regular: '400',
  medium: '500',
  semibold: '600',
  bold: '700',
} as const;

/**
 * 字距。
 *
 * 中文不需要负字距（那是给拉丁字母的）；
 * 大字号拉丁字母才需要 -0.5 ~ -1。
 * 这里只对纯数字/英文大标题生效。
 */
export const TRACK = {
  display: Platform.select({ ios: -0.6, android: 0, default: -0.4 }),
  normal: 0,
} as const;

/** 字体族。系统字体在 iOS 上是 SF Pro，本身足够好；这里只声明等宽体。 */
export const MONO = Platform.select({
  ios: 'Menlo',
  android: 'monospace',
  default: 'ui-monospace, SFMono-Regular, Menlo, monospace',
});
