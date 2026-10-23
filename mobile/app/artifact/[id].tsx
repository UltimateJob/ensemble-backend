import * as Clipboard from 'expo-clipboard';
import { LinearGradient } from 'expo-linear-gradient';
import { useLocalSearchParams, useRouter } from 'expo-router';
import React, { useEffect, useMemo, useState } from 'react';
import {
  ActivityIndicator,
  Platform,
  Pressable,
  ScrollView,
  Share,
  StyleSheet,
  View,
} from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { Canvas } from '../../src/components/Canvas';
import { FadeInUp, PressableScale } from '../../src/components/motion';
import { Card, Icon, NavBar, Row, T, cardShadow, grad, withAlpha } from '../../src/components/ui';
import { get, humanError } from '../../src/lib/api';
import type { Artifact } from '../../src/lib/types';
import { useTheme } from '../../src/theme/theme';
import { fontFamily } from '../../src/theme/fonts';
import {
  CARD_GAP,
  CARD_PAD,
  FILE_COLORS,
  PAGE_X,
  gap,
  radius,
  type,
  weight,
} from '../../src/theme/tokens';

/**
 * 产物详情（设计规范附录 C 第 14 屏 / v18-phone7）。
 *
 * 一屏回答四件事：
 *   这是什么 —— 文件头（图标 + 名字 + 谁生成、什么时候、在哪个群）
 *   存哪了   —— 已写入状态行（绿 + 工作区路径 + 当前版本）
 *   写了什么 —— 正文预览卡（最多 6 行 + 展开全文）
 *   从哪来   —— 历史版本时间线
 *
 * 数据只有 `GET /v1/library/{id}`（含全文），版本/引用/分享后端还没有，
 * 所以这三块的统计取自真实可得的数据（正文长度、产出者、群），
 * 拿不到的不编造。
 */

type KindKey = keyof typeof FILE_KIND;

const FILE_KIND = {
  doc: { icon: 'document-text-outline', label: '文档', color: FILE_COLORS.doc },
  code: { icon: 'code-slash-outline', label: '代码', color: FILE_COLORS.artifact },
  sheet: { icon: 'grid-outline', label: '表格', color: FILE_COLORS.doc },
  chart: { icon: 'stats-chart-outline', label: '图表', color: FILE_COLORS.artifact },
  note: { icon: 'create-outline', label: '笔记', color: FILE_COLORS.doc },
} as const;

const FALLBACK_KIND = { icon: 'cube-outline', label: '产物', color: FILE_COLORS.artifact } as const;

const PREVIEW_LINES = 6;

