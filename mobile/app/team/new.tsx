import { Ionicons } from '@expo/vector-icons';
import { useLocalSearchParams, useRouter } from 'expo-router';
import React, { useEffect, useState } from 'react';
import { ScrollView, StyleSheet, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { AvatarStack } from '../../src/components/Avatar';
import { Canvas } from '../../src/components/Canvas';
import { FadeInUp } from '../../src/components/motion';
import { Surface } from '../../src/components/Surface';
import { Button, Icon, Input, NavBar, Row, T } from '../../src/components/ui';
import { humanError, post } from '../../src/lib/api';
import { useApp } from '../../src/lib/store';
import type { Conversation, Scenario } from '../../src/lib/types';
import { useTheme } from '../../src/theme/theme';
import { CARD_GAP, CARD_PAD, gap, leading, type } from '../../src/theme/tokens';

/**
 * 新建团队。
 *
 * 从预设模板建（继承角色），或者选随机场景自己长出来。
 * 选中态用整张卡的描边 + 右侧勾，比只改背景更明确。
 */
export default function NewTeam() {
  const t = useTheme();
  const router = useRouter();
  const params = useLocalSearchParams<{ scenario?: string }>();
  const scenarios = useApp((s) => s.scenarios);
  const upsertConv = useApp((s) => s.upsertConv);

  const [title, setTitle] = useState('');
  const [studio, setStudio] = useState(params.scenario ?? '');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');

  useEffect(() => {
    if (!params.scenario) return;
    const sc = scenarios.find((s) => s.key === params.scenario);
    if (sc) {
      setTitle(sc.name);
      setStudio(sc.key);
    }
  }, [params.scenario, scenarios]);

  const picked = scenarios.find((s) => s.key === studio);

  async function create() {
    if (!title.trim()) return setErr('给团队起个名字');
    setBusy(true);
    setErr('');
    try {
      const conv = await post<Conversation>('/v1/conversations', {
        title: title.trim(),
        studio: studio || 'companion',
      });
      upsertConv(conv);
      router.replace(`/team/${conv.id}`);
    } catch (e) {
      setErr(humanError(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Canvas>
      <SafeAreaView style={{ flex: 1 }} edges={['top', 'bottom']}>
        <NavBar title="新建团队" onBack={() => router.back()} />
        <ScrollView
          contentContainerStyle={s.page}
          showsVerticalScrollIndicator={false}
          keyboardShouldPersistTaps="handled"
        >
          <T size={type.caption} color={t.ink2} weight="600" style={{ marginBottom: 7 }}>
            团队名称
          </T>
          <Input value={title} onChangeText={setTitle} placeholder="比如「园区改造投标组」" />

          <T size={type.headline} weight="600" style={{ marginTop: gap.xl, marginBottom: gap.sm }}>
            团队模板
          </T>

          <View style={{ gap: CARD_GAP }}>
            {scenarios.map((sc, i) => (
              <ScenarioRow
                key={sc.key}
                sc={sc}
                selected={studio === sc.key}
                onPress={() => {
                  setStudio(sc.key);
                  if (!title.trim() || title === picked?.name) setTitle(sc.name);
                }}
                delay={i * 30}
              />
            ))}
          </View>

          <View
            style={[
              s.note,
              {
                backgroundColor: t.card,
                borderColor: t.dark ? t.cardBorder : 'rgba(16,20,45,0.05)',
              },
            ]}
          >
            <Icon name="information-circle-outline" size={15} color={t.ink3} />
            <T size={type.caption} color={t.ink3} lh={leading.caption} style={{ flex: 1 }}>
              建好之后可以在团队设置里增删角色、改人设、给角色配执行权限。
            </T>
          </View>

          {err ? (
            <T size={type.sub} color={t.bad} style={{ marginTop: gap.md }}>
              {err}
            </T>
          ) : null}

          <Button
            label="创建并进入"
            onPress={create}
            loading={busy}
            disabled={!title.trim()}
            full
            style={{ marginTop: gap.lg }}
          />
        </ScrollView>
      </SafeAreaView>
    </Canvas>
  );
}

function ScenarioRow({
  sc,
  selected,
  onPress,
  delay,
}: {
  sc: Scenario;
  selected: boolean;
  onPress: () => void;
  delay: number;
}) {
  const t = useTheme();
  const names = (sc.agents ?? []).map((a) => a.name);
  const tints = (sc.agents ?? []).map((a) => a.color);
  const count = sc.agents?.length ?? 0;

  return (
    <FadeInUp delay={delay}>
      <Surface
        onPress={onPress}
        outerRadius={20}
        style={[
          { borderWidth: 1.5, borderColor: 'transparent' },
          selected && { borderColor: t.accent, backgroundColor: t.accentFaint },
        ]}
      >
        <Row gap={12} style={{ padding: CARD_PAD }}>
          {count > 0 ? (
            <AvatarStack names={names} tints={tints} size={20} max={4} />
          ) : (
            <View
              style={{
                width: 34,
                height: 34,
                borderRadius: 12,
                backgroundColor: t.cardSunken,
                alignItems: 'center',
                justifyContent: 'center',
              }}
            >
              <Ionicons name="shuffle" size={16} color={t.ink3} />
            </View>
          )}

          <View style={{ flex: 1 }}>
            <T size={type.body} weight="600" numberOfLines={1}>
              {sc.name}
            </T>
            <T size={type.caption} color={t.ink3} numberOfLines={1} style={{ marginTop: 3 }}>
              {sc.random ? '随机角色' : `${count} 位角色`}
              {sc.tagline ? ` · ${sc.tagline}` : ''}
            </T>
          </View>

          {selected ? (
            <View
              style={{
                width: 22,
                height: 22,
                borderRadius: 11,
                backgroundColor: t.accent,
                alignItems: 'center',
                justifyContent: 'center',
              }}
            >
              <Ionicons name="checkmark" size={13} color="#FFFFFF" />
            </View>
          ) : null}
        </Row>
      </Surface>
    </FadeInUp>
  );
}

const s = StyleSheet.create({
  page: { paddingHorizontal: gap.lg, paddingTop: 6, paddingBottom: 100 },
  note: {
    flexDirection: 'row',
    gap: 7,
    alignItems: 'flex-start',
    marginTop: gap.lg,
    padding: 12,
    borderRadius: 14,
    borderWidth: StyleSheet.hairlineWidth,
  },
});
