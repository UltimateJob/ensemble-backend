import { Ionicons } from '@expo/vector-icons';
import { LinearGradient } from 'expo-linear-gradient';
import React, { useEffect, useRef } from 'react';
import { Animated, Easing, Pressable, StyleSheet, View } from 'react-native';
import { useTheme } from '../theme/theme';
import {
  MEMBER_COLORS,
  radius,
  shadow,
  type,
  weight,
} from '../theme/tokens';
import { Avatar } from './Avatar';
import { PulseDot, T, Icon } from './ui';

/**
 * 群聊专用组件。
 *
 * 对应设计规范 §6.1-6.5。这些组件只在群聊里出现，
 * 和门面页的卡片不是一套东西——群聊更紧、更密、更收敛。
 */

/* ============================================================
   §6.1 顶栏
   ============================================================ */

export function ChatHeader({
  title,
  statusLine,
  onBack,
  onMenu,
}: {
  title: string;
  /** 一行状态句：「2 位在干活 · 1 位等你审批」 */
  statusLine?: string;
  onBack: () => void;
  onMenu: () => void;
}) {
  const t = useTheme();
  return (
    <View style={[styles.header, { borderBottomColor: t.hairline }]}>
      <Pressable onPress={onBack} hitSlop={10} style={styles.headerBtn}>
        <Icon name="chevron-back" size={21} color={t.ink} />
      </Pressable>
      <View style={{ flex: 1, alignItems: 'center' }}>
        <T size={type.title} weight={weight.strong} numberOfLines={1}>
          {title}
        </T>
        {statusLine ? (
          <T size={type.micro} color={t.ink3} numberOfLines={1} style={{ marginTop: 2 }}>
            {statusLine}
          </T>
        ) : null}
      </View>
      <Pressable onPress={onMenu} hitSlop={10} style={styles.headerBtn}>
        <Icon name="menu" size={20} color={t.ink} />
      </Pressable>
    </View>
  );
}

/* ============================================================
   §6.2 成员状态条
   ============================================================ */

export type MemberState = 'working' | 'done' | 'blocked' | 'idle';

export interface BarMember {
  key: string;
  name: string;
  color?: string;
  index?: number;
  state: MemberState;
}

/**
 * 成员状态条。
 *
 * 纸团特有的组件：一行头像 + 一句状态文案。
 * 当前发言者头像放大到 39px 并浮到最前（z 序），发言结束缩回 30px。
 *
 * 状态点语义（全 App 统一）：
 *   干活中 = 墨黑点（呼吸脉冲）
 *   刚完成 = 绿
 *   等你审批 = 红
 *   空闲   = 无点
 */
export function MemberBar({
  members,
  speakerKey,
  summary,
  onOpenInfo,
}: {
  members: BarMember[];
  /** 当前发言者的 key */
  speakerKey?: string | null;
  /** 右侧状态文案，如「撰写 刚发了言 · 查证 干活中」 */
  summary?: React.ReactNode;
  onOpenInfo?: () => void;
}) {
  const t = useTheme();
  return (
    <View style={[styles.memberBar, { backgroundColor: t.slab, borderColor: t.hairline }]}>
      <View style={styles.memberStack}>
        {members.slice(0, 4).map((m, i) => (
          <SpeakerAvatar
            key={m.key}
            member={m}
            i={i}
            isSpeaker={m.key === speakerKey}
            zIndex={m.key === speakerKey ? 10 : 4 - i}
          />
        ))}
      </View>
      <Pressable onPress={onOpenInfo} style={{ flex: 1, marginLeft: 14 }} hitSlop={6}>
        <View style={{ flexDirection: 'row', alignItems: 'center', gap: 4 }}>
          <View style={{ flex: 1 }}>{summary}</View>
          {onOpenInfo ? (
            <>
              <T size={type.caption} color={t.ink3}>
                群信息
              </T>
              <Icon name="chevron-forward" size={12} color={t.ink4} />
            </>
          ) : null}
        </View>
      </Pressable>
    </View>
  );
}

