import { LinearGradient } from 'expo-linear-gradient';
import { useLocalSearchParams, useRouter } from 'expo-router';
import React, { useEffect, useRef, useState } from 'react';
import {
  ActivityIndicator,
  KeyboardAvoidingView,
  Platform,
  Pressable,
  ScrollView,
  StyleSheet,
  TextInput,
  View,
} from 'react-native';
import { SafeAreaView, useSafeAreaInsets } from 'react-native-safe-area-context';
import { Avatar, AvatarStack } from '../../src/components/Avatar';
import { Canvas } from '../../src/components/Canvas';
import { FadeInUp, PressableScale } from '../../src/components/motion';
import {
  Icon,
  NavBar,
  RadioDot,
  Row,
  T,
  Tag,
  cardShadow,
  grad,
} from '../../src/components/ui';
import { humanError, post } from '../../src/lib/api';
import { useApp } from '../../src/lib/store';
import type { Conversation, Scenario } from '../../src/lib/types';
import { useTheme, type Theme } from '../../src/theme/theme';
import {
  CARD_GAP,
  CARD_PAD,
  PAGE_X,
  gap,
  leading,
  radius,
  type,
} from '../../src/theme/tokens';

/**
 * 新建团队（附录 C 第 11 屏）。
 *
 * 三块，顺序就是用户在想的顺序：
 *   叫什么 → 用什么模板 → 给多高的权限。
 *
 * 权限在建队时就要定级，而不是建完再补：团队从第一秒开始干活，
 * 第一秒就需要知道「要不要先问过我」。
 */
