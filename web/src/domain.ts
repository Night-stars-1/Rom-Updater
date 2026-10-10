// Pure presentation + validation helpers. Nothing here touches the DOM, Vue or
// the network, so every rule below can be reasoned about (and unit tested) in
// isolation. Messages are copied verbatim from the legacy admin page so the
// feedback a user sees does not change.

import type { PublishDraft } from './types'

const SIZE_UNITS = ['B', 'KiB', 'MiB', 'GiB', 'TiB']

/** Human readable size. `0`/absent renders as an em dash, matching the legacy
 *  tables; the overview metric prints "0 B" itself. */
export function formatSize(bytes: number): string {
  if (!bytes) return '—'
  let i = 0
  let v = bytes
  while (v >= 1024 && i < SIZE_UNITS.length - 1) {
    v /= 1024
    i++
  }
  return (i ? v.toFixed(2) : String(v)) + ' ' + SIZE_UNITS[i]
}

/** Unix seconds rendered in the browser's local timezone. */
export function formatTimestamp(unixSeconds?: number): string {
  return unixSeconds ? new Date(unixSeconds * 1000).toLocaleString() : '—'
}

/** Absolute download URL for a hosted package under the configured base URL. */
export function packageURL(baseURL: string, file: string): string {
  return new URL(encodeURIComponent(file), baseURL.replace(/\/?$/, '/')).href
}

/**
 * Parse a local wall-clock timestamp written as `YYYY-MM-DDTHH:mm:ss`.
 *
 * The value is built field by field and compared back to the input, so any
 * normalisation the engine would apply — 31 February, or a wall time skipped by
 * a daylight-saving transition — returns `null` instead of silently moving to a
 * different instant. Callers additionally require `getTime() > 0` (strictly
 * after the Unix epoch).
 */
export function parseLocalTime(value: string): Date | null {
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})$/.exec(value)
  if (!match) return null
  const parts = match.slice(1).map(Number)
  const [year, month, day, hour, minute, second] = parts
  if (year < 1 || month < 1 || month > 12 || day < 1 || day > 31 || hour > 23 || minute > 59 || second > 59) return null
  const date = new Date(0)
  date.setFullYear(year, month - 1, day)
  date.setHours(hour, minute, second, 0)
  const actual = [date.getFullYear(), date.getMonth() + 1, date.getDate(), date.getHours(), date.getMinutes(), date.getSeconds()]
  return parts.every((n, i) => n === actual[i]) ? date : null
}

/** Format unix seconds back into the `YYYY-MM-DDTHH:mm:ss` local input shape. */
export function toLocalInput(unixSeconds?: number): string {
  if (!unixSeconds) return ''
  const d = new Date(unixSeconds * 1000)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${String(d.getFullYear()).padStart(4, '0')}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

/** Local wall time as Unix seconds; `null` when the value is invalid, rolled
 *  over, in a DST gap, or not strictly after the epoch. */
export function localTimeToUnix(value: string): number | null {
  const date = parseLocalTime(value)
  if (!date || date.getTime() <= 0) return null
  return Math.floor(date.getTime() / 1000)
}

/**
 * Browser-delivery download URL contract (mirrors catalog.go
 * validateDownloadURL): absolute https, a host, no userinfo and no fragment.
 * The raw string is also checked for `#` so a trailing fragment cannot slip
 * through URL normalisation. Returns `''` when valid.
 */
export function validateDownloadURL(value: string): string {
  const raw = value.trim()
  if (!raw) return ''
  let url: URL | null = null
  try {
    url = new URL(raw)
  } catch {
    url = null
  }
  if (!url || url.protocol !== 'https:' || !url.hostname || url.username || url.password || url.hash || raw.includes('#')) {
    return '下载地址必须是绝对 HTTPS 链接，不能包含账号密码或片段（#）。'
  }
  return ''
}

/** Upload form validation, in the legacy order (file, sha, then extension). */
export function validateUploadForm(file: File | null, sha: string): string {
  if (!file) return '请填写或选择文件（*.zip）。'
  if (sha && !/^[0-9a-fA-F]{64}$/.test(sha)) return 'SHA-256（可选）格式不符合要求。'
  if (!/\.zip$/i.test(file.name)) return '请选择扩展名为 .zip 的 OTA 包。'
  return ''
}

/**
 * Publish form validation, following the DOM order of the legacy form so the
 * first message a user sees is unchanged. Returns `''` when the draft is valid.
 *
 * Field visibility/requirement matrix (mirrors validateRelease in catalog.go):
 * - app delivery: `file` required; download_url and the four derived fields are
 *   disabled and never sent.
 * - browser + hosted package: `file` optional-to-empty is not allowed (a file
 *   is chosen by default in app mode); download_url optional but must be a
 *   valid https URL when filled.
 * - browser without a package (external): download_url, build_time and
 *   incremental required; size optional but must be a safe non-negative
 *   integer.
 */
export function validatePublishDraft(draft: PublishDraft): string {
  const browser = draft.delivery === 'browser'
  const external = browser && !draft.file
  const downloadURL = draft.download_url.trim()
  const size = draft.size.trim()
  const sizeIsDigits = /^[0-9]+$/.test(size)

  if (!draft.device.trim()) return '请填写或选择设备。'
  if (!draft.channel.trim()) return '请填写或选择通道。'
  if (!draft.version.trim()) return '请填写或选择版本。'
  if (!browser && !draft.file) return '请填写或选择OTA 包。'
  if (browser) {
    if (external && !downloadURL) return '请填写或选择下载地址。'
    if (downloadURL) {
      // Mirrors the `type="url"` constraint: any absolute URL is syntactically
      // acceptable here; the https contract is enforced further down.
      let absolute = true
      try {
        new URL(downloadURL)
      } catch {
        absolute = false
      }
      if (!absolute) return '下载地址格式无效。'
    }
  }
  if (external && !draft.incremental.trim()) return '请填写或选择Incremental。'
  if (external && size && !sizeIsDigits) return '大小（字节）格式不符合要求。'

  if (browser && downloadURL) {
    const message = validateDownloadURL(downloadURL)
    if (message) return message
  }
  if (external) {
    if (localTimeToUnix(draft.build_time) === null) return '请选择有效的构建日期与本地时间（时间须晚于 Unix 纪元）。'
    if (size && !(sizeIsDigits && Number.isSafeInteger(Number(size)))) {
      return '大小必须是非负整数，且不能超过 9007199254740991 字节。'
    }
  }
  return ''
}