function SpeakerAvatar({
  member,
  i,
  isSpeaker,
  zIndex,
}: {
  member: BarMember;
  i: number;
  isSpeaker: boolean;
  zIndex: number;
}) {
  const t = useTheme();
  const grow = useRef(new Animated.Value(isSpeaker ? 1 : 0)).current;

  useEffect(() => {
    if (isSpeaker) {
      // 规范 §8：放大 320ms，spring 过冲 8%
      Animated.spring(grow, {
        toValue: 1,
        useNativeDriver: false,
        damping: 11,
        stiffness: 170,
        mass: 0.7,
      }).start();
    } else {
      // 缩回 240ms，无过冲
      Animated.timing(grow, {
        toValue: 0,
        duration: 240,
        easing: Easing.out(Easing.cubic),
        useNativeDriver: false,
      }).start();
    }
  }, [isSpeaker, grow]);

  const size = grow.interpolate({ inputRange: [0, 1], outputRange: [36, 46] });
  const dot = Math.max(9, 9);
  const dotColor =
    member.state === 'done'
      ? t.okDot
      : member.state === 'blocked'
        ? t.dangerDot
        : member.state === 'working'
          ? t.ink
          : 'transparent';

  return (
    <Animated.View
      style={{
        width: size,
        height: size,
        // 后一个压前一个；第一个不留负边距
        // 重叠 10px：36px 头像露出 26px，和设计图的比例一致。
        // 再大就会把字压住（规范 §5.3 明确说不得超过 7px 的字宽）
        marginLeft: i === 0 ? 0 : -10,
        // 发言者永远在最前：zIndex 由 isSpeaker 决定，不靠位置推算
        zIndex: isSpeaker ? 99 : 10 - i,
        // 白色分隔环——设计图里头像之间那道边就是这个
        borderRadius: 999,
        borderWidth: 2.5,
        borderColor: t.card,
        backgroundColor: t.card,
      }}
    >
      {/* 关键：裁成正圆。Avatar 内部是圆角矩形，直接放进来会露出方角 */}
      <Animated.View
        style={{
          width: size,
          height: size,
          borderRadius: 999,
          overflow: 'hidden',
          alignItems: 'center',
          justifyContent: 'center',
        }}
      >
        <Avatar name={member.name} size={46} index={member.index} tint={member.color} />
      </Animated.View>

      {/* 状态点：右下角，带与底色同色的描边环（规范 §5.3） */}
      {member.state !== 'idle' ? (
        <View
          style={{
            position: 'absolute',
            right: -1.5,
            bottom: -1.5,
            width: dot,
            height: dot,
            borderRadius: dot / 2,
            borderWidth: 2,
            borderColor: t.card,
            backgroundColor: dotColor,
          }}
        />
      ) : null}
    </Animated.View>
  );
}

/* ============================================================
   §6.3 气泡
   ============================================================ */

/** 成员气泡：白→#FAFBFC 渐变 + 发丝边 + 卡片投影，圆角 15。 */
export function MemberBubble({
  children,
  streaming,
}: {
  children: React.ReactNode;
  streaming?: boolean;
}) {
  const t = useTheme();
  return (
    <LinearGradient
      colors={t.dark ? [t.cardTop, t.card] : ['#FFFFFF', '#FAFBFC']}
      start={{ x: 0, y: 0 }}
      end={{ x: 0, y: 1 }}
      style={[
        styles.memberBubble,
        { borderColor: t.hairline },
      ]}
    >
      {children}
    </LinearGradient>
  );
}

/** 用户气泡：墨黑渐变 + 白字；暗色反转为白底墨字。 */
export function UserBubble({ children }: { children: React.ReactNode }) {
  const t = useTheme();
  return (
    <View style={{ alignItems: 'flex-end' }}>
      {t.dark ? (
        <View style={[styles.userBubble, { backgroundColor: t.primaryBg }]}>{children}</View>
      ) : (
        <LinearGradient
          colors={[t.primaryGrad[0], t.primaryGrad[1]]}
          start={{ x: 0, y: 0 }}
          end={{ x: 0, y: 1 }}
          style={styles.userBubble}
        >
          {children}
        </LinearGradient>
      )}
    </View>
  );
}

/* ============================================================
   §6.4 卡片消息
   ============================================================ */

