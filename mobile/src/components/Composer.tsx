import { Ionicons } from '@expo/vector-icons';
import { LinearGradient } from 'expo-linear-gradient';
import React, { useRef, useState } from 'react';
import {
  Animated,
  Easing,
  Platform,
  Pressable,
  StyleSheet,
  TextInput,
  View,
} from 'react-native';
import { useTheme } from '../theme/theme';
import { gap, type } from '../theme/tokens';
import { grad } from './ui';

/**
 * 输入区。
 *
 * 参照元宝的形态，而不是"一个白色圆角矩形"：
 *
 *   ┌─────────────────────────────────────────┐
 *   │  [@]  说点什么…              [🎤] [ + ] │   ← 一个大容器
 *   └─────────────────────────────────────────┘
 *         [深度] [AI创作] [拍题答疑] …            ← 悬浮在容器上方的建议胶囊
 *
 * 关键区别：
 *   - 图标**内嵌**在容器里（不是外挂的按钮），容器高度 56
 *   - 圆角 28（大胶囊），不是 16
 *   - 有输入内容时，右侧的「+」滑出、发送键渐入——有状态切换动画
 *   - 容器本身是玻璃，浮在页面上
 */
export function Composer({
  value,
  onChangeText,
  onSend,
  onStop,
  busy,
  onMention,
  placeholder,
  chips,
}: {
  value: string;
  onChangeText: (v: string) => void;
  onSend: () => void;
  onStop?: () => void;
  busy?: boolean;
  onMention?: () => void;
  placeholder?: string;
  chips?: { icon: React.ComponentProps<typeof Ionicons>['name']; label: string; tint: string; onPress: () => void }[];
}) {
  const t = useTheme();
  const [focused, setFocused] = useState(false);
  const hasText = value.trim().length > 0;

  // 发送键的出现动画：有文字时从 0 缩放到 1
  const sendAnim = useRef(new Animated.Value(0)).current;
  const lastHasText = useRef(false);
  if (lastHasText.current !== hasText) {
    lastHasText.current = hasText;
    Animated.timing(sendAnim, {
      toValue: hasText ? 1 : 0,
      duration: 200,
      easing: Easing.bezier(0.22, 1, 0.36, 1),
      useNativeDriver: true,
    }).start();
  }

  return (
    <View style={s.wrap}>
      {/* 建议胶囊：横滑，浮在输入框上方 */}
      {chips && chips.length > 0 && !hasText ? (
        <View style={s.chipsRow}>
          {chips.map((c) => (
            <Pressable
              key={c.label}
              onPress={c.onPress}
              style={({ pressed }) => [
                s.chip,
                {
                  backgroundColor: t.dark ? 'rgba(255,255,255,0.06)' : 'rgba(255,255,255,0.9)',
                  borderColor: t.dark ? 'rgba(255,255,255,0.08)' : 'rgba(16,20,45,0.05)',
                  opacity: pressed ? 0.7 : 1,
                },
              ]}
            >
              <Ionicons name={c.icon} size={13} color={c.tint} />
              <Animated.Text style={[s.chipText, { color: t.ink2 }]}>{c.label}</Animated.Text>
            </Pressable>
          ))}
        </View>
      ) : null}

      {/* 主容器 */}
      <View
        style={[
          s.container,
          {
            backgroundColor: t.dark ? 'rgba(30,31,40,0.92)' : '#FFFFFF',
            borderColor: focused
              ? t.dark
                ? 'rgba(255,255,255,0.16)'
                : 'rgba(61,107,245,0.28)'
              : t.dark
                ? 'rgba(255,255,255,0.08)'
                : 'rgba(16,20,45,0.055)',
            shadowOpacity: t.dark ? 0.5 : 0.1,
          },
        ]}
      >
        {/* 左：@ 按钮内嵌在容器里 */}
        <Pressable onPress={onMention} hitSlop={6} style={({ pressed }) => [s.iconBtn, { opacity: pressed ? 0.5 : 1 }]}>
          <Ionicons name="at" size={19} color={t.ink3} />
        </Pressable>

        {/* 中：输入 */}
        <TextInput
          value={value}
          onChangeText={onChangeText}
          onFocus={() => setFocused(true)}
          onBlur={() => setFocused(false)}
          placeholder={placeholder ?? '说点什么…'}
          placeholderTextColor={t.ink4}
          multiline
          style={[s.input, { color: t.ink }]}
          editable={!busy}
        />

        {/* 右：忙碌时停止，否则有文字时发送、无文字时加号 */}
        {busy ? (
          <Pressable onPress={onStop} hitSlop={6} style={s.sendBtn}>
            <View style={[s.stopSquare, { backgroundColor: t.ink }]} />
          </Pressable>
        ) : (
          <View style={s.rightCluster}>
            {!hasText ? (
              <Pressable hitSlop={6} style={({ pressed }) => [s.iconBtn, { opacity: pressed ? 0.5 : 1 }]}>
                <Ionicons name="add" size={21} color={t.ink3} />
              </Pressable>
            ) : null}
            <Animated.View
              style={{
                opacity: sendAnim,
                transform: [
                  { scale: sendAnim.interpolate({ inputRange: [0, 1], outputRange: [0.6, 1] }) },
                ],
                pointerEvents: hasText ? 'auto' : 'none',
              }}
            >
              <Pressable onPress={onSend} hitSlop={6}>
                <LinearGradient
                  colors={grad(t.gradient)}
                  start={{ x: 0, y: 0 }}
                  end={{ x: 1, y: 1 }}
                  style={[
                    s.sendBtn,
                    {
                      shadowColor: t.accent,
                      shadowOpacity: 0.35,
                      shadowRadius: 12,
                      shadowOffset: { width: 0, height: 4 },
                      elevation: 6,
                    },
                  ]}
                >
                  <Ionicons name="arrow-up" size={19} color="#FFFFFF" />
                </LinearGradient>
              </Pressable>
            </Animated.View>
          </View>
        )}
      </View>
    </View>
  );
}