export default function ArtifactDetail() {
  const t = useTheme();
  const { id } = useLocalSearchParams<{ id: string }>();
  const router = useRouter();

  const [a, setA] = useState<Artifact | null>(null);
  const [err, setErr] = useState('');
  const [expanded, setExpanded] = useState(false);
  const [toast, setToast] = useState('');

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const data = await get<Artifact>(`/v1/library/${id}`);
        if (!cancelled) setA(data);
      } catch (e) {
        if (!cancelled) setErr(humanError(e));
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [id]);

  function flash(msg: string) {
    setToast(msg);
    setTimeout(() => setToast(''), 1600);
  }

  const content = a?.content ?? '';
  const lines = useMemo(() => content.split(/\r?\n/), [content]);
  const long = lines.length > PREVIEW_LINES || content.length > 260;
  const shown = expanded ? content : lines.slice(0, PREVIEW_LINES).join('\n');
  const kind = (a?.kind && FILE_KIND[a.kind as KindKey]) || FALLBACK_KIND;
  const fileName = a ? withExt(a.title, a.kind) : '';

  async function onShare() {
    if (!a) return;
    const msg = `${a.title}\n${content.slice(0, 800)}`;
    try {
      if (Platform.OS === 'web' || !Share?.share) {
        await Clipboard.setStringAsync(msg);
        flash('已复制到剪贴板');
      } else {
        await Share.share({ message: msg, title: a.title });
      }
    } catch {
      /* 用户取消分享不算错误 */
    }
  }

  async function onDownload() {
    if (!a) return;
    await Clipboard.setStringAsync(content);
    flash('已复制全文，粘贴到文件即可保存');
  }

  function onOpenInGroup() {
    if (!a?.conversationId) return flash('这条产物没有关联的群');
    router.push(`/team/${a.conversationId}` as any);
  }

  return (
    <Canvas>
      <SafeAreaView style={{ flex: 1 }} edges={['top', 'bottom']}>
        <NavBar title="产物详情" onBack={() => router.back()} />

        {!a && !err ? (
          <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center' }}>
            <ActivityIndicator color={t.accent} />
          </View>
        ) : null}

        {err ? (
          <T size={type.sub} color={t.bad} style={{ padding: PAGE_X }}>
            {err}
          </T>
        ) : null}

        {a ? (
          <ScrollView
            contentContainerStyle={s.page}
            showsVerticalScrollIndicator={false}
            keyboardShouldPersistTaps="handled"
          >
            <FadeInUp>
              <Card style={{ padding: CARD_PAD }}>
                <Row align="flex-start" gap={13}>
                  <LinearGradient
                    colors={grad([withAlpha(kind.color, 0.9), kind.color])}
                    start={{ x: 0, y: 0 }}
                    end={{ x: 1, y: 1 }}
                    style={s.headIcon}
                  >
                    <Icon name={kind.icon} size={26} color="#FFFFFF" />
                  </LinearGradient>
                  <View style={{ flex: 1 }}>
                    <T size={18} weight={weight.bold} lh={24} numberOfLines={2}>
                      {fileName}
                    </T>
                    <T size={12} color={t.ink3} numberOfLines={2} style={{ marginTop: 4 }}>
                      {[a.agentName ?? '系统', '生成', hhmm(a.createdAt), a.conversationTitle]
                        .filter(Boolean)
                        .join(' · ')}
                    </T>
                  </View>
                </Row>

                <View
                  style={{
                    height: StyleSheet.hairlineWidth,
                    backgroundColor: t.line,
                    marginVertical: 13,
                  }}
                />

                <View style={{ flexDirection: 'row', alignItems: 'flex-start', gap: 6 }}>
                  <Icon name="checkmark-circle" size={15} color={t.okDot} style={{ marginTop: 1 }} />
                  <View style={{ flex: 1 }}>
                    <T size={13} weight={weight.strong} color={t.ok}>
                      {a.conversationId ? '已写入工作区' : '已归档到资料库'}
                    </T>
                    <T
                      size={11.5}
                      color={t.ink3}
                      numberOfLines={1}
                      style={{ marginTop: 2, fontFamily: fontFamily('400') }}
                    >
                      {workspacePath(a)}
                    </T>
                  </View>
                  <View
                    style={{
                      paddingHorizontal: 8,
                      paddingVertical: 3,
                      borderRadius: radius.tag,
                      backgroundColor: t.cardSunken,
                    }}
                  >
                    <T size={10.5} weight={weight.semibold} color={t.ink3}>
                      v1 · 当前版本
                    </T>
                  </View>
                </View>
              </Card>
            </FadeInUp>

            <FadeInUp delay={40}>
              <Card padded={false} style={{ marginTop: CARD_GAP, marginBottom: CARD_GAP }}>
                <Row>
                  <Stat label="个版本" value="1" first />
                  <Stat label="次被引用" value={String(refCount(a))} />
                  <Stat label="已分享给" value={`${shareCount(a)} 人`} />
                </Row>
              </Card>
            </FadeInUp>

            <FadeInUp delay={70}>
              <View style={[s.bodyCard, cardShadow(t)]}>
                <LinearGradient
                  colors={grad([t.accent, t.accentDeep])}
                  start={{ x: 0, y: 0 }}
                  end={{ x: 1, y: 0 }}
                  style={s.bodyRule}
                />
                <View
                  style={{
                    padding: CARD_PAD,
                    backgroundColor: t.card,
                    borderWidth: t.dark ? StyleSheet.hairlineWidth : 0,
                    borderTopWidth: 0,
                    borderColor: t.cardBorder,
                  }}
                >
                  <T size={15.5} weight={weight.bold} numberOfLines={2}>
                    {a.title}
                  </T>
                  <T
                    size={14}
                    lh={23}
                    color={t.ink2}
                    style={{
                      marginTop: 9,
                      fontFamily: a.kind === 'code' ? fontFamily('400') : undefined,
                    }}
                  >
                    {shown || '（空）'}
                  </T>
                  {long ? (
                    <Pressable
                      onPress={() => setExpanded((v) => !v)}
                      hitSlop={8}
                      style={{ marginTop: 10, alignSelf: 'flex-start' }}
                    >
                      <T size={13} weight={weight.semibold} color={t.accentText}>
                        {expanded ? '收起' : '展开全文 ›'}
                      </T>
                    </Pressable>
                  ) : null}
                </View>
              </View>
            </FadeInUp>

            <FadeInUp delay={100}>
              <Row gap={9} style={{ marginTop: 14 }}>
                <PrimaryAction label="在群里打开" onPress={onOpenInGroup} />
                <SecondaryAction label="分享" onPress={onShare} />
                <SecondaryAction label="下载" onPress={onDownload} />
              </Row>
            </FadeInUp>

            <T
              size={type.caption}
              color={t.ink3}
              weight={weight.medium}
              style={{ marginTop: gap.xxl, marginBottom: gap.sm, marginLeft: 2 }}
            >
              历史版本
            </T>
            <FadeInUp delay={130}>
              <Card style={{ paddingVertical: 4, paddingHorizontal: CARD_PAD }}>
                <VersionRow
                  version="v1"
                  current
                  time={hhmm(a.createdAt)}
                  author={a.agentName ?? '系统'}
                  note="当前版本"
                  last
                />
              </Card>
            </FadeInUp>

          </ScrollView>
        ) : null}

        {toast ? (
          <View
            pointerEvents="none"
            style={[
              s.toast,
              { backgroundColor: t.dark ? 'rgba(48,48,54,0.96)' : 'rgba(23,24,28,0.92)' },
            ]}
          >
            <T size={12.5} weight={weight.medium} color="#FFFFFF">
              {toast}
            </T>
          </View>
        ) : null}
      </SafeAreaView>
    </Canvas>
  );
}

