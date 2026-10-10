<script setup lang="ts">
import { computed, watchEffect } from 'vue'
import {
  NConfigProvider,
  NDialogProvider,
  NGlobalStyle,
  NNotificationProvider,
  darkTheme,
  dateZhCN,
  useOsTheme,
  zhCN,
  type GlobalThemeOverrides,
} from 'naive-ui'
import AdminDashboard from './components/AdminDashboard.vue'

const osTheme = useOsTheme()
const theme = computed(() => osTheme.value === 'dark' ? darkTheme : null)
const themeOverrides = computed<GlobalThemeOverrides>(() => ({
  common: {
    primaryColor: osTheme.value === 'dark' ? '#a5b4fc' : '#5558d9',
    primaryColorHover: osTheme.value === 'dark' ? '#c7d2fe' : '#676ae5',
    primaryColorPressed: osTheme.value === 'dark' ? '#818cf8' : '#4548bd',
    primaryColorSuppl: '#5558d9',
    borderRadius: '8px',
    fontFamily: 'Inter, system-ui, -apple-system, "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif',
    fontSize: '14px',
    fontWeightStrong: '600',
  },
  Button: { heightMedium: '36px', borderRadiusMedium: '8px' },
  Input: { heightMedium: '38px' },
  Select: { peers: { InternalSelection: { heightMedium: '38px' } } },
  Card: { borderRadius: '10px' },
  Notification: { borderRadius: '10px' },
}))

watchEffect(() => {
  document.documentElement.dataset.theme = osTheme.value === 'dark' ? 'dark' : 'light'
})
</script>

<template>
  <NConfigProvider :theme="theme" :theme-overrides="themeOverrides" :locale="zhCN" :date-locale="dateZhCN">
    <NGlobalStyle />
    <NNotificationProvider placement="top-right" :scrollable="false">
      <NDialogProvider>
        <AdminDashboard />
      </NDialogProvider>
    </NNotificationProvider>
  </NConfigProvider>
</template>
