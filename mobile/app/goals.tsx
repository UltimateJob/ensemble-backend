import { useRouter } from 'expo-router';
import React, { useCallback, useEffect, useState } from 'react';
import { Pressable, RefreshControl, ScrollView, StyleSheet, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { AvatarStack } from '../src/components/Avatar';
import { Canvas } from '../src/components/Canvas';
import { FadeInUp } from '../src/components/motion';
import { ListSkeleton } from '../src/components/Skeleton';
import { Button, Card, Empty, Icon, Input, NavBar, Progress, Row, T, Tag } from '../src/components/ui';
import { Surface } from '../src/components/Surface';
import { EmptyGoals } from '../src/components/Illustration';
import { del, get, humanError, patch, post } from '../src/lib/api';
import type { Goal } from '../src/lib/types';
import { useTheme } from '../src/theme/theme';
import { CARD_GAP, CARD_PAD, gap, leading, radius, type } from '../src/theme/tokens';

/** 目标看板。进度来源必须标明（手动 / 任务派生），否则数字不可信。 */
export default function Goals() {
  const t = useTheme();
  const router = useRouter();
  const [tab, setTab] = useState<'doing' | 'todo'>('doing');
  const [goals, setGoals] = useState<Goal[]>([]);
  const [refreshing, setRefreshing] = useState(false);
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState('');
  const [adding, setAdding] = useState(false);
  const [title, setTitle] = useState('');
  const [desc, setDesc] = useState('');

  const load = useCallback(async () => {
    setErr('');
    try {
      const list = await get<Goal[]>('/v1/goals');
      setGoals(Array.isArray(list) ? list : []);
    } catch (e) {
      setErr(humanError(e));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const shown = goals.filter((g) => (tab === 'doing' ? g.status !== 'todo' : g.status === 'todo'));

  async function create() {
    if (!title.trim()) return;
    try {
      await post('/v1/goals', { title: title.trim(), description: desc.trim(), status: 'todo' });
      setTitle('');
      setDesc('');
      setAdding(false);
      load();
    } catch (e) {
      setErr(humanError(e));
    }
  }

  async function cycle(g: Goal) {
    const next = g.status === 'todo' ? 'doing' : g.status === 'doing' ? 'done' : 'todo';
    try {
      const updated = await patch<Goal>(`/v1/goals/${g.id}`, {
        status: next,
        manualProgress: next === 'done' ? 100 : -1,
      });
      setGoals((l) => l.map((x) => (x.id === g.id ? updated : x)));
    } catch (e) {
      setErr(humanError(e));
    }
  }

  return (
    <Canvas>
      <SafeAreaView style={{ flex: 1 }} edges={['top']}>
        <NavBar title="目标" subtitle={`${goals.filter((g) => g.status === 'done').length} 个已完成`} onBack={() => router.back()} />
        <ScrollView
          contentContainerStyle={s.page}
          refreshControl={
            <RefreshControl
              refreshing={refreshing}
              onRefresh={async () => {
                setRefreshing(true);
                await load();
                setRefreshing(false);
              }}
              tintColor={t.accent}
            />
          }
        >
          <Row style={{ justifyContent: 'space-between', marginBottom: gap.md }}>
            <View style={[s.seg, { backgroundColor: t.cardSunken }]}>
              {(['doing', 'todo'] as const).map((k) => (
                <Pressable key={k} onPress={() => setTab(k)} style={[s.segItem, tab === k && { backgroundColor: t.card }]}>
                  <T size={type.sub} weight={tab === k ? '700' : '400'} color={tab === k ? t.ink : t.ink3}>
                    {k === 'doing' ? '进行中' : '待规划'}
                  </T>
                </Pressable>
              ))}
            </View>
            <Button label={adding ? '取消' : '新建'} size="sm" onPress={() => setAdding((v) => !v)} />
          </Row>

          {adding ? (
            <Card style={{ marginBottom: gap.md }}>
              <View style={{ gap: gap.xs }}>
                <Input value={title} onChangeText={setTitle} placeholder="目标标题" autoFocus />
                <Input value={desc} onChangeText={setDesc} placeholder="补充说明（可选）" multiline />
                <Button label="创建" size="sm" onPress={create} disabled={!title.trim()} />
              </View>
            </Card>
          ) : null}

          {err ? <T size={type.sub} color={t.bad} style={{ marginBottom: gap.xs }}>{err}</T> : null}

          {loading ? <ListSkeleton rows={3} /> : shown.length === 0 ? (
            <Empty
              illustration={<EmptyGoals size={120} />}
              title={tab === 'doing' ? '没有进行中的目标' : '没有待规划的目标'}
              hint="目标把群聊和真实产出连起来：进度来自关联任务，不是手填的数字"
            />
          ) : (
            shown.map((g, i) => (
              <FadeInUp key={g.id} delay={i * 40}>
                <Surface shell={4} outerRadius={22} style={{ marginBottom: CARD_GAP }}>
                  <View style={{ padding: CARD_PAD }}>
                  <Row style={{ justifyContent: 'space-between', alignItems: 'flex-start' }}>
                    <T size={type.headline} weight="600" numberOfLines={2} style={{ flex: 1, marginRight: gap.xs }}>
                      {g.title}
                    </T>
                    <Tag
                      label={g.status === 'done' ? '已完成' : g.status === 'doing' ? '进行中' : '待规划'}
                      tone={g.status === 'done' ? 'done' : g.status === 'doing' ? 'working' : 'neutral'}
                    />
                  </Row>
                  {g.description ? (
                    <T size={type.sub} color={t.ink2} lh={leading.sub} numberOfLines={2} style={{ marginTop: 4 }}>
                      {g.description}
                    </T>
                  ) : null}
                  {g.assignees?.length ? (
                    <View style={{ marginTop: gap.sm }}>
                      <AvatarStack names={g.assignees} size={20} />
                    </View>
                  ) : null}
                  <View style={{ marginTop: gap.md, gap: 7 }}>
                    <Row style={{ justifyContent: 'space-between' }}>
                      <T size={type.caption} color={t.ink3}>
                        {g.progressSource === 'manual' ? '手动设置' : g.progressSource === 'runs' ? '按任务估算' : '按状态推算'}
                      </T>
                      <T size={type.caption} color={t.ink} weight="600">
                        {g.progress}%
                      </T>
                    </Row>
                    <Progress value={g.progress} tone={g.status === 'done' ? 'ok' : 'accent'} />
                  </View>
                  <Row style={{ justifyContent: 'flex-end', marginTop: gap.sm }} gap={gap.md}>
                    <Pressable onPress={() => cycle(g)} hitSlop={10}>
                      <T size={type.caption} color={t.accent} weight="600">
                        切换状态
                      </T>
                    </Pressable>
                    <Pressable
                      onPress={async () => {
                        await del(`/v1/goals/${g.id}`).catch(() => {});
                        load();
                      }}
                      hitSlop={10}
                    >
                      <T size={type.caption} color={t.ink3}>
                        删除
                      </T>
                    </Pressable>
                  </Row>
                  </View>
                </Surface>
              </FadeInUp>
            ))
          )}
        </ScrollView>
      </SafeAreaView>
    </Canvas>
  );
}

const s = StyleSheet.create({
  page: { paddingHorizontal: gap.lg, paddingTop: 10, paddingBottom: 100 },
  seg: { flexDirection: 'row', borderRadius: radius.control, padding: 3 },
  segItem: { paddingHorizontal: gap.md, paddingVertical: 7, borderRadius: radius.control - 3 },
});
