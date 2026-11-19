// Small helpers shared by the Move pages.
import { V1 } from './api'
import type { MoveIn, MoveOut } from './types'

/** Uploads a file as the raw body, reporting progress (0–1). */
export function uploadFile(file: File, onProgress: (p: number) => void): { promise: Promise<MoveIn>; abort: () => void } {
  const xhr = new XMLHttpRequest()
  const promise = new Promise<MoveIn>((resolve, reject) => {
    xhr.open('POST', `${V1}/in?name=${encodeURIComponent(file.name)}`)
    xhr.withCredentials = true
    xhr.setRequestHeader('Content-Type', 'application/octet-stream')
    xhr.upload.onprogress = e => { if (e.lengthComputable) onProgress(e.loaded / e.total) }
    xhr.onload = () => {
      let body: { error?: string } & Partial<MoveIn> = {}
      try { body = JSON.parse(xhr.responseText) } catch { /* not JSON */ }
      if (xhr.status >= 200 && xhr.status < 300) resolve(body as MoveIn)
      else {
        if (xhr.status === 401) window.dispatchEvent(new CustomEvent('mv:unauthorized'))
        reject(new Error(body.error || `Upload failed (HTTP ${xhr.status})`))
      }
    }
    xhr.onerror = () => reject(new Error('The upload stopped: the connection dropped.'))
    xhr.onabort = () => reject(new Error('Upload cancelled.'))
    xhr.send(file)
  })
  return { promise, abort: () => xhr.abort() }
}

export const SOURCE_ICON: Record<string, string> = {
  'google-takeout': 'globe', evernote: 'note', notion: 'file', apple: 'apple', 'pocket-move': 'package', files: 'file',
}

export const IN_STATUS: Record<MoveIn['status'], string> = {
  uploading: 'Uploading', scanning: 'Reading', ready: 'Ready to bring in', running: 'Bringing in', done: 'Done', failed: 'Did not work', expired: 'Removed',
}

export const OUT_STATUS: Record<MoveOut['status'], string> = {
  queued: 'Waiting', running: 'Being made', done: 'Ready', failed: 'Did not work', expired: 'Removed',
}

export const busyIn = (m: MoveIn) => m.status === 'uploading' || m.status === 'scanning' || m.status === 'running'
export const busyOut = (m: MoveOut) => m.status === 'queued' || m.status === 'running'

/** Plain words for an app's answer ({imported: 12, skipped: 3}). */
export function resultLine(r: Record<string, unknown> | null): string {
  if (!r) return ''
  const words: Record<string, string> = { imported: 'added', added: 'added', created: 'added', new: 'added', skipped: 'already there', already: 'already there', failed: 'did not work', merged: 'merged', updated: 'updated' }
  const parts: string[] = []
  for (const [k, v] of Object.entries(r)) {
    if (typeof v !== 'number') continue
    const w = words[k]
    if (w) parts.push(`${v.toLocaleString()} ${w}`)
  }
  if (!parts.length) {
    for (const [k, v] of Object.entries(r)) if (typeof v === 'number') parts.push(`${v.toLocaleString()} ${k.replace(/_/g, ' ')}`)
  }
  return parts.slice(0, 4).join(' · ')
}
