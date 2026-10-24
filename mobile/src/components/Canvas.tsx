import { LinearGradient } from 'expo-linear-gradient';
import React from 'react';
import { Pressable, StyleSheet, View, type StyleProp, type ViewStyle } from 'react-native';
import { probeNow, useConnection } from '../lib/connection';
import { useTheme } from '../theme/theme';
import { Icon, T } from './ui';

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

      {/* 断连提示条。
          手机端的失败大多不是报错，而是「点了没反应」——电脑休眠、
          换了 Wi-Fi、后端重启都会让请求静静超时。这里在顶部挂一条
          可点击重试的提示，全局所有页面共用。 */}
      <OfflineBanner />
    </View>
  );
}

function OfflineBanner() {
  const t = useTheme();
  const state = useConnection();
  if (state !== 'down') return null;
  return (
    <Pressable
      onPress={probeNow}
      accessibilityRole="button"
      accessibilityLabel="重新连接"
      style={[styles.offline, { backgroundColor: t.badSoft, borderColor: t.bad + '33' }]}
    >
      <Icon name="cloud-offline-outline" size={15} color={t.bad} />
      <T size={12.5} weight="600" color={t.bad} style={{ flex: 1 }} numberOfLines={1}>
        连不上电脑 · 点此重试
      </T>
    </Pressable>
  );
}

const styles = StyleSheet.create({
  root: { flex: 1, overflow: 'hidden' },
  orb: { position: 'absolute', borderRadius: 9999 },
  offline: {
    position: 'absolute',
    left: 12,
    right: 12,
    bottom: 12,
    height: 44,
    borderRadius: 14,
    borderWidth: StyleSheet.hairlineWidth,
    flexDirection: 'row',
    alignItems: 'center',
    gap: 8,
    paddingHorizontal: 14,
  },
});
