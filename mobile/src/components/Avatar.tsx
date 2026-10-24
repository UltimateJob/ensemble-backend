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

/**
 * 头像渐变对。
 *
 * 原来这十组是高饱和的糖果色（蜜桃、天蓝、丁香、樱粉…），
 * 一屏下去像色卡：每个人都想抢注意力，结果谁都不重要。
 * 现在统一降到 30-40% 饱和度、同一明度区间，靠色相区分而不是靠亮度。
 * 浅→深的方向保持不变，所以果冻质感还在，只是安静了。
 */
const PAIRS: [string, string][] = [
  ['#A8734F', '#8A5A3C'], // 赭
  ['#7A8B6F', '#5F7056'], // 苔绿
  ['#6E7F92', '#546577'], // 岩蓝
  ['#9B6B6B', '#7D5151'], // 陶红
  ['#8A7A5E', '#6E6046'], // 卡其
  ['#6F7F7A', '#556661'], // 松青
  ['#8C7361', '#6E5949'], // 褐
  ['#7D7286', '#61576B'], // 紫灰
  ['#9A7B5C', '#7C6045'], // 沙棕
  ['#6B7A72', '#525F58'], // 墨绿
];

export function colorFor(name: string): string {
  let h = 0;
  for (let i = 0; i < name.length; i++) h = (h * 31 + name.charCodeAt(i)) >>> 0;
  return PAIRS[h % PAIRS.length][0];
}

/**
 * 把任意色相拉进统一的低饱和区间。
 *
 * 后端场景表里的角色颜色是 Tailwind 默认色（#2563EB、#6D28D9、#DC2626…），
 * 饱和度 70-90%，放在暖纸底上像色卡。前端换 token 压不住这些——
 * 它们直接来自接口。所以在这里做一次归一化：保留色相（人还认得出谁是谁），
 * 只把饱和度和亮度收进 30-45% 的窄带。
 */
function normalizeTint(hex: string): string {
  const h6 = hex.replace('#', '');
  if (h6.length !== 6) return hex;
  const r = parseInt(h6.slice(0, 2), 16) / 255;
  const g = parseInt(h6.slice(2, 4), 16) / 255;
  const b = parseInt(h6.slice(4, 6), 16) / 255;
  const max = Math.max(r, g, b);
  const min = Math.min(r, g, b);
  const l = (max + min) / 2;
  let h = 0;
  let s = 0;
  if (max !== min) {
    const d = max - min;
    s = l > 0.5 ? d / (2 - max - min) : d / (max + min);
    if (max === r) h = ((g - b) / d + (g < b ? 6 : 0)) / 6;
    else if (max === g) h = ((b - r) / d + 2) / 6;
    else h = ((r - g) / d + 4) / 6;
  }
  // 固定亮度与饱和度：颜色之间只靠色相区分，不再靠鲜艳程度抢注意力
  const S = 0.32;
  const L = 0.45;
  const q = L < 0.5 ? L * (1 + S) : L + S - L * S;
  const p = 2 * L - q;
  const hue2rgb = (t: number) => {
    let x = t;
    if (x < 0) x += 1;
    if (x > 1) x -= 1;
    if (x < 1 / 6) return p + (q - p) * 6 * x;
    if (x < 1 / 2) return q;
    if (x < 2 / 3) return p + (q - p) * (2 / 3 - x) * 6;
    return p;
  };
  const to = (v: number) => Math.round(v * 255).toString(16).padStart(2, '0');
  return `#${to(hue2rgb(h + 1 / 3))}${to(hue2rgb(h))}${to(hue2rgb(h - 1 / 3))}`;
}

export function pairFor(name: string, tint?: string): [string, string] {
  if (tint) {
    const base = normalizeTint(tint);
    return [lighten(base, 0.22), base];
  }
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
  /** 外层额外样式（用于动画） */
  style?: any;
}

export function Avatar({ name, size = 44, state, tint, index, showDot, style }: Props) {
  const [c1, c2] = pairFor(name, tint ?? (index != null ? MEMBER_COLORS[index % MEMBER_COLORS.length] : undefined));
  const r = Math.round(size * 0.36);

  return (
    <View style={[{ width: size, height: size }, style]}>
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