const s = StyleSheet.create({
  wrap: { paddingHorizontal: gap.md, paddingBottom: 10 },
  chipsRow: {
    flexDirection: 'row',
    gap: 8,
    marginBottom: 10,
    paddingHorizontal: 2,
    flexWrap: 'wrap',
  },
  chip: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 6,
    paddingHorizontal: 12,
    paddingVertical: 8,
    borderRadius: 999,
    borderWidth: StyleSheet.hairlineWidth,
  },
  chipText: { fontSize: 13, fontWeight: '600' },
  container: {
    flexDirection: 'row',
    // center 而不是 flex-end：左图标和右侧按钮要垂直居中于整行，
    // flex-end 会让它们在多行输入时掉到底部。
    alignItems: 'center',
    gap: 0,
    borderRadius: 28,
    paddingLeft: 8,
    paddingRight: 6,
    paddingVertical: 5,
    minHeight: 56,
    borderWidth: 1,
    shadowColor: '#2B3A77',
    shadowRadius: 22,
    shadowOffset: { width: 0, height: 8 },
    elevation: 8,
  },
  iconBtn: {
    width: 38,
    height: 42,
    alignItems: 'center',
    justifyContent: 'center',
  },
  input: {
    flex: 1,
    minHeight: 46,
    maxHeight: 132,
    paddingTop: 12,
    paddingBottom: 12,
    paddingHorizontal: 0,
    fontSize: 15,
    lineHeight: 22,
    textAlignVertical: 'center',
  },
  rightCluster: { flexDirection: 'row', alignItems: 'center', gap: 2 },
  sendBtn: { width: 40, height: 40, borderRadius: 20, alignItems: 'center', justifyContent: 'center' },
  stopSquare: { width: 12, height: 12, borderRadius: 3 },
});
