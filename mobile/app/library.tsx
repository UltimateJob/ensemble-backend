import { Ionicons } from '@expo/vector-icons';
import { LinearGradient } from 'expo-linear-gradient';
import { useRouter } from 'expo-router';
import React, { useCallback, useEffect, useMemo, useState } from 'react';
import {
  Alert,
  Pressable,
  RefreshControl,
  ScrollView,
  StyleSheet,
  TextInput,
  View,
} from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { Canvas } from '../src/components/Canvas';
import { FadeInUp, PressableScale } from '../src/components/motion';
import { ListSkeleton } from '../src/components/Skeleton';
import { EmptyLibrary } from '../src/components/Illustration';
import {
  Card,
  Empty,
  Icon,
  NavBar,
  Progress,
  Row,
  T,
  cardShadow,
  grad,
  withAlpha,
} from '../src/components/ui';
import { del, get, humanError } from '../src/lib/api';
import type { Artifact } from '../src/lib/types';
import { useTheme } from '../src/theme/theme';
import { fontFamily } from '../src/theme/fonts';
import {
  CARD_GAP,
  CARD_PAD,
  FILE_COLORS,
  PAGE_X,
  gap,
  radius,
  type,
  weight,
} from '../src/theme/tokens';

/**
 * 资料库（设计规范附录 C 第 13 屏 / v18-phone6）。
 *
 * 三层信息密度，从上到下逐渐收紧：
 *   横向预览卡 —— 最近三份产出，给"我记得它长什么样"的人看
 *   筛选 chips  —— 按类型收窄
 *   文件行      —— 完整清单，来源团队 / 产出者 / 时间一行说清
 *
 * 颜色纪律：只有预览卡和图标砖带低饱和身份色，其余全是黑白灰。
 */

const KIND: Record<
  string,
  { icon: React.ComponentProps<typeof Ionicons>['name']; label: string; color: string }
> = {
  doc: { icon: 'document-text-outline', label: '文档', color: FILE_COLORS.doc },
  image: { icon: 'image-outline', label: '图片', color: FILE_COLORS.image },
  code: { icon: 'code-slash-outline', label: '代码', color: FILE_COLORS.artifact },
  sheet: { icon: 'grid-outline', label: '表格', color: FILE_COLORS.doc },
  chart: { icon: 'stats-chart-outline', label: '图表', color: FILE_COLORS.artifact },
  note: { icon: 'create-outline', label: '笔记', color: FILE_COLORS.doc },
  artifact: { icon: 'cube-outline', label: '产物', color: FILE_COLORS.artifact },
};

const FALLBACK = KIND.artifact;

type FilterKey = 'all' | 'doc' | 'image' | 'artifact';

const FILTERS: { key: FilterKey; label: string }[] = [
  { key: 'all', label: '全部' },
  { key: 'doc', label: '文档' },
  { key: 'image', label: '图片' },
  { key: 'artifact', label: '产物' },
];

/** 每个产物只归一个桶，避免同一份文件在「文档」和「产物」里各出现一次。 */
function bucketOf(kind: string): Exclude<FilterKey, 'all'> {
  if (kind === 'doc' || kind === 'note') return 'doc';
  if (kind === 'image' || kind === 'img' || kind === 'photo') return 'image';
  return 'artifact';
}

/** 后端暂未暴露磁盘用量，先按设计稿值展示；接口就绪后替换这两个常量。 */
const STORAGE_USED_GB = 1.2;
const STORAGE_TOTAL_GB = 10;

