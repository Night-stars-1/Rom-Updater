<script setup lang="ts">
/**
 * OTA package table. Only packages referenced by a release are publicly served,
 * so the file name is a download link exactly when `used_by` is non-empty; the
 * delete action is blocked while a release still points at the package.
 *
 * Four columns stay visible (file, devices, build, actions); the full SHA-256,
 * byte size, type and the referencing releases sit in a keyboard-operable
 * per-row disclosure, so no metadata disappears from the console.
 */
import { computed, ref } from 'vue'
import { NButton, NEmpty, NSpin, NTable, NTag } from 'naive-ui'
import { formatSize, formatTimestamp, packageURL } from '../domain'
import type { PackageState } from '../types'
import AdminIcon from './AdminIcon.vue'

const props = defineProps<{
  packages: PackageState[]
  /** Package base URL; kebab-case `base-url` in templates. */
  baseUrl: string
  loading: boolean
}>()

const emit = defineEmits<{
  (event: 'publish', file: string): void
  (event: 'remove', pkg: PackageState): void
}>()

const rows = computed(() => [...props.packages].sort((a, b) => b.mod_time.localeCompare(a.mod_time)))

function downloadURL(file: string): string {
  return packageURL(props.baseUrl, file)
}

// Disclosure state is keyed by file name, so an expanded row survives a refresh
// as long as the package is still listed.
const expanded = ref<Set<string>>(new Set())

function isOpen(pkg: PackageState): boolean {
  return expanded.value.has(pkg.file)
}

function toggle(pkg: PackageState): void {
  const next = new Set(expanded.value)
  if (next.has(pkg.file)) next.delete(pkg.file)
  else next.add(pkg.file)
  expanded.value = next
}

function detailsId(index: number): string {
  return `package-details-${index}`
}
</script>

