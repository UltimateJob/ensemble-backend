import React, { useEffect, useMemo, useState } from 'react';
import {
  ActivityIndicator,
  Modal,
  Pressable,
  ScrollView,
  StyleSheet,
  TextInput,
  View,
} from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { Avatar } from './Avatar';
import { Icon, Row, T } from './ui';
import { get, patch } from '../lib/api';
import type { Conversation, ConvAgent } from '../lib/types';
import { useTheme } from '../theme/theme';
import { gap, radius, type, weight } from '../theme/tokens';

/**
 * 给群里加成员。
 *
 * 两个来源合成一个列表，因为用户想的不是「从哪个系统拉」，
 * 而是「把谁加进来」：
 *   角色库 —— 内置的动漫/影视角色，直接拉进群
 *   我的角色 —— 用户自己建过的人设
 *
 * 两边的后端接口不一样（角色库是复制快照，自定义角色是整组 PATCH），
 * 这里统一成一次选人、一次确认。
 */

interface LibRole {
  name: string;
  work: string;
  tags?: string[];
  emoji?: string;
  tone?: string;
}

interface MyAgent {
  id: string;
  name: string;
  title?: string;
  emoji?: string;
  color?: string;
  prompt?: string;
  capabilities?: string[];
}

export function AddMemberSheet({
  visible,
  conv,
  onClose,
  onAdded,
  onNewAgent,
}: {
  visible: boolean;
  conv: Conversation | null;
  onClose: () => void;
  /** 加成功后把最新的会话回传，调用方直接替换本地状态。 */
  onAdded: (conv: Conversation) => void;
  /** 去创建新角色（现有库里没有想要的）。 */
  onNewAgent?: () => void;
}) {
  const t = useTheme();
  const [q, setQ] = useState('');
  const [lib, setLib] = useState<LibRole[]>([]);
  const [mine, setMine] = useState<MyAgent[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState('');
  const [err, setErr] = useState('');
  /** 模板群在存储里 Agents 是空的，真实角色要从列表接口的 resolvedAgents 拿。 */
  const [baseAgents, setBaseAgents] = useState<ConvAgent[]>([]);

  useEffect(() => {
    if (!visible || !conv?.id) return;
    setQ('');
    setErr('');
    setBusy('');
    setLoading(true);
    let alive = true;
    Promise.all([
      get<LibRole[]>('/v1/characters?q=').catch(() => [] as LibRole[]),
      get<MyAgent[]>('/v1/agents').catch(() => [] as MyAgent[]),
      get<(Conversation & { resolvedAgents?: ConvAgent[] })[]>('/v1/conversations').catch(
        () => [] as (Conversation & { resolvedAgents?: ConvAgent[] })[],
      ),
    ])
      .then(([a, b, list]) => {
        if (!alive) return;
        setLib(Array.isArray(a) ? a : []);
        setMine(Array.isArray(b) ? b : []);
        const self = (Array.isArray(list) ? list : []).find((c) => c.id === conv?.id);
        setBaseAgents(self?.resolvedAgents ?? []);
      })
      .finally(() => alive && setLoading(false));
    return () => {
      alive = false;
    };
  }, [visible, conv?.id]);

  /**
   * 已在群里的名字不再出现在候选里——点了一定失败，不如不显示。
   *
   * 基数把「模板角色」也算进去：内置模板的群，Agents 在存储里是空的，
   * 角色由 studio 在运行时解析。只按 conv.agents 判断的话，
   * 工头/查证这些已经在群里的人还会出现在候选列表里。
   */
  const existing = useMemo(() => {
    const set = new Set<string>();
    for (const a of baseAgents) set.add(a.name);
    for (const a of conv?.agents ?? []) set.add(a.name);
    for (const m of conv?.members ?? []) set.add(m);
    return set;
  }, [conv, baseAgents]);

  /**
   * PATCH 会整组替换 Agents。模板群的 conv.agents 是空的、角色靠 studio
   * 在运行时解析，所以基数必须用解析后的那份，否则加一个人就把
   * 工头/查证/撰写全删了。
   */
  const currentAgents: ConvAgent[] =
    conv?.agents?.length ? conv.agents : baseAgents;

  const kw = q.trim();
  const libShown = useMemo(
    () =>
      lib
        .filter((c) => !existing.has(c.name))
        .filter((c) => !kw || (c.name + c.work + (c.tags ?? []).join('')).includes(kw)),
    [lib, existing, kw],
  );
  const mineShown = useMemo(
    () =>
      mine
        .filter((a) => !existing.has(a.name))
        .filter((a) => !kw || (a.name + (a.title ?? '')).includes(kw)),
    [mine, existing, kw],
  );

  async function addCharacter(c: LibRole) {
    if (!conv) return;
    setBusy('lib:' + c.name);
    setErr('');
    try {
      // 复制成快照进群：角色库以后改了，不该影响已经进群的人设。
      const updated = await patchAgents(conv, [
        ...currentAgents,
        {
          key: `lib_${c.name}`,
          characterId: `lib:${c.name}`,
          name: c.name,
          title: (c.tags ?? []).join(' / ') || c.work,
          emoji: c.emoji || [...c.name][0] || '人',
          color: '',
          prompt: c.tone ?? '',
        },
      ]);
      onAdded(updated);
    } catch (e) {
      setErr(e instanceof Error ? e.message : '添加失败');
    } finally {
      setBusy('');
    }
  }

  async function addAgent(a: MyAgent) {
    if (!conv) return;
    setBusy('own:' + a.id);
    setErr('');
    try {
      const updated = await patchAgents(conv, [
        ...currentAgents,
        {
          key: 'lib_' + a.id,
          characterId: 'lib:' + a.id,
          name: a.name,
          title: a.title ?? '',
          emoji: a.emoji || [...a.name][0] || '人',
          color: a.color ?? '',
          prompt: a.prompt ?? '',
        },
      ]);
      onAdded(updated);
    } catch (e) {
      setErr(e instanceof Error ? e.message : '添加失败');
    } finally {
      setBusy('');
    }
  }

  return (
    <Modal visible={visible} animationType="slide" presentationStyle="pageSheet" onRequestClose={onClose}>
      <SafeAreaView style={{ flex: 1, backgroundColor: t.canvas[0] }} edges={['top', 'bottom']}>
        <View style={s.head}>
          <T size={type.title} weight={weight.strong}>
            添加成员
          </T>
          <Row gap={14} align="center">
            {/* 建一个新角色，而不是从现成的里面挑 */}
            <Pressable
              onPress={() => {
                onClose();
                onNewAgent?.();
              }}
              hitSlop={10}
              accessibilityRole="button"
              accessibilityLabel="新建角色"
            >
              <Row gap={4} align="center">
                <Icon name="add-circle-outline" size={17} color={t.ink2} />
                <T size={type.caption} weight={weight.medium} color={t.ink2}>
                  新建
                </T>
              </Row>
            </Pressable>
            <Pressable onPress={onClose} hitSlop={12} accessibilityLabel="关闭">
              <Icon name="close" size={24} color={t.ink3} />
            </Pressable>
          </Row>
        </View>

        <View style={[s.search, { backgroundColor: t.card, borderColor: t.line }]}>
          <Icon name="search" size={16} color={t.ink4} />
          <TextInput
            value={q}
            onChangeText={setQ}
            placeholder="搜角色或人设"
            placeholderTextColor={t.ink4}
            style={[s.searchInput, { color: t.ink }]}
          />
        </View>

        {err ? (
          <T size={type.caption} color={t.bad} style={{ paddingHorizontal: gap.md, marginBottom: 6 }}>
            {err}
          </T>
        ) : null}

        {loading ? (
          <View style={{ paddingTop: 40, alignItems: 'center' }}>
            <ActivityIndicator color={t.ink3} />
          </View>
        ) : (
          <ScrollView contentContainerStyle={{ paddingBottom: 40 }} keyboardShouldPersistTaps="handled">
            {mineShown.length ? (
              <Section label="我的角色" count={mineShown.length}>
                {mineShown.map((a) => (
                  <MemberRow
                    key={a.id}
                    name={a.name}
                    sub={a.title || '自定义人设'}
                    emoji={a.emoji || [...a.name][0]}
                    color={a.color || t.accent}
                    busy={busy === 'own:' + a.id}
                    disabled={!!busy}
                    canImage={(a.capabilities ?? []).includes('image')}
                    onPress={() => addAgent(a)}
                  />
                ))}
              </Section>
            ) : null}

            {libShown.length ? (
              <Section label="角色库" count={libShown.length}>
                {libShown.map((c) => (
                  <MemberRow
                    key={c.name}
                    name={c.name}
                    sub={c.work}
                    emoji={c.emoji || [...c.name][0]}
                    color={t.slate}
                    busy={busy === 'lib:' + c.name}
                    disabled={!!busy}
                    onPress={() => addCharacter(c)}
                  />
                ))}
              </Section>
            ) : null}

            {!mineShown.length && !libShown.length ? (
              <View style={{ paddingTop: 60, alignItems: 'center', paddingHorizontal: gap.lg }}>
                <T size={type.body} weight={weight.semibold} color={t.ink3}>
                  {kw ? '没有匹配的成员' : '没有可添加的成员'}
                </T>
                <T size={type.caption} color={t.ink4} align="center" style={{ marginTop: 6 }}>
                  {kw ? '换个关键词试试' : '该加的都加进来了'}
                </T>
              </View>
            ) : null}
          </ScrollView>
        )}
      </SafeAreaView>
    </Modal>
  );
}

async function patchAgents(conv: Conversation, agents: Conversation['agents']): Promise<Conversation> {
  return patch<Conversation>(`/v1/conversations/${conv.id}`, { agents });
}

function Section({
  label,
  count,
  children,
}: {
  label: string;
  count: number;
  children: React.ReactNode;
}) {
  const t = useTheme();
  return (
    <View style={{ marginTop: gap.md }}>
      <View style={s.sectionHead}>
        <T size={type.caption} weight={weight.semibold} color={t.ink3}>
          {label}
        </T>
        <T size={type.micro} color={t.faint} tracking={1.2}>
          {count}
        </T>
      </View>
      <View style={{ paddingHorizontal: gap.md, gap: 8 }}>{children}</View>
    </View>
  );
}

function MemberRow({
  name,
  sub,
  emoji,
  color,
  busy,
  disabled,
  canImage,
  onPress,
}: {
  name: string;
  sub: string;
  emoji: string;
  color: string;
  busy: boolean;
  disabled: boolean;
  /** 这个角色是否被授予生图能力，列表里标出来便于挑选。 */
  canImage?: boolean;
  onPress: () => void;
}) {
  const t = useTheme();
  return (
    <Pressable
      onPress={onPress}
      disabled={disabled}
      accessibilityRole="button"
      accessibilityLabel={`添加 ${name}`}
      style={({ pressed }) => [
        s.row,
        { backgroundColor: t.card, borderColor: t.line, opacity: disabled && !busy ? 0.5 : 1 },
        pressed && { opacity: 0.75 },
      ]}
    >
      <Avatar name={name} size={40} tint={color} />
      <View style={{ flex: 1, minWidth: 0 }}>
        <T size={type.card} weight={weight.strong} numberOfLines={1}>
          {name}
        </T>
        <Row gap={6} align="center" style={{ marginTop: 2 }}>
          <T size={type.micro} color={t.ink3} numberOfLines={1} style={{ flexShrink: 1 }}>
            {sub}
          </T>
          {canImage ? (
            <View style={[s.capTag, { backgroundColor: t.accentSoft }]}>
              <Icon name="image" size={9} color={t.accent} />
              <T size={9} weight={weight.semibold} color={t.accent}>
                可生图
              </T>
            </View>
          ) : null}
        </Row>
      </View>
      {busy ? (
        <ActivityIndicator color={t.ink3} />
      ) : (
        <Icon name="add-circle-outline" size={22} color={t.ink3} />
      )}
    </Pressable>
  );
}

const s = StyleSheet.create({
  head: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    paddingHorizontal: gap.md,
    paddingTop: gap.sm,
    paddingBottom: gap.sm,
  },
  search: {
    marginHorizontal: gap.md,
    height: 44,
    borderRadius: radius.sm,
    borderWidth: StyleSheet.hairlineWidth,
    flexDirection: 'row',
    alignItems: 'center',
    gap: 8,
    paddingHorizontal: 12,
    marginBottom: gap.sm,
  },
  searchInput: { flex: 1, fontSize: type.body, padding: 0 },
  sectionHead: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    paddingHorizontal: gap.md,
    marginBottom: 8,
  },
  capTag: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 3,
    paddingHorizontal: 6,
    paddingVertical: 2,
    borderRadius: 5,
  },
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 12,
    padding: 12,
    borderRadius: radius.card,
    borderWidth: StyleSheet.hairlineWidth,
  },
});