export default function Library() {
  const t = useTheme();
  const router = useRouter();

  const [items, setItems] = useState<Artifact[]>([]);
  const [query, setQuery] = useState('');
  const [filter, setFilter] = useState<FilterKey>('all');
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [err, setErr] = useState('');

  const load = useCallback(async () => {
    setErr('');
    try {
      const list = await get<Artifact[]>('/v1/library');
      setItems(Array.isArray(list) ? list : []);
    } catch (e) {
      setErr(humanError(e));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  /** 删除一份产出。后端有 DELETE /v1/library/{id}，之前前端没有入口。 */
  function removeItem(a: Artifact) {
    Alert.alert('删除产出', `「${a.title}」会从资料库移除，群聊里的记录不受影响。`, [
      { text: '取消', style: 'cancel' },
      {
        text: '删除',
        style: 'destructive',
        onPress: async () => {
          try {
            await del(`/v1/library/${a.id}`);
            setItems((l) => l.filter((x) => x.id !== a.id));
          } catch (e) {
            setErr(humanError(e));
          }
        },
      },
    ]);
  }

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return items.filter((a) => {
      if (filter !== 'all' && bucketOf(a.kind) !== filter) return false;
      if (!q) return true;
      return [a.title, a.summary, a.conversationTitle, a.agentName]
        .filter(Boolean)
        .some((v) => String(v).toLowerCase().includes(q));
    });
  }, [items, query, filter]);

  const recent = items.slice(0, 3);

  return (
    <Canvas>
      <SafeAreaView style={{ flex: 1 }} edges={['top']}>
        <NavBar title="资料库" onBack={() => router.back()} />
        <ScrollView
          contentContainerStyle={s.page}
          keyboardShouldPersistTaps="handled"
          showsVerticalScrollIndicator={false}
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
          <SearchBoard
            value={query}
            onChange={setQuery}
            count={items.length}
          />

          {loading ? (
            <ListSkeleton rows={4} />
          ) : items.length === 0 ? (
            <Empty
              illustration={<EmptyLibrary size={120} />}
              title="还没有产出"
              hint="群里的成员产出文件时会自动归档到这里"
            />
          ) : (
            <>
              {recent.length ? (
                <ScrollView
                  horizontal
                  showsHorizontalScrollIndicator={false}
                  style={s.previewScroll}
                  contentContainerStyle={s.previewRow}
                >
                  {recent.map((a) => (
                    <PreviewCard
                      key={a.id}
                      item={a}
                      onPress={() => router.push(`/artifact/${a.id}` as any)}
                    />
                  ))}
                </ScrollView>
              ) : null}

              <ScrollView
                horizontal
                showsHorizontalScrollIndicator={false}
                style={s.chipScroll}
                contentContainerStyle={s.chipRow}
              >
                {FILTERS.map((f) => (
                  <FilterChip
                    key={f.key}
                    label={f.label}
                    on={filter === f.key}
                    onPress={() => setFilter(f.key)}
                  />
                ))}
                <T size={type.caption} color={t.ink4} style={{ alignSelf: 'center', marginLeft: 2 }}>
                  {filtered.length} 项
                </T>
              </ScrollView>

              {err ? (
                <T size={type.caption} color={t.bad} style={{ marginHorizontal: 4, marginBottom: gap.sm }}>
                  {err}
                </T>
              ) : null}

              {/* 长按删除没有任何视觉线索，不写一句用户永远发现不了 */}
              {filtered.length > 0 ? (
                <T
                  size={type.micro}
                  color={t.ink4}
                  style={{ marginHorizontal: 4, marginBottom: gap.sm }}
                >
                  长按任意一项可删除
                </T>
              ) : null}

              {filtered.length === 0 ? (
                <View style={{ paddingVertical: gap.xxxl, alignItems: 'center' }}>
                  <T size={type.sub} color={t.ink3}>
                    没有匹配的文件
                  </T>
                  <Pressable
                    onPress={() => {
                      setQuery('');
                      setFilter('all');
                    }}
                    hitSlop={8}
                    style={{ marginTop: gap.sm }}
                  >
                    <T size={type.caption} color={t.accent} weight={weight.semibold}>
                      清除筛选
                    </T>
                  </Pressable>
                </View>
              ) : (
                <Card padded={false} style={{ marginTop: 2, marginBottom: CARD_GAP }}>
                  {filtered.map((a, i) => (
                    <FadeInUp key={a.id} delay={Math.min(i, 6) * 30}>
                      <FileRow
                        item={a}
                        last={i === filtered.length - 1}
                        onPress={() => router.push(`/artifact/${a.id}` as any)}
                        onDelete={() => removeItem(a)}
                      />
                    </FadeInUp>
                  ))}
                </Card>
              )}

              <StorageCard />
            </>
          )}
        </ScrollView>
      </SafeAreaView>
    </Canvas>
  );
}

/* ============================================================
   搜索板
   ============================================================ */

function SearchBoard({
  value,
  onChange,
  count,
}: {
  value: string;
  onChange: (v: string) => void;
  count: number;
}) {
  const t = useTheme();
  return (
    <View
      style={{
        height: 48,
        flexDirection: 'row',
        alignItems: 'center',
        gap: gap.sm,
        paddingHorizontal: 15,
        borderRadius: 18,
        backgroundColor: t.card,
        borderWidth: t.dark ? StyleSheet.hairlineWidth : 0,
        borderColor: t.cardBorder,
      }}
    >
      <Icon name="search-outline" size={18} color={t.ink3} />
      <TextInput
        value={value}
        onChangeText={onChange}
        placeholder="搜索文件、产物"
        placeholderTextColor={t.ink4}
        returnKeyType="search"
        style={{
          flex: 1,
          fontSize: type.sub,
          color: t.ink,
          padding: 0,
        }}
      />
      {value ? (
        <Pressable onPress={() => onChange('')} hitSlop={8}>
          <Icon name="close-circle" size={17} color={t.ink4} />
        </Pressable>
      ) : (
        <T size={10.5} weight={weight.bold} color={t.ink4} tracking={1.6}>
          {count} FILES
        </T>
      )}
    </View>
  );
}

/* ============================================================
   预览卡
   ============================================================ */

function PreviewCard({ item, onPress }: { item: Artifact; onPress: () => void }) {
  const t = useTheme();
  const k = KIND[item.kind] ?? FALLBACK;
  const c = k.color;

  return (
    <PressableScale onPress={onPress} scaleTo={0.97} style={{ width: 122 }}>
      <LinearGradient
        colors={grad([withAlpha(c, 0.62), withAlpha(c, 0.9)])}
        start={{ x: 0.1, y: 0 }}
        end={{ x: 0.9, y: 1 }}
        style={s.previewCard}
      >
        <View style={s.previewLabel}>
          <T size={10.5} weight={weight.semibold} color="#FFFFFF" tracking={0.3}>
            {k.label}
          </T>
        </View>
        <View style={{ flex: 1 }} />
        <Ionicons name={k.icon} size={19} color="rgba(255,255,255,0.92)" />
        <T
          size={13}
          weight={weight.semibold}
          color="#FFFFFF"
          lh={17}
          numberOfLines={2}
          style={{ marginTop: 6 }}
        >
          {item.title}
        </T>
      </LinearGradient>
      <T size={11} color={t.ink4} numberOfLines={1} style={{ marginTop: 7, marginLeft: 3 }}>
        {[shortTime(item.createdAt), item.agentName ?? '系统'].join(' · ')}
      </T>
    </PressableScale>
  );
}

/* ============================================================
   筛选 chip
   ============================================================ */

function FilterChip({ label, on, onPress }: { label: string; on: boolean; onPress: () => void }) {
  const t = useTheme();
  return (
    <Pressable
      onPress={onPress}
      accessibilityRole="button"
      accessibilityState={{ selected: on }}
      style={[
        s.chip,
        {
          backgroundColor: on ? t.primaryBg : t.card,
          borderWidth: on || t.dark ? StyleSheet.hairlineWidth : 0,
          borderColor: on ? t.primaryBg : t.cardBorder,
          ...(on ? {} : cardShadow(t)),
        },
      ]}
    >
      <T
        size={13}
        weight={on ? weight.semibold : weight.medium}
        color={on ? t.primaryInk : t.ink2}
        numberOfLines={1}
      >
        {label}
      </T>
    </Pressable>
  );
}

/* ============================================================
   文件行
   ============================================================ */

function FileRow({
  item,
  last,
  onPress,
  onDelete,
}: {
  item: Artifact;
  last: boolean;
  onPress: () => void;
  onDelete: () => void;
}) {
  const t = useTheme();
  const k = KIND[item.kind] ?? FALLBACK;

  return (
    <Pressable
      onPress={onPress}
      // 长按删除。列表里塞一个常驻删除按钮太重，
      // 但完全没有删除入口的话，存错的东西永远清不掉。
      onLongPress={onDelete}
      delayLongPress={420}
      accessibilityRole="button"
      accessibilityLabel={`${item.title}，长按可删除`}
      style={({ pressed }) => ({ backgroundColor: pressed ? t.cardSunken : 'transparent' })}
    >
      <View style={s.fileRow}>
        <LinearGradient
          colors={grad([withAlpha(k.color, 0.92), k.color])}
          start={{ x: 0, y: 0 }}
          end={{ x: 1, y: 1 }}
          style={s.fileIcon}
        >
          <Ionicons name={k.icon} size={20} color="#FFFFFF" />
        </LinearGradient>
        <View style={{ flex: 1 }}>
          <T size={14.5} weight={weight.semibold} numberOfLines={1}>
            {item.title}
          </T>
          <T size={12} color={t.ink4} numberOfLines={1} style={{ marginTop: 3 }}>
            {[
              item.conversationTitle ?? '未归档',
              item.agentName ?? '系统',
              shortTime(item.createdAt),
            ].join(' · ')}
          </T>
        </View>
        <Icon name="chevron-forward" size={16} color={t.faint} />
      </View>
      {!last ? (
        <View
          style={{
            height: StyleSheet.hairlineWidth,
            backgroundColor: t.line,
            marginLeft: 14 + 44 + 13,
          }}
        />
      ) : null}
    </Pressable>
  );
}

/* ============================================================
   存储
   ============================================================ */

function StorageCard() {
  const t = useTheme();
  const pct = Math.min(100, (STORAGE_USED_GB / STORAGE_TOTAL_GB) * 100);
  return (
    <Card style={{ padding: CARD_PAD }}>
      <Row style={{ justifyContent: 'space-between' }}>
        <T size={14.5} weight={weight.strong}>
          这台电脑的存储
        </T>
        <T
          size={12}
          color={t.ink4}
          style={{ fontVariant: ['tabular-nums'], fontFamily: fontFamily('500') }}
        >
          {STORAGE_USED_GB} GB / {STORAGE_TOTAL_GB} GB
        </T>
      </Row>
      <View style={{ marginTop: 11 }}>
        <Progress value={pct} />
      </View>
    </Card>
  );
}

/* ============================================================
   工具
   ============================================================ */

function shortTime(iso?: string) {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(+d)) return '';
  const now = new Date();
  const day = (x: Date) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime();
  const diff = Math.round((day(now) - day(d)) / 86400000);
  const hm = `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`;
  if (diff === 0) return `今天 ${hm}`;
  if (diff === 1) return '昨天';
  if (diff < 7) return `${diff} 天前`;
  return `${d.getMonth() + 1}/${d.getDate()}`;
}

const s = StyleSheet.create({
  page: {
    paddingHorizontal: PAGE_X,
    paddingTop: gap.xs,
    paddingBottom: 110,
    gap: 14,
  },
  previewScroll: { marginHorizontal: -PAGE_X },
  previewRow: { paddingHorizontal: PAGE_X, gap: 10 },
  previewCard: {
    height: 164,
    borderRadius: 20,
    padding: 12,
    overflow: 'hidden',
  },
  previewLabel: {
    alignSelf: 'flex-start',
    paddingHorizontal: 7,
    paddingVertical: 3,
    borderRadius: 13,
    backgroundColor: 'rgba(255,255,255,0.24)',
  },
  chipScroll: { marginHorizontal: -PAGE_X, flexGrow: 0 },
  chipRow: { paddingHorizontal: PAGE_X, gap: 8, alignItems: 'center' },
  chip: {
    height: 33,
    justifyContent: 'center',
    paddingHorizontal: 16,
    borderRadius: radius.pill,
  },
  fileRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 13,
    paddingVertical: 12,
    paddingHorizontal: 14,
  },
  fileIcon: {
    width: 44,
    height: 44,
    borderRadius: 13,
    alignItems: 'center',
    justifyContent: 'center',
  },
});
