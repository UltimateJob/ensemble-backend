import { Ionicons } from '@expo/vector-icons';
import { useRouter } from 'expo-router';
import React, { useCallback, useEffect, useState } from 'react';
import { Alert, Modal, Pressable, ScrollView, StyleSheet, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { Canvas } from '../src/components/Canvas';
import { FadeInUp } from '../src/components/motion';
import { ListSkeleton } from '../src/components/Skeleton';
import { Surface } from '../src/components/Surface';
import { Button, Card, Icon, IconTile, Input, NavBar, Row, T, Tag, Toggle } from '../src/components/ui';
import { del, get, humanError, patch, post } from '../src/lib/api';
import type { Connector, ConnectorsResponse } from '../src/lib/types';
import { useTheme } from '../src/theme/theme';
import { CARD_GAP, CARD_PAD, gap, leading, radius, type } from '../src/theme/tokens';

/** 连接器授权管理。凭据加密存盘，「测试连接」真的发请求。 */
const META: Record<string, { icon: any; tint: string; colors: [string, string] }> = {
  github: { icon: 'logo-github', tint: '#24292F', colors: ['#6E7480', '#3A3F49'] },
  notion: { icon: 'document', tint: '#111114', colors: ['#8A8F9C', '#4A4F5C'] },
  feishu: { icon: 'paper-plane', tint: '#3370FF', colors: ['#7FA8F7', '#3370FF'] },
  calendar: { icon: 'calendar', tint: '#14804A', colors: ['#6FD8AE', '#34A87C'] },
  webhook: { icon: 'link', tint: '#8B72D9', colors: ['#B39AF0', '#7C5CE0'] },
};

const FIELDS: Record<string, { secrets: { key: string; label: string; ph?: string }[]; config: { key: string; label: string; ph?: string }[] }> = {
  github: { secrets: [{ key: 'token', label: 'Personal Access Token', ph: 'ghp_…' }], config: [{ key: 'baseUrl', label: 'API 地址（可空）', ph: 'https://api.github.com' }] },
  notion: { secrets: [{ key: 'token', label: 'Integration Token', ph: 'secret_…' }], config: [{ key: 'baseUrl', label: 'API 地址（可空）', ph: 'https://api.notion.com' }] },
  feishu: { secrets: [{ key: 'appId', label: 'App ID', ph: 'cli_…' }, { key: 'appSecret', label: 'App Secret' }], config: [{ key: 'baseUrl', label: '开放平台地址（可空）', ph: 'https://open.feishu.cn' }] },
  calendar: { secrets: [{ key: 'username', label: '用户名（可空）' }, { key: 'password', label: '密码（可空）' }], config: [{ key: 'url', label: '日历地址（CalDAV / ICS）', ph: 'https://…/calendar.ics' }] },
  webhook: { secrets: [], config: [{ key: 'url', label: 'Webhook 地址', ph: 'https://…' }] },
};

export default function Connect() {
  const t = useTheme();
  const router = useRouter();
  const [items, setItems] = useState<Connector[]>([]);
  const [kinds, setKinds] = useState<Record<string, string>>({});
  const [err, setErr] = useState('');
  const [editing, setEditing] = useState<{ kind: string; existing?: Connector } | null>(null);

  const load = useCallback(async () => {
    setErr('');
    try {
      const r = await get<ConnectorsResponse>('/v1/connectors');
      setItems(r.items ?? []);
      setKinds(r.kinds ?? {});
    } catch (e) {
      setErr(humanError(e));
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  async function probe(c: Connector) {
    try {
      const u = await post<Connector>(`/v1/connectors/${c.id}/probe`);
      setItems((l) => l.map((x) => (x.id === c.id ? u : x)));
      if (u.status === 'error') Alert.alert('连接失败', u.lastError || '上游拒绝了这组凭据');
    } catch (e) {
      setErr(humanError(e));
    }
  }

  async function toggle(c: Connector, on: boolean) {
    const u = await patch<Connector>(`/v1/connectors/${c.id}`, { enabled: on }).catch(() => null);
    if (u) setItems((l) => l.map((x) => (x.id === c.id ? u : x)));
  }

  function group(list: Connector[]) {
    if (!list.length) return <T size={type.sub} color={t.ink3}>还没有连接器</T>;
    return list.map((c, i) => {
      const m = META[c.kind] ?? META.webhook;
      return (
        <FadeInUp key={c.id} delay={i * 35}>
          <Surface shell={4} outerRadius={22} style={{ marginBottom: CARD_GAP }}>
            <View style={{ padding: CARD_PAD }}>
              <Row gap={12}>
                <IconTile name={m.icon} colors={m.colors} size={30} iconSize={17} radius={12} />
                <View style={{ flex: 1 }}>
                  <Row gap={6}>
                    <T size={type.body} weight="600">{c.name}</T>
                    <Tag
                      label={c.status === 'ok' ? '已连通' : c.status === 'error' ? '失败' : '未测试'}
                      tone={c.status === 'ok' ? 'done' : c.status === 'error' ? 'blocked' : 'neutral'}
                    />
                  </Row>
                  <T size={type.caption} color={t.ink3} numberOfLines={1} style={{ marginTop: 2 }}>
                    {c.status === 'error' && c.lastError
                      ? c.lastError
                      : `${kinds[c.kind] ?? c.kind}${c.secretKeys?.length ? ` · 已填 ${c.secretKeys.length} 项` : ''}`}
                  </T>
                </View>
                <Toggle on={c.enabled} onChange={(v) => toggle(c, v)} />
              </Row>
              {/* 操作行：和内容同一水平节奏，不再独占半张卡 */}
              <Row style={{ justifyContent: 'flex-end', marginTop: 8 }} gap={16}>
                <Pressable onPress={() => probe(c)} hitSlop={8}>
                  <T size={type.caption} color={t.accent} weight="600">测试连接</T>
                </Pressable>
                <Pressable onPress={() => setEditing({ kind: c.kind, existing: c })} hitSlop={8}>
                  <T size={type.caption} color={t.ink2}>编辑</T>
                </Pressable>
                <Pressable
                  onPress={() =>
                    Alert.alert('删除', `确定删除「${c.name}」？`, [
                      { text: '取消', style: 'cancel' },
                      { text: '删除', style: 'destructive', onPress: async () => { await del(`/v1/connectors/${c.id}`).catch(() => {}); load(); } },
                    ])
                  }
                  hitSlop={8}
                >
                  <T size={type.caption} color={t.ink3}>删除</T>
                </Pressable>
              </Row>
            </View>
          </Surface>
        </FadeInUp>
      );
    });
  }

  return (
    <Canvas>
      <SafeAreaView style={{ flex: 1 }} edges={['top']}>
        <NavBar title="连接器" subtitle="凭据加密存在电脑上" onBack={() => router.back()} />
        <ScrollView contentContainerStyle={s.page}>
          {err ? <T size={type.sub} color={t.bad} style={{ marginBottom: gap.xs }}>{err}</T> : null}
          <T size={type.headline} weight="600" style={{ marginBottom: gap.xs }}>团队公共</T>
          {group(items.filter((c) => c.scope === 'team'))}
          <T size={type.headline} weight="600" style={{ marginTop: gap.lg, marginBottom: gap.xs }}>我的私有</T>
          {group(items.filter((c) => c.scope !== 'team'))}
          <T size={type.headline} weight="600" style={{ marginTop: gap.lg, marginBottom: gap.xs }}>添加</T>
          <Row gap={gap.xs} style={{ flexWrap: 'wrap' }}>
            {Object.entries(kinds).map(([k, label]) => {
              const m = META[k] ?? META.webhook;
              return (
                <Pressable key={k} onPress={() => setEditing({ kind: k })} style={[s.chip, { backgroundColor: t.card }]}>
                  <Icon name={m.icon} size={15} color={m.tint} />
                  <T size={type.sub} color={t.ink2}>{label}</T>
                </Pressable>
              );
            })}
          </Row>
        </ScrollView>

        {editing ? (
          <Form kind={editing.kind} existing={editing.existing} label={kinds[editing.kind] ?? editing.kind} onClose={() => setEditing(null)} onSaved={() => { setEditing(null); load(); }} />
        ) : null}
      </SafeAreaView>
    </Canvas>
  );
}

function Form({ kind, existing, label, onClose, onSaved }: { kind: string; existing?: Connector; label: string; onClose: () => void; onSaved: () => void }) {
  const t = useTheme();
  const spec = FIELDS[kind] ?? { secrets: [], config: [] };
  const [name, setName] = useState(existing?.name ?? label);
  const [scope, setScope] = useState<'team' | 'private'>(existing?.scope ?? 'private');
  const [secrets, setSecrets] = useState<Record<string, string>>({});
  const [config, setConfig] = useState<Record<string, string>>(existing?.config ?? {});
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');

  async function save() {
    setBusy(true);
    setErr('');
    try {
      if (existing) await patch(`/v1/connectors/${existing.id}`, { name, scope, config, secrets });
      else await post('/v1/connectors', { kind, name, scope, config, secrets });
      onSaved();
    } catch (e) {
      setErr(humanError(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal visible transparent animationType="slide" onRequestClose={onClose}>
      <View style={[s.scrim, { backgroundColor: t.scrim }]}>
        <View style={[s.modal, { backgroundColor: t.card }]}>
          <Row style={{ justifyContent: 'space-between', marginBottom: gap.md }}>
            <T size={type.title} weight="600">{existing ? '编辑' : '添加'} {label}</T>
            <Pressable onPress={onClose} hitSlop={12}><Icon name="close" size={20} color={t.ink3} /></Pressable>
          </Row>
          <ScrollView contentContainerStyle={{ gap: gap.md }} keyboardShouldPersistTaps="handled">
            <View style={{ gap: 6 }}>
              <T size={type.caption} color={t.ink2} weight="500">名称</T>
              <Input value={name} onChangeText={setName} placeholder={label} />
            </View>
            <Row gap={gap.xs}>
              {(['private', 'team'] as const).map((v) => (
                <Pressable key={v} onPress={() => setScope(v)} style={[s.chip, { backgroundColor: scope === v ? t.accentSoft : t.cardSunken }]}>
                  <T size={type.sub} color={scope === v ? t.accent : t.ink2}>{v === 'team' ? '团队公共' : '我的私有'}</T>
                </Pressable>
              ))}
            </Row>
            {spec.config.map((f) => (
              <View key={f.key} style={{ gap: 6 }}>
                <T size={type.caption} color={t.ink2} weight="500">{f.label}</T>
                <Input value={config[f.key] ?? ''} onChangeText={(v) => setConfig({ ...config, [f.key]: v })} placeholder={f.ph} />
              </View>
            ))}
            {spec.secrets.map((f) => {
              const filled = existing?.secretKeys?.includes(f.key);
              return (
                <View key={f.key} style={{ gap: 6 }}>
                  <T size={type.caption} color={t.ink2} weight="500">{f.label}{filled ? '（已填，留空不改）' : ''}</T>
                  <Input value={secrets[f.key] ?? ''} onChangeText={(v) => setSecrets({ ...secrets, [f.key]: v })} placeholder={f.ph} secure />
                </View>
              );
            })}
            <View style={[s.note, { backgroundColor: t.cardSunken }]}>
              <Icon name="lock-closed-outline" size={13} color={t.ink3} />
              <T size={type.caption} color={t.ink3} lh={leading.caption} style={{ flex: 1 }}>
                凭据用电脑上的 JWT_SECRET 派生密钥加密存盘，单独拷走 data 目录解不开。
              </T>
            </View>
            {err ? <T size={type.sub} color={t.bad}>{err}</T> : null}
            <Button label="保存" onPress={save} loading={busy} full />
          </ScrollView>
        </View>
      </View>
    </Modal>
  );
}

const s = StyleSheet.create({
  page: { paddingHorizontal: gap.lg, paddingTop: 6, paddingBottom: 100 },
  chip: { flexDirection: 'row', alignItems: 'center', gap: 6, paddingHorizontal: gap.sm, paddingVertical: 8, borderRadius: radius.pill },
  scrim: { flex: 1, justifyContent: 'flex-end' },
  modal: { borderTopLeftRadius: radius.card, borderTopRightRadius: radius.card, padding: gap.lg, maxHeight: '88%' },
  note: { flexDirection: 'row', gap: 7, alignItems: 'flex-start', borderRadius: radius.md, padding: gap.sm },
});
