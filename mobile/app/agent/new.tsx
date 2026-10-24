import { useRouter } from 'expo-router';
import React, { useState } from 'react';
import {
  ActivityIndicator,
  Alert,
  Image,
  KeyboardAvoidingView,
  Platform,
  Pressable,
  ScrollView,
  StyleSheet,
  TextInput,
  View,
} from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { Canvas } from '../../src/components/Canvas';
import { FadeInUp } from '../../src/components/motion';
import { Button, Icon, NavBar, Row, T } from '../../src/components/ui';
import { get, getBaseURL, humanError, post } from '../../src/lib/api';
import { useApp } from '../../src/lib/store';
import type { ConvAgent } from '../../src/lib/types';
import { useTheme } from '../../src/theme/theme';
import { CARD_GAP, CARD_PAD, PAGE_X, gap, leading, radius, type, weight } from '../../src/theme/tokens';

/**
 * 创建角色。
 *
 * 之前手机端只能「从角色库选」——想自己造一个人设得去电脑上。
 * 这里补上，并把三件相关的事放在一起：
 *   1. 写名字和一句简介（可以让 AI 起草）
 *   2. 决定要不要授予「生图」能力
 *   3. 按名字 + 简介生成头像（提示词由平台固定）
 */

interface Draft {
  name: string;
  title: string;
  emoji: string;
  prompt: string;
}

