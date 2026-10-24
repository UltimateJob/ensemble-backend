import { LinearGradient } from 'expo-linear-gradient';
import { useLocalSearchParams, useRouter } from 'expo-router';
import React, { useCallback, useEffect, useState } from 'react';
import { Alert, Modal, Pressable, ScrollView, StyleSheet, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { Avatar } from '../../src/components/Avatar';
import { Canvas } from '../../src/components/Canvas';
import { FadeInUp } from '../../src/components/motion';
import { Surface } from '../../src/components/Surface';
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
} from '../../src/components/ui';
import { del, get, getBaseURL, humanError, patch, post, setBaseURL } from '../../src/lib/api';
import { signOut } from '../../src/lib/session';
import { cacheSize, clearCache } from '../../src/lib/storage';
import { useApp } from '../../src/lib/store';
import { isDeviceOnline, paperNo, type Device, type User } from '../../src/lib/types';
import { useTheme, useThemePref, type FontScale } from '../../src/theme/theme';
import { PAGE_X, gap, leading, radius, type } from '../../src/theme/tokens';

/**
 * 设置（规范 §6.7 / §6.8 / §7，设计图 v18-phone8）。
 *
 * 学 iOS / 元宝的平铺分组：分组头是落在画布上的灰字 + 右上角英文微标签，
 * 每组是一个白块，块内行与行之间只有一道发丝线，末行无线。
 *
 * 通用组的第一行是「默认权限级别」——权限是给人定级，不是逐项打勾，
 * 所以它是三级单选而不是一串开关（§7）。
 */

interface PermissionLevel {
  key: 'ask' | 'approval' | 'full';
  label: string;
  hint: string;
  warn?: boolean;
}

const LEVELS: PermissionLevel[] = [
  { key: 'ask', label: '需要审批', hint: '执行任何操作前，都先征得你同意' },
  { key: 'approval', label: '工作区内修改', hint: '可自由读写工作区；联网、删除、区外操作仍需审批' },
  { key: 'full', label: '完全访问权限', hint: '不再询问，可执行包括删除在内的全部操作', warn: true },
];

const MODES = [
  { key: 'auto' as const, label: '跟随系统', hint: '跟随手机的深浅色设置' },
  { key: 'light' as const, label: '浅色', hint: '始终使用浅色' },
  { key: 'dark' as const, label: '深色', hint: '始终使用深色' },
];

const FONT_SIZES: { key: FontScale; label: string; hint: string }[] = [
  { key: 'small', label: '小', hint: '一屏能看更多' },
  { key: 'normal', label: '标准', hint: '推荐' },
  { key: 'large', label: '大', hint: '更好读，占位更多' },
];

