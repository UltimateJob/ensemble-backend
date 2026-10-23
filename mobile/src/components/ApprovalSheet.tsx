import React from 'react';
import { Modal, StyleSheet, View } from 'react-native';
import { Button, Icon, Row, T } from './ui';
import { useTheme } from '../theme/theme';
import { gap, leading, radius, type } from '../theme/tokens';

/**
 * 审批卡点弹窗。
 *
 * Agent 要执行有副作用的操作时，把「它打算做什么、影响范围、风险」
 * 摆在你面前，由你点头。三个动作里「允许本次」是主按钮，
 * 「本任务内同类都允许」是次按钮——连续跑五条命令时不该点五次。
 */
export function ApprovalSheet({
  visible,
  tool,
  summary,
  risk,
  reason,
  onAllow,
  onDeny,
  onAllowRun,
}: {
  visible: boolean;
  tool: string;
  summary: string;
  risk: string;
  reason?: string;
  onAllow: () => void;
  onDeny: () => void;
  onAllowRun?: () => void;
}) {
  const t = useTheme();
  const s = mkStyles(t);
  if (!visible) return null;

  const riskLabel = RISK_LABEL[risk] ?? risk;
  const danger = risk === 'delete' || risk === 'privilege' || risk === 'critical';

  return (
    <Modal visible transparent animationType="fade" onRequestClose={onDeny}>
      <View style={s.scrim}>
        <View style={s.card}>
          <View style={[s.badge, danger && { backgroundColor: t.badSoft }]}>
            <Icon
              name={danger ? 'warning-outline' : 'shield-checkmark-outline'}
              size={20}
              color={danger ? t.bad : t.accent}
            />
          </View>

          <T size={type.title} weight="600" align="center" style={{ marginTop: gap.sm }}>
            需要你的审批
          </T>
          <T
size={type.sub}
            color={t.ink3}
            align="center"
            lh={leading.sub}
            style={{ marginTop: 4 }}
          >
            Agent 准备执行一个操作，需要你确认
          </T>

          <View style={s.block}>
            <Row style={{ justifyContent: 'space-between', marginBottom: 7 }}>
              <T size={type.caption} color={t.ink3} weight="500">
                {tool}
              </T>
              <Row gap={4}>
                <View
                  style={[
                    s.riskDot,
                    { backgroundColor: danger ? t.bad : t.warn },
                  ]}
                />
                <T
size={type.caption}
                  color={danger ? t.bad : t.warn}
                  weight="600"
                >
                  {riskLabel}
                </T>
              </Row>
            </Row>
            <T size={type.sub} lh={leading.sub}>
              {summary}
            </T>
          </View>

          {reason ? (
            <T size={type.caption} color={t.ink3} lh={leading.caption} style={{ marginTop: gap.xs }}>
              {reason}
            </T>
          ) : null}

          <Row gap={6} align="flex-start" style={{ marginTop: gap.sm }}>
            <Icon name="information-circle-outline" size={13} color={t.ink4} style={{ marginTop: 1 }} />
            <T size={type.caption} color={t.ink4} lh={leading.caption} style={{ flex: 1 }}>
              高危操作需要人工确认，避免 Agent 自主误操作。
            </T>
          </Row>

          <View style={{ gap: gap.xs, marginTop: gap.lg }}>
            <Button label="允许本次执行" onPress={onAllow} full />
            {onAllowRun ? (
              <Button label="本任务内同类都允许" variant="secondary" onPress={onAllowRun} full />
            ) : null}
            <Button label="拒绝" variant="ghost" onPress={onDeny} full />
          </View>
        </View>
      </View>
    </Modal>
  );
}

const RISK_LABEL: Record<string, string> = {
  read: '只读',
  write: '写入',
  network: '联网',
  delete: '删除',
  privilege: '提权',
  'git-push': '推送代码',
  critical: '高危',
};

const mkStyles = (t: any) => StyleSheet.create({
  scrim: {
    flex: 1,
    backgroundColor: t.scrim,
    alignItems: 'center',
    justifyContent: 'center',
    padding: gap.xl,
  },
  card: {
    width: '100%',
    maxWidth: 400,
    backgroundColor: t.card,
    borderRadius: radius.card,
    padding: gap.lg,
    shadowColor: t.shadowColor, shadowOpacity: 0.4, shadowRadius: 32, shadowOffset: { width: 0, height: 14 }, elevation: 16,
  },
  badge: {
    alignSelf: 'center',
    width: 52,
    height: 52,
    borderRadius: 18,
    backgroundColor: t.accentSoft,
    alignItems: 'center',
    justifyContent: 'center',
  },
  block: {
    marginTop: gap.md,
    backgroundColor: t.cardSunken,
    borderRadius: radius.md,
    padding: gap.sm,
  },
  riskDot: { width: 5, height: 5, borderRadius: 3 },
});
