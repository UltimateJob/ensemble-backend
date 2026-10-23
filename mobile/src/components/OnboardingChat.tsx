import React, { useEffect, useRef, useState } from 'react';
import { Animated, Easing, Pressable, StyleSheet, View } from 'react-native';
import { LinearGradient } from 'expo-linear-gradient';
import { Avatar } from './Avatar';
import { FadeInUp, TypingDots } from './motion';
import { Button, Row, T, grad } from './ui';
import { useTheme } from '../theme/theme';
import { gap, leading, radius, type } from '../theme/tokens';

/**
 * 首次使用引导。
 *
 * 用对话形式介绍产品，而不是一个弹窗教程——用户已经在聊天界面里，
 * 教会他的最好方式就是"让它看起来像有人在跟你说话"。
 *
 * 节奏：每条消息先显示「正在输入」1.2 秒左右再出字，
 * 这个停顿是拟人感的关键，直接一次性全打出来就像文档。
 */

export interface OnboardStep {
  name: string;
  text: string;
  /** 这一条之后要展示的动作提示（比如"试试左滑"） */
  hint?: string;
  /** 这一条的强调色 */
  tint?: string;
}

export const DEFAULT_STEPS: OnboardStep[] = [
  {
    name: '纸团',
    text: '你好，我是纸团。这里不是普通的聊天窗口——你面对的是一个「团队」。',
    tint: '#6A5BF0',
  },
  {
    name: '纸团',
    text: '每个群里有几个角色：架构师拆任务、后端写接口、前端做界面、测试挑毛病。它们会自己判断要不要说话。',
    tint: '#4A7FE0',
  },
  {
    name: '纸团',
    text: '有些活儿它们不会在群里刷屏。看头像上的蓝圈——那表示它在后台闷头干活，点进去能看到进度。',
    tint: '#34A87C',
  },
  {
    name: '纸团',
    text: '左下角可以左右滑动试试：往右是团队和目标，往左是你自己的设备和设置。',
    tint: '#D9608A',
    hint: '左滑看个人 · 右滑看团队',
  },
];

export function OnboardingChat({
  steps = DEFAULT_STEPS,
  onDone,
}: {
  steps?: OnboardStep[];
  onDone: () => void;
}) {
  const t = useTheme();
  const [shown, setShown] = useState(0); // 已完整显示的条数
  const [typing, setTyping] = useState(true);

  useEffect(() => {
    if (shown >= steps.length) {
      setTyping(false);
      return;
    }
    setTyping(true);
    const wait = 900 + Math.min(steps[shown].text.length * 12, 1400);
    const timer = setTimeout(() => {
      setShown((n) => n + 1);
    }, wait);
    return () => clearTimeout(timer);
  }, [shown, steps]);

  return (
    <View style={{ flex: 1, paddingHorizontal: gap.md, paddingTop: gap.md }}>
      <Row style={{ justifyContent: 'space-between', marginBottom: gap.lg }}>
        <Row gap={gap.xs}>
          <LinearGradient
            colors={grad(t.gradient)}
            start={{ x: 0, y: 0 }}
            end={{ x: 1, y: 1 }}
            style={s.badge}
          >
            <T size={type.caption} color="#FFFFFF" weight="600">
              新手引导
            </T>
          </LinearGradient>
        </Row>
        <T size={type.caption} color={t.ink3} onPress={onDone}>
          跳过
        </T>
      </Row>

      {steps.slice(0, shown).map((st, i) => (
        <TalkBubble key={i} step={st} delay={0} />
      ))}

      {typing && shown < steps.length ? (
        <View style={{ paddingLeft: 42, marginTop: gap.md }}>
          <Row gap={8} style={{ marginLeft: -42 }}>
            <Avatar name={steps[shown].name} size={30} tint={steps[shown].tint} />
            <T size={type.caption} color={t.ink2} weight="600">
              {steps[shown].name}
            </T>
          </Row>
          <View style={{ marginTop: 10 }}>
            <TypingDots color={t.ink4} />
          </View>
        </View>
      ) : null}

      {shown >= steps.length ? (
        <FadeInUp delay={120} style={{ marginTop: gap.lg }}>
          <LinearGradient
            colors={grad(t.gradientSoft)}
            start={{ x: 0, y: 0 }}
            end={{ x: 1, y: 1 }}
            style={s.doneCard}
          >
            <T size={type.headline} weight="600">
              可以开始了
            </T>
            <T size={type.sub} color={t.ink2} lh={leading.sub} style={{ marginTop: 4 }}>
              这个群会一直留着。想问什么直接说，想建团队往右滑。
            </T>
            <View style={{ marginTop: gap.md, alignSelf: 'flex-start' }}>
              <Button label="开始使用" onPress={onDone} size="sm" />
            </View>
          </LinearGradient>
        </FadeInUp>
      ) : null}
    </View>
  );
}


function TalkBubble({ step, delay }: { step: OnboardStep; delay: number }) {
  const t = useTheme();
  const v = useRef(new Animated.Value(0)).current;
  useEffect(() => {
    Animated.timing(v, {
      toValue: 1,
      duration: 340,
      delay,
      easing: Easing.out(Easing.cubic),
      useNativeDriver: true,
    }).start();
  }, [v, delay]);

  return (
    <Animated.View
      style={{
        opacity: v,
        transform: [{ translateY: v.interpolate({ inputRange: [0, 1], outputRange: [10, 0] }) }],
        marginTop: gap.sm,
      }}
    >
      {/* 与正式对话一致：AI 的话不包气泡，直接铺在背景上 */}
      <View style={{ paddingLeft: 42 }}>
        <Row gap={8} style={{ marginLeft: -42 }}>
          <Avatar name={step.name} size={30} tint={step.tint} />
          <T size={type.caption} color={t.ink2} weight="600">
            {step.name}
          </T>
        </Row>
        <T size={type.headline} lh={26} color={t.ink} style={{ marginTop: 8 }}>
          {step.text}
        </T>
        {step.hint ? (
          <View style={[s.hint, { backgroundColor: t.accentSoft }]}>
            <T size={type.caption} color={t.accent} weight="600">
              {step.hint}
            </T>
          </View>
        ) : null}
      </View>
    </Animated.View>
  );
}

const s = StyleSheet.create({
  badge: { paddingHorizontal: 9, paddingVertical: 4, borderRadius: radius.pill },
  bubble: {
    borderRadius: radius.card,
    borderTopLeftRadius: 8,
    paddingHorizontal: gap.md,
    paddingVertical: 12,
    borderWidth: StyleSheet.hairlineWidth,
  },
  hint: {
    alignSelf: 'flex-start',
    marginTop: 6,
    paddingHorizontal: 10,
    paddingVertical: 5,
    borderRadius: radius.pill,
  },
  doneCard: {
    borderRadius: 22,
    padding: 18,
  },
});