export default function NewAgent() {
  const t = useTheme();
  const router = useRouter();
  const upsertConv = useApp((s) => s.upsertConv);

  const [name, setName] = useState('');
  const [title, setTitle] = useState('');
  const [prompt, setPrompt] = useState('');
  const [canImage, setCanImage] = useState(false);

  const [drafting, setDrafting] = useState(false);
  const [saving, setSaving] = useState(false);
  const [err, setErr] = useState('');

  // 头像：生成是异步的，拿到 job 后轮询，最后换成本地可访问的 URL
  const [avatarJob, setAvatarJob] = useState<string | null>(null);
  const [avatarUrl, setAvatarUrl] = useState<string | null>(null);
  const [avatarBusy, setAvatarBusy] = useState(false);
  const [avatarErr, setAvatarErr] = useState('');

  async function draft() {
    if (!name.trim()) return setErr('先填个名字，AI 才知道往哪个方向写');
    setDrafting(true);
    setErr('');
    try {
      const d = await post<Draft>('/v1/agents/draft', {
        name: name.trim(),
        keywords: title.trim(),
      });
      if (d?.title) setTitle(d.title);
      if (d?.prompt) setPrompt(d.prompt);
      if (d?.emoji) setEmoji(d.emoji);
    } catch (e) {
      setErr(humanError(e));
    } finally {
      setDrafting(false);
    }
  }

  const [emoji, setEmoji] = useState('');

  /**
   * 生成头像。
   *
   * 提交给 /v1/images/avatar：提示词由平台固定，只带名字和简介，
   * 这样同一批角色头像看起来是一套的，也避免这个接口变成
   * 「随便写提示词的生图入口」。
   */
  async function genAvatar() {
    if (!name.trim()) return setAvatarErr('先填个名字');
    setAvatarBusy(true);
    setAvatarErr('');
    setAvatarUrl(null);
    setAvatarJob(null);
    try {
      const job = await post<{ id: string }>('/v1/images/avatar', {
        name: name.trim(),
        bio: [title, prompt].filter(Boolean).join('。').slice(0, 200),
      });
      if (!job?.id) throw new Error('没有拿到任务号');
      setAvatarJob(job.id);
      await pollAvatar(job.id);
    } catch (e) {
      setAvatarErr(humanError(e));
      setAvatarBusy(false);
    }
  }

  async function pollAvatar(id: string) {
    // 生图 10-60 秒。每 2.5 秒查一次，最多等 90 秒。
    const deadline = Date.now() + 90_000;
    while (Date.now() < deadline) {
      await new Promise((r) => setTimeout(r, 2500));
      try {
        const j = await get<{
          status: string;
          files?: { path: string; url?: string }[];
        }>(`/v1/images/${id}`);
        if (j.status === 'done') {
          // 后端返回带短时效签名的 url——<Image> 带不了 Authorization 头，
          // 这里不能自己拼路径。
          const u = j.files?.[0]?.url;
          if (u) setAvatarUrl(`${getBaseURL()}${u}`);
          else setAvatarErr('图片生成了，但没拿到地址');
          setAvatarBusy(false);
          return;
        }
        if (j.status === 'failed') {
          setAvatarErr('生成失败了，换个描述再试');
          setAvatarBusy(false);
          return;
        }
      } catch {
        /* 单次查询失败继续等，网络抖动不该直接判死 */
      }
    }
    setAvatarErr('等了太久还没出图，稍后可以在资料库里找找');
    setAvatarBusy(false);
  }

  async function save() {
    if (!name.trim()) return setErr('给角色起个名字');
    if (prompt.trim().length < 10) return setErr('人设至少写一句，这样它说话才有方向');
    setSaving(true);
    setErr('');
    try {
      await post('/v1/agents', {
        name: name.trim(),
        title: title.trim(),
        emoji: emoji || [...name.trim()][0] || '人',
        prompt: prompt.trim(),
        source: 'manual',
        capabilities: canImage ? ['image'] : [],
      });
      router.back();
    } catch (e) {
      setErr(humanError(e));
    } finally {
      setSaving(false);
    }
  }

  const previewName = name.trim() || '新角色';
  const initial = emoji || [...previewName][0] || '人';

  return (
    <Canvas>
      <SafeAreaView style={{ flex: 1 }} edges={['top', 'bottom']}>
        <NavBar title="创建角色" onBack={() => router.back()} />
        <KeyboardAvoidingView
          style={{ flex: 1 }}
          behavior={Platform.OS === 'ios' ? 'padding' : undefined}
        >
          <ScrollView
            contentContainerStyle={s.page}
            showsVerticalScrollIndicator={false}
            keyboardShouldPersistTaps="handled"
          >
            {/* 头像预览 + 生成 */}
            <FadeInUp>
              <View style={{ alignItems: 'center', marginBottom: gap.lg }}>
                <View
                  style={[
                    s.avatarBox,
                    { backgroundColor: t.card, borderColor: t.line },
                  ]}
                >
                  {avatarUrl ? (
                    <Image source={{ uri: avatarUrl }} style={s.avatarImg} />
                  ) : (
                    <T size={44} weight={weight.bold} color={t.accent}>
                      {initial}
                    </T>
                  )}
                  {avatarBusy ? (
                    <View style={[StyleSheet.absoluteFill, s.avatarMask]}>
                      <ActivityIndicator color={t.ink} />
                    </View>
                  ) : null}
                </View>

                <Pressable
                  onPress={genAvatar}
                  disabled={avatarBusy || !name.trim()}
                  accessibilityRole="button"
                  accessibilityLabel="生成头像"
                  style={{ marginTop: 10 }}
                >
                  <Row gap={5} align="center">
                    <Icon
                      name="sparkles"
                      size={14}
                      color={name.trim() && !avatarBusy ? t.accent : t.ink4}
                    />
                    <T
                      size={type.caption}
                      weight={weight.semibold}
                      color={name.trim() && !avatarBusy ? t.accent : t.ink4}
                    >
                      {avatarBusy ? '正在生成…' : avatarUrl ? '重新生成' : '按名字生成头像'}
                    </T>
                  </Row>
                </Pressable>
                {avatarErr ? (
                  <T size={type.micro} color={t.bad} style={{ marginTop: 6 }}>
                    {avatarErr}
                  </T>
                ) : null}
              </View>
            </FadeInUp>

            <Field label="名字">
              <TextInput
                value={name}
                onChangeText={(v) => {
                  setName(v);
                  if (err) setErr('');
                }}
                placeholder="比如「数据侦探」"
                placeholderTextColor={t.ink4}
                maxLength={30}
                style={[s.input, { color: t.ink, backgroundColor: t.card, borderColor: t.line }]}
              />
            </Field>

            <Field label="一句话定位" hint="它在群里负责什么">
              <TextInput
                value={title}
                onChangeText={setTitle}
                placeholder="比如「查数据、找矛盾」"
                placeholderTextColor={t.ink4}
                maxLength={40}
                style={[s.input, { color: t.ink, backgroundColor: t.card, borderColor: t.line }]}
              />
            </Field>

            <Field label="人设" hint="你希望它怎么说话、怎么做事">
              <TextInput
                value={prompt}
                onChangeText={setPrompt}
                placeholder="比如：你负责查数据。只列事实和出处，不下结论；发现数字对不上就直接指出来。"
                placeholderTextColor={t.ink4}
                multiline
                maxLength={2000}
                style={[
                  s.input,
                  s.textarea,
                  { color: t.ink, backgroundColor: t.card, borderColor: t.line },
                ]}
              />
              <Pressable
                onPress={draft}
                disabled={drafting}
                accessibilityRole="button"
                accessibilityLabel="让 AI 起草人设"
                style={{ marginTop: 9 }}
              >
                <Row gap={5} align="center">
                  {drafting ? (
                    <ActivityIndicator size="small" color={t.ink3} />
                  ) : (
                    <Icon name="sparkles-outline" size={14} color={t.ink3} />
                  )}
                  <T size={type.caption} color={t.ink3} weight={weight.medium}>
                    {drafting ? '正在起草…' : '让 AI 帮我写'}
                  </T>
                </Row>
              </Pressable>
            </Field>

            {/* 能力 */}
            <Field label="能力" hint="授予后这个角色才能调用对应工具">
              <Pressable
                onPress={() => setCanImage((v) => !v)}
                accessibilityRole="switch"
                accessibilityState={{ checked: canImage }}
                style={[s.capRow, { backgroundColor: t.card, borderColor: t.line }]}
              >
                <View style={[s.capIcon, { backgroundColor: t.accentSoft }]}>
                  <Icon name="image" size={17} color={t.accent} />
                </View>
                <View style={{ flex: 1 }}>
                  <T size={type.body} weight={weight.semibold}>
                    生成图片
                  </T>
                  <T size={type.micro} color={t.ink3} style={{ marginTop: 2 }}>
                    能在对话里直接生成配图并保存
                  </T>
                </View>
                <Switch on={canImage} />
              </Pressable>
            </Field>

            {err ? (
              <T size={type.caption} color={t.bad} style={{ marginBottom: gap.sm }}>
                {err}
              </T>
            ) : null}

            <Button
              label={saving ? '保存中…' : '创建角色'}
              full
              onPress={save}
              disabled={saving}
              style={{ marginTop: gap.md }}
            />
          </ScrollView>
        </KeyboardAvoidingView>
      </SafeAreaView>
    </Canvas>
  );
}

