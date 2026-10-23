import React, { useEffect, useState } from 'react';
import { Platform, Pressable, StyleSheet, TextInput, View } from 'react-native';
import { Icon, Row, T } from './ui';
import { getBaseURL, humanError } from '../lib/api';
import { computerConnected, connectComputer, disconnectComputer } from '../lib/session';
import { useTheme } from '../theme/theme';
import { gap, leading, radius, type } from '../theme/tokens';

/**
 * 连接电脑（个人页里的可选能力，不是登录入口）。
 *
 * 为什么单独一块、而不是登录页的第二个 tab：
 *   - 登录是「必须做的事」，连接电脑是「想要更多能力时才做的事」。
 *   - 不连接也能聊天、看记录；连接后才能操作电脑上的工作区。
 *     把两者并排放，会让用户以为不配对就用不了 App。
 *
 * 连接后账号令牌仍然保留，两套令牌共存：
 *   账号令牌 → 会话 / 团队数据（PC 与手机同步）
 *   设备令牌 → 代表「这台手机被允许动那台电脑」
 */
export function ConnectComputer({ onChange }: { onChange?: () => void }) {
  const t = useTheme();
  const [connected, setConnected] = useState(computerConnected());
  const [open, setOpen] = useState(false);
  const [digits, setDigits] = useState<string[]>(['', '', '', '', '', '']);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');

  useEffect(() => {
    setConnected(computerConnected());
  }, []);

  const code = digits.join('');

  function setDigit(i: number, v: string) {
    const clean = v.replace(/\D/g, '');
    if (!clean) {
      setDigits((d) => d.map((x, k) => (k === i ? '' : x)));
      return;
    }
    if (clean.length > 1) {
      const next = clean.slice(0, 6).split('');
      setDigits(Array.from({ length: 6 }, (_, k) => next[k] ?? ''));
      return;
    }
    setDigits((d) => d.map((x, k) => (k === i ? clean : x)));
  }

  async function connect() {
    setErr('');
    if (!/^\d{6}$/.test(code)) return setErr('配对码是 6 位数字');
    setBusy(true);
    try {
      await connectComputer(
        getBaseURL(),
        code,
        Platform.OS === 'ios' ? 'iPhone' : 'Android',
        Platform.OS === 'ios' ? 'ios' : 'android',
      );
      setConnected(true);
      setOpen(false);
      setDigits(['', '', '', '', '', '']);
      onChange?.();
    } catch (e) {
      setErr(humanError(e));
    } finally {
      setBusy(false);
    }
  }

  async function disconnect() {
    setBusy(true);
    setErr('');
    try {
      await disconnectComputer();
      setConnected(false);
      onChange?.();
    } catch (e) {
      setErr(humanError(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <View style={[s.card, { backgroundColor: t.card, borderColor: t.hairline }]}>
      <Pressable
        onPress={() => (connected ? undefined : setOpen((v) => !v))}
        accessibilityRole="button"
        accessibilityLabel={connected ? '已连接电脑' : '连接电脑'}
        style={({ pressed }) => [{ opacity: pressed && !connected ? 0.7 : 1 }]}
      >
        <Row gap={gap.sm}>
          <View
            style={[
              s.icon,
              { backgroundColor: connected ? t.okSoft : t.cardSunken },
            ]}
          >
            <Icon
              name={connected ? 'desktop' : 'desktop-outline'}
              size={19}
              color={connected ? t.ok : t.slate}
            />
          </View>
          <View style={{ flex: 1 }}>
            <Row gap={6}>
              <T size={type.sub} weight="600">
                {connected ? '已连接电脑' : '连接电脑'}
              </T>
              {connected ? (
                <View style={[s.badge, { backgroundColor: t.okSoft }]}>
                  <T size={9.5} color={t.ok} weight="600">
                    可控制工作区
                  </T>
                </View>
              ) : null}
            </Row>
            <T size={type.micro} color={t.ink3} lh={leading.micro} style={{ marginTop: 2 }}>
              {connected
                ? '这台手机可以操作电脑上已绑定的工作区。'
                : '连接后，这台手机才能直接让角色读写电脑上的工作区。不连接也能正常聊天。'}
            </T>
          </View>
          {connected ? null : (
            <Icon name={open ? 'chevron-up' : 'chevron-forward'} size={14} color={t.ink4} />
          )}
        </Row>
      </Pressable>

      {!connected && open ? (
        <View style={{ marginTop: gap.sm, gap: gap.xs }}>
          <T size={type.micro} color={t.ink3} lh={leading.micro}>
            在电脑上打开纸团 → 设置 → 配对新设备，得到 6 位码。5 分钟内有效、只能用一次。
          </T>
          <Row gap={6}>
            {digits.map((d, i) => (
              <View
                key={i}
                style={[
                  s.digitBox,
                  {
                    backgroundColor: t.cardSunken,
                    borderColor: d ? t.ink : 'transparent',
                    borderWidth: d ? 1.5 : 0,
                  },
                ]}
              >
                <TextInput
                  value={d}
                  onChangeText={(v) => setDigit(i, v)}
                  keyboardType="number-pad"
                  maxLength={6}
                  style={[s.digit, { color: t.ink }]}
                  placeholderTextColor={t.ink4}
                />
              </View>
            ))}
          </Row>
          {err ? (
            <T size={type.micro} color={t.bad} lh={leading.micro}>
              {err}
            </T>
          ) : null}
          <Pressable
            onPress={connect}
            disabled={busy || code.length !== 6}
            accessibilityRole="button"
            style={({ pressed }) => [
              s.connectBtn,
              {
                backgroundColor: t.primaryBg,
                opacity: busy || code.length !== 6 ? 0.4 : pressed ? 0.85 : 1,
              },
            ]}
          >
            <T size={type.sub} weight="600" color={t.primaryInk}>
              {busy ? '正在连接…' : '连接'}
            </T>
          </Pressable>
        </View>
      ) : null}

      {connected ? (
        <Pressable
          onPress={disconnect}
          disabled={busy}
          accessibilityRole="button"
          style={({ pressed }) => [s.disconnect, { opacity: pressed || busy ? 0.6 : 1 }]}
        >
          <T size={type.micro} color={t.ink3}>
            {busy ? '正在断开…' : '断开这台手机的连接'}
          </T>
        </Pressable>
      ) : null}
    </View>
  );
}

const s = StyleSheet.create({
  card: {
    borderRadius: radius.card,
    borderWidth: StyleSheet.hairlineWidth,
    padding: 14,
  },
  icon: {
    width: 38,
    height: 38,
    borderRadius: 12,
    alignItems: 'center',
    justifyContent: 'center',
  },
  badge: { paddingHorizontal: 6, paddingVertical: 2, borderRadius: 6 },
  digitBox: {
    flex: 1,
    height: 46,
    borderRadius: 12,
    alignItems: 'center',
    justifyContent: 'center',
  },
  digit: {
    fontSize: 19,
    fontWeight: '600',
    textAlign: 'center',
    width: '100%',
    padding: 0,
  },
  connectBtn: {
    height: 42,
    borderRadius: radius.control,
    alignItems: 'center',
    justifyContent: 'center',
    marginTop: 2,
  },
  disconnect: { marginTop: gap.sm, paddingTop: gap.xs },
});
