import { useEffect, useMemo, useState } from 'react'
import { AlertCircle, ArrowDown, ArrowUp, ArrowUpDown, RefreshCw, Search } from 'lucide-react'
import { getRankings } from '@/api/client'
import type { RankingRow } from '@/api/types'
import { Alert } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { formatDate, formatDecimal } from '@/lib/utils'

type SortKey = 'rank' | 'name' | 'tournaments' | 'games' | 'points' | 'ppg' | 'goals'
function compare(a: RankingRow, b: RankingRow, key: SortKey) {
  const values: Record<SortKey, [string | number | null, string | number | null]> = {
    rank: [a.rank, b.rank], name: [a.name, b.name], tournaments: [a.includedTournamentCount, b.includedTournamentCount],
    games: [a.gamesPlayed, b.gamesPlayed], points: [a.totalPoints, b.totalPoints], ppg: [a.pointsPerGame, b.pointsPerGame], goals: [a.goalDifference, b.goalDifference]
  }
  const [left, right] = values[key]
  if (left === null || left === undefined || left === '') return right === null || right === undefined || right === '' ? 0 : 1
  if (right === null || right === undefined || right === '') return -1
  if (key === 'name') return String(left).localeCompare(String(right), 'de', { sensitivity: 'base' })
  return Number(left) - Number(right)
}

function SortButton({ label, active, direction, onClick }: { label: string; active: boolean; direction: 'asc' | 'desc'; onClick: () => void }) {
  return <button type="button" className="inline-flex min-h-11 items-center gap-1 rounded px-2 text-left font-medium hover:bg-muted" aria-sort={active ? (direction === 'asc' ? 'ascending' : 'descending') : 'none'} onClick={onClick}>{label}{active ? direction === 'asc' ? <ArrowUp size={14} aria-hidden="true" /> : <ArrowDown size={14} aria-hidden="true" /> : <ArrowUpDown size={14} aria-hidden="true" />}</button>
}

export function RankingPage() {
  const [rows, setRows] = useState<RankingRow[]>([])
  const [lastSync, setLastSync] = useState<string | null>(null)
  const [status, setStatus] = useState<'loading' | 'ready' | 'error'>('loading')
  const [query, setQuery] = useState('')
  const [sort, setSort] = useState<{ key: SortKey; direction: 'asc' | 'desc' }>({ key: 'rank', direction: 'asc' })
  const load = () => { setStatus('loading'); getRankings().then(value => { setRows(value.items); setLastSync(value.lastSyncAt); setStatus('ready') }).catch(() => setStatus('error')) }
  useEffect(load, [])
  const filtered = useMemo(() => rows.filter(row => row.name.toLocaleLowerCase('de-DE').includes(query.toLocaleLowerCase('de-DE'))).sort((a, b) => compare(a, b, sort.key) * (sort.direction === 'asc' ? 1 : -1)), [rows, query, sort])
  const changeSort = (key: SortKey) => setSort(current => current.key === key ? { key, direction: current.direction === 'asc' ? 'desc' : 'asc' } : { key, direction: 'asc' })
  const columns: Array<[string, SortKey, string]> = [['#', 'rank', 'text-right'], ['Spieler', 'name', 'text-left'], ['Turniere', 'tournaments', 'text-right'], ['Spiele', 'games', 'text-right'], ['Punkte', 'points', 'text-right'], ['Punkte/Spiel', 'ppg', 'text-right'], ['Tordifferenz', 'goals', 'text-right']]
  return <main className="page-shell"><div className="mb-6 flex flex-col justify-between gap-4 sm:flex-row sm:items-end"><div><p className="mb-2 text-sm font-medium text-primary">Öffentliche Rangliste</p><h1 className="text-3xl font-bold tracking-tight sm:text-4xl">Spieler-Ranking</h1><p className="mt-2 max-w-2xl text-muted-foreground">Akkumulierte Werte aus den ausdrücklich einbezogenen abgeschlossenen Turnieren.</p></div><div className="text-sm text-muted-foreground">Letzte Synchronisierung: {formatDate(lastSync)}</div></div><Card><CardHeader className="gap-4 sm:flex-row sm:items-center sm:justify-between"><div><CardTitle>Rangliste</CardTitle><CardDescription>{filtered.length} von {rows.length} Spielern</CardDescription></div><div className="relative w-full sm:max-w-xs"><Search className="absolute left-3 top-3.5 text-muted-foreground" size={17} aria-hidden="true" /><label className="sr-only" htmlFor="ranking-search">Spieler suchen</label><Input id="ranking-search" className="pl-10" placeholder="Spieler suchen" value={query} onChange={event => setQuery(event.target.value)} /></div></CardHeader><CardContent>{status === 'loading' && <div className="space-y-3" role="status" aria-label="Rangliste wird geladen">{[1, 2, 3, 4, 5].map(item => <Skeleton key={item} className="h-12 w-full" />)}</div>}{status === 'error' && <Alert variant="destructive"><div className="flex items-start gap-3"><AlertCircle aria-hidden="true" /><div><p className="font-semibold">Rangliste konnte nicht geladen werden.</p><Button variant="outline" className="mt-3" onClick={load}><RefreshCw aria-hidden="true" />Erneut versuchen</Button></div></div></Alert>}{status === 'ready' && filtered.length === 0 && <div className="py-12 text-center text-muted-foreground">Keine passenden Spieler gefunden.</div>}{status === 'ready' && filtered.length > 0 && <><div className="hidden md:block"><Table><TableHeader><TableRow>{columns.map(([label, key, align]) => <TableHead key={key} className={align}><SortButton label={label} active={sort.key === key} direction={sort.direction} onClick={() => changeSort(key)} /></TableHead>)}</TableRow></TableHeader><TableBody>{filtered.map(row => <TableRow key={row.name}><TableCell className="text-right tabular">{row.rank}</TableCell><TableCell className="font-medium">{row.name}</TableCell><TableCell className="text-right tabular">{row.includedTournamentCount}</TableCell><TableCell className="text-right tabular">{row.gamesPlayed ?? '—'}</TableCell><TableCell className="text-right tabular">{formatDecimal(row.totalPoints)}</TableCell><TableCell className="text-right tabular">{formatDecimal(row.pointsPerGame)}</TableCell><TableCell className="text-right tabular">{row.goalDifference ?? '—'}</TableCell></TableRow>)}</TableBody></Table></div><div className="grid gap-3 md:hidden">{filtered.map(row => <article key={row.name} className="rounded-md border p-4"><div className="flex items-baseline justify-between gap-4"><span className="text-sm text-muted-foreground">#{row.rank}</span><h2 className="font-semibold">{row.name}</h2></div><dl className="mt-4 grid grid-cols-2 gap-3 text-sm"><div><dt className="text-muted-foreground">Turniere</dt><dd className="tabular">{row.includedTournamentCount}</dd></div><div><dt className="text-muted-foreground">Spiele</dt><dd className="tabular">{row.gamesPlayed ?? '—'}</dd></div><div><dt className="text-muted-foreground">Punkte</dt><dd className="tabular">{formatDecimal(row.totalPoints)}</dd></div><div><dt className="text-muted-foreground">Punkte/Spiel</dt><dd className="tabular">{formatDecimal(row.pointsPerGame)}</dd></div><div><dt className="text-muted-foreground">Tordifferenz</dt><dd className="tabular">{row.goalDifference ?? '—'}</dd></div></dl></article>)}</div></>}</CardContent></Card></main>
}
