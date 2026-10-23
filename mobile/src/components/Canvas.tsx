import { LinearGradient } from 'expo-linear-gradient';
import React from 'react';
import { StyleSheet, View, type StyleProp, type ViewStyle } from 'react-native';
import { useTheme } from '../theme/theme';

/**
 * 页面画布。
 *
 * 整套视觉的底座：不是纯白，而是一层极淡的渐变，外加两团几乎看不见的
 * 彩色光晕（蓝紫在上、青色在下）。白卡片放在上面才有"浮起来"的感觉。
 *
 * 光晕用大半径圆形 + 极低透明度模拟，而不是真正的模糊滤镜——
 * 原生端没有便宜的 blur，圆形渐变已经足够。
 */
export function Canvas({
  children,
  style,
}: {
  children: React.ReactNode;
  style?: StyleProp<ViewStyle>;
}) {
  const t = useTheme();
  return (
    <View style={[styles.root, { backgroundColor: t.canvas[1] }, style]}>
      <LinearGradient
        colors={[t.canvas[0], t.canvas[1], t.canvas[2]]}
        start={{ x: 0.1, y: 0 }}
        end={{ x: 0.9, y: 1 }}
        style={StyleSheet.absoluteFill}
      />
      {/* 左上蓝光 */}
      <LinearGradient
        colors={[t.orbA, 'transparent']}
        start={{ x: 0.5, y: 0.5 }}
        end={{ x: 1, y: 1 }}
        style={[styles.orb, { top: -180, left: -140, width: 420, height: 420 }]}
      />
      {/* 右上紫光 */}
      <LinearGradient
        colors={[t.orbB, 'transparent']}
        start={{ x: 0.5, y: 0.5 }}
        end={{ x: 0, y: 1 }}
        style={[styles.orb, { top: -120, right: -160, width: 380, height: 380 }]}
      />
      {children}
    </View>
  );
}

const styles = StyleSheet.create({
  root: { flex: 1, overflow: 'hidden' },
  orb: { position: 'absolute', borderRadius: 9999 },
});
