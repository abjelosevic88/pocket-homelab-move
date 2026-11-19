import { useCallback, useEffect, useRef, useState } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Item, MoveIn, MoveInDetail } from '../lib/types'
import { ago, fmtBytes, plural } from '../lib/format'
import { IN_STATUS, SOURCE_ICON, busyIn, resultLine, uploadFile } from '../lib/move'
import { Icon } from '../components/icons'
import { Confirm, Empty, Loading, Switch, errMsg, useToast } from '../components/ui'
import { Select } from '../components/Select'
import { announceChange, useCrumb, useOnChange } from '../components/Layout'

// ---------- the list and the drop zone ----------

export default function MovingIn() {
  const { settings } = useApp()
  const navigate = useNavigate()
  const { toast } = useToast()
  const [sp, setSp] = useSearchParams()
  const [list, setList] = useState<MoveIn[] | null>(null)
  const [up, setUp] = useState<{ name: string; p: number; abort: () => void } | null>(null)
  const [drag, setDrag] = useState(false)
  const input = useRef<HTMLInputElement>(null)
  const load = useCallback(() => { api.get<MoveIn[]>(`${V1}/in`).then(setList).catch(() => setList([])) }, [])
  useEffect(load, [load])
  useOnChange(load)
  useEffect(() => {
    if (!list?.some(busyIn)) return
    const t = setTimeout(load, 2500)
    return () => clearTimeout(t)
  }, [list, load])
  useEffect(() => { if (sp.get('pick')) { setSp(new URLSearchParams(), { replace: true }); setTimeout(() => input.current?.click(), 50) } }, []) // eslint-disable-line react-hooks/exhaustive-deps

  const send = (file: File) => {
    if (up) return
    const max = settings?.max_upload_bytes || 0
    if (max && file.size > max) { toast(`That file is ${fmtBytes(file.size)}; this box takes up to ${fmtBytes(max)}.`, 'err'); return }
    const job = uploadFile(file, p => setUp(u => u ? { ...u, p } : u))
    setUp({ name: file.name, p: 0, abort: job.abort })
    job.promise.then(m => { setUp(null); announceChange(); navigate(`/in/${m.id}`) })
      .catch(e => { setUp(null); toast(errMsg(e), 'err') })
  }
  return (
    <div className="page">
      <div
        className={`mv-drop ${drag ? 'over' : ''} ${up ? 'busy' : ''}`}
        onDragOver={e => { e.preventDefault(); setDrag(true) }}
        onDragLeave={() => setDrag(false)}
        onDrop={e => { e.preventDefault(); setDrag(false); const f = e.dataTransfer.files?.[0]; if (f) send(f) }}
      >
        <Icon name="upload" size={26} />
        {up ? (
          <>
            <div className="mv-drop-t">Uploading {up.name}</div>
            <div className="mv-bar"><span style={{ width: `${Math.round(up.p * 100)}%` }} /></div>
            <div className="muted small tnum">{Math.round(up.p * 100)}% · keep this page open</div>
            <button type="button" className="btn sm" onClick={() => up.abort()}>Cancel</button>
          </>
        ) : (
          <>
            <div className="mv-drop-t">Drop a file here, or <button type="button" className="linkish" onClick={() => input.current?.click()}>choose one</button></div>
            <div className="muted small">A Google Takeout zip, an Evernote .enex, a Notion export, an iCloud download, a Pocket Move archive, or one file: .vcf, .ics, .opml, a Goodreads or bank CSV. Big zips are fine{settings?.max_upload_bytes ? ` (up to ${fmtBytes(settings.max_upload_bytes)})` : ''}.</div>
          </>
        )}
        <input ref={input} type="file" hidden onChange={e => { const f = e.target.files?.[0]; e.target.value = ''; if (f) send(f) }} />
      </div>

      <section className="card">
        <div className="card-head"><h3>Your moves in</h3><span className="spacer" /><Link to="/sources" className="btn ghost sm"><Icon name="help" size={14} />What Move can read</Link></div>
        {!list ? <Loading /> : list.length === 0 ? <Empty title="Nothing brought in yet" icon="inbox">Files you drop above show up here, with what Move found in them.</Empty> : (
          <div className="list">
            {list.map(m => (
              <Link key={m.id} to={`/in/${m.id}`} className="list-row mv-row">
                <span className="list-ico"><Icon name={SOURCE_ICON[m.source] || 'upload'} size={16} /></span>
                <span className="grow ellipsis"><b>{m.name}</b>{m.source_name && m.source !== 'files' && <span className="muted small"> · {m.source_name}</span>}</span>
                <span className={`badge ${m.status === 'done' ? 'ok' : m.status === 'failed' ? 'bad' : m.status === 'ready' ? 'accent' : ''}`}>{IN_STATUS[m.status]}</span>
                <span className="muted small nowrap mv-when">{ago(m.created_at)}</span>
              </Link>
            ))}
          </div>
        )}
      </section>
    </div>
  )
}

