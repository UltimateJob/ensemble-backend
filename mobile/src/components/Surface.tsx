import React from 'react';
import { StyleSheet, View, type StyleProp, type ViewStyle } from 'react-native';
import { PressableScale } from './motion';
import { useTheme } from '../theme/theme';
import { gap, radius } from '../theme/tokens';

/**
 * 卡片。
 *
 * 曾经做过 Double-Bezel（外壳 + 内芯两层）。实测在手机尺寸下
 * 外壳只有 4px，两层看不出层次，只多出一圈像"脏边"的灰线，
 * 反而显得廉价——所以回到了单层。
 *
 * 现在的层次靠三件事，而不是嵌套：
 *   1. 纯白/深灰的实底，和画布明确分开
 *   2. 柔和但真实的阴影（亮色偏蓝、暗色几乎无）
 *   3. 暗色下补一道 1px 的浅描边——没有阴影时靠它定义边界
 */
export function Surface({
  children,
  style,
  onPress,
  inner,
  shell: _shell,
  outerRadius = 20,
}: {
  children: React.ReactNode;
  style?: StyleProp<ViewStyle>;
  onPress?: () => void;
  inner?: StyleProp<ViewStyle>;
  shell?: number;
  outerRadius?: number;
}) {
  const t = useTheme();

  const body = (
    <View
      style={[
        {
          backgroundColor: t.card,
          borderRadius: outerRadius,
          borderWidth: t.dark ? StyleSheet.hairlineWidth : 0,
          borderColor: t.cardBorder,
          shadowColor: t.shadowColor,
          shadowOpacity: t.dark ? 0.45 : 0.055,
          shadowRadius: t.dark ? 14 : 12,
          shadowOffset: { width: 0, height: t.dark ? 5 : 3 },
          elevation: t.dark ? 2 : 2,
          overflow: 'hidden',
        },
        style,
      ]}
    >
      <View style={inner}>{children}</View>
    </View>
  );

  if (!onPress) return body;
  return (
    <PressableScale onPress={onPress} scaleTo={0.98}>
      {body}
    </PressableScale>
  );
}

/** 渐变描边的强调卡（用在"当前群组"这类唯一焦点上）。 */
export function GlowSurface({
  children,
  style,
  onPress,
  tint,
  outerRadius = 20,
}: {
  children: React.ReactNode;
  style?: StyleProp<ViewStyle>;
  onPress?: () => void;
  tint?: [string, string];
  shell?: number;
  outerRadius?: number;
}) {
  const t = useTheme();
  const c = tint ?? [t.accent, t.accentDeep];

  const body = (
    <View
      style={[
        {
          backgroundColor: t.card,
          borderRadius: outerRadius,
          borderWidth: 1.5,
          borderColor: c[0],
          overflow: 'hidden',
          shadowColor: t.shadowColor,
          shadowOpacity: t.dark ? 0.4 : 0.07,
          shadowRadius: 14,
          shadowOffset: { width: 0, height: 4 },
          elevation: 3,
        },
        style,
      ]}
    >
      {children}
    </View>
  );

  if (!onPress) return body;
  return (
    <PressableScale onPress={onPress} scaleTo={0.98}>
      {body}
    </PressableScale>
  );
}

export { radius, gap };
