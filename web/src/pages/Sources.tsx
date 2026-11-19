import { Link } from 'react-router-dom'
import { Icon } from '../components/icons'

const SOURCES: { icon: string; name: string; how: string; what: [string, string][] }[] = [
  {
    icon: 'globe', name: 'Google Takeout',
    how: 'takeout.google.com → pick what you want → one zip (or several parts: bring in each).',
    what: [['Contacts', 'Contacts'], ['Calendar', 'Calendar'], ['Keep', 'Notes, with labels, check lists and pictures'], ['Tasks', 'Tasks, a list for each list'],
      ['Maps (your places)', 'Places: saved places and your lists'], ['Location History (Timeline)', 'Movement'], ['YouTube subscriptions', 'Podcasts follows each channel'], ['Drive', 'Files, in a folder of its own'],
      ['Google Photos', 'Your photo library: Move tells you how to upload them to Immich']],
  },
  {
    icon: 'note', name: 'Evernote',
    how: 'In Evernote, export each notebook as .enex (or all of them into a zip).',
    what: [['Notes', 'Notes, a folder per notebook, with tags, check boxes, pictures and attachments']],
  },
  {
    icon: 'file', name: 'Notion',
    how: 'Settings → Export all workspace content → Markdown & CSV, with subpages.',
    what: [['Pages', 'Notes, keeping the page tree and links between pages'], ['Databases', 'As CSV files next to the pages']],
  },
  {
    icon: 'apple', name: 'iCloud and Apple',
    how: 'privacy.apple.com → Obtain a copy of your data, or export from the app itself.',
    what: [['Contacts (.vcf)', 'Contacts'], ['Calendars (.ics)', 'Calendar'], ['Reminders (.ics)', 'Tasks'], ['iCloud Notes', 'Notes'], ['Podcasts (OPML)', 'Podcasts']],
  },
  {
    icon: 'package', name: 'Another Pocket Box',
    how: 'Moving out on the other box → download the archive → bring it in here as it is.',
    what: [['Every app', 'Back into the same app, each from its own copy']],
  },
  {
    icon: 'upload', name: 'Single files',
    how: 'Drop the file as it is.',
    what: [['.vcf', 'Contacts'], ['.ics', 'Calendar (events) or Tasks (to-dos)'], ['.opml', 'Reader (feeds) or Podcasts'], ['.enex', 'Notes'], ['Markdown, or a zip of it', 'Notes (Obsidian, Bear, Joplin…)'],
      ['Goodreads or StoryGraph CSV', 'Bookshelf'], ['A bank statement (CSV)', 'Money, into an account named after the file'], ['.gpx', 'Trails']],
  },
]

export default function Sources() {
  return (
    <div className="page">
      <div className="mv-hello">
        <h2>What Move can read</h2>
        <p className="muted">Drop any of these on <Link to="/in">Moving in</Link>. Move looks inside, converts each part and shows you where it goes before anything happens.</p>
      </div>
      <div className="mv-sources">
        {SOURCES.map(s => (
          <section key={s.name} className="card mv-source">
            <div className="card-head"><span className="list-ico"><Icon name={s.icon} size={16} /></span><h3>{s.name}</h3></div>
            <div className="pad">
              <p className="muted small">{s.how}</p>
              <dl className="mv-map">
                {s.what.map(([a, b]) => <div key={a}><dt>{a}</dt><dd><Icon name="chevron-right" size={13} className="muted" />{b}</dd></div>)}
              </dl>
            </div>
          </section>
        ))}
      </div>
      <div className="callout info"><Icon name="lock" size={15} /><div><b>Passwords</b> (1Password, Bitwarden, LastPass, Chrome) are encrypted on your own devices, so no server can bring them in: Move recognises the file and tells you how to import it in Pocket Vault, then deletes its copy.</div></div>
    </div>
  )
}
