import React, { useEffect, useRef } from 'react';
import {
  Animated,
  Easing,
  Pressable,
  type PressableProps,
  type StyleProp,
  type ViewStyle,
} from 'react-native';
import { motion } from '../theme/tokens';

/**
 * 动效曲线。
 *
 * 默认的 ease-in-out 是"算出来的"，看起来像机器在动。
 * 这里用几条手工调过的曲线，各自对应一种物理直觉：
 *
 *   out     —— 减速停下（进场、展开）：快起慢收，像东西被甩出去后自然停住
 *   inOut   —— 两头慢中间快（位置迁移）：有质量感的位移
 *   springy —— 略过冲（弹窗、按压回弹）：有弹性的材质
 */
const EASE = {
  out: Easing.bezier(0.22, 1, 0.36, 1),
  inOut: Easing.bezier(0.65, 0, 0.35, 1),
  springy: Easing.bezier(0.34, 1.56, 0.64, 1),
} as const;

/**
 * 动效原语。
 *
 * 这里的每一条都是为了让界面"活"起来——按压有回弹、进场有错帧、
 * 状态有呼吸。静态截图看不出差别，但它是「精致」和「廉价」的分界线。
 */

/**
 * 弹性按压。
 *
 * iOS 的手感来自两件事：按下时轻微缩小 + 松开时弹回。
 * 用 Animated.spring 而不是 timing，因为 spring 的回弹是"物理的"，
 * timing 的是"算出来的"，后者永远差一点。
 */
export function PressableScale({
  children,
  style,
  scaleTo = 0.965,
  haptic,
  ...rest
}: PressableProps & {
  children: React.ReactNode;
  style?: StyleProp<ViewStyle>;
  scaleTo?: number;
  haptic?: boolean;
}) {
  const scale = useRef(new Animated.Value(1)).current;

  const to = (v: number) =>
    Animated.spring(scale, {
      toValue: v,
      useNativeDriver: true,
      damping: 22,
      stiffness: 380,
      mass: 0.5,
      overshootClamping: v < 1, // 按下时不回弹，松开时轻微过冲
    }).start();

  return (
    <Animated.View style={[{ transform: [{ scale }] }, style]}>
      <Pressable
        {...rest}
        onPressIn={(e) => {
          to(scaleTo);
          rest.onPressIn?.(e);
        }}
        onPressOut={(e) => {
          to(1);
          rest.onPressOut?.(e);
        }}
      >
        {children}
      </Pressable>
    </Animated.View>
  );
}

/**
 * 入场：从下方淡入上浮。
 *
 * delay 用于列表错帧——第一张卡 0ms、第二张 45ms、第三张 90ms……
 * 一次性全部同时出现会显得很"机械"。
 */
export { EASE };

export function FadeInUp({
  children,
  delay = 0,
  distance = 14,
  style,
}: {
  children: React.ReactNode;
  delay?: number;
  distance?: number;
  style?: StyleProp<ViewStyle>;
}) {
  const v = useRef(new Animated.Value(0)).current;

  useEffect(() => {
    Animated.timing(v, {
      toValue: 1,
      duration: 520,
      delay,
      easing: EASE.out,
      useNativeDriver: true,
    }).start();
  }, [v, delay]);

  return (
    <Animated.View
      style={[
        {
          opacity: v,
          transform: [
            {
              translateY: v.interpolate({ inputRange: [0, 1], outputRange: [distance, 0] }),
            },
          ],
        },
        style,
      ]}
    >
      {children}
    </Animated.View>
  );
}

/**
 * 呼吸：给"正在干活"的元素用。
 *
 * 不是简单的 opacity 闪，而是 scale + opacity 一起变——
 * 单独变透明度像"在闪烁"，加上 scale 才像"在呼吸"。
 */
export function Breathe({
  children,
  active = true,
  min = 0.86,
  duration = 2200,
  style,
}: {
  children: React.ReactNode;
  active?: boolean;
  min?: number;
  duration?: number;
  style?: StyleProp<ViewStyle>;
}) {
  const v = useRef(new Animated.Value(1)).current;

  useEffect(() => {
    if (!active) {
      v.setValue(1);
      return;
    }
    const loop = Animated.loop(
      Animated.sequence([
        Animated.timing(v, {
          toValue: min,
          duration: duration / 2,
          easing: Easing.inOut(Easing.sin),
          useNativeDriver: true,
        }),
        Animated.timing(v, {
          toValue: 1,
          duration: duration / 2,
          easing: Easing.inOut(Easing.sin),
          useNativeDriver: true,
        }),
      ]),
    );
    loop.start();
    return () => loop.stop();
  }, [active, min, duration, v]);

  return (
    <Animated.View style={[{ opacity: v, transform: [{ scale: v }] }, style]}>
      {children}
    </Animated.View>
  );
}

/** 三个点的打字指示器，点依次亮起。 */
export function TypingDots({ color }: { color: string }) {
  const dots = [useRef(new Animated.Value(0.35)).current, useRef(new Animated.Value(0.35)).current, useRef(new Animated.Value(0.35)).current];

  useEffect(() => {
    const loops = dots.map((d, i) =>
      Animated.loop(
        Animated.sequence([
          Animated.delay(i * 180),
          Animated.timing(d, {
            toValue: 1,
            duration: 420,
            easing: Easing.inOut(Easing.quad),
            useNativeDriver: true,
          }),
          Animated.timing(d, {
            toValue: 0.35,
            duration: 420,
            easing: Easing.inOut(Easing.quad),
            useNativeDriver: true,
          }),
          Animated.delay((2 - i) * 180),
        ]),
      ),
    );
    loops.forEach((l) => l.start());
    return () => loops.forEach((l) => l.stop());
  }, [dots]);

  return (
    <Animated.View style={{ flexDirection: 'row', gap: 5, paddingVertical: 4 }}>
      {dots.map((d, i) => (
        <Animated.View
          key={i}
          style={{
            width: 6,
            height: 6,
            borderRadius: 3,
            backgroundColor: color,
            opacity: d,
          }}
        />
      ))}
    </Animated.View>
  );
}
