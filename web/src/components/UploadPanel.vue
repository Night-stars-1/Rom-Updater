<script setup lang="ts">
/**
 * Upload form. Shell renders this inside an NModal card, so the component owns
 * no chrome of its own: no card, no heading, no long guidance — just the form,
 * the progress line and the cancel action.
 *
 * NUpload/NUploadDragger only select a File. Automatic upload is disabled;
 * useAdmin still streams the raw ZIP through its existing XHR PUT path.
 *
 * Progress is `null` while the server verifies the package (upload finished,
 * response pending), which renders as an indeterminate bar instead of a fake
 * percentage.
 *
 * The SHA-256 field is an advanced, collapsed-by-default option. It is hidden
 * with `v-show`, never unmounted, so the value and its validation state cannot
 * change behind the user's back; a non-empty value always forces it open.
 */
import { computed, ref, watch } from 'vue'
import { NAlert, NButton, NInput, NProgress, NUpload, NUploadDragger, type UploadFileInfo, type UploadInst, type UploadSettledFileInfo } from 'naive-ui'
import { formatSize } from '../domain'
import AdminIcon from './AdminIcon.vue'

const props = defineProps<{
  file: File | null
  sha: string
  status: string
  /** 0–100, or null while the server verifies the upload. */
  progress: number | null
  uploading: boolean
  /** In-form message from useAdmin (legacy #upload-error). */
  error: string
}>()

const emit = defineEmits<{
  (event: 'update:file', file: File | null): void
  (event: 'update:sha', sha: string): void
  (event: 'start'): void
  (event: 'cancel'): void
}>()

const uploadRef = ref<UploadInst | null>(null)
const invalidFile = ref('')
const zipAccept = '.zip,.ziP,.zIp,.zIP,.Zip,.ZiP,.ZIp,.ZIP,application/zip,application/x-zip-compressed'
const fileList = computed<UploadFileInfo[]>(() => props.file ? [{
  id: `${props.file.name}:${props.file.size}:${props.file.lastModified}`,
  name: props.file.name,
  status: 'pending',
  file: props.file,
}] : [])

const fileLabel = computed(() =>
  props.file ? `${props.file.name} · ${formatSize(props.file.size)}` : '未选择文件',
)

const progressStatus = computed(() => {
  if (props.status.startsWith('上传失败') || props.status.startsWith('上传中断')) return 'error' as const
  if (props.progress === 100 && props.status.startsWith('已上传')) return 'success' as const
  return 'default' as const
})

const shaOpen = ref(props.sha !== '')

// A value that arrives from outside (or from a previous visit) is never hidden.
watch(
  () => props.sha,
  sha => {
    if (sha) shaOpen.value = true
  },
)

function chooseFile(): void {
  if (!props.uploading) uploadRef.value?.openOpenFileDialog()
}

function beforeUpload({ file }: { file: UploadSettledFileInfo }): boolean {
  if (!file.file || !/\.zip$/i.test(file.name)) {
    invalidFile.value = '请选择扩展名为 .zip 的 OTA 包。'
    return false
  }
  invalidFile.value = ''
  return true
}

function updateFileList(files: UploadFileInfo[]): void {
  emit('update:file', files.at(-1)?.file ?? null)
}

function reportInvalidDrop(event: DragEvent): void {
  if (props.uploading) return
  const file = event.dataTransfer?.files[0]
  if (file && !/\.zip$/i.test(file.name)) invalidFile.value = '仅支持拖入 .zip 文件。'
}
</script>

