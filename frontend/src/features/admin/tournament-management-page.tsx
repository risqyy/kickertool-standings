import { useEffect, useMemo, useState } from 'react'
import { ArrowDown, ArrowUp, Check, Filter, RefreshCw, Search, X } from 'lucide-react'
import { useSearchParams } from 'react-router-dom'
import { ApiError, getTournaments, setTournamentInclusion } from '@/api/client'
import type { Tournament } from '@/api/types'
import { useAdminSession } from '@/app/providers'
import { Alert } from '@/components/ui/alert'
import { AlertDialog } from '@/components/ui/alert-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import { formatDate } from '@/lib/utils'

export function TournamentManagementPage() {
  const session = useAdminSession()
  const [urlParams, setUrlParams] = useSearchParams()
  const searchParams = useMemo(() => ({
    q: urlParams.get('q') ?? '',
    included: urlParams.get('included') ?? '',
    state: urlParams.get('state') ?? '',
    source: urlParams.get('source') ?? '',
    page: Math.max(1, Number(urlParams.get('page') ?? '1') || 1),
    sort: urlParams.get('sort') ?? 'date',
    direction: urlParams.get('direction') === 'asc' ? 'asc' as const : 'desc' as const,
  }), [urlParams])
  const [draftQuery, setDraftQuery] = useState(searchParams.q)
  const [items, setItems] = useState<Tournament[]>([])
  const [total, setTotal] = useState(0)
  const [status, setStatus] = useState<'loading' | 'ready' | 'error'>('loading')
  const [selected, setSelected] = useState<number[]>([])
  const [dialog, setDialog] = useState<{ included: boolean } | null>(null)
  const [message, setMessage] = useState<{ kind: 'success' | 'error'; text: string } | null>(null)
  const load = () => { setStatus('loading'); getTournaments(searchParams).then(value => { setItems(value.items); setTotal(value.total); setStatus('ready') }).catch(() => setStatus('error')) }
  useEffect(load, [searchParams])
  useEffect(() => setDraftQuery(searchParams.q), [searchParams.q])
  const updateSearchParams = (patch: Partial<typeof searchParams>) => {
    const next = new URLSearchParams(urlParams)
    for (const [key, value] of Object.entries(patch)) {
      if (value === undefined || value === '') next.delete(key)
      else next.set(key, String(value))
    }
    setUrlParams(next)
  }
  const allSelected = useMemo(() => items.length > 0 && items.every(item => selected.includes(item.id)), [items, selected])
  const toggleSelection = (id: number) => setSelected(current => current.includes(id) ? current.filter(value => value !== id) : [...current, id])
  const toggleSort = (sort: string) => updateSearchParams({ sort, direction: searchParams.sort === sort && searchParams.direction === 'asc' ? 'desc' : 'asc', page: 1 })
  const updateInclusion = async (item: Tournament, included: boolean) => {
    try { const value = await setTournamentInclusion(session.csrf, item.id, included, item.inclusionVersion, 'Manuelle Ranking-Entscheidung'); setItems(current => current.map(row => row.id === item.id ? value.tournament : row)); setMessage({ kind: 'success', text: included ? 'Turnier wieder einbezogen.' : 'Turnier aus dem Ranking ausgeschlossen.' }) }
    catch (error) { setMessage({ kind: 'error', text: error instanceof ApiError && error.status === 409 ? 'Das Turnier wurde inzwischen geändert. Liste neu laden und erneut prüfen.' : 'Einbeziehung konnte nicht geändert werden.' }) }
  }
  const applyBulk = async () => {
    if (!dialog) return
    const next = dialog.included
    setDialog(null)
    const chosen = items.filter(item => selected.includes(item.id))
    for (const item of chosen) await updateInclusion(item, next)
    setSelected([])
  }
  return <div><div className="mb-6 flex flex-col justify-between gap-4 sm:flex-row sm:items-end"><div><p className="text-sm font-medium text-primary">Datenpflege</p><h1 className="mt-1 text-3xl font-bold tracking-tight">Turniere</h1><p className="mt-2 text-muted-foreground">Der Crawler speichert alle Turniere; hier steuerst du nur den Ranking-Beitrag.</p></div><Button variant="outline" onClick={load}><RefreshCw aria-hidden="true" />Aktualisieren</Button></div>{message && <Alert variant={message.kind === 'error' ? 'destructive' : 'success'} className="mb-4">{message.text}<button className="float-right" aria-label="Meldung schließen" onClick={() => setMessage(null)}><X size={16} /></button></Alert>}<Card><CardHeader><CardTitle>Ranking-Beiträge</CardTitle><CardDescription>{total} Turniere · Einbeziehung ändert keine Crawler-Daten.</CardDescription><form className="mt-4 grid gap-3 md:grid-cols-[1fr_auto_auto_auto]" onSubmit={event => { event.preventDefault(); updateSearchParams({ q: draftQuery, page: 1 }) }}><label className="relative"><span className="sr-only">Turniere suchen</span><Search className="absolute left-3 top-3.5 text-muted-foreground" size={17} aria-hidden="true" /><Input className="pl-10" value={draftQuery} onChange={event => setDraftQuery(event.target.value)} placeholder="Name oder Quelle" /></label><Select aria-label="Einbeziehung filtern" value={searchParams.included} onChange={event => updateSearchParams({ included: event.target.value, page: 1 })}><option value="">Alle Einbeziehungen</option><option value="true">Im Ranking</option><option value="false">Ausgeschlossen</option></Select><Select aria-label="Status filtern" value={searchParams.state} onChange={event => updateSearchParams({ state: event.target.value, page: 1 })}><option value="">Alle Status</option><option value="finished">Beendet</option><option value="running">Laufend</option><option value="cancelled">Abgesagt</option></Select><Button type="submit"><Filter aria-hidden="true" />Filtern</Button></form></CardHeader><CardContent>{selected.length > 0 && <div className="mb-4 flex flex-wrap items-center gap-3 rounded-md border border-primary/30 bg-primary/5 p-3"><span className="text-sm font-medium">{selected.length} ausgewählt</span><Button size="sm" onClick={() => setDialog({ included: true })}><Check aria-hidden="true" />Einbeziehen</Button><Button size="sm" variant="destructive" onClick={() => setDialog({ included: false })}>Ausschließen</Button><Button size="sm" variant="ghost" onClick={() => setSelected([])}>Auswahl aufheben</Button></div>}{status === 'loading' && <div className="space-y-3">{[1, 2, 3, 4, 5].map(item => <Skeleton key={item} className="h-16" />)}</div>}{status === 'error' && <Alert variant="destructive">Turniere konnten nicht geladen werden. <Button variant="outline" onClick={load}>Erneut versuchen</Button></Alert>}{status === 'ready' && items.length === 0 && <div className="py-12 text-center text-muted-foreground">Keine Turniere für diese Filter.</div>}{status === 'ready' && items.length > 0 && <><div className="hidden overflow-x-auto md:block"><table className="w-full min-w-[760px] text-sm"><thead><tr className="border-b"><th className="w-12 p-3"><Checkbox aria-label="Alle sichtbaren Turniere auswählen" checked={allSelected} onChange={event => setSelected(event.target.checked ? items.map(item => item.id) : [])} /></th><SortHead label="Turnier" keyName="name" current={searchParams} onSort={toggleSort} /><SortHead label="Datum" keyName="date" current={searchParams} onSort={toggleSort} /><SortHead label="Status" keyName="status" current={searchParams} onSort={toggleSort} /><th className="p-3 text-left">Standings</th><th className="p-3 text-left">Ranking</th></tr></thead><tbody>{items.map(item => <TournamentRow key={item.id} item={item} selected={selected.includes(item.id)} onSelect={() => toggleSelection(item.id)} onToggle={included => updateInclusion(item, included)} />)}</tbody></table></div><div className="grid gap-3 md:hidden">{items.map(item => <TournamentCard key={item.id} item={item} selected={selected.includes(item.id)} onSelect={() => toggleSelection(item.id)} onToggle={included => updateInclusion(item, included)} />)}</div><div className="mt-5 flex items-center justify-between border-t pt-4 text-sm text-muted-foreground"><span>Seite {searchParams.page} · {total} Treffer</span><div className="flex gap-2"><Button size="sm" variant="outline" disabled={searchParams.page <= 1} onClick={() => updateSearchParams({ page: searchParams.page - 1 })}>Zurück</Button><Button size="sm" variant="outline" disabled={searchParams.page * 25 >= total} onClick={() => updateSearchParams({ page: searchParams.page + 1 })}>Weiter</Button></div></div></>}</CardContent></Card><AlertDialog open={dialog !== null} title={dialog?.included ? 'Turniere einbeziehen?' : 'Turniere ausschließen?'} description={dialog?.included ? 'Die ausgewählten Turniere werden wieder in die Rangliste aufgenommen.' : 'Die ausgewählten Turniere bleiben gespeichert, tragen aber nicht mehr zur Rangliste bei.'} confirmLabel={dialog?.included ? 'Einbeziehen' : 'Ausschließen'} destructive={dialog?.included === false} onCancel={() => setDialog(null)} onConfirm={applyBulk} /></div>
}

