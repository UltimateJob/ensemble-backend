import { useLocalSearchParams, useRouter } from 'expo-router';
import React, { useEffect, useState } from 'react';
import { ScrollView, StyleSheet, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { Canvas } from '../../src/components/Canvas';
import { Surface } from '../../src/components/Surface';
import { Button, Icon, Input, NavBar, Row, T } from '../../src/components/ui';
import { get, humanError, post } from '../../src/lib/api';
import { useTheme } from '../../src/theme/theme';
import { CARD_PAD, gap, leading, type } from '../../src/theme/tokens';

/**
 * 交给团队：后端负责规划、并行、验证、返工、评分。
 * 手机只提交目标并回到聊天里看结果；工作区必须先在电脑上绑定。
 */
export default function TeamTask() {
  const t = useTheme();
  const router = useRouter();
  const { convId } = useLocalSearchParams<{ convId?: string }>();
  const [task, setTask] = useState('');
  const [steps, setSteps] = useState('20');
  const [bound, setBound] = useState<boolean | null>(null);
  const [perm, setPerm] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');

  useEffect(() => {
    if (!convId) return;
    get<any>(`/v1/conversations/${convId}/activity`)
      .then((d) => {
        setBound(!!d?.workspace?.bound);
        setPerm(d?.defaultPermission || '');
      })
      .catch(() => setBound(false));
  }, [convId]);

  async function start() {
    if (!convId) return;
    if (!task.trim()) return setErr('先写清目标');
    setBusy(true);
    setErr('');
    try {
      await post(`/v1/conversations/${convId}/team`, {
        task: task.trim(),
        maxSteps: Math.max(4, Math.min(40, Number(steps) || 20)),
      });
      router.back();
    } catch (e) {
      setErr(humanError(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Canvas>
      <SafeAreaView style={{ flex: 1 }} edges={['top', 'bottom']}>
        <NavBar title="交给团队" onBack={() => router.back()} />
        <ScrollView contentContainerStyle={s.page} keyboardShouldPersistTaps="handled">
          <T size={type.sub} color={t.ink3} lh={leading.sub} style={{ marginBottom: gap.lg }}>
            架构角色先出计划，前后端按分工并行改代码，服务端跑验证；不通过会定向返工一轮，
            最后给出评分。整个过程在电脑上执行，关掉手机也不影响。
          </T>

          {bound === false ? (
            <Surface shell={4} outerRadius={22} style={{ marginBottom: gap.md }}>
              <View style={{ padding: CARD_PAD }}>
                <Row gap={gap.xs} align="flex-start">
                  <Icon name="alert-circle-outline" size={16} color={t.bad} style={{ marginTop: 1 }} />
                  <T size={type.sub} color={t.ink2} lh={leading.sub} style={{ flex: 1 }}>
                    这个团队还没绑定工作区。请在电脑上打开该团队 → 右上角文件夹图标，选择一个本地目录后再试。
                  </T>
                </Row>
              </View>
            </Surface>
          ) : null}

          <T size={type.caption} color={t.ink2} weight="600" style={{ marginBottom: 7 }}>
            目标
          </T>
          <Input
            value={task}
            onChangeText={setTask}
            placeholder="例如：给 /api/todos 加上 name 校验，前端显示错误；补测试并确保通过"
            multiline
            style={{ minHeight: 120, alignItems: 'flex-start' }}
          />

          <T size={type.caption} color={t.ink2} weight="600" style={{ marginTop: gap.lg, marginBottom: 7 }}>
            每步上限
          </T>
          <Input value={steps} onChangeText={setSteps} keyboardType="numeric" placeholder="20" />

          <T size={type.caption} color={t.ink3} style={{ marginTop: gap.sm }}>
            当前默认权限：{perm || '跟随角色设置'}。团队执行需要「自动接受编辑」或「完全访问」。
          </T>

          {err ? (
            <T size={type.sub} color={t.bad} style={{ marginTop: gap.sm }}>{err}</T>
          ) : null}

          <Button
            label={busy ? '正在启动…' : '开始协作'}
            full
            disabled={busy || bound === false}
            style={{ marginTop: gap.lg }}
            onPress={start}
          />
        </ScrollView>
      </SafeAreaView>
    </Canvas>
  );
}

const s = StyleSheet.create({
  page: { padding: gap.lg, paddingBottom: 48 },
});
