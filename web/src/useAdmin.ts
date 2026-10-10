// Owns authentication, API calls, streaming uploads and the publish field matrix.
// Components supply notification and confirmation adapters; persistent manifest
// errors remain in state.error and refresh status in stateLoadError.

import { computed, onBeforeUnmount, onMounted, reactive, ref, shallowRef, watch } from 'vue'
import type { ComputedRef, Ref, ShallowRef } from 'vue'
import { formatSize, localTimeToUnix, toLocalInput, validatePublishDraft, validateUploadForm } from './domain'
import type { AdminState, Confirm, Notify, PackageState, PackageType, PublishDraft, ReleaseConfig, ReleaseResponse, UploadResponse } from './types'

const TOKEN_KEY = 'ota-admin-token'
const UNREADABLE = '服务端响应无法解析。'

/** Raised after a 401 has already been handled by logout(); callers must not
 *  show a second notification for it. */
class AuthError extends Error {
  constructor() {
    super('unauthorized')
    this.name = 'AuthError'
  }
}

/** The Credential Management API's `PasswordCredential` was dropped from
 *  lib.dom, so its constructor is described here and looked up at runtime. */
type PasswordCredentialConstructor = new (init: { id: string; name: string; password: string }) => Credential

/** POST /admin/releases body. The server decodes with DisallowUnknownFields, so
 *  derived fields are only present for an external browser release. */
interface ReleaseRequest {
  device: string
  channel: string
  version: string
  changelog: string
  file: string
  delivery?: 'browser'
  download_url?: string
  build_timestamp?: number
  incremental?: string
  size?: number
  type?: PackageType
}

function messageOf(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
}

export interface UseAdminOptions {
  notify: Notify
  confirm: Confirm
  /** Dismisses a confirm dialog that is still open, so it cannot outlive the
   *  session. The UI adapter resolves the pending promise with `false`. */
  closeConfirm?: () => void
}

export interface PublishOptions {
  release?: ReleaseConfig
  file?: string
}

/** Everything the admin UI consumes. Refs are returned unwrapped-by-value so a
 *  component can destructure them and let the template unwrap `.value`. */
export interface AdminController {
  state: ShallowRef<AdminState | null>
  token: Ref<string>
  loginBusy: Ref<boolean>
  loading: Ref<boolean>
  lastLoadedAt: ShallowRef<Date | null>
  stateLoadError: Ref<string>
  authenticated: ComputedRef<boolean>
  uploadFile: ShallowRef<File | null>
  uploadSHA: Ref<string>
  uploadStatus: Ref<string>
  uploadProgress: Ref<number | null>
  uploading: Ref<boolean>
  uploadError: Ref<string>
  publishOpen: Ref<boolean>
  publishBusy: Ref<boolean>
  editing: ShallowRef<ReleaseConfig | null>
  publishError: Ref<string>
  draft: PublishDraft
  selectedPackage: ComputedRef<PackageState | undefined>
  externalRelease: ComputedRef<boolean>
  publishWarning: ComputedRef<string>
  fileOptions: ComputedRef<Array<{ label: string; value: string; disabled?: boolean }>>
  login: () => Promise<boolean>
  logout: (reason?: string) => void
  refresh: (notifyRefresh?: boolean) => Promise<boolean>
  upload: () => void
  cancelUpload: () => void
  openPublish: (options?: PublishOptions) => void
  closePublish: () => void
  publish: () => Promise<boolean>
  unpublish: (release: ReleaseConfig) => Promise<void>
  deletePackage: (pkg: PackageState) => Promise<void>
}

