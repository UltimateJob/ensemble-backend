import { BlurView } from 'expo-blur';
import React from 'react';
import { Platform, StyleSheet, View, type StyleProp, type ViewStyle } from 'react-native';
import { useTheme } from '../theme/theme';

/**
 * 真玻璃。
 *
 * iOS 上是原生 UIVisualEffectView（expo-blur 背后就是它），
 * 真的在模糊下面的内容——不是一层半透明白色。
 *
 * 玻璃要成立，光靠 blur 不够，必须有三件套：
 *   1. blur 本身
 *   2. 一层很淡的着色（否则模糊后对比度不够，文字浮不起来）
 *   3. **1px 内描边 + 顶部高光**——模拟玻璃边缘的折射
 * 只有 1 就是"磨砂"，加上 2、3 才是"玻璃"。
 */
export function Glass({
  children,
  style,
  intensity,
  radius = 24,
  border = true,
  tint,
}: {
  children: React.ReactNode;
  style?: StyleProp<ViewStyle>;
  intensity?: number;
  radius?: number;
  border?: boolean;
  tint?: 'light' | 'dark';
}) {
  const t = useTheme();
  const mode = tint ?? (t.dark ? 'dark' : 'light');
  const blurIntensity = intensity ?? (mode === 'dark' ? 40 : 60);

  const overlay = {
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: mode === 'dark' ? 'rgba(255,255,255,0.10)' : 'rgba(255,255,255,0.65)',
    borderTopWidth: border ? 1 : 0,
    borderTopColor: mode === 'dark' ? 'rgba(255,255,255,0.14)' : 'rgba(255,255,255,0.95)',
  };

  // Web 上 expo-blur 依赖 backdrop-filter，Chromium 支持，
  // 但不支持时会静默退化——所以下面垫一层半透明色保证可读。
  if (Platform.OS === 'web') {
    return (
      <View
        style={[
          {
            borderRadius: radius,
            overflow: 'hidden',
            backgroundColor: mode === 'dark' ? 'rgba(28,29,38,0.72)' : 'rgba(255,255,255,0.78)',
          },
          overlay,
          style,
        ]}
      >
        {children}
      </View>
    );
  }

  return (
    <BlurView
      intensity={blurIntensity}
      tint={mode}
      style={[{ borderRadius: radius, overflow: 'hidden' }, overlay, style]}
    >
      <View
        style={{
          flex: 1,
          backgroundColor: mode === 'dark' ? 'rgba(22,23,30,0.55)' : 'rgba(255,255,255,0.55)',
        }}
      >
        {children}
      </View>
    </BlurView>
  );
}
