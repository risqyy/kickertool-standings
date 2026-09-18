import { useEffect, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { getPlayers, type PlayerQuery } from '@/api/client'
import type { PlayerPage, PlayerSummary } from '@/api/types'
import { Alert } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

export function PlayersPage() {
  const [params, setParams] = useSearchParams()
  const query = params.get('q') ?? ''
  const rawState = params.get('state')
  const state: PlayerQuery['state'] = rawState === 'active' || rawState === 'merged' ? rawState : 'all'
  const rawPage = Number(params.get('page') ?? 1)
  const page = Number.isSafeInteger(rawPage) && rawPage > 0 ? rawPage : 1
  const [data, setData] = useState<PlayerPage | null>(null)
  const [error, setError] = useState(false)
  const [retry, setRetry] = useState(0)
  useEffect(() => {
    let current = true
    setData(null)
    setError(false)
    getPlayers({ q: query, state, page, limit: 25 }).then(value => {
      if (current) setData(value)
    }).catch(() => { if (current) setError(true) })
    return () => { current = false }
  }, [query, state, page, retry])

  function update(key: string, value: string) {
    const next = new URLSearchParams(params)
    if (value) next.set(key, value)
    else next.delete(key)
    if (key !== 'page') next.delete('page')
    setParams(next, { replace: key === 'q' })
  }
  const detailLink = (player: PlayerSummary) => <Link className="font-medium text-primary underline underline-offset-4" to={`/admin/players/${player.id}`} state={{ playersSearch: params.toString() }}>{player.displayName}</Link>
  return <div className="space-y-6">
    <div><h1 className="text-2xl font-bold tracking-tight">Spieler</h1><p className="mt-2 text-muted-foreground">Alle gespeicherten Identitäten und ihre Statistik – auch ohne Ergebnisse oder nach einer Zusammenführung.</p></div>
    <div className="grid gap-4 rounded-lg border bg-card p-4 sm:grid-cols-[1fr_15rem]">
      <div><label htmlFor="players-search" className="text-sm font-medium">Spieler suchen</label><Input id="players-search" className="mt-2" value={query} onChange={event => update('q', event.target.value)} placeholder="Name oder Alias" /></div>
      <div><label htmlFor="players-state" className="text-sm font-medium">Identitäten</label><select id="players-state" className="mt-2 h-11 w-full rounded-md border bg-background px-3 text-sm" value={state} onChange={event => update('state', event.target.value)}><option value="all">Alle Spieler</option><option value="active">Aktive Identitäten</option><option value="merged">Zusammengeführte Identitäten</option></select></div>
    </div>
    {error ? <Alert variant="destructive"><p>Spieler konnten nicht geladen werden.</p><Button className="mt-3" variant="outline" onClick={() => setRetry(value => value + 1)}>Erneut versuchen</Button></Alert> : !data ? <p role="status">Spieler werden geladen …</p> : <>
      <p role="status" className="text-sm text-muted-foreground">{data.total} Spieler gefunden</p>
      {data.items.length === 0 ? <p className="rounded-lg border bg-card p-6">Keine Spieler für diese Auswahl.</p> : <>
        <div className="hidden overflow-hidden rounded-lg border bg-card md:block"><table className="w-full text-left text-sm"><caption className="sr-only">Gespeicherte Spieler</caption><thead className="border-b bg-muted/40"><tr><th className="p-4" scope="col">Spieler</th><th className="p-4" scope="col">Identität</th><th className="p-4" scope="col">ID</th></tr></thead><tbody>{data.items.map(player => <tr key={player.id} className="border-b last:border-0"><td className="p-4">{detailLink(player)}</td><td className="p-4">{identityLabel(player)}</td><td className="p-4 tabular">{player.id}</td></tr>)}</tbody></table></div>
        <div className="space-y-3 md:hidden">{data.items.map(player => <article className="rounded-lg border bg-card p-4" key={player.id}><h2 className="break-words">{detailLink(player)}</h2><p className="mt-2 text-sm text-muted-foreground">{identityLabel(player)} · ID {player.id}</p></article>)}</div>
      </>}
      <nav aria-label="Spieler-Seiten" className="flex flex-wrap items-center justify-between gap-3"><Button variant="outline" disabled={page <= 1} onClick={() => update('page', String(page - 1))}>Zurück</Button><span className="text-sm">Seite {data.page} von {Math.max(1, Math.ceil(data.total / data.limit))}</span><Button variant="outline" disabled={data.page * data.limit >= data.total} onClick={() => update('page', String(page + 1))}>Weiter</Button></nav>
    </>}
  </div>
}

function identityLabel(player: PlayerSummary) {
  return player.mergedIntoPlayerId ? `Zusammengeführt → Spieler #${player.mergedIntoPlayerId}` : player.active ? 'Aktiv' : 'Inaktiv'
}
