<script setup lang="ts">
/**
 * OTA admin console shell. Owns the single useAdmin instance and adapts Naive's
 * notification/dialog providers to the `notify`/`confirm` contract the
 * composable expects — everything else is presentational.
 *
 * The console deliberately does not nest providers: App.vue mounts
 * NConfigProvider (system light/dark + zhCN), NNotificationProvider(top-right)
 * and NDialogProvider, so login failures, upload results, publish results and
 * every confirm request are rendered by those hosts.
 *
 * Layout: a thin top bar plus two compact views ("版本" / "文件") that share one
 * toolbar. Only the active view is rendered; the upload window is a single
 * NModal owned here so it can never stack with the publish dialog.
 */
import {
  computed,
  defineAsyncComponent,
  h,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  watch,
} from 'vue'
import {
  NAlert,
  NButton,
  NModal,
  NTab,
  NTabs,
  useDialog,
  useNotification,
  type DialogOptions,
} from 'naive-ui'
import { useAdmin } from '../useAdmin'
import type { Confirm, Notify, PackageState, ReleaseConfig } from '../types'
import AdminIcon from './AdminIcon.vue'
import LoginPanel from './LoginPanel.vue'
import PackagesTable from './PackagesTable.vue'
import PublishDialog from './PublishDialog.vue'
import ReleasesTable from './ReleasesTable.vue'
const UploadPanel = defineAsyncComponent(() => import('./UploadPanel.vue'))

const notification = useNotification()
const dialog = useDialog()

/**
 * Naive's notification host carries no live-region semantics, so the console
 * keeps two always-present visually hidden regions next to it: successful and
 * informational messages go to the polite one, failures to the assertive one.
 * The text is cleared before it is rewritten, which is what makes an identical
 * repeat ("已退出登录" twice) announce again instead of being swallowed.
 */
const livePolite = ref('')
const liveAssertive = ref('')

function announce(message: string, assertive: boolean): void {
  const region = assertive ? liveAssertive : livePolite
  const other = assertive ? livePolite : liveAssertive
  other.value = ''
  region.value = ''
  void nextTick(() => {
    region.value = message
  })
}

/** Top-right notifications: 8s for errors, 3s otherwise, manually closable. */
const notify: Notify = (message, kind) => {
  announce(message, kind === 'error')
  const options = {
    content: message,
    duration: kind === 'error' ? 8000 : 3000,
    keepAliveOnHover: true,
    closable: true,
  }
  if (kind === 'success') notification.success(options)
  else if (kind === 'error') notification.error(options)
  else notification.info(options)
}

/**
 * Confirm adapter. `onAfterLeave` is the safety net: every way of dismissing the
 * dialog (negative button, close button, Esc, mask, destroyAll on logout) has to
 * settle the promise, so the composable can never await a dead dialog.
 */
const confirm: Confirm = options =>
  new Promise<boolean>(resolve => {
    let settled = false
    const settle = (value: boolean) => {
      if (!settled) {
        settled = true
        resolve(value)
      }
    }
    const dialogOptions: DialogOptions = {
      title: options.title,
      content: () => h('div', { class: 'admin-confirm-text' }, options.message),
      positiveText: options.positiveText,
      negativeText: '取消',
      // No close icon: with autoFocus the first focusable element is then the
      // cancel button, which is the safe default for a destructive confirm.
      closable: false,
      autoFocus: true,
      closeOnEsc: true,
      maskClosable: false,
      onPositiveClick: () => settle(true),
      onNegativeClick: () => settle(false),
      onClose: () => settle(false),
      onEsc: () => settle(false),
      onAfterLeave: () => settle(false),
    }
    if (options.danger) dialog.error(dialogOptions)
    else dialog.warning(dialogOptions)
  })

const {
  state,
  token,
  loginBusy,
  loading,
  lastLoadedAt,
  stateLoadError,
  authenticated,
  uploadFile,
  uploadSHA,
  uploadStatus,
  uploadProgress,
  uploading,
  uploadError,
  publishOpen,
  publishBusy,
  editing,
  publishError,
  draft,
  selectedPackage,
  externalRelease,
  publishWarning,
  fileOptions,
  login,
  logout,
  refresh,
  upload,
  cancelUpload,
  openPublish,
  closePublish,
  publish,
  unpublish,
  deletePackage,
} = useAdmin({ notify, confirm, closeConfirm: () => dialog.destroyAll() })

/* ------------------------------------------------------------- views -- */

type ViewId = 'releases' | 'files'