// ---------- one move-in: what was found and where it goes ----------

const optKey = (t: { app: string; import: { path: string; kind: string } }) => `${t.app} ${t.import.path} ${t.import.kind}`

export function MoveInPage() {
  const { id } = useParams()
  const navigate = useNavigate()
  const { toast } = useToast()
  const [d, setD] = useState<MoveInDetail | null>(null)
  const [err, setErr] = useState('')
  const [confirmDel, setConfirmDel] = useState(false)
  const [showNotes, setShowNotes] = useState(false)
  const load = useCallback(() => { api.get<MoveInDetail>(`${V1}/in/${id}`).then(setD).catch(e => setErr(errMsg(e))) }, [id])
  useEffect(load, [load])
  useEffect(() => {
    if (!d || !busyIn(d.move)) return
    const t = setTimeout(load, 2000)
    return () => clearTimeout(t)
  }, [d, load])
  useCrumb(d?.move.name)
  if (err) return <div className="page"><div className="callout danger"><Icon name="alert" size={15} /><span>{err}</span></div></div>
  if (!d) return <div className="page"><Loading /></div>
  const m = d.move
  const chosen = d.items.filter(it => it.include && it.target)
  const running = m.status === 'running'

  const update = async (it: Item, patch: { include?: boolean; target?: string }) => {
    try {
      await api.put(`${V1}/in/${m.id}/items/${it.id}`, patch)
      load()
    } catch (e) { toast(errMsg(e), 'err') }
  }
  const run = async () => {
    try { await api.post(`${V1}/in/${m.id}/run`); toast('Bringing it in…'); load(); announceChange() } catch (e) { toast(errMsg(e), 'err') }
  }
  const del = async () => {
    try { await api.del(`${V1}/in/${m.id}`); announceChange(); navigate('/in') } catch (e) { toast(errMsg(e), 'err') }
  }
  return (
    <div className="page">
      <div className="mv-head">
        <span className="mv-head-ico"><Icon name={SOURCE_ICON[m.source] || 'upload'} size={20} /></span>
        <div className="grow">
          <h2 className="ellipsis">{m.source_name && m.source !== 'files' ? m.source_name : m.name}</h2>
          <p className="muted small">{m.name} · {fmtBytes(m.size_bytes)} · {ago(m.created_at)}</p>
        </div>
        <span className={`badge ${m.status === 'done' ? 'ok' : m.status === 'failed' ? 'bad' : m.status === 'ready' ? 'accent' : ''}`}>{IN_STATUS[m.status]}</span>
      </div>

      {m.status === 'scanning' && <div className="callout info"><span className="spinner" /><div>Reading the file and converting what is in it. A big Takeout takes a few minutes; you can leave this page.</div></div>}
      {m.error && <div className="callout danger"><Icon name="alert" size={15} /><span>{m.error}</span></div>}

      {d.items.length > 0 && (
        <section className="card">
          <div className="card-head">
            <h3>What Move found</h3>
            <span className="spacer" />
            <span className="muted small">{plural(chosen.length, 'part')} chosen</span>
          </div>
          <div className="mv-items">
            {d.items.map(it => <ItemRow key={it.id} it={it} moveId={m.id} locked={running || m.status === 'scanning'} onChange={p => update(it, p)} />)}
          </div>
          <div className="mv-actions">
            <button type="button" className="btn primary" onClick={run} disabled={!d.core || running || chosen.filter(it => it.status !== 'sent').length === 0}>
              {running ? <><span className="spinner" />Bringing in…</> : <><Icon name="check" size={15} />{m.status === 'done' ? 'Bring in what is left' : `Bring in ${plural(chosen.length, 'part')}`}</>}
            </button>
            <span className="muted small">Your apps add what is new and leave what you have alone. Running it twice adds nothing twice.</span>
          </div>
        </section>
      )}

      {m.notes.length > 0 && (
        <section className="card">
          <button type="button" className="card-head mv-fold" onClick={() => setShowNotes(s => !s)} aria-expanded={showNotes}>
            <h3>Left out ({m.notes.length})</h3><span className="spacer" /><Icon name={showNotes ? 'chevron-up' : 'chevron-down'} size={16} />
          </button>
          {showNotes && <ul className="mv-notes">{m.notes.map((n, i) => <li key={i} className="small">{n}</li>)}</ul>}
        </section>
      )}

      <div className="row wrap">
        <button type="button" className="btn" onClick={() => setConfirmDel(true)} disabled={busyIn(m)}><Icon name="trash" size={15} />Remove this move</button>
        <span className="muted small">Removes Move’s copy of the file and what it converted. What is already in your apps stays.</span>
      </div>
      {confirmDel && <Confirm title="Remove this move?" message="Move deletes its copy of the file and the converted parts. Everything already brought into your apps stays there." confirmLabel="Remove" onConfirm={() => { setConfirmDel(false); del() }} onCancel={() => setConfirmDel(false)} />}
    </div>
  )
}

