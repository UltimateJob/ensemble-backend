import { Ionicons } from '@expo/vector-icons';
import { useRouter } from 'expo-router';
import React, { useCallback, useEffect, useState } from 'react';
import { Pressable, RefreshControl, ScrollView, StyleSheet, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { Avatar } from '../src/components/Avatar';
import { Canvas } from '../src/components/Canvas';
import { FadeInUp } from '../src/components/motion';
import { ListSkeleton } from '../src/components/Skeleton';
import { Card, Empty, Icon, IconTile, NavBar, Row, T } from '../src/components/ui';
import { EmptyLibrary } from '../src/components/Illustration';
import { get, humanError } from '../src/lib/api';
import { useApp } from '../src/lib/store';
import type { Artifact } from '../src/lib/types';
import { useTheme } from '../src/theme/theme';
import { CARD_GAP, CARD_PAD, gap, leading, radius, type } from '../src/theme/tokens';

/** 资料库：团队产出归档。按类型给每种文件自己的色彩身份。 */
const KIND: Record<string, { icon: any; label: string; colors: [string, string] }> = {
  doc: { icon: 'document-text', label: '文档', colors: ['#7FA8F7', '#4A7FE0'] },
  code: { icon: 'code-slash', label: '代码', colors: ['#B39AF0', '#7C5CE0'] },
  sheet: { icon: 'grid', label: '表格', colors: ['#6FD8AE', '#34A87C'] },
  chart: { icon: 'stats-chart', label: '图表', colors: ['#F5C56B', '#DFA02F'] },
  note: { icon: 'create', label: '笔记', colors: ['#F5A972', '#E8734A'] },
};

export default function Library() {
  const t = useTheme();
  const router = useRouter();
  const convs = useApp((s) => s.convs);
  const [items, setItems] = useState<Artifact[]>([]);
  const [filter, setFilter] = useState('');
  const [refreshing, setRefreshing] = useState(false);
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState('');

  const load = useCallback(async () => {
    setErr('');
    try {
      const q = filter ? `?conversationId=${encodeURIComponent(filter)}` : '';
      const list = await get<Artifact[]>(`/v1/library${q}`);
      setItems(Array.isArray(list) ? list : []);
    } catch (e) {
      setErr(humanError(e));
    } finally {
      setLoading(false);
    }
  }, [filter]);

  useEffect(() => {
    load();
  }, [load]);

  return (
    <Canvas>
      <SafeAreaView style={{ flex: 1 }} edges={['top']}>
        <NavBar title="资料" subtitle="团队产出都归档在这里" onBack={() => router.back()} />
        <ScrollView
          horizontal
          showsHorizontalScrollIndicator={false}
          style={{ flexGrow: 0, flexShrink: 0, height: 48 }}
          contentContainerStyle={s.filters}
        >
          <Chip label="全部" on={filter === ''} onPress={() => setFilter('')} />
          {convs.map((c) => (
            <Chip key={c.id} label={c.title} on={filter === c.id} onPress={() => setFilter(c.id)} />
          ))}
        </ScrollView>

        <ScrollView
          contentContainerStyle={s.grid}
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
          {err ? <T size={type.sub} color={t.bad}>{err}</T> : null}
          {loading ? <ListSkeleton rows={3} /> : items.length === 0 ? (
            <Empty
              illustration={<EmptyLibrary size={120} />}
              title="还没有产出"
              hint="群里的角色产出文件时会自动归档到这里"
            />
          ) : (
            items.map((a, i) => {
              const k = KIND[a.kind] ?? KIND.doc;
              return (
                <FadeInUp key={a.id} delay={i * 35} style={s.cell}>
                  <View style={{ flex: 1 }}>
                  <Card onPress={() => router.push(`/artifact/${a.id}` as any)} style={{ flex: 1 }}>
                    <IconTile name={k.icon} colors={k.colors} size={36} iconSize={17} radius={12} />
                    {/* 标题最多两行，但不再强制 42 高——单行标题会留一片空白 */}
                    <T size={type.body} weight="600" numberOfLines={2} lh={20} style={{ marginTop: 10, minHeight: 20 }}>
                      {a.title}
                    </T>
                    <T size={type.caption} color={t.ink3} numberOfLines={1} style={{ marginTop: 3 }}>
                      {a.conversationTitle ?? k.label}
                    </T>
                    <Row style={{ justifyContent: 'space-between', marginTop: 10 }}>
                      <Row gap={5}>
                        <Avatar name={a.agentName ?? '?'} size={16} />
                        <T size={type.caption} color={t.ink3} numberOfLines={1} style={{ maxWidth: 60 }}>
                          {a.agentName ?? '系统'}
                        </T>
                      </Row>
                      <T size={type.caption} color={t.ink4}>
                        {fmt(a.createdAt)}
                      </T>
                    </Row>
                  </Card>
                  </View>
                </FadeInUp>
              );
            })
          )}
        </ScrollView>
      </SafeAreaView>
    </Canvas>
  );
}

function Chip({ label, on, onPress }: { label: string; on: boolean; onPress: () => void }) {
  const t = useTheme();
  return (
    <Pressable
      onPress={onPress}
      style={[
        s.chip,
        { backgroundColor: on ? t.accentSoft : t.card },
      ]}
    >
      <T size={type.sub} color={on ? t.accent : t.ink2} weight={on ? '700' : '400'} numberOfLines={1}>
        {label}
      </T>
    </Pressable>
  );
}

function fmt(iso: string) {
  if (!iso) return '';
  const d = new Date(iso);
  return `${d.getMonth() + 1}/${d.getDate()}`;
}

const s = StyleSheet.create({
  filters: {
    paddingHorizontal: gap.lg,
    gap: 8,
    alignItems: 'center',
    paddingVertical: 6,
  },
  chip: {
    height: 32,
    justifyContent: 'center',
    paddingHorizontal: 14,
    borderRadius: radius.pill,
    maxWidth: 170,
  },
  grid: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: gap.xs,
    paddingHorizontal: gap.lg,
    paddingTop: gap.md,
    paddingBottom: 100,
  },
  cell: { flexBasis: '48%', flexGrow: 1, maxWidth: '48.6%' },
});