/** 任务卡：类型行 → 标题 → 进度行。宽 252-262，圆角 17。 */
export function TaskCard({
  title,
  step,
  total,
  status,
  statusTone = 'neutral',
  onPress,
}: {
  title: string;
  step: number;
  total: number;
  status: string;
  statusTone?: 'neutral' | 'blocked' | 'done' | 'working';
  onPress?: () => void;
}) {
  const t = useTheme();
  const pct = total > 0 ? Math.min(100, (step / total) * 100) : 0;
  return (
    <Pressable onPress={onPress} style={[styles.cardMsg, { backgroundColor: t.card, borderColor: t.hairline }]}>
      <View style={{ flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between' }}>
        <View style={{ flexDirection: 'row', alignItems: 'center', gap: 6 }}>
          <Icon name="checkbox-outline" size={13} color={t.ink2} />
          <T size={type.micro} color={t.ink2} weight={weight.semibold}>
            任务
          </T>
        </View>
        <View
          style={[
            styles.statusPill,
            {
              backgroundColor:
                statusTone === 'blocked'
                  ? t.badSoft
                  : statusTone === 'done'
                    ? t.okSoft
                    : t.cardSunken,
            },
          ]}
        >
          <T
            size={type.micro}
            weight={weight.semibold}
            color={
              statusTone === 'blocked' ? t.bad : statusTone === 'done' ? t.ok : t.ink3
            }
          >
            {status}
          </T>
        </View>
      </View>
      <T size={type.card} weight={weight.strong} style={{ marginTop: 9 }} lh={21} numberOfLines={2}>
        {title}
      </T>
      <View style={{ flexDirection: 'row', alignItems: 'center', gap: 10, marginTop: 11 }}>
        <View style={[styles.progressTrack, { backgroundColor: t.track }]}>
          <View
            style={{
              width: `${pct}%`,
              height: 5,
              borderRadius: 99,
              backgroundColor: t.dark ? '#F2F2F2' : t.ink,
            }}
          />
        </View>
        <T size={type.micro} color={t.ink3} weight={weight.semibold}>
          {step} / {total} 步
        </T>
      </View>
    </Pressable>
  );
}

/** 文件卡：42px 图标底 + 文件名 + 元信息。 */
export function FileCard({
  name,
  meta,
  icon = 'document-text',
  tint,
  onPress,
}: {
  name: string;
  meta: string;
  icon?: React.ComponentProps<typeof Ionicons>['name'];
  tint?: string;
  onPress?: () => void;
}) {
  const t = useTheme();
  return (
    <Pressable onPress={onPress} style={[styles.fileCard, { backgroundColor: t.card, borderColor: t.hairline }]}>
      <View
        style={[
          styles.fileIcon,
          { backgroundColor: withAlpha(tint ?? '#7C93A8', 0.16) },
        ]}
      >
        <Ionicons name={icon} size={20} color={tint ?? '#7C93A8'} />
      </View>
      <View style={{ flex: 1 }}>
        <T size={type.sub} weight={weight.strong} numberOfLines={1}>
          {name}
        </T>
        <T size={type.micro} color={t.ink3} style={{ marginTop: 3 }}>
          {meta}
        </T>
      </View>
    </Pressable>
  );
}

/* ============================================================
   工具
   ============================================================ */

export function memberColor(index: number): string {
  return MEMBER_COLORS[index % MEMBER_COLORS.length];
}

export function withAlpha(hex: string, a: number): string {
  const h = hex.replace('#', '');
  return `rgba(${parseInt(h.slice(0, 2), 16)},${parseInt(h.slice(2, 4), 16)},${parseInt(h.slice(4, 6), 16)},${a})`;
}

const styles = StyleSheet.create({
  header: {
    height: 56,
    flexDirection: 'row',
    alignItems: 'center',
    paddingHorizontal: 10,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  headerBtn: { width: 36, height: 36, alignItems: 'center', justifyContent: 'center' },

  memberBar: {
    flexDirection: 'row',
    alignItems: 'center',
    marginHorizontal: 12,
    marginTop: 8,
    paddingLeft: 10,
    paddingRight: 12,
    paddingVertical: 9,
    borderRadius: radius.pill,
    borderWidth: StyleSheet.hairlineWidth,
    ...shadow.slab,
  },
  // 左侧留 2px：容器有圆角 + overflow hidden，第一个头像贴边会被裁
  memberStack: { flexDirection: 'row', alignItems: 'center', paddingLeft: 2 },

  memberBubble: {
    borderRadius: radius.bubble,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 13,
    paddingVertical: 10,
    alignSelf: 'flex-start',
    maxWidth: '100%',
    ...shadow.card,
  },
  userBubble: {
    borderRadius: radius.bubble,
    paddingHorizontal: 13,
    paddingVertical: 10,
    maxWidth: '84%',
  },

  cardMsg: {
    width: 258,
    borderRadius: radius.message,
    borderWidth: StyleSheet.hairlineWidth,
    padding: 13,
    ...shadow.card,
  },
  statusPill: {
    paddingHorizontal: 8,
    paddingVertical: 3,
    borderRadius: radius.pill,
  },
  progressTrack: { flex: 1, height: 5, borderRadius: 99, overflow: 'hidden' },

  fileCard: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 11,
    width: 250,
    borderRadius: radius.message,
    borderWidth: StyleSheet.hairlineWidth,
    padding: 11,
    ...shadow.card,
  },
  fileIcon: {
    width: 42,
    height: 42,
    borderRadius: 12,
    alignItems: 'center',
    justifyContent: 'center',
  },
});
