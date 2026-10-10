<script setup lang="ts">
/**
 * Publish dialog. Mirrors the field visibility/requirement matrix of
 * catalog.go's validateRelease, which useAdmin owns through `externalRelease`,
 * `fileOptions` and `publishWarning`; this component only renders that state and
 * writes the user's input back into the shared `draft` object. Before an
 * external release is submitted it also asks the build-time field to confirm
 * the value Naive accepted, so text Naive rejected cannot fall back to the
 * previously committed timestamp.
 *
 * The modal is an NModal (never a native <dialog>), so notifications stay above
 * it instead of being trapped behind the browser's top layer.
 */
import { computed, defineAsyncComponent, nextTick, ref, type CSSProperties } from 'vue'
import { NAlert, NAutoComplete, NButton, NInput, NModal, NSelect, type AutoCompleteInst, type InputInst } from 'naive-ui'
import { formatSize } from '../domain'
import type { PackageState, PublishDraft, ReleaseConfig } from '../types'
import AdminIcon from './AdminIcon.vue'
const BuildTimePicker = defineAsyncComponent(() => import('./BuildTimePicker.vue'))

const props = defineProps<{
  show: boolean
  editing: ReleaseConfig | null
  draft: PublishDraft
  releases: ReleaseConfig[]
  packages: PackageState[]
  fileOptions: Array<{ label: string; value: string; disabled?: boolean }>
  selectedPackage: PackageState | undefined
  /** browser delivery without a hosted package: derived fields are required. */
  externalRelease: boolean
  publishWarning: string
  /** In-form message from useAdmin (legacy #publish-error). */
  publishError: string
  busy: boolean
}>()

const emit = defineEmits<{
  (event: 'update:show', value: boolean): void
  (event: 'submit'): void
}>()

interface BuildTimePickerInst {
  validate: () => boolean
}

const deviceRef = ref<InputInst | AutoCompleteInst | null>(null)
const channelRef = ref<InputInst | AutoCompleteInst | null>(null)
const versionRef = ref<InputInst | null>(null)
const buildTimeRef = ref<BuildTimePickerInst | null>(null)

const knownDevices = computed(() => [...new Set([
  ...props.releases.map(release => release.device),
  ...props.packages.flatMap(pkg => pkg.devices ?? []),
])].filter(Boolean).sort((a, b) => a.localeCompare(b)))

const deviceOptions = computed(() => {
  const query = props.draft.device.trim().toLocaleLowerCase()
  return knownDevices.value.filter(device => device.toLocaleLowerCase().includes(query))
})

const knownChannels = computed(() => [...new Set(props.releases.map(release => release.channel))]
  .filter(Boolean).sort((a, b) => a.localeCompare(b)))

const orderedChannels = computed(() => {
  const preferred = new Set(props.releases
    .filter(release => release.device === props.draft.device.trim())
    .map(release => release.channel))
  return [...knownChannels.value].sort((a, b) =>
    Number(preferred.has(b)) - Number(preferred.has(a)) || a.localeCompare(b))
})

const channelOptions = computed(() => {
  const query = props.draft.channel.trim().toLocaleLowerCase()
  return orderedChannels.value.filter(channel => channel.toLocaleLowerCase().includes(query))
})

const deliveryOptions = [
  { label: '应用内下载安装', value: 'app' },
  { label: '浏览器打开链接', value: 'browser' },
]
const typeOptions = [
  { label: '未指定', value: '' },
  { label: 'full', value: 'full' },
  { label: 'incremental', value: 'incremental' },
]

const title = computed(() =>
  props.editing ? `编辑 ${props.editing.device}/${props.editing.channel}` : '新建发布',
)

const browser = computed(() => props.draft.delivery === 'browser')
const fileRequired = computed(() => !browser.value)
const urlRequired = computed(() => props.externalRelease)
const derivedRequired = computed(() => props.externalRelease)

