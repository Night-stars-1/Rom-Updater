// Wire types for the Rom-Updater admin API. Field names and optionality mirror
// the Go structs exactly (catalog.go adminState/packageState/releaseConfig,
// admin.go uploadResponse); every optional key is absent from the JSON rather
// than null because the server uses omitempty.

export type Delivery = 'app' | 'browser'
export type PackageType = '' | 'full' | 'incremental'

export interface ReleaseConfig {
  device: string
  channel: string
  version: string
  changelog: string
  /** Present only for browser delivery. */
  delivery?: Delivery
  /** Empty string for external (browser-only) releases. */
  file: string
  download_url?: string
  /** Derived from the package unless delivery is browser without a file. */
  build_timestamp?: number
  incremental?: string
  size?: number
  type?: PackageType
}

export interface PackageState {
  file: string
  size: number
  /** Go time.Time marshalled as RFC3339 with nanoseconds. */
  mod_time: string
  /** Empty until the file has been hashed. */
  sha256?: string
  type?: string
  devices?: string[]
  build_timestamp?: number
  incremental?: string
  /** Why the package cannot be read as an OTA package. */
  error?: string
  /** Always present; entries are "device/channel". */
  used_by: string[]
}

export interface AdminState {
  base_url: string
  /** Set when the manifest or a package is broken; clients then get 503. */
  error?: string
  releases: ReleaseConfig[]
  packages: PackageState[]
}

export interface ReleaseResponse {
  schema_version: 1 | 2
  device: string
  channel: string
  version: string
  build_timestamp: number
  incremental: string
  changelog: string
  delivery?: 'browser'
  download_url?: string
  package?: { type?: string; url?: string; size?: number; sha256?: string }
}

export interface UploadResponse {
  file: string
  size: number
  sha256: string
  type: string
  devices: string[]
  build_timestamp: number
  incremental: string
}

/** Editable publish form model. Every free-text control stays a string so the
 *  form never coerces (or normalises) what the user typed; domain.ts owns the
 *  strict parsing. */
export interface PublishDraft {
  device: string
  channel: string
  version: string
  changelog: string
  file: string
  delivery: Delivery
  download_url: string
  build_time: string
  incremental: string
  size: string
  type: PackageType
}

export type NotifyKind = 'success' | 'error' | 'info'
export type Notify = (message: string, kind?: NotifyKind) => void

export interface ConfirmOptions {
  title: string
  message: string
  positiveText: string
  danger?: boolean
}
export type Confirm = (options: ConfirmOptions) => Promise<boolean>
