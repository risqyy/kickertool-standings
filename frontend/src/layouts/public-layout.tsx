import { Link, Outlet } from 'react-router-dom'
import { Trophy } from 'lucide-react'

export function PublicLayout() {
  return <div className="min-h-dvh bg-background"><header className="border-b bg-card"><div className="page-shell flex min-h-16 items-center justify-between py-3"><Link to="/" className="flex items-center gap-3 font-semibold tracking-tight"><span className="flex h-9 w-9 items-center justify-center rounded-md bg-primary text-primary-foreground"><Trophy size={19} aria-hidden="true" /></span>Kickertool Ranking</Link><nav aria-label="Öffentliche Navigation" className="flex items-center gap-1 text-sm"><Link className="rounded-md px-3 py-2 text-muted-foreground hover:bg-muted hover:text-foreground" to="/standings">Rangliste</Link><Link className="rounded-md px-3 py-2 text-muted-foreground hover:bg-muted hover:text-foreground" to="/admin">Verwaltung</Link></nav></div></header><Outlet /></div>
}
