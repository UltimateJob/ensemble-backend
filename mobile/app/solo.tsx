import { useRouter } from 'expo-router';
import React, { useEffect, useMemo, useState } from 'react';
import { ActivityIndicator, ScrollView, StyleSheet, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { Avatar } from '../src/components/Avatar';
import { Canvas } from '../src/components/Canvas';
import { FadeInUp } from '../src/components/motion';
import { Surface } from '../src/components/Surface';
import { Button, Empty, Input, NavBar, Row, T } from '../src/components/ui';
import { get, humanError, post } from '../src/lib/api';
import { useApp } from '../src/lib/store';
import type { Character, Conversation } from '../src/lib/types';
import { useTheme } from '../src/theme/theme';
import { CARD_GAP, CARD_PAD, gap, leading, type } from '../src/theme/tokens';

/**
 * 单聊：一个房间只有你和一位角色。
 *
 * 简单任务先找一个人干；复杂任务再交给团队。和后端的 Members=1 对应，
 * 编排器只会让这一位角色开口，不会把整套场景角色拉进来。
 */
export default function Solo() {
  const t = useTheme();
  const router = useRouter();
  const scenarios = useApp((s) => s.scenarios);
  const upsertConv = useApp((s) => s.upsertConv);

  const [q, setQ] = useState('');
  const [chars, setChars] = useState<Character[]>([]);
  const [busyKey, setBusyKey] = useState('');
  const [err, setErr] = useState('');
  // 首帧 char 还是空数组时不能直接显示「没找到角色」——
  // 那会在每次打开时闪一下假空态，网络失败也被伪装成「没有匹配」。
  const [loading, setLoading] = useState(true);
  const [loadErr, setLoadErr] = useState('');

  const code = scenarios.find((s) => s.key === 'code');

  useEffect(() => {
    get<Character[]>('/v1/characters?q=')
      .then((list) => setChars(Array.isArray(list) ? list : []))
      .catch((e) => setLoadErr(humanError(e)))
      .finally(() => setLoading(false));
  }, []);

  const filtered = useMemo(() => {
    const key = q.trim();
    if (!key) return chars;
    return chars.filter((c) => (c.name + c.work + (c.tags || []).join('')).includes(key));
  }, [chars, q]);

  async function startWithCode(agent: { key: string; name: string }) {
    setBusyKey(agent.key);
    setErr('');
    try {
      const conv = await post<Conversation>('/v1/conversations', {
        title: `和「${agent.name}」单聊`,
        studio: 'code',
        members: [agent.key],
        defaultPermission: 'ask',
      });
      upsertConv(conv);
      router.replace(`/team/${conv.id}`);
    } catch (e) {
      setErr(humanError(e));
    } finally {
      setBusyKey('');
    }
  }

  async function startWithCharacter(c: Character) {
    setBusyKey('lib:' + c.name);
    setErr('');
    try {
      const conv = await post<Conversation>('/v1/conversations', {
        title: `和「${c.name}」单聊`,
        studio: 'custom',
        defaultPermission: 'ask',
        agents: [{
          key: 'lib_' + c.name,
          characterId: 'lib:' + c.name,
          name: c.name,
          title: (c.tags || []).join(' / '),
          emoji: [...c.name][0] || '人',
          color: '',
          prompt: c.tone || '',
        }],
      });
      upsertConv(conv);
      router.replace(`/team/${conv.id}`);
    } catch (e) {
      setErr(humanError(e));
    } finally {
      setBusyKey('');
    }
  }

  return (
    <Canvas>
      <SafeAreaView style={{ flex: 1 }} edges={['top', 'bottom']}>
        <NavBar title="单聊" onBack={() => router.back()} />
        <ScrollView contentContainerStyle={s.page} showsVerticalScrollIndicator={false}>
          <T size={type.sub} color={t.ink3} lh={leading.sub} style={{ marginBottom: gap.lg }}>
            简单任务直接找一个人做；需要拆解、并行、互相验收时，再回团队开团队任务。
            绑定工作区之后，可以直接让它读代码、改文件、跑测试。
          </T>

          {err ? (
            <T size={type.sub} color={t.bad} style={{ marginBottom: gap.sm }}>{err}</T>
          ) : null}

          <T size={type.headline} weight="600" style={{ marginBottom: gap.sm }}>
            代码角色
          </T>
          <View style={{ gap: CARD_GAP }}>
            {(code?.agents || []).map((a, i) => (
              <FadeInUp key={a.key} delay={i * 40}>
                <Surface shell={4} outerRadius={22}>
                  <Row gap={gap.sm} style={{ padding: CARD_PAD }}>
                    <Avatar name={a.name} tint={a.color} size={40} />
                    <View style={{ flex: 1 }}>
                      <T size={type.body} weight="600">{a.name}</T>
                      <T size={type.caption} color={t.ink3} style={{ marginTop: 2 }}>{a.title}</T>
                    </View>
                    <Button
                      label={busyKey === a.key ? '创建中…' : '开始'}
                      variant="secondary"
                      size="sm"
                      disabled={!!busyKey}
                      onPress={() => startWithCode(a)}
                    />
                  </Row>
                </Surface>
              </FadeInUp>
            ))}
          </View>

          <T size={type.headline} weight="600" style={{ marginTop: gap.xl, marginBottom: gap.sm }}>
            从角色库选
          </T>
          <Input value={q} onChangeText={setQ} placeholder="搜名字、作品或性格" />
          <View style={{ gap: CARD_GAP, marginTop: gap.sm }}>
            {filtered.slice(0, 20).map((c, i) => (
              <FadeInUp key={c.name} delay={Math.min(i, 8) * 30}>
                <Surface shell={4} outerRadius={22}>
                  <Row gap={gap.sm} style={{ padding: CARD_PAD }}>
                    <Avatar name={c.name} size={40} />
                    <View style={{ flex: 1 }}>
                      <T size={type.body} weight="600">{c.name}</T>
                      <T size={type.caption} color={t.ink3} style={{ marginTop: 2 }} numberOfLines={1}>
                        {c.work}
                      </T>
                    </View>
                    <Button
                      label={busyKey === 'lib:' + c.name ? '创建中…' : '开始'}
                      variant="secondary"
                      size="sm"
                      disabled={!!busyKey}
                      onPress={() => startWithCharacter(c)}
                    />
                  </Row>
                </Surface>
              </FadeInUp>
            ))}
            {loading ? (
              <View style={{ paddingVertical: gap.xl, alignItems: 'center' }}>
                <ActivityIndicator color={t.accent} />
              </View>
            ) : loadErr ? (
              <Empty icon="cloud-offline-outline" title="角色库加载失败" hint={loadErr} />
            ) : !filtered.length ? (
              <Empty
                icon="people-outline"
                title={q.trim() ? '没找到角色' : '角色库是空的'}
                hint={q.trim() ? '换个关键词试试' : '稍后再来看看'}
              />
            ) : null}
          </View>
        </ScrollView>
      </SafeAreaView>
    </Canvas>
  );
}

const s = StyleSheet.create({
  page: { padding: gap.lg, paddingBottom: 48 },
});