function Field({
  label,
  hint,
  children,
}: {
  label: string;
  hint?: string;
  children: React.ReactNode;
}) {
  const t = useTheme();
  return (
    <View style={{ marginBottom: gap.lg }}>
      <Row gap={6} align="center" style={{ marginBottom: 7 }}>
        <T size={type.caption} weight={weight.semibold} color={t.ink2}>
          {label}
        </T>
        {hint ? (
          <T size={type.micro} color={t.ink4}>
            {hint}
          </T>
        ) : null}
      </Row>
      {children}
    </View>
  );
}

/** 简化开关：和设置页同款，但不引 ui.tsx 的 Toggle 以免尺寸不一致。 */
function Switch({ on }: { on: boolean }) {
  const t = useTheme();
  return (
    <View
      style={[
        s.switchTrack,
        { backgroundColor: on ? t.primaryBg : t.cardSunken },
      ]}
    >
      <View
        style={[
          s.switchKnob,
          {
            backgroundColor: t.card,
            alignSelf: on ? 'flex-end' : 'flex-start',
          },
        ]}
      />
    </View>
  );
}

const s = StyleSheet.create({
  page: { paddingHorizontal: PAGE_X, paddingTop: gap.md, paddingBottom: 48 },
  avatarBox: {
    width: 104,
    height: 104,
    borderRadius: 34,
    borderWidth: StyleSheet.hairlineWidth,
    alignItems: 'center',
    justifyContent: 'center',
    overflow: 'hidden',
  },
  avatarImg: { width: 104, height: 104 },
  avatarMask: { alignItems: 'center', justifyContent: 'center', backgroundColor: 'rgba(255,255,255,0.62)' },
  input: {
    borderRadius: radius.sm,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 14,
    paddingVertical: 13,
    fontSize: type.body,
  },
  textarea: { minHeight: 116, textAlignVertical: 'top', lineHeight: leading.body ?? 23 },
  capRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 12,
    padding: CARD_PAD,
    borderRadius: radius.card,
    borderWidth: StyleSheet.hairlineWidth,
  },
  capIcon: {
    width: 34,
    height: 34,
    borderRadius: 11,
    alignItems: 'center',
    justifyContent: 'center',
  },
  switchTrack: { width: 48, height: 29, borderRadius: 15, padding: 2.5, justifyContent: 'center' },
  switchKnob: {
    width: 24,
    height: 24,
    borderRadius: 12,
    shadowColor: '#000',
    shadowOpacity: 0.14,
    shadowRadius: 2,
    shadowOffset: { width: 0, height: 1 },
    elevation: 2,
  },
});