const HASH_VIEWS: Record<string, ViewId> = { '#releases': 'releases', '#files': 'files' }

const view = ref<ViewId>('releases')

const views = computed<{ id: ViewId; label: string; icon: string; count: number }[]>(() => [
  { id: 'releases', label: '版本', icon: 'release', count: state.value?.releases?.length ?? 0 },
  { id: 'files', label: '文件', icon: 'package', count: state.value?.packages?.length ?? 0 },
])


function setView(next: ViewId): void {
  view.value = next
  const hash = `#${next}`
  if (window.location.hash !== hash) window.location.hash = hash
}

function updateView(next: string | number): void {
  if (next === 'releases' || next === 'files') setView(next)
}

function handleTabKey(event: KeyboardEvent, index: number): void {
  let next = index
  const last = views.value.length - 1
  if (event.key === 'ArrowRight' || event.key === 'ArrowDown') next = index === last ? 0 : index + 1
  else if (event.key === 'ArrowLeft' || event.key === 'ArrowUp') next = index === 0 ? last : index - 1
  else if (event.key === 'Home') next = 0
  else if (event.key === 'End') next = last
  else if (event.key !== 'Enter' && event.key !== ' ') return
  event.preventDefault()
  const id = views.value[next].id
  setView(id)
  void nextTick(() => document.getElementById(`tab-${id}`)?.focus())
}

/** Only #releases / #files are views; every other hash falls back to 版本. */
function syncViewFromHash(): void {
  view.value = HASH_VIEWS[window.location.hash] ?? 'releases'
}


/* ------------------------------------------------------------- upload -- */

/**
 * The upload window is UI state owned here; the File, the XHR and the progress
 * all live in useAdmin, so closing this window never aborts an upload. While an
 * upload runs the toolbar keeps offering a "上传中" entry that reopens it.
 */
const uploadOpen = ref(false)

function openUpload(): void {
  // Never stack the upload window on top of the publish dialog.
  if (publishOpen.value) return
  uploadOpen.value = true
}

const uploadLabel = computed(() => {
  if (uploading.value) {
    return uploadProgress.value === null ? '上传中…' : `上传中 ${Math.round(uploadProgress.value)}%`
  }
  return view.value === 'files' ? '上传文件' : '上传'
})

// A successful upload opens the publish dialog; close the upload window first so
// the two modals never overlap. Logging out (including a 401) closes it too.
watch(publishOpen, open => {
  if (open) uploadOpen.value = false
})
watch(authenticated, ok => {
  if (!ok) uploadOpen.value = false
})

/* ------------------------------------------------------------ actions -- */

const loadedAtText = computed(() =>
  lastLoadedAt.value ? `更新于 ${lastLoadedAt.value.toLocaleTimeString()}` : '',
)

async function handleLogin(): Promise<void> {
  await login()
}

function handleLogout(): void {
  uploadOpen.value = false
  logout()
  notify('已退出登录')
}

async function handlePublish(): Promise<void> {
  await publish()
}

onMounted(() => {
  syncViewFromHash()
  window.addEventListener('hashchange', syncViewFromHash)
})

onBeforeUnmount(() => {
  window.removeEventListener('hashchange', syncViewFromHash)
})
</script>

