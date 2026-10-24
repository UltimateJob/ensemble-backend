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
          <T size={type.micro} color={t.ink3} numberOfLines={1} style={{ marginTop: 4 }}>
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

/** 成员条里头像的本体尺寸。发言放大只加白环，不改这个值。 */
const AVATAR_BASE = 36;

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
          />
        ))}
        {/* 超过 4 人时给个 +N：只摆 4 个头像又不说话，
            用户会以为群里就这几个人。 */}
        {members.length > 4 ? (
          <View
            style={[
              styles.memberOverflow,
              // 用墨色底 + 白字：和彩色头像同权重，读起来是「还有 N 个人」，
              // 而不是一块洗淡的灰点。
              { backgroundColor: t.ink, borderColor: t.slab },
            ]}
          >
            <T size={11} weight={weight.semibold} color={t.dark ? '#141518' : '#FFFFFF'}>
              +{members.length - 4}
            </T>
          </View>
        ) : null}
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
}: {
  member: BarMember;
  i: number;
  isSpeaker: boolean;
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

  // 布局尺寸恒定为 36，说话时只做 transform 缩放。
  // 之前是动 width/height——每次有人开口，整个成员条都会重排，
  // 而且缩放量吃掉了重叠量，导致圆与圆的间距忽大忽小。
  // 现在布局不动，只有视觉上放大 1.16 倍（36 → 41.8）。
  const scale = grow.interpolate({ inputRange: [0, 1], outputRange: [1, 1.16] });
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
        width: AVATAR_BASE,
        height: AVATAR_BASE,
        transform: [{ scale }],
        // 重叠 8px：36px 的圆露出 28px，字的左右各有约 6px 留白。
        // 之前重叠 10px 时字会贴到邻居的环上。
        marginLeft: i === 0 ? 0 : -8,
        // 左边的圆压住右边的圆（i 越小 zIndex 越高）——这是头像组的常规做法，
        // 每个圆只露出右侧一条，文字区始终完整。
        // 之前是反过来（10 - i），后一个圆压住前一个，正好压掉别人半个字。
        // 发言者仍然永远在最前。
        zIndex: isSpeaker ? 99 : 20 - i,
        borderRadius: 999,
        // 这里不填底色。容器本身是透明的，只有头像那一圈上色，
        // 重叠时才不会出现「一块白色月牙盖住邻居」的缺口。
        alignItems: 'center',
        justifyContent: 'center',
      }}
    >
      {/* 白色外环 + 内层头像：环和头像一起被裁成同心圆，
          所以从任何角度看，边缘都是完整的白色弧线。 */}
      <View
        style={{
          width: AVATAR_BASE,
          height: AVATAR_BASE,
          borderRadius: 999,
          backgroundColor: t.card,
          alignItems: 'center',
          justifyContent: 'center',
        }}
      >
        <View
          style={{
            width: AVATAR_BASE - 4,
            height: AVATAR_BASE - 4,
            borderRadius: 999,
            overflow: 'hidden',
          }}
        >
          <Avatar
            name={member.name}
            size={AVATAR_BASE - 4}
            index={member.index}
            tint={member.color}
          />
        </View>
      </View>

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
    // 两行标题（群名 + 「N 位成员」）在 56 里会挤在一起。
    // 62 给两行各留够行高，副标题离主标题也有一点呼吸。
    minHeight: 62,
    flexDirection: 'row',
    alignItems: 'center',
    paddingHorizontal: 10,
    paddingVertical: 6,
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
  memberStack: { flexDirection: 'row', alignItems: 'center', paddingLeft: 2, flexShrink: 0 },
  memberOverflow: {
    width: 36,
    height: 36,
    borderRadius: 18,
    borderWidth: 2,
    alignItems: 'center',
    justifyContent: 'center',
    // 和头像同样的重叠量，否则 +N 会比头像突出半截
    marginLeft: -8,
  },

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