const fileHint = computed(() => {
  const pkg = props.selectedPackage
  if (pkg) {
    const devices = (pkg.devices ?? []).join(', ')
    return `${pkg.type || '?'} · ${formatSize(pkg.size)} · 设备 ${devices} · ${pkg.incremental ?? ''}`
  }
  return browser.value ? '构建信息需手动填写。' : '应用内分发必须选择一个已上传的包。'
})

const modalStyle: CSSProperties = { width: '720px', maxWidth: 'calc(100vw - 20px)' }
const contentStyle: CSSProperties = { maxHeight: 'min(66vh, 580px)', overflow: 'auto' }

function setDelivery(value: string): void {
  props.draft.delivery = value === 'browser' ? 'browser' : 'app'
}

function setType(value: string): void {
  props.draft.type = value === 'full' ? 'full' : value === 'incremental' ? 'incremental' : ''
}

/**
 * An external release must not be submitted while the build-time field holds
 * text Naive rejected: the draft would still carry the previously committed
 * timestamp, and publishing it would be silently wrong.
 */
function onFormSubmit(): void {
  if (props.externalRelease && (!buildTimeRef.value || !buildTimeRef.value.validate())) return
  emit('submit')
}

function focusFirstField(): void {
  void nextTick(() => {
    // A user may start typing before the entrance transition finishes.
    if (document.getElementById('publish-form')?.contains(document.activeElement)) return
    if (props.editing) versionRef.value?.focus()
    else if (props.draft.device) channelRef.value?.focus()
    else deviceRef.value?.focus()
  })
}
</script>