function SortHead({ label, keyName, current, onSort }: { label: string; keyName: string; current: { sort: string; direction: 'asc' | 'desc' }; onSort: (key: string) => void }) {
  const active = current.sort === keyName
  return <th className="p-3 text-left"><button className="inline-flex min-h-11 items-center gap-1 font-medium" onClick={() => onSort(keyName)} aria-sort={active ? current.direction === 'asc' ? 'ascending' : 'descending' : 'none'}>{label}{active ? current.direction === 'asc' ? <ArrowUp size={14} aria-hidden="true" /> : <ArrowDown size={14} aria-hidden="true" /> : null}</button></th>
}
function TournamentRow({ item, selected, onSelect, onToggle }: { item: Tournament; selected: boolean; onSelect: () => void; onToggle: (included: boolean) => void }) {
  return <tr className="border-b last:border-0"><td className="p-3"><Checkbox aria-label={item.name + ' auswählen'} checked={selected} onChange={onSelect} /></td><td className="p-3"><p className="font-medium">{item.name}</p><p className="mt-1 text-xs text-muted-foreground">{item.source} · {item.entryType || 'unbekannt'}</p></td><td className="p-3 tabular">{formatDate(item.date)}</td><td className="p-3"><Badge variant={item.isLive ? 'warning' : item.status === 'finished' ? 'success' : 'secondary'}>{item.isLive ? 'Laufend' : item.status || 'unbekannt'}</Badge></td><td className="p-3 tabular">{item.standingCount} / {item.playerCount} Spieler</td><td className="p-3"><div className="flex items-center gap-2"><Switch aria-label={item.name + ' im Ranking'} checked={item.includedInRanking} onChange={event => onToggle(event.target.checked)} /><span className="text-xs text-muted-foreground">{item.includedInRanking ? 'Einbezogen' : 'Ausgeschlossen'}</span></div></td></tr>
}
function TournamentCard({ item, selected, onSelect, onToggle }: { item: Tournament; selected: boolean; onSelect: () => void; onToggle: (included: boolean) => void }) {
  return <article className="rounded-md border bg-card p-4"><div className="flex items-start justify-between gap-3"><label className="flex items-center gap-3 font-medium"><Checkbox aria-label={item.name + ' auswählen'} checked={selected} onChange={onSelect} />{item.name}</label><Badge variant={item.includedInRanking ? 'success' : 'secondary'}>{item.includedInRanking ? 'Im Ranking' : 'Ausgeschlossen'}</Badge></div><dl className="mt-4 grid grid-cols-2 gap-3 text-sm"><div><dt className="text-muted-foreground">Datum</dt><dd className="tabular">{formatDate(item.date)}</dd></div><div><dt className="text-muted-foreground">Status</dt><dd>{item.status || 'unbekannt'}</dd></div><div><dt className="text-muted-foreground">Standings</dt><dd className="tabular">{item.standingCount} / {item.playerCount}</dd></div><div><dt className="text-muted-foreground">Quelle</dt><dd>{item.source}</dd></div></dl><div className="mt-4 flex items-center justify-between border-t pt-3"><span className="text-sm">Ranking-Beitrag</span><Switch aria-label={item.name + ' im Ranking'} checked={item.includedInRanking} onChange={event => onToggle(event.target.checked)} /></div></article>
}
