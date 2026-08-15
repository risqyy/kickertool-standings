import { useEffect, useState } from 'react'
import { AlertCircle, CalendarCheck, Database, ListChecks, RefreshCw, Users } from 'lucide-react'
import { getDashboard } from '@/api/client'
import type { Dashboard } from '@/api/types'
import { Alert } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { formatDate } from '@/lib/utils'

export function DashboardPage() {
  const [data, setData] = useState<Dashboard | null>(null)
  const [error, setError] = useState(false)
  const load = () => { setError(false); getDashboard().then(setData).catch(() => setError(true)) }
  useEffect(load, [])
  if (error) return <Alert variant="destructive"><div className="flex items-center gap-3"><AlertCircle aria-hidden="true" />Dashboard konnte nicht geladen werden.<Button variant="outline" onClick={load}><RefreshCw aria-hidden="true" />Erneut laden</Button></div></Alert>
  return <div><div className="mb-6"><p className="text-sm font-medium text-primary">Geschützter Bereich</p><h1 className="mt-1 text-3xl font-bold tracking-tight">Übersicht</h1><p className="mt-2 text-muted-foreground">Crawler-Daten und Ranking-Einbeziehung auf einen Blick.</p></div><div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">{data ? <><Metric icon={Database} label="Turniere gesamt" value={data.tournamentCount} /><Metric icon={ListChecks} label="Im Ranking" value={data.includedTournamentCount} /><Metric icon={CalendarCheck} label="Ausgeschlossen" value={data.excludedTournamentCount} /><Metric icon={Users} label="Aktive Spieler" value={data.playerCount} /></> : [1, 2, 3, 4].map(item => <Skeleton key={item} className="h-32" />)}</div><Card className="mt-6"><CardHeader><CardTitle>Synchronisierung</CardTitle><CardDescription>Der Crawler läuft unabhängig von manuellen Ranking-Entscheidungen.</CardDescription></CardHeader><CardContent>{data?.lastSyncAt ? <p className="text-sm text-muted-foreground">Letzte gespeicherte Standings-Synchronisierung: <span className="font-medium text-foreground">{formatDate(data.lastSyncAt)}</span></p> : <p className="text-sm text-muted-foreground">Noch keine Standings-Synchronisierung gespeichert.</p>}</CardContent></Card></div>
}

function Metric({ icon: Icon, label, value }: { icon: typeof Database; label: string; value: number }) {
  return <Card><CardContent className="flex items-center gap-4 p-5"><span className="flex h-10 w-10 items-center justify-center rounded-md bg-muted text-primary"><Icon size={19} aria-hidden="true" /></span><div><p className="text-sm text-muted-foreground">{label}</p><p className="mt-1 text-2xl font-semibold tabular">{value}</p></div></CardContent></Card>
}
