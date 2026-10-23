import { LinearGradient } from 'expo-linear-gradient';
import React, { useEffect, useRef } from 'react';
import { Animated, Easing, StyleSheet, Text, View } from 'react-native';
import { useTheme } from '../theme/theme';
import { MEMBER_COLORS } from '../theme/tokens';
import type { AgentState } from '../lib/types';

/**
 * 角色头像。
 *
 * 用**双色渐变 + 高光**做出有体积感的"果冻/黏土"质感，
 * 而不是一块纯色方块。同一角色永远同一组颜色（由名字 hash 决定）。
 *
 * 渐变对是配好的：深→浅，同色系，不会出现脏色。
 */

const PAIRS: [string, string][] = [
  ['#F29B6E', '#E8734A'], // 蜜桃
  ['#6FA6F5', '#4A7FE0'], // 天蓝
  ['#A78BFA', '#7C5CE0'], // 丁香
  ['#5FD3A8', '#34A87C'], // 薄荷
  ['#F5C56B', '#DFA02F'], // 蜜糖
  ['#F58FB0', '#D9608A'], // 樱粉
  ['#7FB3E8', '#5382C4'], // 雾蓝
  ['#7ED49A', '#48AE6E'], // 苔绿
  ['#F0A47A', '#CE7440'], // 陶土
  ['#B39AF0', '#8B6FD6'], // 紫藤
];

export function colorFor(name: string): string {
  let h = 0;
  for (let i = 0; i < name.length; i++) h = (h * 31 + name.charCodeAt(i)) >>> 0;
  return PAIRS[h % PAIRS.length][0];
}

export function pairFor(name: string, tint?: string): [string, string] {
  if (tint) return [lighten(tint, 0.22), tint];
  let h = 0;
  for (let i = 0; i < name.length; i++) h = (h * 31 + name.charCodeAt(i)) >>> 0;
  return PAIRS[h % PAIRS.length];
}

export function initialOf(name: string): string {
  const t = name.trim();
  if (!t) return '?';
  const first = t[0];
  if (/[a-zA-Z]/.test(first)) return first.toUpperCase();
  return first;
}

interface Props {
  name: string;
  size?: number;
  state?: AgentState;
  tint?: string;
  /** 成员序号：用于按规范 §2.3 循环取大地色身份色 */
  index?: number;
  /** 是否显示右下角状态点（规范 §5.3） */
  showDot?: boolean;
}

export function Avatar({ name, size = 44, state, tint, index, showDot }: Props) {
  const [c1, c2] = pairFor(name, tint ?? (index != null ? MEMBER_COLORS[index % MEMBER_COLORS.length] : undefined));
  const r = Math.round(size * 0.36);

  return (
    <View style={{ width: size, height: size }}>
      {state ? <StateRing state={state} size={size} /> : null}
      <View
        style={{
          width: size,
          height: size,
          borderRadius: r,
          overflow: 'hidden',
          alignItems: 'center',
          justifyContent: 'center',
        }}
      >
        <LinearGradient
          colors={[c1, c2]}
          start={{ x: 0.15, y: 0 }}
          end={{ x: 0.85, y: 1 }}
          style={StyleSheet.absoluteFill}
        />
        {/* 左上高光：让方块看起来是凸的 */}
        <View
          pointerEvents="none"
          style={{
            position: 'absolute',
            top: -size * 0.3,
            left: -size * 0.2,
            width: size * 0.95,
            height: size * 0.75,
            borderRadius: size * 0.5,
            backgroundColor: 'rgba(255,255,255,0.28)',
            transform: [{ rotate: '-20deg' }],
          }}
        />
        {/* 底部内阴影 */}
        <View
          pointerEvents="none"
          style={{
            position: 'absolute',
            bottom: -size * 0.24,
            right: -size * 0.16,
            width: size * 0.8,
            height: size * 0.62,
            borderRadius: size * 0.42,
            backgroundColor: 'rgba(0,0,0,0.09)',
          }}
        />
        <Text
          style={{
            color: '#FFFFFF',
            fontSize: size * 0.38,
            fontWeight: '700',
            includeFontPadding: false,
            textShadowColor: 'rgba(0,0,0,0.10)',
            textShadowOffset: { width: 0, height: 1 },
            textShadowRadius: 2,
          }}
        >
          {initialOf(name)}
        </Text>
      </View>
    </View>
  );
}