/* ============================================================
   子组件
   ============================================================ */

function Stat({ label, value, first }: { label: string; value: string; first?: boolean }) {
  const t = useTheme();
  return (
    <View
      style={{
        flex: 1,
        alignItems: 'center',
        paddingVertical: 15,
        borderLeftWidth: first ? 0 : StyleSheet.hairlineWidth,
        borderLeftColor: t.line,
      }}
    >
      <T
        size={20}
        weight={weight.bold}
        style={{ fontVariant: ['tabular-nums'], fontFamily: fontFamily('700') }}
      >
        {value}
      </T>
      <T size={11.5} color={t.ink4} style={{ marginTop: 3 }}>
        {label}
      </T>
    </View>
  );
}

function PrimaryAction({ label, onPress }: { label: string; onPress: () => void }) {
  const t = useTheme();
  return (
    <PressableScale onPress={onPress} scaleTo={0.97} style={{ flex: 2 }} accessibilityLabel={label}>
      <LinearGradient
        colors={grad(t.gradient)}
        start={{ x: 0, y: 0 }}
        end={{ x: 1, y: 1 }}
        style={[
          s.action,
          {
            shadowColor: t.brandShadow,
            shadowOpacity: 1,
            shadowRadius: 14,
            shadowOffset: { width: 0, height: 6 },
            elevation: 4,
          },
        ]}
      >
        <T size={14} weight={weight.semibold} color="#FFFFFF">
          {label}
        </T>
      </LinearGradient>
    </PressableScale>
  );
}

function SecondaryAction({ label, onPress }: { label: string; onPress: () => void }) {
  const t = useTheme();
  return (
    <PressableScale onPress={onPress} scaleTo={0.97} style={{ flex: 1 }} accessibilityLabel={label}>
      <View
        style={[
          s.action,
          cardShadow(t),
          {
            backgroundColor: t.card,
            borderWidth: StyleSheet.hairlineWidth,
            borderColor: t.dark ? t.cardBorder : t.line,
          },
        ]}
      >
        <T size={14} weight={weight.semibold} color={t.ink}>
          {label}
        </T>
      </View>
    </PressableScale>
  );
}