<template>
  <NModal
    :show="show"
    preset="card"
    :title="title"
    :style="modalStyle"
    :content-style="contentStyle"
    :bordered="false"
    :mask-closable="false"
    :close-on-esc="true"
    :auto-focus="false"
    transform-origin="center"
    @update:show="(value: boolean) => { if (!value) emit('update:show', false) }"
    @close="emit('update:show', false)"
    @after-enter="focusFirstField"
  >
    <form id="publish-form" class="admin-publish-form" novalidate @submit.prevent="onFormSubmit">
      <div class="admin-publish-grid">
        <label class="admin-field">
          <span class="admin-field-label">设备 <span class="admin-required">*</span></span>
          <NInput
            v-if="editing"
            ref="deviceRef"
            :value="draft.device"
            readonly
            :input-props="{ 'aria-label': '设备' }"
          />
          <NAutoComplete
            v-else
            ref="deviceRef"
            :value="draft.device"
            :options="deviceOptions"
            :get-show="() => deviceOptions.length > 0"
            placeholder="选择或输入设备"
            :input-props="{ 'aria-label': '设备', autocomplete: 'off' }"
            @update:value="(value: string) => { draft.device = value }"
          />
        </label>
        <label class="admin-field">
          <span class="admin-field-label">通道 <span class="admin-required">*</span></span>
          <NInput
            v-if="editing"
            ref="channelRef"
            :value="draft.channel"
            readonly
            :input-props="{ 'aria-label': '通道' }"
          />
          <NAutoComplete
            v-else
            ref="channelRef"
            :value="draft.channel"
            :options="channelOptions"
            :get-show="() => channelOptions.length > 0"
            placeholder="选择或输入通道"
            :input-props="{ 'aria-label': '通道', autocomplete: 'off' }"
            @update:value="(value: string) => { draft.channel = value }"
          />
        </label>
        <label class="admin-field">
          <span class="admin-field-label">版本 <span class="admin-required">*</span></span>
          <NInput
            ref="versionRef"
            :value="draft.version"
            placeholder="17.0-20261008"
            :input-props="{ 'aria-label': '版本' }"
            @update:value="(value: string) => { draft.version = value }"
          />
        </label>
      </div>

      <p v-if="editing" class="admin-hint" style="margin: -4px 0 0">
        设备与通道是发布主键，编辑时不可修改（改名会新增一条发布，而不是重命名）。
      </p>

      <div class="admin-publish-grid-2">
        <label class="admin-field">
          <span class="admin-field-label">分发方式</span>
          <NSelect
            :value="draft.delivery"
            :options="deliveryOptions"
            aria-label="分发方式"
            @update:value="setDelivery"
          />
        </label>
        <label class="admin-field">
          <span class="admin-field-label">
            OTA 包 <span v-if="fileRequired" class="admin-required">*</span>
          </span>
          <NSelect
            :value="draft.file"
            :options="fileOptions"
            aria-label="OTA 包"
            @update:value="(value: string) => { draft.file = value }"
          />
        </label>
      </div>

      <p class="admin-hint" style="margin: -4px 0 0">{{ fileHint }}</p>

      <label v-if="browser" class="admin-field">
        <span class="admin-field-label">
          下载地址 <span v-if="urlRequired" class="admin-required">*</span>
        </span>
        <NInput
          :value="draft.download_url"
          placeholder="https://…"
          :input-props="{ 'aria-label': '下载地址', inputmode: 'url' }"
          @update:value="(value: string) => { draft.download_url = value }"
        />
        <span class="admin-hint">必须为 https；选择了 OTA 包时可留空，默认使用本服务的包地址。</span>
      </label>

      <div v-if="externalRelease" class="admin-fieldset">
        <span class="admin-fieldset-title">外部发布构建信息</span>
        <div class="admin-publish-grid-2">
          <label class="admin-field">
            <span class="admin-field-label">
              构建时间 <span v-if="derivedRequired" class="admin-required">*</span>
            </span>
            <BuildTimePicker
              ref="buildTimeRef"
              :model-value="draft.build_time"
              :disabled="!externalRelease"
              @update:model-value="(value: string) => { draft.build_time = value }"
            />
          </label>
          <label class="admin-field">
            <span class="admin-field-label">
              Incremental <span v-if="derivedRequired" class="admin-required">*</span>
            </span>
            <NInput
              :value="draft.incremental"
              :input-props="{ 'aria-label': 'Incremental' }"
              @update:value="(value: string) => { draft.incremental = value }"
            />
          </label>
        </div>
        <div class="admin-publish-grid-2">
          <label class="admin-field">
            <span class="admin-field-label">大小（字节）</span>
            <NInput
              :value="draft.size"
              placeholder="非负整数"
              :input-props="{ 'aria-label': '大小（字节）', inputmode: 'numeric', pattern: '[0-9]+' }"
              @update:value="(value: string) => { draft.size = value }"
            />
          </label>
          <label class="admin-field">
            <span class="admin-field-label">类型</span>
            <NSelect
              :value="draft.type"
              :options="typeOptions"
              aria-label="类型"
              @update:value="setType"
            />
          </label>
        </div>
      </div>

      <label class="admin-field">
        <span class="admin-field-label">更新说明</span>
        <NInput
          :value="draft.changelog"
          type="textarea"
          placeholder="向用户说明本次更新的内容…"
          :autosize="{ minRows: 3, maxRows: 6 }"
          :input-props="{ 'aria-label': '更新说明' }"
          @update:value="(value: string) => { draft.changelog = value }"
        />
      </label>

      <NAlert v-if="publishWarning" type="warning" :show-icon="true" role="status">
        {{ publishWarning }}
      </NAlert>
      <NAlert v-if="publishError" type="error" :show-icon="true" role="alert">
        {{ publishError }}
      </NAlert>

      <div class="admin-modal-footer">
        <NButton id="publish-close" attr-type="button" @click="emit('update:show', false)">
          <template #icon><AdminIcon name="close" :size="15" /></template>
          取消
        </NButton>
        <NButton id="publish-submit" attr-type="submit" type="primary" :loading="busy" :disabled="busy">
          <template #icon><AdminIcon name="check" :size="16" /></template>
          发布
        </NButton>
      </div>
    </form>
  </NModal>
</template>