/**
 * 状态环。
 *
 * 空闲不画环。干活时是**扩散脉冲**（两圈错开，像信号在发出），
 * 而不是简单的透明度闪烁——扩散比闪烁更像"正在做事"。
 */
function StateRing({ state, size }: { state: AgentState; size: number }) {
  const t = useTheme();
  const RING = {
    idle: t.ringIdle,
    working: t.ringWorking,
    done: t.ringDone,
    blocked: t.ringBlocked,
  } as const;
  const pulse1 = useRef(new Animated.Value(0)).current;
  const pulse2 = useRef(new Animated.Value(0)).current;

  useEffect(() => {
    if (state !== 'working') {
      pulse1.setValue(0);
      pulse2.setValue(0);
      return;
    }
    const make = (v: Animated.Value, delay: number) =>
      Animated.loop(
        Animated.sequence([
          Animated.delay(delay),
          Animated.timing(v, {
            toValue: 1,
            duration: 1800,
            easing: Easing.out(Easing.quad),
            useNativeDriver: true,
          }),
          Animated.timing(v, { toValue: 0, duration: 0, useNativeDriver: true }),
          Animated.delay(1800 - delay),
        ]),
      );
    const l1 = make(pulse1, 0);
    const l2 = make(pulse2, 900);
    l1.start();
    l2.start();
    return () => {
      l1.stop();
      l2.stop();
    };
  }, [state, pulse1, pulse2]);

  if (state === 'idle') return null;

  const pad = 3;
  const r = Math.round((size + pad * 2) * 0.36);

  return (
    <>
      {state === 'working' ? (
        <>
          {[pulse1, pulse2].map((p, i) => (
            <Animated.View
              key={i}
              pointerEvents="none"
              style={{
                position: 'absolute',
                top: -pad,
                left: -pad,
                width: size + pad * 2,
                height: size + pad * 2,
                borderRadius: r,
                borderWidth: 2,
                borderColor: t.ringWorking,
                opacity: p.interpolate({ inputRange: [0, 1], outputRange: [0.55, 0] }),
                transform: [
                  { scale: p.interpolate({ inputRange: [0, 1], outputRange: [1, 1.28] }) },
                ],
              }}
            />
          ))}
        </>
      ) : null}
      <View
        pointerEvents="none"
        style={{
          position: 'absolute',
          top: -pad,
          left: -pad,
          width: size + pad * 2,
          height: size + pad * 2,
          borderRadius: r,
          borderWidth: 2,
          borderColor: RING[state],
        }}
      />
    </>
  );
}

/** 头像叠放。 */
export function AvatarStack({
  names,
  size = 22,
  max = 5,
  tints,
}: {
  names: string[];
  size?: number;
  max?: number;
  tints?: string[];
}) {
  const t = useTheme();
  const shown = names.slice(0, max);
  const rest = names.length - shown.length;
  return (
    <View style={{ flexDirection: 'row', alignItems: 'center' }}>
      {shown.map((n, i) => (
        <View
          key={n + i}
          style={{
            marginLeft: i === 0 ? 0 : -size * 0.32,
            borderRadius: Math.round(size * 0.36) + 2,
            borderWidth: 2.5,
            borderColor: t.card,
          }}
        >
          <Avatar name={n} size={size} tint={tints?.[i]} />
        </View>
      ))}
      {rest > 0 ? (
        <View
          style={{
            marginLeft: -size * 0.32,
            width: size,
            height: size,
            borderRadius: Math.round(size * 0.36),
            backgroundColor: t.cardSunken,
            borderWidth: 2.5,
            borderColor: t.card,
            alignItems: 'center',
            justifyContent: 'center',
          }}
        >
          <Text style={{ fontSize: size * 0.32, color: t.ink2, fontWeight: '700' }}>+{rest}</Text>
        </View>
      ) : null}
    </View>
  );
}

/** 把十六进制色提亮，用于生成渐变的浅端。 */
function lighten(hex: string, amount: number): string {
  const h = hex.replace('#', '');
  const r = parseInt(h.slice(0, 2), 16);
  const g = parseInt(h.slice(2, 4), 16);
  const b = parseInt(h.slice(4, 6), 16);
  const mix = (c: number) => Math.round(c + (255 - c) * amount);
  return `#${[mix(r), mix(g), mix(b)].map((v) => v.toString(16).padStart(2, '0')).join('')}`;
}
