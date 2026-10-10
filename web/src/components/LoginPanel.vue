<script setup lang="ts">
/**
 * Sign-in screen: a single centred card. Keeps the real `<form>`
 * (autocomplete, credential-manager handoff) but every failure — including an
 * empty token — is reported through the top-right notification host by
 * useAdmin; this component never renders an inline error banner.
 */
import { NButton, NCard, NInput } from 'naive-ui'
import AdminIcon from './AdminIcon.vue'

defineProps<{
  /** Candidate token, owned by useAdmin so login() reads the same value. */
  token: string
  busy: boolean
}>()

const emit = defineEmits<{
  (event: 'update:token', value: string): void
  (event: 'submit'): void
}>()

const username = 'admin'
</script>

<template>
  <div class="admin-login">
    <div class="admin-login-card">
      <div class="admin-login-head">
        <span class="admin-login-logo"><AdminIcon name="logo" :size="18" /></span>
        <div>
          <h1 class="admin-login-title">Rom Updater</h1>
          <p class="admin-login-subtitle">OTA 发布控制台</p>
        </div>
      </div>

      <NCard :bordered="true" :content-style="{ padding: '20px 18px' }">
        <form
          id="login-form"
          class="admin-login-form"
          autocomplete="on"
          novalidate
          @submit.prevent="emit('submit')"
        >
          <input id="username" name="username" type="hidden" autocomplete="username" :value="username">

          <label class="admin-field">
            <span class="admin-field-label">管理令牌</span>
            <NInput
              :value="token"
              type="password"
              show-password-on="click"
              autofocus
              :disabled="busy"
              placeholder="OTA_ADMIN_TOKEN"
              :input-props="{
                id: 'token',
                name: 'password',
                autocomplete: 'current-password',
                'aria-describedby': 'token-hint',
              }"
              @update:value="emit('update:token', $event)"
            />
            <span id="token-hint" class="admin-hint">
              会话仅保存在当前标签页。
            </span>
          </label>

          <NButton id="login-submit" type="primary" attr-type="submit" block :loading="busy" :disabled="busy">
            登录控制台
          </NButton>
        </form>
      </NCard>

    </div>
  </div>
</template>
