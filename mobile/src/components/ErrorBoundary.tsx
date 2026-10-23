import React from 'react';
import { Pressable, StyleSheet, Text, View } from 'react-native';
import { colors, font, radius, space, weights } from '../theme/tokens';

/**
 * 全局错误边界。
 *
 * 没有它时，任何一页里一个 null 解引用就会让整棵组件树卸载，
 * 用户看到纯白屏幕——这在实际开发中已经发生过（随机场景的
 * agents 为 null）。有它至少能说清「哪一页坏了」并给一条出路。
 */
interface Props {
  children: React.ReactNode;
}

interface State {
  error: Error | null;
}

export class ErrorBoundary extends React.Component<Props, State> {
  state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  componentDidCatch(error: Error, info: React.ErrorInfo) {
    // 控制台留全量信息，界面上只给人话
    console.error('[ErrorBoundary]', error, info.componentStack);
  }

  render() {
    if (!this.state.error) return this.props.children;

    return (
      <View style={s.wrap}>
        <Text style={s.title}>这一页出了点问题</Text>
        <Text style={s.hint}>
          其他功能还能用。如果反复出现，试试点「重试」，或者在设置里重新配对。
        </Text>
        <Text style={s.tech} numberOfLines={4}>
          {this.state.error.message}
        </Text>
        <Pressable style={s.btn} onPress={() => this.setState({ error: null })}>
          <Text style={s.btnText}>重试</Text>
        </Pressable>
      </View>
    );
  }
}

const s = StyleSheet.create({
  wrap: {
    flex: 1,
    alignItems: 'center',
    justifyContent: 'center',
    padding: space.xxl,
    backgroundColor: colors.bg,
    gap: space.sm,
  },
  title: { fontSize: font.h2, fontWeight: weights.semibold, color: colors.text },
  hint: { fontSize: font.small, color: colors.text3, textAlign: 'center', lineHeight: 20 },
  tech: {
    fontSize: font.tiny,
    color: colors.text4,
    textAlign: 'center',
    marginTop: space.sm,
    paddingHorizontal: space.md,
  },
  btn: {
    marginTop: space.lg,
    backgroundColor: colors.accent,
    borderRadius: radius.sm,
    paddingHorizontal: space.xxl,
    paddingVertical: space.md,
  },
  btnText: { color: '#FFFFFF', fontSize: font.body, fontWeight: weights.semibold },
});
