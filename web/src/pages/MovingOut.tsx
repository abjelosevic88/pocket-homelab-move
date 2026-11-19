import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { AppsResponse, AppView, MoveOut } from '../lib/types'
import { ago, fmtBytes, fmtDateTime, plural } from '../lib/format'
import { OUT_STATUS, busyOut } from '../lib/move'
import { Icon } from '../components/icons'
import { Confirm, CoreHint, Empty, Loading, errMsg, useToast } from '../components/ui'
import { announceChange, useOnChange } from '../components/Layout'

export default function MovingOut() {
  const { settings } = useApp()
  const { toast } = useToast()
  const [apps, setApps] = useState<AppsResponse | null>(null)
  const [list, setList] = useState<MoveOut[] | null>(null)
  const [picked, setPicked] = useState<Set<string> | null>(null)
  const [del, setDel] = useState<MoveOut | null>(null)
  const [open, setOpen] = useState<number | null>(null)
  const coreOn = (!!settings?.core_configured && settings?.core_registered !== false) || !!settings?.demo_links
  const loadList = useCallback(() => { api.get<MoveOut[]>(`${V1}/out`).then(setList).catch(() => setList([])) }, [])
  useEffect(() => { api.get<AppsResponse>(`${V1}/apps`).then(setApps).catch(e => setApps({ core: true, error: errMsg(e), apps: [] })) }, [])
  useEffect(loadList, [loadList])
  useOnChange(loadList)
  useEffect(() => {
    if (!list?.some(busyOut)) return
    const t = setTimeout(loadList, 2000)
    return () => clearTimeout(t)
  }, [list, loadList])

  const ready = useMemo(() => (apps?.apps || []).filter(a => a.out.on && !a.error && a.exports.length > 0), [apps])
  const notReady = useMemo(() => (apps?.apps || []).filter(a => !(a.out.on && !a.error && a.exports.length > 0) && a.out.state !== 'not_offered' && a.out.state !== 'target_missing'), [apps])
  const later = useMemo(() => (apps?.apps || []).filter(a => a.out.state === 'not_offered'), [apps])
  useEffect(() => { if (apps && !picked) setPicked(new Set(ready.map(a => a.slug))) }, [apps, ready, picked])

  const make = async () => {
    if (!picked?.size) return
    try { await api.post(`${V1}/out`, { apps: [...picked] }); toast('Making your archive…'); loadList(); announceChange() } catch (e) { toast(errMsg(e), 'err') }
  }
  const remove = async (m: MoveOut) => {
    try { await api.del(`${V1}/out/${m.id}`); loadList(); announceChange() } catch (e) { toast(errMsg(e), 'err') }
  }
  const toggle = (slug: string) => setPicked(p => { const n = new Set(p); if (n.has(slug)) n.delete(slug); else n.add(slug); return n })
  const making = list?.some(busyOut)

  return (
    <div className="page">
      <section className="card">
        <div className="card-head"><h3>Make an archive</h3></div>
        <div className="pad stack">
          <p className="muted">Everything you keep in the apps you tick, in one zip: each app’s own copy (which Pocket Move on another box puts straight back) and, next to it, files any program opens. Only your own data; other people on this box are not in it.</p>
          {!coreOn ? <div className="callout info"><Icon name="info" size={15} /><div>Moving out needs Pocket Core to reach your apps. <CoreHint /></div></div>
            : !apps ? <Loading /> : (
              <>
                {apps.error && <div className="callout danger"><Icon name="alert" size={15} /><span>{apps.error}</span></div>}
                {ready.length === 0 ? <div className="callout info"><Icon name="info" size={15} /><div>No app lets Move copy out yet. Switch the Pocket Move links on in Pocket Core → Links, then come back. <Link to="/settings/apps">See which</Link></div></div> : (
                  <>
                    <div className="row wrap">
                      <span className="muted small">{plural(picked?.size || 0, 'app')} of {ready.length}</span>
                      <span className="spacer" />
                      <button type="button" className="btn ghost sm" onClick={() => setPicked(new Set(ready.map(a => a.slug)))}>All</button>
                      <button type="button" className="btn ghost sm" onClick={() => setPicked(new Set())}>None</button>
                    </div>
                    <div className="mv-apps">
                      {ready.map(a => <AppTick key={a.slug} a={a} on={!!picked?.has(a.slug)} onToggle={() => toggle(a.slug)} />)}
                    </div>
                  </>
                )}
                {notReady.length > 0 && <p className="muted small">Switched off for Move: {notReady.map(a => a.name.replace(/^Pocket /, '')).join(', ')}. <Link to="/settings/apps">Why</Link></p>}
                {later.length > 0 && <p className="muted small">Not yet ({later.length}): these apps need an update before Move can copy them.</p>}
                <div className="row wrap">
                  <button type="button" className="btn primary" onClick={make} disabled={!picked?.size || !!making}>
                    {making ? <><span className="spinner" />Making it…</> : <><Icon name="package" size={15} />Make my archive</>}
                  </button>
                  {settings?.open_formats === false && <span className="muted small">Only the apps’ own copies (open formats are off in Settings).</span>}
                </div>
              </>
            )}
        </div>
      </section>

      <section className="card">
        <div className="card-head"><h3>Your archives</h3><span className="spacer" />{settings && <span className="muted small">kept {settings.keep_days} days</span>}</div>
        {!list ? <Loading /> : list.length === 0 ? <Empty title="No archives yet" icon="archive">An archive you make shows up here, ready to download.</Empty> : (
          <div className="list">
            {list.map(m => (
              <div key={m.id} className="mv-out">
                <div className="list-row">
                  <span className="list-ico"><Icon name="package" size={16} /></span>
                  <button type="button" className="grow mv-out-t" onClick={() => setOpen(open === m.id ? null : m.id)} aria-expanded={open === m.id}>
                    <b>{plural(m.apps.length, 'app')}</b>
                    <span className="muted small"> · {m.size_bytes ? fmtBytes(m.size_bytes) + ' · ' : ''}{ago(m.created_at)}</span>
                  </button>
                  <span className={`badge ${m.status === 'done' ? 'ok' : m.status === 'failed' ? 'bad' : ''}`}>{busyOut(m) && <span className="spinner" />}{OUT_STATUS[m.status]}</span>
                  {m.status === 'done' && <a className="btn sm primary" href={`${V1}/out/${m.id}/download`}><Icon name="download" size={14} />Download</a>}
                  {!busyOut(m) && <button type="button" className="btn ghost icon-only sm" aria-label="Remove" title="Remove" onClick={() => setDel(m)}><Icon name="trash" size={14} /></button>}
                </div>
                {(open === m.id || busyOut(m)) && (
                  <div className="mv-out-apps">
                    {m.results.map(r => (
                      <div key={r.app} className="mv-out-app small">
                        <Icon name={r.state === 'done' ? 'check' : r.state === 'failed' ? 'x' : r.state === 'skipped' ? 'minus' : 'clock'} size={13} className={r.state === 'done' ? 'ok' : r.state === 'failed' ? 'bad' : 'muted'} />
                        <span className="mv-out-name">{r.name.replace(/^Pocket /, '')}</span>
                        <span className="muted ellipsis">{r.error || r.files.map(f => f.path.split('/').pop()).join(', ')}</span>
                      </div>
                    ))}
                    {m.status === 'done' && m.expires_at && <div className="muted small">Removed on {fmtDateTime(m.expires_at)}.</div>}
                    {m.error && <div className="small bad">{m.error}</div>}
                  </div>
                )}
              </div>
            ))}
          </div>
        )}
      </section>
      {del && <Confirm title="Remove this archive?" message="The zip is deleted from this box. Your apps are not touched." confirmLabel="Remove" onConfirm={() => { const m = del; setDel(null); remove(m) }} onCancel={() => setDel(null)} />}
    </div>
  )
}

function AppTick({ a, on, onToggle }: { a: AppView; on: boolean; onToggle: () => void }) {
  return (
    <label className={`mv-app ${on ? 'on' : ''}`}>
      <input type="checkbox" checked={on} onChange={onToggle} />
      <span className="grow">
        <span className="mv-app-t">{a.name.replace(/^Pocket /, '')}</span>
        <span className="muted small ellipsis">{a.exports.map(e => e.file).join(', ')}</span>
      </span>
    </label>
  )
}
