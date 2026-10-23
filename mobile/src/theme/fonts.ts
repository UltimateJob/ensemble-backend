import {
  Geist_400Regular,
  Geist_500Medium,
  Geist_600SemiBold,
  Geist_700Bold,
} from '@expo-google-fonts/geist';
import { Platform } from 'react-native';

/**
 * 字体。
 *
 * 中文由系统字体渲染（iOS 苹方 / Android 思源黑体），
 * Geist 只接管拉丁字母与数字——额度、百分比、版本号、英文角色名。
 * 这一点必须说清楚：中文界面里换英文字体，观众能感知到的是
 * 数字和字母的细节变干净了，而不是"整页换了字体"。
 *
 * 为什么选 Geist 而不是 Inter：Inter 是最大众化的选择，
 * 到处都是；Geist 的字腔更收、数字更方，在密集的元信息里更清晰。
 */
export const FONT_ASSETS = {
  Geist_400Regular,
  Geist_500Medium,
  Geist_600SemiBold,
  Geist_700Bold,
};

/** 按字重取字体族。中文会自动 fallback 到系统字体。 */
export function fontFamily(weight?: string | number): string | undefined {
  // Web: expo-font 会注入 @font-face，family 名是注册名（Geist_400Regular 这种），
  // 不是 "Geist"。写错了会静默 fallback 到系统字体——之前就是这样。
  if (Platform.OS === 'web') {
    const name =
      String(weight ?? '400') === '700'
        ? 'Geist_700Bold'
        : String(weight ?? '400') === '600'
          ? 'Geist_600SemiBold'
          : String(weight ?? '400') === '500'
            ? 'Geist_500Medium'
            : 'Geist_400Regular';
    // 后面的中文 fallback 交给系统
    return `${name}, -apple-system, BlinkMacSystemFont, "PingFang SC", "Microsoft YaHei", sans-serif`;
  }
  switch (String(weight ?? '400')) {
    case '700':
      return 'Geist_700Bold';
    case '600':
      return 'Geist_600SemiBold';
    case '500':
      return 'Geist_500Medium';
    default:
      return 'Geist_400Regular';
  }
}