<template>
  <form id="upload-form" class="up-form" novalidate @submit.prevent="emit('start')">
    <div class="up-field">
      <label class="admin-field-label" for="upload-file">
        文件（*.zip）<span class="admin-required">*</span>
      </label>
      <NUpload
        id="upload-dropzone"
        ref="uploadRef"
        :file-list="fileList"
        :default-upload="false"
        :show-file-list="false"
        :multiple="false"
        :disabled="uploading"
        :accept="zipAccept"
        :input-props="{ id: 'upload-file', tabindex: '-1', disabled: uploading }"
        :on-before-upload="beforeUpload"
        @update:file-list="updateFileList"
      >
        <NUploadDragger
          id="upload-choose"
          class="up-dropzone"
          role="button"
          :tabindex="uploading ? -1 : 0"
          :aria-disabled="uploading"
          aria-label="拖入 ZIP 文件或点击选择"
          aria-describedby="upload-file-status"
          @keydown.enter.prevent="chooseFile"
          @keydown.space.prevent="chooseFile"
          @drop="reportInvalidDrop"
        >
          <div class="up-drop-content">
            <AdminIcon name="upload" :size="24" />
            <span class="up-drop-label">拖入 ZIP 文件，或点击选择</span>
            <span id="upload-file-status" class="up-file-status" role="status">{{ fileLabel }}</span>
          </div>
        </NUploadDragger>
      </NUpload>
    </div>

    <div class="up-field">
      <NButton
        text
        attr-type="button"
        class="up-toggle"
        :aria-expanded="shaOpen"
        aria-controls="upload-sha-advanced"
        @click="shaOpen = !shaOpen"
      >
        <template #icon>
          <AdminIcon :name="shaOpen ? 'chevron-down' : 'chevron-right'" :size="15" />
        </template>
        SHA-256 校验（可选）
      </NButton>
      <div v-show="shaOpen" id="upload-sha-advanced" class="up-advanced">
        <label class="admin-field-label" for="upload-sha">SHA-256</label>
        <NInput
          class="up-sha-input"
          :value="sha"
          :disabled="uploading"
          placeholder="留空则不校验"
          :input-props="{ id: 'upload-sha', pattern: '[0-9a-fA-F]{64}', spellcheck: 'false' }"
          @update:value="emit('update:sha', $event)"
        />
        <p class="up-hint">
          SHA-256 填 <code>sha256sum 文件名</code> 的输出；留空则不校验。
        </p>
      </div>
    </div>

    <NAlert v-if="invalidFile || error" type="error" :show-icon="true" role="alert">
      {{ invalidFile || error }}
    </NAlert>

    <div v-if="uploading || status" class="admin-progress">
      <NProgress
        type="line"
        :percentage="progress ?? 100"
        :processing="progress === null"
        :show-indicator="false"
        :status="progressStatus"
        :height="8"
        aria-label="OTA 包上传进度"
      />
      <div class="admin-progress-text" role="status">{{ status }}</div>
    </div>

    <div class="up-actions">
      <NButton
        id="upload-start"
        type="primary"
        attr-type="submit"
        :loading="uploading"
        :disabled="uploading"
      >
        <template #icon><AdminIcon name="upload" :size="16" /></template>
        上传
      </NButton>
      <NButton v-if="uploading" id="upload-cancel" secondary attr-type="button" @click="emit('cancel')">
        取消上传
      </NButton>
    </div>
  </form>
</template>

<style scoped>
/* Layout only; colours come from the shared tokens/classes in styles.css. */
.up-form {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.up-field {
  display: flex;
  flex-direction: column;
  gap: 7px;
}

.up-dropzone:focus-visible {
  outline: 2px solid var(--admin-primary, #5558d9);
  outline-offset: 2px;
}

.up-drop-content {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
}

.up-drop-label {
  font-size: 13px;
  font-weight: 500;
}

.up-file-status {
  min-width: 0;
  font-size: 12.5px;
  color: var(--admin-text-muted, #6a7183);
  overflow-wrap: anywhere;
}

.up-toggle {
  align-self: flex-start;
  font-size: 12.5px;
  font-weight: 600;
}

.up-advanced {
  display: flex;
  flex-direction: column;
  gap: 7px;
}

.up-hint {
  margin: 0;
  font-size: 11.5px;
  line-height: 1.55;
  color: var(--admin-text-faint, #8d93a3);
}

.up-hint code {
  font-family: ui-monospace, SFMono-Regular, 'SF Mono', Menlo, Consolas, 'Liberation Mono', monospace;
}

.up-sha-input :deep(.n-input__input-el) {
  font-family: ui-monospace, SFMono-Regular, 'SF Mono', Menlo, Consolas, 'Liberation Mono', monospace;
}

.up-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  justify-content: flex-end;
}
</style>