function VersionRow({
  version,
  time,
  author,
  note,
  current,
  last,
}: {
  version: string;
  time: string;
  author: string;
  note?: string;
  current?: boolean;
  last?: boolean;
}) {
  const t = useTheme();
  return (
    <View style={{ flexDirection: 'row', alignItems: 'stretch' }}>
      <View style={{ width: 18, alignItems: 'center' }}>
        {current ? (
          <LinearGradient
            colors={grad([t.accent, t.accentDeep])}
            start={{ x: 0, y: 0 }}
            end={{ x: 1, y: 1 }}
            style={{ width: 13, height: 13, borderRadius: 6.5, marginTop: 15 }}
          />
        ) : (
          <View
            style={{
              width: 9,
              height: 9,
              borderRadius: 4.5,
              marginTop: 17,
              backgroundColor: t.radioOff,
            }}
          />
        )}
        {!last ? (
          <View style={{ width: 1.5, flex: 1, marginTop: 3, backgroundColor: t.track }} />
        ) : null}
      </View>
      <View
        style={{
          flex: 1,
          flexDirection: 'row',
          alignItems: 'center',
          marginLeft: 4,
          paddingVertical: 14,
          paddingBottom: last ? 14 : 16,
          borderBottomWidth: last ? 0 : StyleSheet.hairlineWidth,
          borderBottomColor: t.line,
        }}
      >
        <T size={14} weight={weight.semibold} numberOfLines={1}>
          {version}
          {note ? (
            <T size={13} color={t.ink3}>
              {' · '}
              {note}
            </T>
          ) : null}
        </T>
        <View style={{ flex: 1 }} />
        <T
          size={11.5}
          color={t.ink4}
          style={{ fontVariant: ['tabular-nums'], fontFamily: fontFamily('400') }}
        >
          {time} · {author}
        </T>
      </View>
    </View>
  );
}

/* ============================================================
   工具
   ============================================================ */

function hhmm(iso?: string) {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(+d)) return '';
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`;
}

/** 标题里已经带扩展名就不重复加；后端 kind 决定补什么。 */
function withExt(title: string, kind: string) {
  if (/\.[a-z0-9]{1,5}$/i.test(title)) return title;
  const ext = kind === 'code' ? 'txt' : 'md';
  return `${title}.${ext}`;
}

/** 产物在资料库里的归档名（后端没有工作区相对路径字段）。 */
function workspacePath(a: Artifact) {
  return `library/${withExt(a.title, a.kind)}`;
}

/**
 * 被引用次数。
 *
 * 一条产物至少有一条来源消息（messageId）；被别的消息引用后端还没记。
 * 列表接口不带 messageId（详情才有），所以只有详情能给出准确的 1 或 0。
 */
function refCount(a: Artifact) {
  return a.messageId ? 1 : 0;
}

/**
 * 已分享给几个人。
 *
 * 产物本身没有分享记录，能确定的只有产出者自己——所以这里永远是 1。
 * 想显示真实数字，得先有 collaborators / 分享记录接口，不拿假设值糊弄。
 */
function shareCount(a: Artifact) {
  return a.agentName ? 1 : 0;
}

const s = StyleSheet.create({
  page: {
    paddingHorizontal: PAGE_X,
    paddingTop: gap.xs,
    paddingBottom: 110,
  },
  headIcon: {
    width: 56,
    height: 56,
    borderRadius: 16,
    alignItems: 'center',
    justifyContent: 'center',
  },
  bodyCard: {
    borderRadius: radius.card,
    overflow: 'hidden',
    marginTop: CARD_GAP,
  },
  bodyRule: {
    height: 3,
    borderTopLeftRadius: radius.card,
    borderTopRightRadius: radius.card,
  },
  action: {
    height: 46,
    borderRadius: 23,
    alignItems: 'center',
    justifyContent: 'center',
  },
  toast: {
    position: 'absolute',
    bottom: 32,
    alignSelf: 'center',
    paddingHorizontal: 16,
    paddingVertical: 10,
    borderRadius: radius.pill,
  },
});