export default function NewTeam() {
  const t = useTheme();
  const router = useRouter();
  const insets = useSafeAreaInsets();
  const params = useLocalSearchParams<{ scenario?: string }>();
  const scenarios = useApp((s) => s.scenarios);
  const upsertConv = useApp((s) => s.upsertConv);

  const [title, setTitle] = useState('');
  const [studio, setStudio] = useState(params.scenario ?? '');
  const [perm, setPerm] = useState<PermKey>('approval');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');

  // 两列模板卡：排除开群才随机挑角色的场景（它们没有可预览的成员）
  const templates = scenarios.filter((sc) => (sc.agents?.length ?? 0) > 0);
  const picked = templates.find((sc) => sc.key === studio);
  const members = picked?.agents ?? [];

  // 模板到达后默认选第一个，并把名字带进输入框
  useEffect(() => {
    if (studio || templates.length === 0) return;
    const first = templates.find((sc) => sc.key === params.scenario) ?? templates[0];
    setStudio(first.key);
    setTitle((v) => v || first.name);
    // templates 每次渲染都是新数组，不能进依赖；只跟数据长度与参数走
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [templates.length, params.scenario]);

  function pick(sc: Scenario) {
    setStudio(sc.key);
    setErr('');
    setTitle((v) => (v.trim() && v !== picked?.name ? v : sc.name));
  }

  async function create() {
    if (!title.trim()) return setErr('给团队起个名字');
    setBusy(true);
    setErr('');
    try {
      const conv = await post<Conversation>('/v1/conversations', {
        title: title.trim(),
        studio: studio || 'companion',
        defaultPermission: perm,
      });
      upsertConv(conv);
      router.replace(`/team/${conv.id}`);
    } catch (e) {
      setErr(humanError(e));
    } finally {
      setBusy(false);
    }
  }

  const footerPad = Math.max(insets.bottom, gap.md);

  return (
    <Canvas>
      <SafeAreaView style={{ flex: 1 }} edges={['top']}>
        <NavBar title="新建团队" onBack={() => router.back()} />
        <KeyboardAvoidingView
          style={{ flex: 1 }}
          behavior={Platform.OS === 'ios' ? 'padding' : undefined}
        >
          <ScrollView
            contentContainerStyle={[s.page, { paddingBottom: 108 + footerPad }]}
            showsVerticalScrollIndicator={false}
            keyboardShouldPersistTaps="handled"
          >
            {/* 名称 */}
            <SectionLabel>团队名称</SectionLabel>
            <View style={[s.nameCard, { backgroundColor: t.card }, cardShadow(t)]}>
              <TextInput
                value={title}
                onChangeText={(v) => {
                  setTitle(v);
                  if (err) setErr('');
                }}
                placeholder="比如「园区改造投标组」"
                placeholderTextColor={t.ink4}
                returnKeyType="done"
                style={[s.nameInput, { color: t.ink }]}
              />
            </View>

            {/* 模板 */}
            <SectionLabel style={{ marginTop: gap.xxl }}>选择模板</SectionLabel>
            <View style={s.grid}>
              {templates.map((sc, i) => (
                <FadeInUp key={sc.key} delay={i * 30} style={s.gridItem}>
                  <TemplateCard sc={sc} selected={studio === sc.key} onPress={() => pick(sc)} />
                </FadeInUp>
              ))}
            </View>
            {templates.length === 0 ? (
              <T size={type.caption} color={t.ink4} style={{ marginTop: gap.xs }}>
                正在读取模板…
              </T>
            ) : null}

            {/* 模板成员 */}
            <SectionLabel style={{ marginTop: gap.xxl }}>
              模板成员（创建后可增减）
            </SectionLabel>
            <View style={[s.listCard, { backgroundColor: t.card }, cardShadow(t)]}>
              {members.length === 0 ? (
                <View style={{ padding: CARD_PAD }}>
                  <T size={type.sub} color={t.ink4}>
                    选一个模板，成员会出现在这里。
                  </T>
                </View>
              ) : (
                members.map((a, i) => (
                  <View key={a.key}>
                    <View style={s.memberRow}>
                      <Avatar name={a.name} size={38} index={i} tint={a.color} />
                      <T size={type.body} weight="600" numberOfLines={1}>
                        {a.name}
                      </T>
                      <T
                        size={type.caption}
                        color={t.ink3}
                        numberOfLines={1}
                        style={{ flex: 1, textAlign: 'right' }}
                      >
                        {a.title}
                      </T>
                    </View>
                    {i < members.length - 1 ? (
                      <View style={[s.hair, { backgroundColor: t.hairline }]} />
                    ) : null}
                  </View>
                ))
              )}
            </View>

            {/* 默认权限 */}
            <SectionLabel style={{ marginTop: gap.xxl }}>默认权限级别</SectionLabel>
            <View style={[s.permCard, { backgroundColor: t.card }, cardShadow(t)]}>
              {LEVELS.map((l) => {
                const on = perm === l.key;
                return (
                  <Pressable
                    key={l.key}
                    onPress={() => setPerm(l.key)}
                    accessibilityRole="radio"
                    accessibilityState={{ selected: on }}
                    style={[
                      s.permRow,
                      on && { backgroundColor: t.cardSunken },
                    ]}
                  >
                    <RadioDot on={on} />
                    <View style={{ flex: 1 }}>
                      <Row gap={6}>
                        <T size={type.body} weight={on ? '600' : '500'}>
                          {l.label}
                        </T>
                        {l.warn ? <Tag label="谨慎授予" tone="gold" /> : null}
                      </Row>
                      <T
                        size={type.caption}
                        color={on ? t.ink2 : t.ink3}
                        lh={leading.caption}
                        style={{ marginTop: 2 }}
                      >
                        {l.hint}
                      </T>
                    </View>
                  </Pressable>
                );
              })}
            </View>

            <T size={type.micro} color={t.ink4} lh={16} style={{ marginTop: gap.sm }}>
              建队后仍可在这里和每位成员的详情页里改级别。
            </T>

            {err ? (
              <Row gap={5} align="flex-start" style={{ marginTop: gap.md }}>
                <Icon name="alert-circle-outline" size={13} color={t.bad} style={{ marginTop: 2 }} />
                <T size={type.caption} color={t.bad} lh={leading.caption} style={{ flex: 1 }}>
                  {err}
                </T>
              </Row>
            ) : null}
          </ScrollView>

          {/* 底部固定主按钮 */}
          <View
            style={[
              s.footer,
              {
                paddingBottom: footerPad,
                // 与画布同底：滚动内容从按钮底下「消失」，而不是被一块异色挡住
                backgroundColor: t.canvas[1],
                borderTopColor: t.line,
              },
            ]}
          >
            <PressableScale
              onPress={create}
              disabled={busy || !title.trim()}
              scaleTo={0.97}
              accessibilityRole="button"
              accessibilityLabel="创建团队"
            >
              <LinearGradient
                colors={grad(t.primaryGrad)}
                start={{ x: 0, y: 0 }}
                end={{ x: 1, y: 1 }}
                style={[s.createBtn, { opacity: busy || !title.trim() ? 0.4 : 1 }, primaryShadow(t)]}
              >
                {busy ? (
                  <ActivityIndicator size="small" color={t.primaryInk} />
                ) : (
                  <T size={15.5} weight="650" color={t.primaryInk}>
                    创建团队
                  </T>
                )}
              </LinearGradient>
            </PressableScale>
          </View>
        </KeyboardAvoidingView>
      </SafeAreaView>
    </Canvas>
  );
}

/* ---------------- 权限三级（规范 §7） ---------------- */

type PermKey = 'ask' | 'approval' | 'full';

const LEVELS: { key: PermKey; label: string; hint: string; warn?: boolean }[] = [
  { key: 'ask', label: '需要审批', hint: '任何操作都先问过你' },
  { key: 'approval', label: '工作区内修改', hint: '区内自由读写，区外与高危仍询问' },
  { key: 'full', label: '完全访问权限', hint: '全部操作不再询问', warn: true },
];

/* ---------------- 模板卡 ---------------- */

function TemplateCard({
  sc,
  selected,
  onPress,
}: {
  sc: Scenario;
  selected: boolean;
  onPress: () => void;
}) {
  const t = useTheme();
  const names = (sc.agents ?? []).map((a) => a.name);
  const tints = (sc.agents ?? []).map((a) => a.color);
  const line = names.slice(0, 4).join(' · ');

  return (
    <PressableScale onPress={onPress} scaleTo={0.98} accessibilityRole="radio" accessibilityState={{ selected }}>
      <View
        style={[
          s.tplCard,
          {
            backgroundColor: t.card,
            // 未选也占 2px，选中时不会因为描边出现而抖动
            borderColor: selected ? t.ink : 'transparent',
            borderWidth: 2,
          },
          cardShadow(t),
        ]}
      >
        <View style={{ overflow: 'hidden' }}>
          <T size={15} weight="650" numberOfLines={1}>
            {sc.name}
          </T>
          <View style={{ marginTop: gap.sm, height: 24 }}>
            <AvatarStack names={names} tints={tints} size={24} max={4} />
          </View>
          {/* 固定两行高度：有的模板成员名短、一行就够，有的要折两行。
              不固定的话同一排两张卡会一高一低。 */}
          <T
            size={type.micro}
            color={t.ink3}
            numberOfLines={2}
            lh={16}
            style={{ marginTop: gap.xs, height: 32 }}
          >
            {line}
          </T>
        </View>
        {selected ? (
          <View style={[s.check, { backgroundColor: t.ink }]}>
            <Icon name="checkmark" size={12} color={t.dark ? t.primaryInk : t.white} />
          </View>
        ) : null}
      </View>
    </PressableScale>
  );
}

/* ---------------- 小件 ---------------- */

function SectionLabel({ children, style }: { children: React.ReactNode; style?: object }) {
  const t = useTheme();
  return (
    <T size={type.caption} color={t.ink3} weight="600" style={[{ marginBottom: gap.xs }, style]}>
      {children}
    </T>
  );
}

/** 主按钮投影（规范 §5.1）：中性灰黑，不带彩色光晕。 */
function primaryShadow(t: Theme) {
  return {
    shadowColor: t.shadowColor,
    shadowOpacity: t.dark ? 0.45 : 0.22,
    shadowRadius: 22,
    shadowOffset: { width: 0, height: 10 },
    elevation: 6,
  };
}

const s = StyleSheet.create({
  page: { paddingHorizontal: PAGE_X, paddingTop: gap.xs },
  nameCard: { borderRadius: radius.card, paddingHorizontal: gap.lg },
  nameInput: { height: 62, fontSize: 17.5, fontWeight: '600' },
  grid: { flexDirection: 'row', flexWrap: 'wrap', gap: CARD_GAP },
  gridItem: { width: '48%', flexGrow: 1 },
  tplCard: {
    borderRadius: 20,
    // 选中时有 2px 描边。内边距要比描边多留一些，
    // 否则文字会紧贴黑框，看起来像是被框住了。
    paddingHorizontal: 16,
    paddingVertical: 15,
    minHeight: 126,
    justifyContent: 'space-between',
  },
  check: {
    position: 'absolute',
    top: 9,
    right: 9,
    width: 20,
    height: 20,
    borderRadius: 10,
    alignItems: 'center',
    justifyContent: 'center',
  },
  listCard: { borderRadius: radius.card, paddingHorizontal: CARD_PAD, paddingVertical: gap.xxs },
  memberRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: gap.md,
    paddingVertical: 12,
  },
  hair: { height: StyleSheet.hairlineWidth },
  permCard: { borderRadius: radius.card, padding: gap.xs },
  permRow: {
    flexDirection: 'row',
    alignItems: 'flex-start',
    gap: gap.md,
    paddingVertical: 12,
    paddingHorizontal: 11,
    borderRadius: 14,
  },
  footer: {
    position: 'absolute',
    left: 0,
    right: 0,
    bottom: 0,
    borderTopWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: PAGE_X,
    paddingTop: gap.md,
  },
  createBtn: {
    height: 54,
    borderRadius: radius.pill,
    alignItems: 'center',
    justifyContent: 'center',
  },
});