export function useAdmin({ notify, confirm, closeConfirm }: UseAdminOptions): AdminController {
  const state = shallowRef<AdminState | null>(null)
  const token = ref('')
  const loginBusy = ref(false)
  const loading = ref(false)
  const lastLoadedAt = shallowRef<Date | null>(null)
  const stateLoadError = ref('')
  const authenticated = computed(() => state.value !== null)

  const uploadFile = shallowRef<File | null>(null)
  const uploadSHA = ref('')
  const uploadStatus = ref('')
  const uploadProgress = ref<number | null>(null)
  const uploading = ref(false)
  const uploadError = ref('')

  const publishOpen = ref(false)
  const publishBusy = ref(false)
  const editing = shallowRef<ReleaseConfig | null>(null)
  const publishError = ref('')
  const draft: PublishDraft = reactive<PublishDraft>({
    device: '',
    channel: '',
    version: '',
    changelog: '',
    file: '',
    delivery: 'app',
    download_url: '',
    build_time: '',
    incremental: '',
    size: '',
    type: '',
  })

  const selectedPackage = computed(() => state.value?.packages.find(pkg => pkg.file === draft.file))
  const externalRelease = computed(() => draft.delivery === 'browser' && !draft.file)
  const publishWarning = computed(() => {
    const pkg = selectedPackage.value
    const device = draft.device.trim()
    return pkg && device && pkg.devices && !pkg.devices.includes(device)
      ? `该包的 pre-device 为 ${pkg.devices.join(', ')}，不包含 ${device}，服务端会拒绝发布。`
      : ''
  })
  const fileOptions = computed<Array<{ label: string; value: string; disabled?: boolean }>>(() => {
    const options: Array<{ label: string; value: string; disabled?: boolean }> = [
      { label: '（不使用本服务的包，仅外部链接）', value: '', disabled: draft.delivery !== 'browser' },
    ]
    const files = (state.value?.packages ?? []).filter(pkg => !pkg.error).map(pkg => pkg.file)
    // A release may point at a package that is no longer listed; keep it
    // selectable so editing such a release cannot silently change its target.
    if (draft.file && !files.includes(draft.file)) files.unshift(draft.file)
    for (const file of files) options.push({ label: file, value: file })
    return options
  })

  let uploadXHR: XMLHttpRequest | null = null

  function logout(reason?: string): void {
    sessionStorage.removeItem(TOKEN_KEY)
    token.value = ''
    state.value = null
    stateLoadError.value = ''
    lastLoadedAt.value = null
    publishOpen.value = false
    publishError.value = ''
    uploadError.value = ''
    // A confirm left open must not outlive the session; the UI resolves its
    // pending promise with false.
    closeConfirm?.()
    if (reason) notify(reason, 'error')
  }

  /** Typed admin request. The server always answers 200 with a JSON body; a
   *  204 (used by the DELETE endpoints, whose results callers ignore) resolves
   *  to `null`. */
  async function api<T>(method: string, path: string, body?: unknown): Promise<T> {
    const headers: Record<string, string> = { Authorization: 'Bearer ' + (sessionStorage.getItem(TOKEN_KEY) ?? '') }
    const init: RequestInit = { method, headers, cache: 'no-store' }
    if (body !== undefined) {
      headers['Content-Type'] = 'application/json'
      init.body = JSON.stringify(body)
    }
    const response = await fetch(path, init)
    if (response.status === 401) {
      logout('令牌无效或已更换，请重新登录。')
      throw new AuthError()
    }
    const text = await response.text()
    if (!response.ok) throw new Error(text.trim() || `${response.status} ${response.statusText}`)
    return (text ? JSON.parse(text) : null) as T
  }

  /** Load the manifest. `authenticatedState` short-circuits the request right
   *  after login; a failure keeps whatever was last rendered. */
  async function load(authenticatedState?: AdminState, notifyRefresh = false): Promise<boolean> {
    loading.value = true
    try {
      const loaded = authenticatedState ?? (await api<AdminState>('GET', '/admin/state'))
      state.value = loaded
      stateLoadError.value = loaded.error ? '清单异常 · 客户端 503' : ''
      lastLoadedAt.value = new Date()
      if (notifyRefresh && !loaded.error) notify('清单已刷新', 'success')
      return true
    } catch (error) {
      if (error instanceof AuthError) return false
      stateLoadError.value = '刷新失败 · 显示上次数据'
      notify('加载失败：' + messageOf(error), 'error')
      return false
    } finally {
      loading.value = false
    }
  }

  function refresh(notifyRefresh?: boolean): Promise<boolean> {
    return load(undefined, notifyRefresh)
  }

  async function login(): Promise<boolean> {
    if (loginBusy.value) return false
    const secret = token.value
    if (!secret) {
      notify('请填写或选择管理令牌。', 'error')
      return false
    }
    loginBusy.value = true
    try {
      // Authenticate first, without committing the candidate key to the session.
      const response = await fetch('/admin/state', { headers: { Authorization: 'Bearer ' + secret }, cache: 'no-store' })
      const text = await response.text()
      if (!response.ok) throw new Error(text.trim() || `${response.status} ${response.statusText}`)
      // The body is /admin/state itself, so a successful login skips a second request.
      const authenticatedState = JSON.parse(text) as AdminState
      sessionStorage.setItem(TOKEN_KEY, secret)
      let credentialHandedOff = false
      // Best-effort handoff: browsers are free to ignore or decline it, and that
      // must never block a valid login.
      const candidate: unknown = Reflect.get(window, 'PasswordCredential')
      // The value is only ever used after the runtime check above.
      const PasswordCredentialCtor =
        typeof candidate === 'function' ? (candidate as PasswordCredentialConstructor) : null
      if (window.isSecureContext && PasswordCredentialCtor && navigator.credentials?.store) {
        try {
          await navigator.credentials.store(new PasswordCredentialCtor({ id: 'admin', name: 'Rom-Updater 管理员', password: secret }))
          credentialHandedOff = true
        } catch {
          // Unsupported or declined password storage.
        }
      }
      await load(authenticatedState)
      notify('登录成功', 'success')
      // Keep unsupported or declined handoffs recognisable to standard password
      // managers until the admin logs out.
      if (credentialHandedOff) token.value = ''
      return true
    } catch (error) {
      sessionStorage.removeItem(TOKEN_KEY)
      notify('登录失败：' + messageOf(error), 'error')
      return false
    } finally {
      loginBusy.value = false
    }
  }

  // Upload uses XHR rather than fetch: fetch exposes no upload progress and
  // packages are multiple GB.
  function upload(): void {
    if (uploadXHR) return
    uploadError.value = ''
    const file = uploadFile.value
    if (!file) {
      uploadError.value = '请填写或选择文件（*.zip）。'
      return
    }
    const sha = uploadSHA.value.trim()
    const problem = validateUploadForm(file, sha)
    if (problem) {
      uploadError.value = problem
      return
    }

    let path = '/admin/files/' + encodeURIComponent(file.name)
    if (sha) path += '?sha256=' + encodeURIComponent(sha.toLowerCase())
    const xhr = new XMLHttpRequest()
    uploadXHR = xhr
    xhr.open('PUT', path)
    xhr.setRequestHeader('Authorization', 'Bearer ' + (sessionStorage.getItem(TOKEN_KEY) ?? ''))
    xhr.setRequestHeader('Content-Type', 'application/zip')

    const started = performance.now()
    uploading.value = true
    uploadProgress.value = null
    uploadStatus.value = '连接中…'

    xhr.upload.onprogress = event => {
      if (!event.lengthComputable) return
      const complete = event.loaded >= event.total
      const rate = event.loaded / ((performance.now() - started) / 1000)
      uploadStatus.value = complete
        ? '上传完成，服务端校验中…'
        : `${formatSize(event.loaded)} / ${formatSize(event.total)} · ${formatSize(rate)}/s`
      uploadProgress.value = complete ? null : (event.loaded / event.total) * 100
    }

    const done = (message: string) => {
      uploadXHR = null
      uploading.value = false
      uploadStatus.value = message
      uploadProgress.value = uploadProgress.value === null ? 0 : uploadProgress.value
      if (message.startsWith('上传失败') || message.startsWith('上传中断')) notify(message, 'error')
    }

    xhr.onload = () => {
      if (xhr.status === 401) {
        done('')
        logout('令牌无效或已更换，请重新登录。')
        return
      }
      if (xhr.status !== 201) {
        done('上传失败：' + (xhr.responseText.trim() || String(xhr.status)))
        return
      }
      let uploaded: UploadResponse
      try {
        uploaded = JSON.parse(xhr.responseText) as UploadResponse
      } catch {
        done('上传失败：' + UNREADABLE)
        return
      }
      uploadProgress.value = 100
      done(`已上传 ${uploaded.file}：${uploaded.type}，设备 ${uploaded.devices.join(', ')}，${uploaded.incremental}`)
      notify('已上传 ' + uploaded.file, 'success')
      uploadFile.value = null
      uploadSHA.value = ''
      // A successful upload opens the publish dialog with that package selected.
      void load().then(ok => {
        if (ok) openPublish({ file: uploaded.file })
      })
    }
    xhr.onerror = () => done('上传中断：网络错误')
    xhr.onabort = () => {
      done('已取消')
      notify('已取消上传')
    }
    xhr.send(file)
  }

  function cancelUpload(): void {
    uploadXHR?.abort()
  }

  function openPublish(options: PublishOptions = {}): void {
    const source = options.release ?? null
    const pkg = state.value?.packages.find(item => item.file === options.file)
    const singleDevice = pkg && pkg.devices && pkg.devices.length === 1 ? pkg.devices[0] : ''
    const size = source?.size
    editing.value = source
    draft.device = source?.device || singleDevice
    draft.channel = source?.channel || ''
    draft.version = source?.version || ''
    draft.delivery = source?.delivery === 'browser' ? 'browser' : 'app'
    draft.download_url = source?.download_url || ''
    draft.changelog = source?.changelog || ''
    draft.build_time = source?.file ? '' : toLocalInput(source?.build_timestamp)
    draft.incremental = source?.file ? '' : source?.incremental || ''
    draft.size = source && !source.file && size ? String(size) : ''
    draft.type = source?.file ? '' : source?.type || ''
    draft.file = (source ? source.file : options.file) || ''
    if (draft.delivery !== 'browser' && !draft.file) {
      draft.file = (state.value?.packages ?? []).find(item => !item.error)?.file ?? ''
    }
    publishError.value = ''
    publishOpen.value = true
  }

  function closePublish(): void {
    publishOpen.value = false
  }

  // The app-delivery form always targets a real package, so switching back to
  // it with nothing selected picks the first publishable one (legacy
  // syncPublishForm behaviour).
  watch(
    () => draft.delivery,
    delivery => {
      if (delivery !== 'browser' && !draft.file) {
        draft.file = (state.value?.packages ?? []).find(pkg => !pkg.error)?.file ?? ''
      }
    },
  )

  /** Request body for POST /admin/releases. Derived fields are only sent for an
   *  external browser release; app and hosted-browser releases derive them
   *  server-side, and DisallowUnknownFields rejects stray keys. */
  function publishBody(): ReleaseRequest {
    const body: ReleaseRequest = {
      device: draft.device.trim(),
      channel: draft.channel.trim(),
      version: draft.version.trim(),
      changelog: draft.changelog,
      file: draft.file,
    }
    if (draft.delivery !== 'browser') return body
    body.delivery = 'browser'
    const downloadURL = draft.download_url.trim()
    if (downloadURL) body.download_url = downloadURL
    if (!draft.file) {
      const timestamp = localTimeToUnix(draft.build_time)
      if (timestamp !== null) body.build_timestamp = timestamp
      body.incremental = draft.incremental.trim()
      const size = draft.size.trim()
      if (size) body.size = Number(size)
      if (draft.type) body.type = draft.type
    }
    return body
  }

  async function publish(): Promise<boolean> {
    if (publishBusy.value) return false
    publishError.value = ''
    const problem = validatePublishDraft(draft)
    if (problem) {
      publishError.value = problem
      return false
    }
    const body = publishBody()
    const exists = (state.value?.releases ?? []).some(
      release => release.device === body.device && release.channel === body.channel,
    )
    publishBusy.value = true
    try {
      // Only a new release can overwrite an existing entry; editing keeps its
      // device/channel and therefore always updates in place.
      if (!editing.value && exists) {
        const accepted = await confirm({
          title: '覆盖现有发布',
          message: `${body.device}/${body.channel} 已有发布版本。\n确定用 ${body.version} 覆盖？`,
          positiveText: '确认覆盖',
          danger: true,
        })
        if (!accepted) return false
      }
      const response = await api<ReleaseResponse>('POST', '/admin/releases', body)
      publishOpen.value = false
      notify(`已发布 ${response.device}/${response.channel} → ${response.version}`, 'success')
      void load()
      return true
    } catch (error) {
      // A 401 already logged out and notified; just get out of the dialog.
      if (error instanceof AuthError) publishOpen.value = false
      else notify('发布失败：' + messageOf(error), 'error')
      return false
    } finally {
      publishBusy.value = false
    }
  }

  async function unpublish(release: ReleaseConfig): Promise<void> {
    const accepted = await confirm({
      title: '下架发布版本',
      message: `下架 ${release.device}/${release.channel}（${release.version}）？\n客户端将查询不到该通道的更新，包文件保留。`,
      positiveText: '确认下架',
      danger: true,
    })
    if (!accepted) return
    try {
      await api<null>('DELETE', `/admin/releases/${encodeURIComponent(release.device)}/${encodeURIComponent(release.channel)}`)
      notify(`已下架 ${release.device}/${release.channel}`, 'success')
    } catch (error) {
      if (!(error instanceof AuthError)) notify('下架失败：' + messageOf(error), 'error')
    }
    void load()
  }

  async function deletePackage(pkg: PackageState): Promise<void> {
    const accepted = await confirm({
      title: '永久删除 OTA 包',
      message: `永久删除 ${pkg.file}？\n此操作不可撤销。`,
      positiveText: '永久删除',
      danger: true,
    })
    if (!accepted) return
    try {
      await api<null>('DELETE', '/admin/files/' + encodeURIComponent(pkg.file))
      notify('已删除 ' + pkg.file, 'success')
    } catch (error) {
      if (!(error instanceof AuthError)) notify('删除失败：' + messageOf(error), 'error')
    }
    void load()
  }

  function handleBeforeUnload(event: BeforeUnloadEvent): void {
    if (uploadXHR) event.preventDefault()
  }

  onMounted(() => {
    window.addEventListener('beforeunload', handleBeforeUnload)
    if (sessionStorage.getItem(TOKEN_KEY)) void load()
    else logout()
  })
  onBeforeUnmount(() => {
    window.removeEventListener('beforeunload', handleBeforeUnload)
  })

  return {
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
  }
}
