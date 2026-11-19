import { useEffect, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { MoveIn, MoveOut } from '../lib/types'
import { ago, fmtBytes } from '../lib/format'
import { IN_STATUS, OUT_STATUS, SOURCE_ICON } from '../lib/move'
import { Icon } from '../components/icons'
import { CoreHint, Loading } from '../components/ui'
import { useOnChange } from '../components/Layout'

export default function Home() {
  const { user, settings } = useApp()
  const navigate = useNavigate()
  const [ins, setIns] = useState<MoveIn[] | null>(null)
  const [outs, setOuts] = useState<MoveOut[] | null>(null)
  const load = () => {
    api.get<MoveIn[]>(`${V1}/in`).then(setIns).catch(() => setIns([]))
    api.get<MoveOut[]>(`${V1}/out`).then(setOuts).catch(() => setOuts([]))
  }
  useEffect(load, [])
  useOnChange(load)
  const coreOn = (!!settings?.core_configured && settings?.core_registered !== false) || !!settings?.demo_links
  const first = (user?.name || '').split(' ')[0]
  const recent = [
    ...(ins || []).map(m => ({ at: m.created_at, key: `i${m.id}`, to: `/in/${m.id}`, icon: SOURCE_ICON[m.source] || 'upload', title: m.source_name && m.source !== 'files' ? `${m.source_name}: ${m.name}` : m.name, sub: IN_STATUS[m.status] })),
    ...(outs || []).map(m => ({ at: m.created_at, key: `o${m.id}`, to: '/out', icon: 'package', title: `Archive of ${m.apps.length === 1 ? '1 app' : `${m.apps.length} apps`}`, sub: OUT_STATUS[m.status] + (m.size_bytes ? ` · ${fmtBytes(m.size_bytes)}` : '') })),
  ].sort((a, b) => b.at.localeCompare(a.at)).slice(0, 6)
  return (
    <div className="page mv-home">
      <div className="mv-hello">
        <h2>{first ? `Hi ${first}.` : 'Hi.'} Moving in, or moving out?</h2>
        <p className="muted">Bring your data here from Google, Evernote, Notion, iCloud or another Pocket Box, or take everything you keep here with you as plain files.</p>
      </div>
      <div className="mv-doors">
        <button type="button" className="mv-door" onClick={() => navigate('/in?pick=1')}>
          <span className="mv-door-ico in"><Icon name="upload" size={22} /></span>
          <span className="mv-door-t">Moving in</span>
          <span className="mv-door-s">Drop a Google Takeout, an Evernote or Notion export, an iCloud download, contacts, calendars, a bank statement… Move sorts it into the right apps.</span>
        </button>
        <button type="button" className="mv-door" onClick={() => navigate('/out')}>
          <span className="mv-door-ico out"><Icon name="package" size={22} /></span>
          <span className="mv-door-t">Moving out</span>
          <span className="mv-door-s">One archive with everything of yours, in files any program opens. Bring it into Pocket Move on another box and it all goes back where it was.</span>
        </button>
      </div>
      {!coreOn && <div className="callout info"><Icon name="info" size={15} /><div>Without Pocket Core, Move reads and converts what you bring in and you download the results; putting them into your apps and making archives needs Core. <CoreHint /></div></div>}
      <section className="card">
        <div className="card-head"><h3>Recently</h3><span className="spacer" /><Link to="/sources" className="btn ghost sm"><Icon name="help" size={14} />What Move can read</Link></div>
        {!ins || !outs ? <Loading /> : recent.length === 0 ? <p className="muted pad">Nothing yet.</p> : (
          <div className="list">
            {recent.map(r => (
              <Link key={r.key} to={r.to} className="list-row mv-row">
                <span className="list-ico"><Icon name={r.icon} size={16} /></span>
                <span className="grow ellipsis"><b>{r.title}</b><span className="muted small"> · {r.sub}</span></span>
                <span className="muted small nowrap">{ago(r.at)}</span>
              </Link>
            ))}
          </div>
        )}
      </section>
    </div>
  )
}