<template>
  <div class="admin-app">
    <!-- Screen-reader mirror of the visual notifications; always present so the
         regions exist before their text changes. -->
    <div class="admin-visually-hidden" role="status" aria-live="polite" aria-atomic="true">
      {{ livePolite }}
    </div>
    <div class="admin-visually-hidden" role="alert" aria-live="assertive" aria-atomic="true">
      {{ liveAssertive }}
    </div>

    <LoginPanel
      v-if="!authenticated"
      v-model:token="token"
      :busy="loginBusy"
      @submit="handleLogin"
    />

    <template v-else>
      <header class="admin-topbar">
        <div class="admin-topbar-brand">
          <span class="admin-topbar-logo"><AdminIcon name="logo" :size="17" /></span>
          <span class="admin-topbar-name">Rom Updater</span>
        </div>

        <div class="admin-topbar-actions">
          <div class="admin-topbar-status">
            <span
              v-if="state?.base_url"
              class="admin-topbar-url"
              :title="`公共地址：${state.base_url}`"
            >
              {{ state.base_url }}
            </span>
            <span v-if="loadedAtText" class="admin-topbar-time">{{ loadedAtText }}</span>
          </div>

          <NButton id="refresh" size="small" secondary :loading="loading" :disabled="loading" @click="refresh(true)">
            <template #icon><AdminIcon name="refresh" :size="15" /></template>
            刷新
          </NButton>
          <NButton id="logout" size="small" quaternary @click="handleLogout">
            <template #icon><AdminIcon name="logout" :size="15" /></template>
            退出
          </NButton>
        </div>
      </header>

      <main class="admin-content">
        <!-- Exactly one persistent error banner: when the manifest itself is
             broken both state.error and stateLoadError are set, so the manifest
             detail wins and the refresh status is not repeated. -->
        <NAlert
          v-if="state?.error"
          type="error"
          :show-icon="true"
          title="清单或包有错误，客户端当前收到 503"
          role="alert"
        >
          {{ state.error }}
        </NAlert>
        <NAlert
          v-else-if="stateLoadError"
          type="error"
          :show-icon="true"
          :title="stateLoadError"
          role="alert"
        />

        <div class="admin-viewbar">
          <NTabs
            class="admin-tabs"
            type="segment"
            size="small"
            :value="view"
            :animated="false"
            role="tablist"
            aria-label="控制台视图"
            @update:value="updateView"
          >
            <NTab
              v-for="(tab, index) in views"
              :id="`tab-${tab.id}`"
              :key="tab.id"
              :name="tab.id"
              :aria-controls="`view-${tab.id}`"
              role="tab"
              :aria-selected="view === tab.id"
              :tabindex="view === tab.id ? 0 : -1"
              @keydown="handleTabKey($event, index)"
            >
              <span class="admin-tab-label">
                <AdminIcon :name="tab.icon" :size="15" />
                <span>{{ tab.label }}</span>
                <span class="admin-tab-count">{{ tab.count }}</span>
              </span>
            </NTab>
          </NTabs>

          <div class="admin-view-actions">
            <NButton
              id="upload-open"
              size="small"
              :type="view === 'files' ? 'primary' : 'default'"
              :secondary="view === 'releases'"
              @click="openUpload"
            >
              <template #icon><AdminIcon name="upload" :size="15" /></template>
              {{ uploadLabel }}
            </NButton>
            <NButton
              v-if="view === 'releases'"
              id="new-release"
              size="small"
              type="primary"
              @click="openPublish()"
            >
              <template #icon><AdminIcon name="plus" :size="15" /></template>
              新建发布
            </NButton>
          </div>
        </div>

        <section
          v-if="view === 'releases'"
          id="view-releases"
          class="admin-view-panel"
          role="tabpanel"
          aria-labelledby="tab-releases"
        >
          <ReleasesTable
            :releases="state?.releases ?? []"
            :packages="state?.packages ?? []"
            :loading="loading"
            @edit="(release: ReleaseConfig) => openPublish({ release })"
            @unpublish="(release: ReleaseConfig) => unpublish(release)"
          />
        </section>

        <section
          v-else
          id="view-files"
          class="admin-view-panel"
          role="tabpanel"
          aria-labelledby="tab-files"
        >
          <PackagesTable
            :packages="state?.packages ?? []"
            :base-url="state?.base_url ?? ''"
            :loading="loading"
            @publish="(file: string) => openPublish({ file })"
            @remove="(pkg: PackageState) => deletePackage(pkg)"
          />
        </section>
      </main>
    </template>

    <!-- Upload window: preset=card, mask-closable=false so a stray click cannot
         dismiss it; Esc and the close button still work and never abort the XHR. -->
    <NModal
      v-model:show="uploadOpen"
      preset="card"
      class="admin-upload-modal"
      title="上传 OTA 包"
      :style="{ width: 'calc(100vw - 32px)', maxWidth: '560px' }"
      :mask-closable="false"
      :close-on-esc="true"
    >
      <UploadPanel
        :file="uploadFile"
        :sha="uploadSHA"
        :status="uploadStatus"
        :progress="uploadProgress"
        :uploading="uploading"
        :error="uploadError"
        @update:file="(value: File | null) => { uploadFile = value }"
        @update:sha="(value: string) => { uploadSHA = value }"
        @start="upload"
        @cancel="cancelUpload"
      />
    </NModal>

    <PublishDialog
      :show="publishOpen"
      :editing="editing"
      :draft="draft"
      :releases="state?.releases ?? []"
      :packages="state?.packages ?? []"
      :file-options="fileOptions"
      :selected-package="selectedPackage"
      :external-release="externalRelease"
      :publish-warning="publishWarning"
      :publish-error="publishError"
      :busy="publishBusy"
      @update:show="(value: boolean) => { if (!value) closePublish() }"
      @submit="handlePublish"
    />
  </div>
</template>