<template>
  <div id="packages-section" class="admin-table-panel">
    <NSpin :show="loading" size="small">
      <div
        class="admin-table-wrap"
        tabindex="0"
        role="region"
        :aria-label="`OTA 包列表，共 ${rows.length} 项，可横向滚动`"
      >
        <NTable class="admin-table" size="small" :bordered="false" aria-label="OTA 包">
          <thead>
            <tr>
              <th scope="col">文件</th>
              <th scope="col">设备</th>
              <th scope="col">构建</th>
              <th scope="col">操作</th>
            </tr>
          </thead>
          <tbody>
            <template v-for="(pkg, index) in rows" :key="pkg.file">
              <tr>
                <td>
                  <a
                    v-if="pkg.used_by.length"
                    class="admin-mono admin-trunc admin-link"
                    :href="downloadURL(pkg.file)"
                    :download="pkg.file"
                    :title="pkg.file"
                  >
                    {{ pkg.file }}
                  </a>
                  <span v-else class="admin-mono admin-trunc" :title="pkg.file">{{ pkg.file }}</span>
                  <div class="admin-cell-secondary">
                    <span :title="`${pkg.size} 字节`">{{ formatSize(pkg.size) }}</span>
                    <NTag v-if="pkg.type" :bordered="false" size="small">{{ pkg.type }}</NTag>
                    <NTag v-if="pkg.error" type="error" :bordered="false" size="small">
                      {{ pkg.error }}
                    </NTag>
                  </div>
                </td>
                <td>
                  <div class="admin-tags">
                    <NTag
                      v-for="device in pkg.devices ?? []"
                      :key="device"
                      :bordered="false"
                      size="small"
                    >
                      {{ device }}
                    </NTag>
                    <span v-if="!(pkg.devices ?? []).length" class="admin-muted">—</span>
                  </div>
                </td>
                <td>
                  <span class="admin-cell-strong">{{ formatTimestamp(pkg.build_timestamp) }}</span>
                  <div class="admin-cell-secondary admin-mono">{{ pkg.incremental || '—' }}</div>
                </td>
                <td class="pk-actions-cell">
                  <div class="pk-actions">
                    <NButton
                      size="small"
                      quaternary
                      :aria-expanded="isOpen(pkg)"
                      :aria-controls="detailsId(index)"
                      :aria-label="`${isOpen(pkg) ? '收起' : '展开'} ${pkg.file} 的详情`"
                      @click="toggle(pkg)"
                    >
                      <template #icon>
                        <AdminIcon :name="isOpen(pkg) ? 'chevron-down' : 'chevron-right'" :size="15" />
                      </template>
                      详情
                    </NButton>
                    <NButton
                      size="small"
                      secondary
                      :disabled="!!pkg.error"
                      :aria-label="`发布 ${pkg.file}`"
                      @click="emit('publish', pkg.file)"
                    >
                      <template #icon><AdminIcon name="release" :size="15" /></template>
                      发布
                    </NButton>
                    <NButton
                      size="small"
                      type="error"
                      secondary
                      :disabled="pkg.used_by.length > 0"
                      :title="pkg.used_by.length > 0 ? '先下架引用它的版本' : undefined"
                      :aria-label="
                        pkg.used_by.length > 0
                          ? `删除 ${pkg.file}（先下架引用它的版本）`
                          : `删除 ${pkg.file}`
                      "
                      @click="emit('remove', pkg)"
                    >
                      删除
                    </NButton>
                  </div>
                </td>
              </tr>
              <tr v-show="isOpen(pkg)">
                <td :id="detailsId(index)" colspan="4">
                  <div class="admin-row-details pk-details">
                    <div class="admin-detail-grid">
                      <div class="admin-detail-item">
                        <span class="admin-detail-label">文件</span>
                        <span class="admin-detail-value admin-mono">{{ pkg.file }}</span>
                      </div>
                      <div class="admin-detail-item">
                        <span class="admin-detail-label">SHA-256</span>
                        <span class="admin-detail-value admin-mono">{{ pkg.sha256 || '未计算' }}</span>
                      </div>
                      <div class="admin-detail-item">
                        <span class="admin-detail-label">大小</span>
                        <span class="admin-detail-value admin-mono">
                          {{ pkg.size }} 字节（{{ formatSize(pkg.size) }}）
                        </span>
                      </div>
                      <div class="admin-detail-item">
                        <span class="admin-detail-label">类型</span>
                        <span class="admin-detail-value">{{ pkg.type || '—' }}</span>
                      </div>
                      <div class="admin-detail-item">
                        <span class="admin-detail-label">被引用</span>
                        <div v-if="pkg.used_by.length" class="admin-tags">
                          <NTag
                            v-for="usage in pkg.used_by"
                            :key="usage"
                            :bordered="false"
                            size="small"
                          >
                            {{ usage }}
                          </NTag>
                        </div>
                        <span v-else class="admin-detail-value admin-muted">未被引用</span>
                      </div>
                      <div v-if="pkg.used_by.length" class="admin-detail-item">
                        <span class="admin-detail-label">下载地址</span>
                        <a
                          class="admin-detail-value admin-mono admin-link"
                          :href="downloadURL(pkg.file)"
                          :download="pkg.file"
                        >
                          {{ downloadURL(pkg.file) }}
                        </a>
                      </div>
                    </div>
                  </div>
                </td>
              </tr>
            </template>
            <tr v-if="!rows.length">
              <td colspan="4">
                <div class="admin-empty">
                  <NEmpty description="暂无文件">
                    <template #icon><AdminIcon name="package" :size="28" /></template>
                  </NEmpty>
                </div>
              </td>
            </tr>
          </tbody>
        </NTable>
      </div>
    </NSpin>
  </div>
</template>

<style scoped>
/* Layout only: colours/typography come from the shared classes in styles.css. */

.admin-empty {
  align-items: center;
  justify-content: center;
  min-height: 180px;
}

.pk-actions-cell {
  text-align: right;
}

.pk-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  justify-content: flex-end;
}

/* The colspan cell inherits the table's centred empty-state alignment. */
.pk-details {
  text-align: left;
}

.admin-detail-item {
  display: flex;
  flex-direction: column;
  gap: 3px;
  min-width: 0;
}

.admin-detail-value {
  min-width: 0;
  overflow-wrap: anywhere;
  white-space: pre-wrap;
}

.pk-empty {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 200px;
}
</style>
