import { LinearGradient } from 'expo-linear-gradient';
import { useRouter } from 'expo-router';
import React, { useCallback, useEffect, useState } from 'react';
import { Alert, Modal, Pressable, ScrollView, StyleSheet, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { Canvas } from '../src/components/Canvas';
import { FadeInUp } from '../src/components/motion';
import { Skeleton } from '../src/components/Skeleton';
import { Surface } from '../src/components/Surface';
import {
  Divider,
  Icon,
  Input,
  NavBar,
  RadioDot,
  Row,
  T,
  Toggle,
  grad,
  withAlpha,
  type IoniconName,
} from '../src/components/ui';
import { del, get, humanError, patch, post } from '../src/lib/api';
import type { Connector, ConnectorsResponse } from '../src/lib/types';
import { useTheme, type Theme } from '../src/theme/theme';
import { PAGE_X, gap, leading, radius, type } from '../src/theme/tokens';

/**
 * 连接器（附录 C #15，设计图 v18-phone10）。
 *
 * 已连接 = 墨黑开关；未连接 = 品牌渐变「连接」胶囊。
 * 页尾说明：连接器已连接 ≠ 可免审批调用——是否要问过你，由成员的
 * 权限级别决定（§7），这句话必须在页面上出现。
 *
 * 凭据只存电脑端（加密落盘），这里只负责表单和「测试连接」——
 * 测试会真的发一次鉴权请求，不是只看字段非空。
 */

type Field = { key: string; label: string; ph?: string; secure?: boolean };

interface Spec {
  label: string;
  icon: IoniconName;
  /** 图标砖底色从主题取（服务身份色压饱和），不写裸色值 */
  tint: (t: Theme) => string;
  /** 已连接时的副标题描述 */
  connectedHint: string;
  /** 英雄卡里「成员可调用你的…」这一段 */
  capability: string;
  secrets: Field[];
  config: Field[];
}

const SPECS: Record<string, Spec> = {
  github: {
    label: 'GitHub',
    icon: 'logo-github',
    tint: (t) => (t.dark ? t.chipBg : t.ink),
    connectedHint: '已授权仓库与 Issue',
    capability: '成员可调用你的仓库与 Issue',
    secrets: [{ key: 'token', label: 'Personal Access Token', ph: 'ghp_…', secure: true }],
    config: [{ key: 'baseUrl', label: 'API 地址（可空）', ph: 'https://api.github.com' }],
  },
  qqmail: {
    label: 'QQ 邮箱',
    icon: 'mail',
    tint: (t) => t.meBlue,
    connectedHint: '已授权收发邮件',
    capability: '成员可调用你的收发件',
    secrets: [
      { key: 'address', label: '邮箱地址', ph: 'name@qq.com' },
      { key: 'authCode', label: 'IMAP 授权码', ph: '在 QQ 邮箱设置里生成', secure: true },
    ],
    config: [],
  },
  notion: {
    label: 'Notion',
    icon: 'document-text',
    tint: (t) => (t.dark ? t.chipBg : t.ink),
    connectedHint: '已授权页面与数据库',
    capability: '成员可调用你的页面与数据库',
    secrets: [{ key: 'token', label: 'Integration Token', ph: 'secret_…', secure: true }],
    config: [{ key: 'baseUrl', label: 'API 地址（可空）', ph: 'https://api.notion.com' }],
  },
  calendar: {
    label: '系统日历',
    icon: 'calendar',
    tint: (t) => t.ok,
    connectedHint: '已授权日程读取',
    capability: '成员可读取你的日程',
    secrets: [
      { key: 'username', label: '用户名（可空）' },
      { key: 'password', label: '密码（可空）', secure: true },
    ],
    config: [{ key: 'url', label: '日历地址（CalDAV / ICS）', ph: 'https://…/calendar.ics' }],
  },
  feishu: {
    label: '飞书',
    icon: 'paper-plane',
    tint: (t) => t.accent,
    connectedHint: '已授权企业应用',
    capability: '成员可调用你的企业应用',
    secrets: [
      { key: 'appId', label: 'App ID', ph: 'cli_…' },
      { key: 'appSecret', label: 'App Secret', secure: true },
    ],
    config: [{ key: 'baseUrl', label: '开放平台地址（可空）', ph: 'https://open.feishu.cn' }],
  },
  webhook: {
    label: 'Webhook',
    icon: 'link',
    tint: (t) => t.accentDeep,
    connectedHint: '已配置回调地址',
    capability: '任务结束后可回调你的地址',
    secrets: [],
    config: [{ key: 'url', label: 'Webhook 地址', ph: 'https://…' }],
  },
};

/** 页面固定顺序（附录 C/设计图）：GitHub / QQ 邮箱 / Notion / 系统日历。 */
const ORDER = ['github', 'qqmail', 'notion', 'calendar'];

function specOf(kind: string): Spec {
  return SPECS[kind] ?? SPECS.webhook;
}

export default function Connect() {
  const t = useTheme();
  const router = useRouter();
  const [items, setItems] = useState<Connector[]>([]);
  const [kinds, setKinds] = useState<Record<string, string>>({});
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState('');
  const [editing, setEditing] = useState<{ kind: string; existing?: Connector } | null>(null);
  const [adding, setAdding] = useState(false);

  const load = useCallback(async () => {
    setErr('');
    try {
      const r = await get<ConnectorsResponse>('/v1/connectors');
      setItems(r.items ?? []);
      setKinds(r.kinds ?? {});
    } catch (e) {
      setErr(humanError(e));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  async function toggle(c: Connector, on: boolean) {
    // 开关先动，失败再回滚。
    setItems((l) => l.map((x) => (x.id === c.id ? { ...x, enabled: on } : x)));
    try {
      const u = await patch<Connector>(`/v1/connectors/${c.id}`, { enabled: on });
      setItems((l) => l.map((x) => (x.id === c.id ? u : x)));
    } catch (e) {
      setItems((l) => l.map((x) => (x.id === c.id ? c : x)));
      setErr(humanError(e));
    }
  }

  async function probe(c: Connector) {
    try {
      const u = await post<Connector>(`/v1/connectors/${c.id}/probe`);
      setItems((l) => l.map((x) => (x.id === c.id ? u : x)));
      if (u.status === 'error') Alert.alert('连接失败', u.lastError || '上游拒绝了这组凭据');
      else if (u.status === 'ok') Alert.alert('连接正常', `${specOf(c.kind).label} 已通过鉴权测试。`);
    } catch (e) {
      setErr(humanError(e));
    }
  }

  function remove(c: Connector) {
    Alert.alert('删除连接器', `确定删除「${c.name}」？凭据会一并从电脑上清掉。`, [
      { text: '取消', style: 'cancel' },
      {
        text: '删除',
        style: 'destructive',
        onPress: async () => {
          // 之前这里 .catch(() => {}) 把失败吞掉了：界面看起来删成功了，
          // 刷新一下它又回来，用户完全不知道发生了什么。
          try {
            await del(`/v1/connectors/${c.id}`);
          } catch (e) {
            Alert.alert('删除失败', humanError(e));
          }
          load();
        },
      },
    ]);
  }

  /** 每个服务取一条：优先团队公共，其次私有。 */
  const byKind = new Map<string, Connector>();
  for (const c of items) {
    const prev = byKind.get(c.kind);
    if (!prev || (prev.scope !== 'team' && c.scope === 'team')) byKind.set(c.kind, c);
  }
  const presetRows = ORDER.map((kind) => ({ kind, conn: byKind.get(kind) }));
  const extraRows = Object.keys(kinds)
    .filter((k) => !ORDER.includes(k))
    .map((kind) => ({ kind, conn: byKind.get(kind) }))
    .filter((r) => r.conn || items.some((c) => c.kind === r.kind));

  const connected = items.filter((c) => c.enabled && !c.lastError);
  const first = connected[0];
  const firstSpec = first ? specOf(first.kind) : null;

  const addKinds = Array.from(new Set([...ORDER, ...Object.keys(kinds)]));

  return (
    <Canvas>
      <SafeAreaView style={{ flex: 1 }} edges={['top', 'bottom']}>
        <NavBar title="连接器" onBack={() => router.back()} />
        <ScrollView contentContainerStyle={s.page} showsVerticalScrollIndicator={false}>
          {err ? (
            <T size={type.caption} color={t.bad} style={{ marginBottom: gap.sm }}>
              {err}
            </T>
          ) : null}

          {/* 顶部深色英雄卡（附录 A2） */}
          <FadeInUp>
            <View style={[s.hero, { backgroundColor: t.heroBg }]}>
              <LinearGradient
                colors={grad(t.gradient)}
                start={{ x: 0, y: 0 }}
                end={{ x: 1, y: 1 }}
                style={[StyleSheet.absoluteFill, { opacity: t.dark ? 0.26 : 0.18 }]}
              />
              <View style={[s.heroShield, { backgroundColor: withAlpha(t.white, 0.11) }]}>
                <Icon name="shield-checkmark" size={18} color={t.white} />
              </View>
              <T size={10.5} weight="700" color={t.white} tracking={2.2}>
                CONNECTED
              </T>
              <Row gap={8} align="flex-end" style={{ marginTop: 8 }}>
                <T size={40} weight="750" color={t.white} lh={44}>
                  {connected.length}
                </T>
                <T size={type.card} weight="600" color={t.white} lh={26} style={{ marginBottom: 3 }}>
                  个服务已连接
                </T>
              </Row>
              <T size={type.sub} color={t.white} style={{ marginTop: 4, opacity: 0.72 }} numberOfLines={2}>
                {first && firstSpec
                  ? `${firstSpec.label} 已授权 · ${firstSpec.capability}`
                  : '还没有连上任何服务 · 成员暂时调用不了外部能力'}
              </T>
              <View style={[s.heroRule, { backgroundColor: withAlpha(t.white, 0.14) }]} />
              <T size={type.caption} color={t.white} style={{ opacity: 0.48 }}>
                凭据加密保存在电脑本地，仅调用时由本机使用
              </T>
            </View>
          </FadeInUp>

          {/* 连接器行 */}
          <View style={{ marginTop: gap.md }}>
            {loading ? (
              <View style={{ gap: 10 }}>
                {[0, 1, 2, 3].map((i) => (
                  <Surface key={i} outerRadius={radius.card} style={{ paddingVertical: 14, paddingHorizontal: 15 }}>
                    <Row gap={gap.md}>
                      <Skeleton width={44} height={44} radius={radius.icon} />
                      <View style={{ flex: 1, gap: 7 }}>
                        <Skeleton width="42%" height={14} />
                        <Skeleton width="66%" height={10} />
                      </View>
                      <Skeleton width={52} height={30} radius={15} />
                    </Row>
                  </Surface>
                ))}
              </View>
            ) : (
              [...presetRows, ...extraRows].map(({ kind, conn }, i) => (
                <FadeInUp key={kind} delay={i * 45}>
                  <ConnectorRow
                    kind={kind}
                    conn={conn}
                    onOpen={() => setEditing({ kind, existing: conn })}
                    onConnect={() => setEditing({ kind })}
                    onToggle={(v) => conn && toggle(conn, v)}
                  />
                </FadeInUp>
              ))
            )}
          </View>

          {/* 虚线添加框 */}
          <Pressable
            onPress={() => setAdding(true)}
            style={[s.dashed, { borderColor: t.radioOff }]}
            accessibilityRole="button"
            accessibilityLabel="添加连接器"
          >
            <Icon name="add" size={18} color={t.ink2} />
            <T size={type.sub} color={t.ink2} weight="550">
              添加连接器
            </T>
          </Pressable>

          {/* 页尾说明：连接器与权限级别的关系（§7 末段） */}
          <View style={[s.note, { backgroundColor: t.accentSoft }]}>
            <View style={[s.noteIcon, { backgroundColor: withAlpha(t.accent, 0.16) }]}>
              <Icon name="shield-checkmark-outline" size={17} color={t.accent} />
            </View>
            <T size={type.caption} color={t.ink2} lh={leading.caption + 3} style={{ flex: 1 }}>
              连接器能不能被调用，还受每个成员的
              <T size={type.caption} weight="700" color={t.ink}>
                权限级别
              </T>
              约束：「需要审批」的成员调用任何连接器都要先问过你。
            </T>
          </View>
        </ScrollView>

        {editing ? (
          <ConnectorForm
            kind={editing.kind}
            existing={editing.existing}
            onClose={() => setEditing(null)}
            onDelete={editing.existing ? () => { const c = editing.existing!; setEditing(null); remove(c); } : undefined}
            onSaved={() => {
              setEditing(null);
              load();
            }}
          />
        ) : null}

        <AddSheet
          visible={adding}
          kinds={addKinds}
          labels={kinds}
          onPick={(kind) => {
            setAdding(false);
            setEditing({ kind, existing: byKind.get(kind) });
          }}
          onClose={() => setAdding(false)}
        />
      </SafeAreaView>
    </Canvas>
  );
}

/* ============================================================
   行
   ============================================================ */

function ConnectorRow({
  kind,
  conn,
  onOpen,
  onConnect,
  onToggle,
}: {
  kind: string;
  conn?: Connector;
  onOpen: () => void;
  onConnect: () => void;
  onToggle: (v: boolean) => void;
}) {
  const t = useTheme();
  const spec = specOf(kind);
  const connected = !!conn;
  const failing = !!conn?.lastError || conn?.status === 'error';
  const sub = !conn
    ? '未连接'
    : failing
      ? conn.lastError || '上次测试失败，点开检查凭据'
      : !conn.enabled
        ? '已暂停 · 成员暂时调用不了'
        : conn.status === 'unset'
          ? `${spec.connectedHint} · 未测试`
          : `${spec.connectedHint}`;

  return (
    <Pressable
      onPress={onOpen}
      accessibilityRole="button"
      accessibilityLabel={`${spec.label}${connected ? '已连接' : '未连接'}`}
      style={{ marginBottom: 10 }}
    >
      <Surface outerRadius={radius.card} style={{ paddingVertical: 14, paddingHorizontal: 15 }}>
        <Row gap={gap.md}>
          <Brick icon={spec.icon} tint={spec.tint(t)} />
          <View style={{ flex: 1, minWidth: 0 }}>
            <Row gap={6}>
              <T size={type.card} weight="650" numberOfLines={1}>
                {spec.label}
              </T>
              {conn?.enabled && !failing ? (
                <View style={[s.dot, { backgroundColor: t.okDot }]} />
              ) : failing ? (
                <View style={[s.dot, { backgroundColor: t.dangerDot }]} />
              ) : null}
            </Row>
            <T
              size={type.caption}
              color={failing ? t.bad : t.ink3}
              numberOfLines={1}
              style={{ marginTop: 3 }}
            >
              {sub}
            </T>
          </View>
          {connected ? (
            <Toggle on={!!conn!.enabled} onChange={onToggle} />
          ) : (
            <Pressable onPress={onConnect} accessibilityRole="button" accessibilityLabel={`连接 ${spec.label}`}>
              <LinearGradient
                colors={grad(t.gradient)}
                start={{ x: 0, y: 0 }}
                end={{ x: 1, y: 1 }}
                style={s.connectBtn}
              >
                <T size={type.caption} weight="700" color={t.white}>
                  连接
                </T>
              </LinearGradient>
            </Pressable>
          )}
        </Row>
      </Surface>
    </Pressable>
  );
}

/** 44px 图标砖（附录 A5）：同色系双色渐变 + 顶部内高光。 */
function Brick({ icon, tint }: { icon: IoniconName; tint: string }) {
  const t = useTheme();
  return (
    <LinearGradient
      colors={grad([withAlpha(tint, 1), withAlpha(tint, 0.76)])}
      start={{ x: 0.1, y: 0 }}
      end={{ x: 0.9, y: 1 }}
      style={s.brick}
    >
      <View pointerEvents="none" style={[s.brickGloss, { backgroundColor: t.white }]} />
      <Icon name={icon} size={21} color={t.white} />
    </LinearGradient>
  );
}

/* ============================================================
   授权表单
   ============================================================ */

function ConnectorForm({
  kind,
  existing,
  onClose,
  onSaved,
  onDelete,
}: {
  kind: string;
  existing?: Connector;
  onClose: () => void;
  onSaved: () => void;
  onDelete?: () => void;
}) {
  const t = useTheme();
  const spec = specOf(kind);
  const [name, setName] = useState(existing?.name ?? spec.label);
  const [scope, setScope] = useState<'team' | 'private'>(existing?.scope ?? 'private');
  const [secrets, setSecrets] = useState<Record<string, string>>({});
  const [config, setConfig] = useState<Record<string, string>>(existing?.config ?? {});
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState(existing?.lastError ?? '');

  /** 保存（新建或更新）。返回落盘后的连接器，供「测试连接」接着用。 */
  async function persist(): Promise<Connector | null> {
    const body = { name: name.trim() || spec.label, scope, config, secrets };
    if (existing) return patch<Connector>(`/v1/connectors/${existing.id}`, body);
    return post<Connector>('/v1/connectors', { kind, ...body });
  }

  async function save(thenProbe = false) {
    setBusy(true);
    setErr('');
    try {
      const c = await persist();
      if (thenProbe && c) {
        const u = await post<Connector>(`/v1/connectors/${c.id}/probe`);
        if (u.status === 'error') {
          setErr(u.lastError || '上游拒绝了这组凭据');
          onSaved();
          return;
        }
      }
      onSaved();
    } catch (e) {
      const msg = humanError(e);
      // 旧版电脑端不认识邮箱连接器：把服务端的「不支持」翻译成下一步怎么走。
      setErr(kind === 'qqmail' && msg.includes('不支持') ? '这台电脑上的纸团版本还不支持邮箱连接器，请先升级电脑端。' : msg);
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal visible transparent animationType="slide" onRequestClose={onClose}>
      <View style={{ flex: 1, justifyContent: 'flex-end' }}>
        <Pressable style={[StyleSheet.absoluteFill, { backgroundColor: t.scrim }]} onPress={onClose} />
        <View style={[s.form, { backgroundColor: t.card }]}>
          <Row gap={gap.sm} style={{ marginBottom: gap.md }}>
            <Brick icon={spec.icon} tint={spec.tint(t)} />
            <View style={{ flex: 1 }}>
              <T size={type.title} weight="650">
                {existing ? '编辑连接器' : '连接'} · {spec.label}
              </T>
              <T size={type.caption} color={t.ink3} style={{ marginTop: 2 }}>
                {existing ? '留空的凭据字段保持原值不变' : '凭据只存到你的电脑上，用本机密钥加密'}
              </T>
            </View>
            <Pressable onPress={onClose} hitSlop={12} accessibilityLabel="关闭">
              <Icon name="close" size={20} color={t.ink3} />
            </Pressable>
          </Row>

          <ScrollView contentContainerStyle={{ gap: gap.md, paddingBottom: gap.xs }} keyboardShouldPersistTaps="handled">
            <View style={{ gap: 6 }}>
              <T size={type.caption} color={t.ink2} weight="550">
                名称
              </T>
              <Input value={name} onChangeText={setName} placeholder={spec.label} />
            </View>

            <View style={{ gap: 6 }}>
              <T size={type.caption} color={t.ink2} weight="550">
                谁可以用
              </T>
              <View style={{ gap: gap.xs }}>
                <ScopeRow
                  label="我的私有"
                  hint="只有你发起的任务能调用"
                  on={scope === 'private'}
                  onPress={() => setScope('private')}
                />
                <ScopeRow
                  label="团队公共"
                  hint="团队里所有成员都能调用（仍受权限级别约束）"
                  on={scope === 'team'}
                  onPress={() => setScope('team')}
                />
              </View>
            </View>

            {spec.config.map((f) => (
              <View key={f.key} style={{ gap: 6 }}>
                <T size={type.caption} color={t.ink2} weight="550">
                  {f.label}
                </T>
                <Input
                  value={config[f.key] ?? ''}
                  onChangeText={(v) => setConfig({ ...config, [f.key]: v })}
                  placeholder={f.ph}
                  keyboardType={f.key === 'url' || f.key === 'baseUrl' ? 'url' : 'default'}
                />
              </View>
            ))}

            {spec.secrets.map((f) => {
              const filled = existing?.secretKeys?.includes(f.key);
              return (
                <View key={f.key} style={{ gap: 6 }}>
                  <T size={type.caption} color={t.ink2} weight="550">
                    {f.label}
                    {filled ? '（已填，留空不改）' : ''}
                  </T>
                  <Input
                    value={secrets[f.key] ?? ''}
                    onChangeText={(v) => setSecrets({ ...secrets, [f.key]: v })}
                    placeholder={f.ph}
                    secure={f.secure}
                    keyboardType={f.key === 'address' ? 'email-address' : 'default'}
                  />
                </View>
              );
            })}

            {kind === 'qqmail' ? (
              <View style={[s.hint, { backgroundColor: t.cardSunken }]}>
                <Icon name="information-circle-outline" size={14} color={t.ink3} />
                <T size={type.caption} color={t.ink3} lh={leading.caption} style={{ flex: 1 }}>
                  授权码不是 QQ 密码：在 QQ 邮箱「设置 → 账号 → IMAP/SMTP 服务」里生成。
                </T>
              </View>
            ) : null}

            <View style={[s.hint, { backgroundColor: t.cardSunken }]}>
              <Icon name="lock-closed-outline" size={14} color={t.ink3} />
              <T size={type.caption} color={t.ink3} lh={leading.caption} style={{ flex: 1 }}>
                凭据用电脑上的密钥加密存盘，单独拷走 data 目录也解不开。
              </T>
            </View>

            {err ? (
              <T size={type.caption} color={t.bad} lh={leading.caption}>
                {err}
              </T>
            ) : null}
          </ScrollView>

          <Row gap={gap.sm} style={{ marginTop: gap.md }}>
            {onDelete ? (
              <Pressable onPress={onDelete} hitSlop={8} style={{ paddingVertical: 12, paddingRight: gap.sm }}>
                <T size={type.sub} color={t.bad} weight="550">
                  删除
                </T>
              </Pressable>
            ) : null}
            <View style={{ flex: 1 }} />
            <Pressable
              disabled={busy}
              onPress={() => save(true)}
              style={[s.secondary, { borderColor: t.hairline, opacity: busy ? 0.45 : 1 }]}
            >
              <T size={type.sub} weight="550" color={t.ink}>
                测试连接
              </T>
            </Pressable>
            <Pressable
              disabled={busy}
              onPress={() => save(false)}
              style={[s.primary, { backgroundColor: t.primaryBg, opacity: busy ? 0.45 : 1 }]}
            >
              <T size={type.sub} weight="600" color={t.primaryInk}>
                {busy ? '保存中…' : existing ? '保存' : '连接'}
              </T>
            </Pressable>
          </Row>
        </View>
      </View>
    </Modal>
  );
}

function ScopeRow({
  label,
  hint,
  on,
  onPress,
}: {
  label: string;
  hint: string;
  on: boolean;
  onPress: () => void;
}) {
  const t = useTheme();
  return (
    <Pressable
      onPress={onPress}
      accessibilityRole="radio"
      accessibilityState={{ selected: on }}
      style={[s.scopeRow, on && { backgroundColor: t.cardSunken }]}
    >
      <RadioDot on={on} size={19} />
      <View style={{ flex: 1 }}>
        <T size={type.sub} weight={on ? '600' : '550'}>
          {label}
        </T>
        <T size={11.5} color={t.ink3} lh={16} style={{ marginTop: 1 }}>
          {hint}
        </T>
      </View>
    </Pressable>
  );
}

/* ============================================================
   添加连接器
   ============================================================ */

function AddSheet({
  visible,
  kinds,
  labels,
  onPick,
  onClose,
}: {
  visible: boolean;
  kinds: string[];
  labels: Record<string, string>;
  onPick: (kind: string) => void;
  onClose: () => void;
}) {
  const t = useTheme();
  return (
    <Modal visible={visible} transparent animationType="fade" onRequestClose={onClose}>
      <View style={{ flex: 1, justifyContent: 'flex-end' }}>
        <Pressable style={[StyleSheet.absoluteFill, { backgroundColor: t.scrim }]} onPress={onClose} />
        <View style={[s.form, { backgroundColor: t.card }]}>
          <Row style={{ justifyContent: 'space-between', marginBottom: gap.md }}>
            <T size={type.title} weight="650">
              添加连接器
            </T>
            <Pressable onPress={onClose} hitSlop={12} accessibilityLabel="关闭">
              <Icon name="close" size={20} color={t.ink3} />
            </Pressable>
          </Row>
          <T size={type.caption} color={t.ink3} lh={leading.caption} style={{ marginBottom: gap.sm }}>
            同一个服务可以加多个连接器（比如两个 GitHub 账号），在表单里改个名字区分。
          </T>
          {kinds.map((k, i) => {
            const spec = specOf(k);
            return (
              <View key={k}>
                {i > 0 ? <Divider /> : null}
                <Pressable onPress={() => onPick(k)} style={s.addRow} accessibilityRole="button">
                  <Brick icon={spec.icon} tint={spec.tint(t)} />
                  <View style={{ flex: 1 }}>
                    <T size={type.body} weight="550">
                      {spec.label}
                    </T>
                    <T size={type.caption} color={t.ink3} style={{ marginTop: 2 }}>
                      {labels[k] ? `服务类型 · ${labels[k]}` : '服务类型'}
                    </T>
                  </View>
                  <Icon name="chevron-forward" size={16} color={t.ink4} />
                </Pressable>
              </View>
            );
          })}
        </View>
      </View>
    </Modal>
  );
}

const s = StyleSheet.create({
  page: { paddingHorizontal: PAGE_X, paddingTop: gap.xs, paddingBottom: 100 },

  hero: {
    borderRadius: 28,
    paddingHorizontal: 18,
    paddingTop: 17,
    paddingBottom: 15,
    overflow: 'hidden',
  },
  heroShield: {
    position: 'absolute',
    top: 16,
    right: 16,
    width: 34,
    height: 34,
    borderRadius: 17,
    alignItems: 'center',
    justifyContent: 'center',
  },
  heroRule: { height: StyleSheet.hairlineWidth, marginTop: 14, marginBottom: 10 },

  brick: {
    width: 44,
    height: 44,
    borderRadius: radius.icon,
    alignItems: 'center',
    justifyContent: 'center',
    overflow: 'hidden',
  },
  brickGloss: {
    position: 'absolute',
    top: 0,
    left: 0,
    right: 0,
    height: 1.5,
    opacity: 0.34,
  },
  dot: { width: 6, height: 6, borderRadius: 3 },

  connectBtn: {
    borderRadius: radius.pill,
    paddingHorizontal: 17,
    paddingVertical: 9,
    minWidth: 64,
    alignItems: 'center',
  },

  dashed: {
    marginTop: 4,
    height: 56,
    borderRadius: radius.card,
    borderWidth: 1.2,
    borderStyle: 'dashed',
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 6,
  },

  note: {
    flexDirection: 'row',
    gap: gap.sm,
    alignItems: 'flex-start',
    borderRadius: radius.card,
    padding: 14,
    marginTop: gap.md,
  },
  noteIcon: {
    width: 30,
    height: 30,
    borderRadius: 15,
    alignItems: 'center',
    justifyContent: 'center',
  },

  form: {
    borderTopLeftRadius: 26,
    borderTopRightRadius: 26,
    paddingHorizontal: gap.lg,
    paddingTop: 18,
    paddingBottom: 28,
    maxHeight: '90%',
  },
  scopeRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: gap.sm,
    borderRadius: radius.bubble,
    paddingHorizontal: 11,
    paddingVertical: 10,
  },
  hint: { flexDirection: 'row', gap: 7, alignItems: 'flex-start', borderRadius: radius.md, padding: gap.sm },

  secondary: {
    height: 44,
    borderRadius: radius.control,
    borderWidth: 1,
    paddingHorizontal: gap.lg,
    alignItems: 'center',
    justifyContent: 'center',
  },
  primary: {
    height: 44,
    borderRadius: radius.control,
    paddingHorizontal: gap.xl,
    alignItems: 'center',
    justifyContent: 'center',
  },

  addRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: gap.md,
    paddingVertical: 10,
  },
});
