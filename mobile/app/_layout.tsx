import { Stack } from 'expo-router';
import { StatusBar } from 'expo-status-bar';
import React, { useEffect, useState } from 'react';
import { ActivityIndicator, View } from 'react-native';
import { SafeAreaProvider } from 'react-native-safe-area-context';
import { ErrorBoundary } from '../src/components/ErrorBoundary';
import { loadBaseURL } from '../src/lib/api';
import { boot } from '../src/lib/session';
import { useApp } from '../src/lib/store';
import { useTheme } from '../src/theme/theme';
import { useFonts } from 'expo-font';
import { FONT_ASSETS } from '../src/theme/fonts';

export default function RootLayout() {
  const t = useTheme();
  const ready = useApp((s) => s.ready);
  const [phase, setPhase] = useState<'booting' | 'done'>('booting');
  // 字体加载失败不阻塞启动——退回系统字体，界面仍然可用
  const [fontsLoaded] = useFonts(FONT_ASSETS);

  useEffect(() => {
    (async () => {
      try {
        const base = await loadBaseURL();
        if (base) await boot();
      } catch {
        // 启动失败不该把用户永久卡在转圈上——回到入口页可改地址或重配对
        useApp.getState().setReady(false);
      } finally {
        setPhase('done');
      }
    })();
  }, []);

  if (phase === 'booting' || !fontsLoaded) {
    return (
      <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center', backgroundColor: t.canvas[1] }}>
        <ActivityIndicator color={t.accent} />
      </View>
    );
  }

  return (
    <SafeAreaProvider>
      <StatusBar style={t.dark ? 'light' : 'dark'} />
      <ErrorBoundary>
        <Stack
          screenOptions={{
            headerShown: false,
            contentStyle: { backgroundColor: t.canvas[1] },
            animation: 'slide_from_right',
          }}
        >
          <Stack.Screen name="index" options={{ animation: 'fade' }} />
          <Stack.Protected guard={ready}>
            <Stack.Screen name="(main)" options={{ animation: 'fade' }} />
            <Stack.Screen name="team/[id]" />
            <Stack.Screen name="team/new" options={{ presentation: 'modal', animation: 'slide_from_bottom' }} />
            <Stack.Screen name="goals" />
            <Stack.Screen name="library" />
            <Stack.Screen name="artifact/[id]" />
            <Stack.Screen name="connect" />
            <Stack.Screen name="settings/index" />
          </Stack.Protected>
        </Stack>
      </ErrorBoundary>
    </SafeAreaProvider>
  );
}