function ItemRow({ it, moveId, locked, onChange }: { it: Item; moveId: number; locked: boolean; onChange: (p: { include?: boolean; target?: string }) => void }) {
  const current = it.options.find(o => o.app === it.target && o.import.path === it.path)
  const status = it.status === 'sent' ? <span className="badge ok"><Icon name="check" size={12} />In {current?.name.replace(/^Pocket /, '') || 'your app'}</span>
    : it.status === 'failed' ? <span className="badge bad">Did not work</span>
      : it.status === 'done-before' ? <span className="badge" title={it.sent_at}>Brought in before</span>
        : it.status === 'manual' ? <span className="badge">By hand</span> : null
  return (
    <div className={`mv-item ${it.include ? '' : 'off'}`}>
      <div className="mv-item-main">
        <div className="grow">
          <div className="mv-item-t">{it.label}</div>
          <div className="muted small">{it.detail}{it.bytes > 0 ? ` · ${fmtBytes(it.bytes)}` : ''}</div>
        </div>
        {status}
        {!it.manual && <Switch checked={it.include} onChange={v => onChange({ include: v })} disabled={locked || !it.target || it.status === 'sent'} label={`Bring in ${it.label}`} />}
      </div>
      {it.manual ? (
        <div className="callout info mv-manual"><Icon name="info" size={15} /><div>{it.manual}</div></div>
      ) : (
        <div className="mv-item-to">
          <span className="muted small">Into</span>
          {it.options.length > 0 ? (
            <Select ariaLabel={`Where ${it.label} goes`} value={current ? optKey(current) : ''} disabled={locked || it.status === 'sent'}
              onChange={v => onChange({ target: v })}
              options={it.options.map(o => ({ value: optKey(o), label: o.name.replace(/^Pocket /, '') + (o.import.title ? ` · ${o.import.title}` : '') }))} />
          ) : <span className="small mv-hint">{it.hint || 'No app takes this yet.'}</span>}
          <span className="spacer" />
          {it.bytes > 0 && <a className="btn ghost sm" href={`${V1}/in/${moveId}/items/${it.id}/download`}><Icon name="download" size={14} />{it.file_name}</a>}
        </div>
      )}
      {it.status === 'sent' && it.result && <div className="small ok">{resultLine(it.result) || 'Done.'}</div>}
      {it.error && <div className="small bad">{it.error}</div>}
    </div>
  )
}
