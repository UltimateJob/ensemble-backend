import React, { useEffect, useRef } from 'react';
import { Animated, Easing, StyleSheet, View, type StyleProp, type ViewStyle } from 'react-native';
import { useTheme } from '../theme/theme';

/**
 * 骨架屏。
 *
 * 比转圈 loading 好，因为它**告诉用户将要出现什么形状**——
 * 页面不会从"全空"突变成"全满"，不会有跳动感。
 *
 * 呼吸节奏和真实内容一致：1.6s 一个来回，和卡片的入场动画同频。
 */
export function Skeleton({
  width,
  height,
  radius = 8,
  style,
}: {
  width?: number | `${number}%`;
  height: number;
  radius?: number;
  style?: StyleProp<ViewStyle>;
}) {
  const t = useTheme();
  const v = useRef(new Animated.Value(0.45)).current;

  useEffect(() => {
    const loop = Animated.loop(
      Animated.sequence([
        Animated.timing(v, {
          toValue: 1,
          duration: 800,
          easing: Easing.inOut(Easing.quad),
          useNativeDriver: true,
        }),
        Animated.timing(v, {
          toValue: 0.45,
          duration: 800,
          easing: Easing.inOut(Easing.quad),
          useNativeDriver: true,
        }),
      ]),
    );
    loop.start();
    return () => loop.stop();
  }, [v]);

  return (
    <Animated.View
      style={[
        {
          width: width ?? '100%',
          height,
          borderRadius: radius,
          backgroundColor: t.cardSunken,
          opacity: v,
        },
        style,
      ]}
    />
  );
}

/** 列表骨架：n 行「图标 + 两行字」的形状。 */
export function ListSkeleton({ rows = 4 }: { rows?: number }) {
  const t = useTheme();
  return (
    <View style={{ gap: 10, paddingHorizontal: 20, paddingTop: 12 }}>
      {Array.from({ length: rows }).map((_, i) => (
        <View
          key={i}
          style={{
            flexDirection: 'row',
            alignItems: 'center',
            gap: 12,
            padding: 14,
            borderRadius: 22,
            backgroundColor: t.card,
          }}
        >
          <Skeleton width={38} height={38} radius={12} />
          <View style={{ flex: 1, gap: 7 }}>
            <Skeleton width="55%" height={13} />
            <Skeleton width="32%" height={10} />
          </View>
        </View>
      ))}
    </View>
  );
}

export const skeletonStyles = StyleSheet.create({});
