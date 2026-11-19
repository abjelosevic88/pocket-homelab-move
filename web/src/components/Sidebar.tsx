// Left column: new-move button, the sections, what is going on right now — the Pocket Calendar sidebar.
import { useEffect, useState } from 'react'
import { Link, NavLink } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { MoveIn, MoveOut } from '../lib/types'
import { Icon } from './icons'
import { createItems, useOnChange, type CreateKind } from './Layout'
import { type Anchor, Menu, rectOf } from './ui'

export function Sidebar({ onCreate, onNavigate }: { onCreate: (k: CreateKind) => void; onNavigate: () => void }) {
  const { status, settings } = useApp()
  const [menu, setMenu] = useState<Anchor | null>(null)
  const [ins, setIns] = useState<MoveIn[]>([])
  const [outs, setOuts] = useState<MoveOut[]>([])
  const load = () => {
    api.get<MoveIn[]>(`${V1}/in`).then(setIns).catch(() => {})
    api.get<MoveOut[]>(`${V1}/out`).then(setOuts).catch(() => {})
  }
  useEffect(load, [])
  useOnChange(load)
  const title = settings?.title || status?.title || 'Pocket Move'
  const busyIn = ins.filter(m => m.status === 'scanning' || m.status === 'running').length
  const readyIn = ins.filter(m => m.status === 'ready').length
  const busyOut = outs.filter(m => m.status === 'queued' || m.status === 'running').length
  const nav: { to: string; label: string; icon: string; end?: boolean; count?: string }[] = [
    { to: '/', label: 'Home', icon: 'home', end: true },
    { to: '/in', label: 'Moving in', icon: 'inbox', count: readyIn ? String(readyIn) : undefined },
    { to: '/out', label: 'Moving out', icon: 'archive', count: outs.filter(o => o.status === 'done').length ? String(outs.filter(o => o.status === 'done').length) : undefined },
    { to: '/sources', label: 'What Move can read', icon: 'help' },
  ]
  return (
    <aside className="sidebar" aria-label="Move">
      <div className="sb-brand">
        <img src="/favicon.svg" alt="" className="sb-logo" />
        <span className="sb-title">{title !== 'Pocket Move' ? title : <>Pocket <b>Move</b></>}</span>
        {status?.version && <span className="sb-version" title={`Version ${status.version}`}>{/^v?\d+\.\d+/.test(status.version) ? 'v' + status.version.replace(/^v/, '').replace(/[-+].*$/, '') : status.version}</span>}
      </div>
      <button type="button" className="btn primary create-btn" aria-haspopup="menu" aria-expanded={!!menu} onClick={e => setMenu(menu ? null : rectOf(e.currentTarget))}>
        <Icon name="plus" size={15} strokeWidth={2.2} />New move<Icon name="chevron-down" size={15} className="create-caret" />
      </button>
      {menu && <Menu anchor={menu} items={createItems(onCreate)} onClose={() => setMenu(null)} width={248} />}

      <nav className="sb-nav" aria-label="Sections">
        {nav.map(n => (
          <NavLink key={n.to} to={n.to} end={n.end} onClick={onNavigate} className={({ isActive }) => `sb-nav-item ${isActive ? 'active' : ''}`}>
            <Icon name={n.icon} size={16} />{n.label}
            {!!n.count && <span className="sb-count">{n.count}</span>}
          </NavLink>
        ))}
      </nav>

      {(busyIn > 0 || busyOut > 0) && (
        <section className="sb-sec" aria-label="Working">
          <h4 className="sb-h"><span className="sb-h-btn">Working on</span></h4>
          <div className="sb-work muted small">
            {busyIn > 0 && <div><Icon name="upload" size={13} /> {busyIn === 1 ? 'Reading one file' : `Reading ${busyIn} files`}</div>}
            {busyOut > 0 && <div><Icon name="package" size={13} /> {busyOut === 1 ? 'Making an archive' : `Making ${busyOut} archives`}</div>}
          </div>
        </section>
      )}

      <div className="sb-foot">
        <Link to="/settings" className="sb-foot-link" onClick={onNavigate}><Icon name="settings" size={15} />Settings</Link>
        <button type="button" className="sb-foot-link" onClick={() => { onNavigate(); window.dispatchEvent(new CustomEvent('mv:shortcuts')) }}><Icon name="keyboard" size={15} />Shortcuts</button>
      </div>
    </aside>
  )
}
