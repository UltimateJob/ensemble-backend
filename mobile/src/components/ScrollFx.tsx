import React, { useRef } from 'react';
import { Animated, type StyleProp, type ViewStyle } from 'react-native';

/**
 * 滚动视差容器。
 *
 * 把 ScrollView 的 scrollY 传给子元素，让它们按各自系数位移/缩放。
 * 纯 transform + opacity，不触发重排，滚动时不会掉帧。
 *
 * 这里的"视差"做得很克制：只让内容以 0.15 的系数反向位移，
 * 并且用一个 120px 的淡出区。幅度再大就会晕。
 */
export function useParallax(scrollY: Animated.Value, distance = 60) {
  return {
    transform: [
      {
        translateY: scrollY.interpolate({
          inputRange: [0, distance * 2],
          outputRange: [0, distance],
          extrapolate: 'clamp',
        }),
      },
    ],
    opacity: scrollY.interpolate({
      inputRange: [0, distance * 1.6],
      outputRange: [1, 0],
      extrapolate: 'clamp',
    }),
  };
}

/**
 * 随滚动淡出的头部。
 *
 * 用一个可复用的 Animated 风格：越往下滚，头部越淡、越往上顶。
 * 这是 iOS 大标题收起效果的轻量版。
 */
export function FadingHeader({
  scrollY,
  children,
  style,
}: {
  scrollY: Animated.Value;
  children: React.ReactNode;
  style?: StyleProp<ViewStyle>;
}) {
  const anim = useParallax(scrollY, 48);
  return (
    <Animated.View style={[style, anim as any]} pointerEvents="box-none">
      {children}
    </Animated.View>
  );
}

/** 滚动驱动的进度条（顶部细条），用于长列表指示。 */
export function ScrollProgress({
  scrollY,
  contentHeight,
  viewportHeight,
  color,
}: {
  scrollY: Animated.Value;
  contentHeight: number;
  viewportHeight: number;
  color: string;
}) {
  const max = Math.max(1, contentHeight - viewportHeight);
  const width = scrollY.interpolate({
    inputRange: [0, max],
    outputRange: ['0%', '100%'],
    extrapolate: 'clamp',
  });
  return (
    <Animated.View
      style={{
        height: 2,
        width,
        backgroundColor: color,
        borderRadius: 1,
        opacity: 0.6,
      }}
    />
  );
}