export default function Settings() {
  const t = useTheme();
  const router = useRouter();
  const user = useApp((s) => s.user);
  const setUser = useApp((s) => s.setUser);
  const pref = useThemePref((s) => s.pref);
  const setPref = useThemePref((s) => s.setPref);
  const fontScale = useThemePref((s) => s.fontScale);
  const setFontScale = useThemePref((s) => s.setFontScale);

  const [devices, setDevices] = useState<Device[]>([]);
  // 加载完成前不能断言「未连接」——那会在打开瞬间闪一下错误状态。
  const [devicesReady, setDevicesReady] = useState(false);
  const [err, setErr] = useState('');

  const [level, setLevel] = useState<PermissionLevel['key']>('approval');
  const [notify, setNotify] = useState(true);
  const [approvalNotify, setApprovalNotify] = useState(true);

  const [cache, setCache] = useState('—');

  // 支持从别处深链直接打开某个面板：/settings?sheet=devices
  // 「我」页的「我的设备」就是走这条，省得用户进来还要再找一次。
  const params = useLocalSearchParams<{ sheet?: string }>();
  const [sheet, setSheet] = useState<'theme' | 'level' | 'font' | 'devices' | 'server' | 'account' | null>(
    (params.sheet as any) ?? null,
  );
  const [host, setHost] = useState(getBaseURL());
  const [hostBusy, setHostBusy] = useState(false);

  const [hasPw, setHasPw] = useState(false);
  const [oldPw, setOldPw] = useState('');
  const [newPw, setNewPw] = useState('');
  const [newPw2, setNewPw2] = useState('');
  const [pwMsg, setPwMsg] = useState('');
  const [pwBusy, setPwBusy] = useState(false);

  const loadDevices = useCallback(async () => {
    try {
      const list = await get<Device[]>('/v1/devices');
      setDevices(Array.isArray(list) ? list : []);
      setDevicesReady(true);
    } catch (e) {
      setErr(humanError(e));
    }
  }, []);

  useEffect(() => {
    loadDevices();
    void formatCacheSize().then(setCache);
  }, [loadDevices]);

  useEffect(() => {
    get<any>('/v1/me')
      .then((d) => {
        setHasPw(!!d?.hasPassword);
        const p: Record<string, string> = d?.preferences ?? {};
        if (p.defaultPermission === 'ask' || p.defaultPermission === 'approval' || p.defaultPermission === 'full') {
          setLevel(p.defaultPermission);
        }
        if (p.notify === 'false') setNotify(false);
        if (p.notify === 'true') setNotify(true);
        if (p.approvalNotify === 'false') setApprovalNotify(false);
        if (p.approvalNotify === 'true') setApprovalNotify(true);
        if (p.fontScale === 'small' || p.fontScale === 'normal' || p.fontScale === 'large') {
          setFontScale(p.fontScale);
        }
        if (p.theme === 'auto' || p.theme === 'light' || p.theme === 'dark') setPref(p.theme);
        const u = (d?.user ?? d) as User | undefined;
        if (u?.id) setUser(u);
      })
      .catch(() => {
        /* 未登录或旧服务端时按默认值展示 */
      });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  /** 偏好落盘失败不打扰用户：界面已经先变了，下次进来再对齐。 */
  function savePref(key: string, value: string) {
    void patch('/v1/me', { preferences: { [key]: value } }).catch(() => {});
  }

  async function savePassword() {
    setPwMsg('');
    if (newPw.length < 8 || newPw.length > 72) return setPwMsg('密码长度需在 8-72 个字符之间');
    if (newPw !== newPw2) return setPwMsg('两次输入的新密码不一致');
    setPwBusy(true);
    try {
      await post('/v1/me/password', { oldPassword: oldPw, newPassword: newPw });
      setHasPw(true);
      setOldPw('');
      setNewPw('');
      setNewPw2('');
      setPwMsg('已保存');
    } catch (e) {
      setPwMsg(humanError(e));
    } finally {
      setPwBusy(false);
    }
  }

  async function toggleApproval(d: Device, v: boolean) {
    // 先改本地，失败再回滚：开关不该等一次网络往返才动。
    setDevices((l) => l.map((x) => (x.id === d.id ? { ...x, requireApproval: v } : x)));
    try {
      const u = await patch<Device>(`/v1/devices/${d.id}`, { requireApproval: v });
      setDevices((l) => l.map((x) => (x.id === d.id ? u : x)));
    } catch (e) {
      setDevices((l) => l.map((x) => (x.id === d.id ? d : x)));
      setErr(humanError(e));
    }
  }

  function revoke(d: Device) {
    Alert.alert('吊销设备', `「${d.name}」将立即失去访问权。`, [
      { text: '取消', style: 'cancel' },
      {
        text: '吊销',
        style: 'destructive',
        onPress: async () => {
          await del(`/v1/devices/${d.id}`).catch(() => {});
          loadDevices();
        },
      },
    ]);
  }

  const activeDevices = devices.filter((d) => !d.revokedAt);
  // 配过对不等于在线：要看设备最后一次心跳。电脑关机时应显示「已连接 · 离线」。
  const onlineDevice = activeDevices.find((d) => isDeviceOnline(d));
  /** 「执行前审批」作用于当前这台设备；有多台时取最近活跃的那台。 */
  const currentDevice = onlineDevice ?? activeDevices[0];
  const computerValue = !devicesReady
    ? '…'
    : activeDevices.length
      ? onlineDevice
        ? '已连接 · 在线'
        : '已连接 · 离线'
      : '未连接';
  const levelLabel = LEVELS.find((l) => l.key === level)?.label ?? '工作区内修改';
  const paperId = paperNo(user?.id);
  const planLabel = user?.plan === 'pro' ? 'PRO 版' : user?.plan === 'team' ? '团队版' : '免费版';
  const hostLabel = host.replace(/^https?:\/\//, '') || '未设置';

  return (
    <Canvas>
      <SafeAreaView style={{ flex: 1 }} edges={['top', 'bottom']}>
        <NavBar title="设置" onBack={() => router.back()} />
        <ScrollView contentContainerStyle={s.page} showsVerticalScrollIndicator={false}>
          {err ? (
            <T size={type.caption} color={t.bad} style={{ marginBottom: gap.sm }}>
              {err}
            </T>
          ) : null}

          {/* 顶部用户卡：深色英雄卡（附录 A2），明暗两套都用同一个深底色 */}
          <FadeInUp>
            <Pressable onPress={() => setSheet('account')} accessibilityRole="button" accessibilityLabel="账号与安全">
              <View style={[s.hero, { backgroundColor: t.heroBg }]}>
                <LinearGradient
                  colors={grad(t.gradient)}
                  start={{ x: 0, y: 0 }}
                  end={{ x: 1, y: 1 }}
                  style={[StyleSheet.absoluteFill, { opacity: t.dark ? 0.24 : 0.16 }]}
                />
                <Avatar name={user?.name ?? '纸团'} size={48} />
                <View style={{ flex: 1, minWidth: 0 }}>
                  <Row gap={8}>
                    <T size={type.title} weight="600" color={t.white} numberOfLines={1}>
                      {user?.name ?? '纸团'}
                    </T>
                    {/* 只有付费套餐才挂徽章。以前无条件渲染，免费账号也顶着
                        「PRO」而下一行写着「免费版」，自相矛盾。 */}
                    {user?.plan === 'pro' || user?.plan === 'team' ? (
                      <LinearGradient
                        colors={grad(t.gradient)}
                        start={{ x: 0, y: 0 }}
                        end={{ x: 1, y: 1 }}
                        style={s.pro}
                      >
                        <T size={10.5} weight="700" color={t.white} tracking={1}>
                          {user?.plan === 'team' ? 'TEAM' : 'PRO'}
                        </T>
                      </LinearGradient>
                    ) : null}
                  </Row>
                  <T size={type.caption} color="rgba(255,255,255,0.62)" style={{ marginTop: 3 }} numberOfLines={1}>
                    纸团号 {paperId} · {planLabel}
                  </T>
                </View>
                <Icon name="chevron-forward" size={18} color="rgba(255,255,255,0.55)" />
              </View>
            </Pressable>
          </FadeInUp>

          {/* ---------------- 通用：首位是默认权限级别（§7） ---------------- */}
          <GroupHead label="通用" en="GENERAL" />
          <Surface outerRadius={radius.card}>
            <SettingRow
              icon="shield-checkmark"
              tint={t.brickBg}
              label="默认权限级别"
              value={levelLabel}
              onPress={() => setSheet('level')}
            />
            <Divider />
            <SettingRow
              icon="notifications"
              tint={t.brickBg}
              label="消息通知"
              right={
                <Toggle
                  on={notify}
                  onChange={(v) => {
                    setNotify(v);
                    savePref('notify', v ? 'true' : '');
                  }}
                />
              }
            />
            <Divider />
            <SettingRow
              icon="shield-half"
              tint={t.brickBg}
              label="审批提醒"
              last
              right={
                <Toggle
                  on={approvalNotify}
                  onChange={(v) => {
                    setApprovalNotify(v);
                    savePref('approvalNotify', v ? 'true' : '');
                  }}
                />
              }
            />
          </Surface>

          {/* ---------------- 外观与显示 ---------------- */}
          <GroupHead label="外观与显示" en="APPEARANCE" />
          <Surface outerRadius={radius.card}>
            <SettingRow
              icon="moon"
              tint={t.dark ? t.brickBg : t.ink}
              iconTint={t.dark ? t.ink : t.white}
              label="深浅模式"
              value={pref === 'auto' ? '跟随系统' : pref === 'light' ? '浅色' : '深色'}
              onPress={() => setSheet('theme')}
            />
            <Divider />
            <SettingRow
              icon="text"
              tint={t.brickBg}
              label="字体大小"
              value={FONT_SIZES.find((f) => f.key === fontScale)?.label ?? '标准'}
              last
              onPress={() => setSheet('font')}
            />
          </Surface>

          {/* ---------------- 设备与数据 ---------------- */}
          <GroupHead label="设备与数据" en="DEVICE & DATA" />
          <Surface outerRadius={radius.card}>
            <SettingRow
              icon="desktop"
              tint={t.brickBg}
              label="我的电脑"
              value={computerValue}
              onPress={() => setSheet('devices')}
            />
            <Divider />
            <SettingRow
              icon="shield-checkmark-outline"
              tint={t.brickBg}
              label="执行前审批"
              value={
                currentDevice ? (currentDevice.requireApproval ? '总是先问我' : '不强制审批') : '未连接电脑'
              }
              onPress={() => {
                // 这是「这台手机远程操作电脑时要不要先问我」——是设备属性，
                // 不是账号偏好，所以写 devices 接口而不是 preferences。
                if (!currentDevice) return setSheet('devices');
                void toggleApproval(currentDevice, !currentDevice.requireApproval);
              }}
            />
            <Divider />
            <SettingRow
              icon="server"
              tint={t.brickBg}
              label="清除缓存"
              value={cache}
              onPress={() =>
                Alert.alert('清除缓存', `将清掉 ${cache} 本地记录，不会退出登录，也不影响设备配对。`, [
                  { text: '取消', style: 'cancel' },
                  {
                    text: '清除',
                    style: 'destructive',
                    onPress: async () => {
                      await clearCache();
                      setCache(await formatCacheSize());
                    },
                  },
                ])
              }
            />
            <Divider />
            <SettingRow
              icon="git-network-outline"
              tint={t.brickBg}
              label="服务器地址"
              value={hostLabel}
              last
              onPress={() => {
                setHost(getBaseURL());
                setSheet('server');
              }}
            />
          </Surface>

          {/* ---------------- 关于 ---------------- */}
          <GroupHead label="关于" en="ABOUT" />
          <Surface outerRadius={radius.card}>
            <SettingRow
              icon="information-circle"
              tint={t.brickBg}
              label="版本"
              value="v1.1.1"
              onPress={() => Alert.alert('纸团', '当前已是最新版本。')}
            />
            <Divider />
            <SettingRow
              icon="information-circle"
              tint={t.brickBg}
              label="关于纸团"
              last
              onPress={() => Alert.alert('关于纸团', '把一件事，揉成一群人。\n工作区与文件留在你自己的电脑上；模型推理由所选模型服务商提供。')}
            />
          </Surface>

          {/* 退出登录：红字独立成块（§6.7 危险文字按钮） */}
          <Pressable
            onPress={() =>
              Alert.alert('退出登录', '退出后需要重新配对或登录。', [
                { text: '取消', style: 'cancel' },
                {
                  text: '退出',
                  style: 'destructive',
                  onPress: async () => {
                    await signOut();
                    router.replace('/');
                  },
                },
              ])
            }
            style={{ marginTop: gap.lg }}
            accessibilityRole="button"
          >
            <Surface outerRadius={radius.card} style={{ paddingVertical: 15 }}>
              <T size={type.body} color={t.bad} weight="600" align="center">
                退出登录
              </T>
            </Surface>
          </Pressable>
        </ScrollView>

        {/* ---------------- 深浅模式 ---------------- */}
        <Sheet
          visible={sheet === 'theme'}
          title="深浅模式"
          subtitle="门面页的分组、卡片与发丝线会随主题整体切换。"
          onClose={() => setSheet(null)}
        >
          {MODES.map((m) => (
            <ChoiceRow
              key={m.key}
              label={m.label}
              hint={m.hint}
              on={pref === m.key}
              onPress={() => {
                setPref(m.key);
                savePref('theme', m.key);
                setSheet(null);
              }}
            />
          ))}
        </Sheet>

        {/* ---------------- 默认权限级别（三级单选） ---------------- */}
        <Sheet
          visible={sheet === 'level'}
          title="默认权限级别"
          subtitle="新建团队时带入这个级别；单个成员仍可在成员详情里单独设置。"
          onClose={() => setSheet(null)}
        >
          {LEVELS.map((l) => (
            <ChoiceRow
              key={l.key}
              label={l.label}
              hint={l.hint}
              on={level === l.key}
              warn={l.warn}
              onPress={() => {
                setLevel(l.key);
                savePref('defaultPermission', l.key);
                setSheet(null);
              }}
            />
          ))}
        </Sheet>

        {/* ---------------- 字体大小 ---------------- */}
        <Sheet
          visible={sheet === 'font'}
          title="字体大小"
          subtitle="只影响文字。图标和间距不变，排版不会被撑歪。"
          onClose={() => setSheet(null)}
        >
          {FONT_SIZES.map((f) => (
            <ChoiceRow
              key={f.key}
              label={f.label}
              hint={f.hint}
              on={fontScale === f.key}
              onPress={() => {
                setFontScale(f.key);
                savePref('fontScale', f.key);
                setSheet(null);
              }}
            />
          ))}
        </Sheet>

        {/* ---------------- 我的电脑 / 设备 ---------------- */}
        <Sheet
          visible={sheet === 'devices'}
          title="我的电脑"
          subtitle="每台配对过的设备单独授权，吊销后立刻失效。"
          onClose={() => setSheet(null)}
        >
          {devices.length === 0 ? (
            <T size={type.sub} color={t.ink3} lh={leading.sub}>
              还没有配对的设备。在电脑上打开纸团 → 设置 → 配对新设备，得到 6 位码（5 分钟内有效、只能用一次）。
            </T>
          ) : (
            devices.map((d) => {
              const revoked = !!d.revokedAt;
              return (
                <View
                  key={d.id}
                  style={[
                    s.device,
                    { backgroundColor: t.cardSunken, borderColor: t.cardBorder },
                    revoked && { opacity: 0.55 },
                  ]}
                >
                  <Row gap={gap.sm}>
                    <View style={[s.deviceDot, { backgroundColor: revoked ? t.ink4 : t.okDot }]} />
                    <View style={{ flex: 1, minWidth: 0 }}>
                      <T size={type.sub} weight="600" numberOfLines={1}>
                        {d.name}
                      </T>
                      <T size={type.caption} color={t.ink3} numberOfLines={1} style={{ marginTop: 2 }}>
                        {d.platform} · {revoked ? '已吊销' : `最近活跃 ${ago(d.lastSeen)}`}
                      </T>
                    </View>
                    {revoked ? null : (
                      <Pressable onPress={() => revoke(d)} hitSlop={10}>
                        <T size={type.caption} color={t.bad} weight="600">
                          吊销
                        </T>
                      </Pressable>
                    )}
                  </Row>
                  {revoked ? null : (
                    <Row style={{ justifyContent: 'space-between', marginTop: gap.sm }}>
                      <View style={{ flex: 1, paddingRight: gap.sm }}>
                        <T size={type.caption} color={t.ink2}>
                          执行前需要审批
                        </T>
                        <T size={11} color={t.ink4} style={{ marginTop: 2 }}>
                          关掉后按权限级别直接执行
                        </T>
                      </View>
                      <Toggle on={d.requireApproval} onChange={(v) => toggleApproval(d, v)} />
                    </Row>
                  )}
                </View>
              );
            })
          )}
        </Sheet>

        {/* ---------------- 服务器地址（局域网必需） ---------------- */}
        <Sheet
          visible={sheet === 'server'}
          title="服务器地址"
          subtitle="手机连的是电脑上的纸团。首次配对会自动写入，换网络时可以手动改。"
          onClose={() => setSheet(null)}
        >
          <Input value={host} onChangeText={setHost} placeholder="192.168.1.5:8080" autoCapitalize="none" />
          <View style={{ marginTop: gap.md }}>
            <Pressable
              disabled={hostBusy}
              onPress={async () => {
                setHostBusy(true);
                setErr('');
                try {
                  await setBaseURL(host);
                  setHost(getBaseURL());
                  await loadDevices();
                  setSheet(null);
                } catch (e) {
                  setErr(humanError(e));
                } finally {
                  setHostBusy(false);
                }
              }}
              style={[s.primary, { backgroundColor: t.primaryBg, opacity: hostBusy ? 0.5 : 1 }]}
            >
              <T size={type.body} weight="600" color={t.primaryInk} align="center">
                {hostBusy ? '保存中…' : '保存地址'}
              </T>
            </Pressable>
          </View>
        </Sheet>

        {/* ---------------- 账号与安全 ---------------- */}
        <Sheet
          visible={sheet === 'account'}
          title="账号与安全"
          subtitle={hasPw ? '已设置密码。修改后旧密码立即失效，验证码登录不受影响。' : '设置密码后，可以用手机号/邮箱 + 密码登录。'}
          onClose={() => setSheet(null)}
        >
          <View style={{ gap: gap.xs }}>
            {hasPw ? <Input value={oldPw} onChangeText={setOldPw} placeholder="原密码" secure /> : null}
            <Input value={newPw} onChangeText={setNewPw} placeholder="新密码（8-72 位）" secure />
            <Input value={newPw2} onChangeText={setNewPw2} placeholder="再输一次新密码" secure />
            {pwMsg ? (
              <T size={type.caption} color={pwMsg === '已保存' ? t.ok : t.bad}>
                {pwMsg}
              </T>
            ) : null}
            <Pressable
              disabled={pwBusy}
              onPress={savePassword}
              style={[s.primary, { backgroundColor: t.primaryBg, opacity: pwBusy ? 0.5 : 1, marginTop: gap.xs }]}
            >
              <T size={type.body} weight="600" color={t.primaryInk} align="center">
                {pwBusy ? '保存中…' : hasPw ? '修改密码' : '设置密码'}
              </T>
            </Pressable>
          </View>
        </Sheet>
      </SafeAreaView>
    </Canvas>
  );
}

/* ============================================================
   分组与行
   ============================================================ */

/** 分组头：灰字直接落在画布上，右上角英文微标签（附录 A3）。 */
function GroupHead({ label, en }: { label: string; en: string }) {
  const t = useTheme();
  return (
    <Row style={{ justifyContent: 'space-between', marginTop: gap.xxl, marginBottom: gap.xs, paddingHorizontal: 4 }}>
      <T size={type.caption} color={t.ink3} weight="550">
        {label}
      </T>
      <T size={10.5} color={t.faint} weight="700" tracking={2.2}>
        {en}
      </T>
    </Row>
  );
}

/**
 * 设置行（§6.8）：44px 图标砖 + 标签 15.5/550 + 右侧值 + 箭头。
 * 行高靠 padding 13 撑，行间发丝线由调用处插入，末行无线。
 */
function SettingRow({
  icon,
  tint,
  iconTint,
  label,
  value,
  right,
  onPress,
  last,
}: {
  icon: IoniconName;
  tint: string;
  iconTint?: string;
  label: string;
  value?: string;
  right?: React.ReactNode;
  onPress?: () => void;
  last?: boolean;
}) {
  const t = useTheme();
  const body = (
    <View style={[s.row, last && { paddingBottom: 15 }]}>
      <IconBrick icon={icon} tint={tint} iconTint={iconTint} />
      {/* 标签可压缩、取值不压缩：取值通常是「未连接」「跟随系统」这类
          短词，截断后完全读不出意思；标签即使截断也还有图标和上下文
          兜底。两者都参与压缩时会互相抢，最后两边都读不全。 */}
      <T size={type.body} weight="550" numberOfLines={1} style={{ flexShrink: 1 }}>
        {label}
      </T>
      <View style={{ flex: 1, minWidth: 0 }} />
      {right}
      {value ? (
        <T size={type.sub} color={t.ink3} numberOfLines={1} style={{ flexShrink: 0, textAlign: 'right' }}>
          {value}
        </T>
      ) : null}
      {onPress ? <Icon name="chevron-forward" size={16} color={t.ink4} /> : null}
    </View>
  );
  if (!onPress) return body;
  return (
    <Pressable onPress={onPress} accessibilityRole="button" accessibilityLabel={label}>
      {body}
    </Pressable>
  );
}

/**
 * 44px 图标砖（附录 A5）：圆角 13、同色系双色渐变、顶部一条内高光。
 * 颜色一律由调用方从 useTheme() 取，页面里不出现裸色值。
 */
function IconBrick({
  icon,
  tint,
  iconTint,
}: {
  icon: IoniconName;
  tint: string;
  iconTint?: string;
}) {
  const t = useTheme();
  return (
    <LinearGradient
      colors={grad([tint, tint])}
      start={{ x: 0.1, y: 0 }}
      end={{ x: 0.9, y: 1 }}
      style={s.brick}
    >
      {/* 高光在深色下要收着，否则九个砖各带一块白斑 */}
      <View
        pointerEvents="none"
        style={[s.brickGloss, { backgroundColor: t.dark ? 'rgba(255,255,255,0.06)' : 'rgba(255,255,255,0.18)' }]}
      />
      <Icon name={icon} size={20} color={iconTint ?? t.brickInk} />
    </LinearGradient>
  );
}

/* ============================================================
   底部面板与单选行
   ============================================================ */

function Sheet({
  visible,
  title,
  subtitle,
  onClose,
  children,
}: {
  visible: boolean;
  title: string;
  subtitle?: string;
  onClose: () => void;
  children: React.ReactNode;
}) {
  const t = useTheme();
  return (
    <Modal visible={visible} transparent animationType="fade" onRequestClose={onClose}>
      <View style={{ flex: 1, justifyContent: 'flex-end' }}>
        <Pressable
          style={[StyleSheet.absoluteFill, { backgroundColor: t.scrim }]}
          onPress={onClose}
          accessibilityLabel="关闭"
        />
        <View style={[s.sheet, { backgroundColor: t.card }]}>
          <View style={[s.handle, { backgroundColor: t.track }]} />
          <T size={type.title} weight="600">
            {title}
          </T>
          {subtitle ? (
            <T size={type.caption} color={t.ink3} lh={leading.caption} style={{ marginTop: 4 }}>
              {subtitle}
            </T>
          ) : null}
          <ScrollView
            style={{ marginTop: gap.md, maxHeight: 460 }}
            contentContainerStyle={{ gap: gap.xs, paddingBottom: 2 }}
            showsVerticalScrollIndicator={false}
            keyboardShouldPersistTaps="handled"
          >
            {children}
          </ScrollView>
        </View>
      </View>
    </Modal>
  );
}

/** 单选行：选中行底 surfaceSoft、圆角 14（§6.8）。 */
function ChoiceRow({
  label,
  hint,
  on,
  warn,
  onPress,
}: {
  label: string;
  hint?: string;
  on: boolean;
  warn?: boolean;
  onPress: () => void;
}) {
  const t = useTheme();
  return (
    <Pressable
      onPress={onPress}
      accessibilityRole="radio"
      accessibilityState={{ selected: on }}
      style={[s.choice, on && { backgroundColor: t.cardSunken }]}
    >
      <RadioDot on={on} />
      <View style={{ flex: 1 }}>
        <Row gap={6}>
          <T size={type.body} weight={on ? '600' : '550'}>
            {label}
          </T>
          {warn ? (
            <View style={[s.warn, { backgroundColor: t.goldBg }]}>
              <T size={10.5} color={t.goldFg} weight="700" tracking={0.5}>
                谨慎授予
              </T>
            </View>
          ) : null}
        </Row>
        {hint ? (
          <T size={type.caption} color={t.ink3} lh={leading.caption} style={{ marginTop: 2 }}>
            {hint}
          </T>
        ) : null}
      </View>
    </Pressable>
  );
}

/* ============================================================
   工具
   ============================================================ */

/** 本地缓存大小的可读文案。真算一遍，不再写死一个数字骗人。 */
async function formatCacheSize(): Promise<string> {
  const bytes = await cacheSize();
  if (bytes < 1024) return `${bytes} B`;
  return `${(bytes / 1024).toFixed(1)} KB`;
}

function ago(iso: string) {
  if (!iso) return '—';
  const d = Date.now() - new Date(iso).getTime();
  if (d < 60000) return '刚刚';
  if (d < 3600000) return `${Math.floor(d / 60000)} 分钟前`;
  if (d < 86400000) return `${Math.floor(d / 3600000)} 小时前`;
  return `${Math.floor(d / 86400000)} 天前`;
}

const s = StyleSheet.create({
  page: { paddingHorizontal: PAGE_X, paddingTop: gap.xs, paddingBottom: 100 },

  hero: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: gap.md,
    borderRadius: radius.card,
    paddingHorizontal: 16,
    paddingVertical: 17,
    overflow: 'hidden',
  },
  pro: {
    borderRadius: radius.tag,
    paddingHorizontal: 7,
    paddingVertical: 2.5,
  },

  row: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: gap.md,
    paddingHorizontal: 15,
    paddingVertical: 13,
    minHeight: 62,
  },
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

  sheet: {
    borderTopLeftRadius: 26,
    borderTopRightRadius: 26,
    paddingHorizontal: gap.lg,
    paddingTop: 10,
    paddingBottom: 34,
  },
  handle: { width: 38, height: 4, borderRadius: 2, alignSelf: 'center', marginBottom: gap.md },
  choice: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: gap.md,
    borderRadius: radius.bubble,
    paddingHorizontal: 12,
    paddingVertical: 11,
  },
  warn: { borderRadius: radius.tag, paddingHorizontal: 6, paddingVertical: 1.5 },
  primary: { borderRadius: radius.control, paddingVertical: 14 },
  device: {
    borderRadius: radius.message,
    borderWidth: StyleSheet.hairlineWidth,
    padding: gap.md,
  },
  deviceDot: { width: 8, height: 8, borderRadius: 4 },
});
