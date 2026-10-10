<script setup lang="ts">
/**
 * Released-version table. Rows are sorted by device then channel (the order the
 * client sees), and a release whose package is missing from the manifest is
 * flagged so the mismatch is visible before a client hits it.
 *
 * The console stays four columns wide: delivery mode rides along with the
 * version, and the changelog / download URL / full build info live in a
 * keyboard-operable per-row disclosure so nothing is lost, only folded away.
 */
import { computed, ref } from 'vue'
import { NButton, NEmpty, NSpin, NTable, NTag } from 'naive-ui'
import { formatSize, formatTimestamp } from '../domain'
import type { PackageState, ReleaseConfig } from '../types'
import AdminIcon from './AdminIcon.vue'

const props = defineProps<{
  releases: ReleaseConfig[]
  packages: PackageState[]
  loading: boolean
}>()

const emit = defineEmits<{
  (event: 'edit', release: ReleaseConfig): void
  (event: 'unpublish', release: ReleaseConfig): void
}>()

const rows = computed(() =>
  [...props.releases].sort(
    (a, b) => a.device.localeCompare(b.device) || a.channel.localeCompare(b.channel),
  ),
)

const knownFiles = computed(() => new Set(props.packages.map(pkg => pkg.file)))

/** Show hosted-package metadata or the fields configured for an external release. */
function buildSummary(release: ReleaseConfig): string {
  const build = release.file ? props.packages.find(pkg => pkg.file === release.file) : release
  if (!build) return '—'
  const parts = [formatTimestamp(build.build_timestamp), build.incremental ?? '']
  if (build.size) parts.push(formatSize(build.size))
  if (build.type) parts.push(build.type)
  return parts.filter(Boolean).join(' · ')
}

/** Primary key of a release row: `device/channel`. */
function rowKey(release: ReleaseConfig): string {
  return `${release.device}/${release.channel}`
}

// Disclosure state is keyed by `device/channel` (the row key), so expanding a
// row survives re-sorting and refreshes that keep the same releases.
const expanded = ref<Set<string>>(new Set())

function isOpen(release: ReleaseConfig): boolean {
  return expanded.value.has(rowKey(release))
}

function toggle(release: ReleaseConfig): void {
  const key = rowKey(release)
  const next = new Set(expanded.value)
  if (next.has(key)) next.delete(key)
  else next.add(key)
  expanded.value = next
}

function detailsId(index: number): string {
  return `release-details-${index}`
}
</script>

<template>
  <div id="releases-section" class="admin-table-panel">
    <NSpin :show="loading" size="small">
      <div
        class="admin-table-wrap"
        tabindex="0"
        role="region"
        :aria-label="`已发布版本，共 ${rows.length} 项，可横向滚动`"
      >
        <NTable class="admin-table" size="small" :bordered="false" aria-label="已发布版本">
          <thead>
            <tr>
              <th scope="col">目标</th>
              <th scope="col">版本</th>
              <th scope="col">包 / 来源</th>
              <th scope="col">操作</th>
            </tr>
          </thead>
          <tbody>
            <template v-for="(release, index) in rows" :key="rowKey(release)">
              <tr>
                <td>
                  <div class="admin-cell-strong">{{ release.device }}</div>
                  <div class="admin-cell-secondary">
                    <NTag :bordered="false" size="small">{{ release.channel }}</NTag>
                  </div>
                </td>
                <td>
                  <span class="admin-mono admin-cell-strong">{{ release.version }}</span>
                  <div class="admin-cell-secondary">
                    <NTag
                      :type="release.delivery === 'browser' ? 'info' : 'success'"
                      :bordered="false"
                      size="small"
                    >
                      {{ release.delivery === 'browser' ? '浏览器' : '应用内' }}
                    </NTag>
                  </div>
                </td>
                <td>
                  <span v-if="release.file" class="admin-mono admin-trunc" :title="release.file">
                    {{ release.file }}
                  </span>
                  <a
                    v-if="release.download_url"
                    class="admin-mono admin-trunc admin-link"
                    :href="release.download_url"
                    target="_blank"
                    rel="noreferrer"
                    :title="release.download_url"
                  >
                    {{ release.download_url }}
                  </a>
                  <div
                    v-if="release.file && !knownFiles.has(release.file)"
                    class="admin-cell-secondary"
                  >
                    <NTag type="error" :bordered="false" size="small">包不存在</NTag>
                  </div>
                  <div
                    v-else-if="release.delivery === 'browser' && !release.file"
                    class="admin-cell-secondary"
                  >
                    <NTag type="info" :bordered="false" size="small">外部发布</NTag>
                  </div>
                  <span v-if="!release.file && !release.download_url" class="admin-muted">—</span>
                </td>
                <td class="rt-actions-cell">
                  <div class="rt-actions">
                    <NButton
                      size="small"
                      quaternary
                      :aria-expanded="isOpen(release)"
                      :aria-controls="detailsId(index)"
                      :aria-label="`${isOpen(release) ? '收起' : '展开'} ${rowKey(release)} 的详情`"
                      @click="toggle(release)"
                    >
                      <template #icon>
                        <AdminIcon :name="isOpen(release) ? 'chevron-down' : 'chevron-right'" :size="15" />
                      </template>
                      详情
                    </NButton>
                    <NButton
                      size="small"
                      secondary
                      :aria-label="`编辑 ${rowKey(release)}`"
                      @click="emit('edit', release)"
                    >
                      <template #icon><AdminIcon name="edit" :size="15" /></template>
                      编辑
                    </NButton>
                    <NButton
                      size="small"
                      type="error"
                      secondary
                      :aria-label="`下架 ${rowKey(release)}`"
                      @click="emit('unpublish', release)"
                    >
                      下架
                    </NButton>
                  </div>
                </td>
              </tr>
              <tr v-show="isOpen(release)">
                <td :id="detailsId(index)" colspan="4">
                  <div class="admin-row-details rt-details">
                    <div class="admin-detail-grid">
                      <div v-if="release.file" class="admin-detail-item">
                        <span class="admin-detail-label">文件</span>
                        <span class="admin-detail-value admin-mono">{{ release.file }}</span>
                      </div>
                      <div v-if="release.download_url" class="admin-detail-item">
                        <span class="admin-detail-label">下载地址</span>
                        <a
                          class="admin-detail-value admin-mono admin-link"
                          :href="release.download_url"
                          target="_blank"
                          rel="noreferrer"
                        >
                          {{ release.download_url }}
                        </a>
                      </div>
                      <div class="admin-detail-item">
                        <span class="admin-detail-label">构建信息</span>
                        <span class="admin-detail-value admin-mono">
                          {{ buildSummary(release) }}
                        </span>
                      </div>
                      <div v-if="release.changelog" class="admin-detail-item">
                        <span class="admin-detail-label">更新说明</span>
                        <span class="admin-detail-value rt-changelog">{{ release.changelog }}</span>
                      </div>
                    </div>
                  </div>
                </td>
              </tr>
            </template>
            <tr v-if="!rows.length">
              <td colspan="4">
                <div class="admin-empty">
                  <NEmpty description="暂无版本">
                    <template #icon><AdminIcon name="release" :size="28" /></template>
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

.rt-actions-cell {
  text-align: right;
}

.rt-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  justify-content: flex-end;
}

/* The colspan cell inherits the table's centred empty-state alignment. */
.rt-details {
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

.rt-changelog {
  font-size: 12.5px;
}

.rt-empty {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 200px;
}
</style>
