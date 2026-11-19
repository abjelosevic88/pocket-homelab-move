export interface User { id: number; email: string; name: string; role: 'admin' | 'member'; created_at: string }
export interface APIToken { id: number; user_id: number; name: string; prefix: string; last_used_at: string | null; created_at: string }

export interface Settings {
  core_configured: boolean
  core_registered?: boolean
  demo_links?: boolean
  title: string; date_format: string; time_format: string
  notify_url: string; notify_format: string; notify_configured: boolean; notify_done: boolean
  open_formats: boolean
  setup_complete: boolean; base_url: string
  keep_days: number; max_upload_bytes: number
}

// ---- apps ----

export interface LinkState { on: boolean; state: string; reason?: string }
export interface PortableExport { path: string; file: string; kind: string; title?: string }
export interface PortableImport { path: string; kind: string; form?: string; content_type?: string; max_bytes?: number; title?: string }
export interface AppView {
  slug: string; name: string
  out: LinkState; in: LinkState
  exports: PortableExport[]; imports: PortableImport[]
  error?: string
}
export interface AppsResponse { core: boolean; error?: string; apps: AppView[] }

// ---- moving out ----

export interface OutFile { path: string; kind: string; title?: string; bytes: number; sha256: string }
export interface OutAppResult { app: string; name: string; state: 'waiting' | 'running' | 'done' | 'failed' | 'skipped'; files: OutFile[]; error?: string }
export interface MoveOut {
  id: number; status: 'queued' | 'running' | 'done' | 'failed' | 'expired'
  apps: string[]; results: OutAppResult[]; size_bytes: number; error: string
  created_at: string; finished_at: string; expires_at: string
}

// ---- moving in ----

export interface MoveIn {
  id: number; status: 'uploading' | 'scanning' | 'ready' | 'running' | 'done' | 'failed' | 'expired'
  name: string; source: string; source_name: string; size_bytes: number; error: string; notes: string[]
  created_at: string; finished_at: string
}
export interface Target { app: string; name: string; import: PortableImport }
export interface Item {
  id: number; move_id: number; label: string; detail: string; kind: string; count: number
  file_name: string; bytes: number; sha256: string
  target: string; path: string; extra: Record<string, string>
  include: boolean; manual: string
  status: 'waiting' | 'sent' | 'failed' | 'skipped' | 'done-before' | 'manual'
  result: Record<string, number | string | string[]> | null; error: string; sent_at: string
  options: Target[]; hint?: string
}
export interface MoveInDetail { move: MoveIn; items: Item[]; core: boolean }
