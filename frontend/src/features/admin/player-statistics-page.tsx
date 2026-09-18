import { useEffect, useState } from 'react'
import { Link, useLocation, useParams } from 'react-router-dom'
import { ApiError, getPlayerStatistics } from '@/api/client'
import type { PlayerStatistics, PlayerTournamentContribution } from '@/api/types'
import { Alert } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { formatDecimal } from '@/lib/utils'

const reasons: Record<PlayerTournamentContribution['reason'], string> = {
  counted: 'Gewertet', excluded: 'Turnier ausgeschlossen', zero_games: 'Nicht gewertet: 0 Spiele', superseded: 'Überholtes Ergebnis'
}
const dateFormat = new Intl.DateTimeFormat('de-DE', { dateStyle: 'medium', timeZone: 'Europe/Berlin' })
const timeFormat = new Intl.DateTimeFormat('de-DE', { dateStyle: 'medium', timeStyle: 'short', timeZone: 'Europe/Berlin' })

export function PlayerStatisticsPage() {
  const { id } = useParams()
  const playerId = Number(id)
  const validId = /^\d+$/.test(id ?? '') && Number.isSafeInteger(playerId) && playerId > 0
  const location = useLocation()
  const search = typeof location.state?.playersSearch === 'string' ? location.state.playersSearch : ''
  const [data, setData] = useState<PlayerStatistics | null>(null)
  const [error, setError] = useState<'missing' | 'failed' | null>(null)
  const [retry, setRetry] = useState(0)
  useEffect(() => {
    let current = true
    setData(null)
    setError(null)
    if (!validId) { setError('missing'); return }
    getPlayerStatistics(playerId).then(value => { if (current) setData(value) }).catch(value => {
      if (current) setError(value instanceof ApiError && value.status === 404 ? 'missing' : 'failed')
    })
    return () => { current = false }
  }, [playerId, validId, retry])

  return <div className="space-y-6">
    <Link to={'/admin/players' + (search ? '?' + search : '')} className="inline-flex min-h-11 items-center text-sm text-primary underline underline-offset-4">Zur Spielerübersicht</Link>
    {error ? <Alert variant="destructive"><h1 className="font-semibold">{error === 'missing' ? 'Spieler nicht gefunden' : 'Statistik konnte nicht geladen werden'}</h1>{error === 'failed' && <Button className="mt-3" variant="outline" onClick={() => setRetry(value => value + 1)}>Erneut versuchen</Button>}</Alert> : !data ? <p role="status">Spielerstatistik wird geladen …</p> : <>
      <header><h1 className="break-words text-2xl font-bold tracking-tight">{data.requestedPlayer.displayName}</h1><p className="mt-2 text-sm text-muted-foreground">Spieler #{data.requestedPlayer.id} · Gesamtstand vom {timeFormat.format(new Date(data.computedAt))} (Europe/Berlin)</p></header>
      {data.requestedPlayer.id !== data.player.id && <Alert>Diese Identität wurde zusammengeführt. Die angezeigte Statistik gehört zu <Link className="font-medium text-primary underline" to={`/admin/players/${data.player.id}`}>{data.player.displayName}</Link> (Spieler #{data.player.id}) und umfasst dessen zugeordnete Ergebnisse.</Alert>}
      <Card><CardHeader><CardTitle>Gesamtstatistik · {data.player.displayName}</CardTitle><CardDescription>Gewertete Turnierbeiträge und aktuell wirksame manuelle Korrekturen. Fehlende Werte werden als „—“ angezeigt.</CardDescription></CardHeader><CardContent><dl className="grid grid-cols-2 gap-4 sm:grid-cols-3 xl:grid-cols-5"><Metric label="Turniere" value={data.player.tournamentCount} /><SourceMetrics value={data.player} /></dl></CardContent></Card>
      <section aria-labelledby="player-tournaments-heading" className="space-y-4"><div><h2 id="player-tournaments-heading" className="text-xl font-semibold">Turnierbeiträge</h2><p className="mt-2 text-sm text-muted-foreground">Die Werte zeigen die gelieferten Ergebnisse. Nur „Gewertet“ fließt in die Gesamtstatistik ein. Punkte/Spiel wird aus den gesamten Punkten und Spielen berechnet, nicht addiert.</p></div>
        {data.tournaments.length === 0 ? <p className="rounded-lg border bg-card p-5">Keine Turnierergebnisse vorhanden.</p> : data.tournaments.map(row => <TournamentContribution key={row.id} row={row} />)}
      </section>
      <section aria-labelledby="player-corrections-heading" className="space-y-4"><h2 id="player-corrections-heading" className="text-xl font-semibold">Manuelle Korrekturen</h2>
        {data.corrections.length === 0 ? <p className="rounded-lg border bg-card p-5">Keine manuellen Korrekturen vorhanden.</p> : data.corrections.map(({ correction, effective }) => <article key={correction.id} className="rounded-lg border bg-card p-4 sm:p-5"><h3 className="font-medium">Korrektur #{correction.id} · {effective ? 'Wirksam' : correction.status === 'revoked' ? 'Aufgehoben' : correction.status === 'replaced' ? 'Ersetzt' : correction.status === 'active' ? 'Geplant' : 'Nicht wirksam'}</h3><p className="mt-2 text-sm text-muted-foreground">Wirksamkeitsdatum: {date(correction.effectiveDate)}</p><p className="mt-2 break-words text-sm">{correction.reason}</p><dl className="mt-4 grid grid-cols-2 gap-4 sm:grid-cols-4"><Metric label="Turniere (Delta)" value={signed(correction.tournamentCountDelta)} /><Metric label="Spiele (Delta)" value={signed(correction.gamesPlayedDelta)} /><Metric label="Punkte (Delta)" value={signed(correction.pointsCentsDelta / 100, true)} /><Metric label="Tordifferenz (Delta)" value={signed(correction.goalDifferenceDelta)} /></dl>{correction.replacedByCorrectionId && <p className="mt-3 text-sm text-muted-foreground">Ersetzt durch Korrektur #{correction.replacedByCorrectionId}</p>}</article>)}
      </section>
    </>}
  </div>
}

function TournamentContribution({ row }: { row: PlayerTournamentContribution }) {
  const url = safeUrl(row.url)
  return <article className="rounded-lg border bg-card p-4 sm:p-5">
    <div className="flex flex-wrap items-start justify-between gap-3"><div className="min-w-0"><h3 className="break-words font-semibold">{row.name}</h3><p className="mt-1 text-sm text-muted-foreground">{date(row.date)} · {row.status}</p></div><span className="rounded-md bg-muted px-3 py-1 text-sm font-medium">{reasons[row.reason]}</span></div>
    <dl className="mt-4 grid grid-cols-1 gap-3 rounded-md bg-muted/40 p-3 sm:grid-cols-2">
      <Provenance label="Standing-Platz (Quelle)" value={row.standingRank == null ? 'Unbekannt' : `Platz ${row.standingRank}`} />
      <Provenance label="Spielername in der Quelle" value={row.sourcePlayerName} />
      <Provenance label="Quell-Ergebnis-ID" value={row.standingSourceId} />
      <Provenance label="Standing-Key" value={row.standingKey} />
    </dl>
    <p className="mt-3 break-all text-xs text-muted-foreground">Quelle: {row.source} · Quell-Turnier-ID: {row.sourceId} · Internes Ergebnis #{row.id}</p>
    {url && <a className="mt-1 inline-flex min-h-11 items-center text-sm text-primary underline underline-offset-4" href={url} target="_blank" rel="noopener noreferrer">Ergebnis in der Quelle öffnen<span className="sr-only"> (neuer Tab)</span></a>}
    <dl className="mt-4 grid grid-cols-2 gap-4 sm:grid-cols-4"><SourceMetrics value={row} /></dl>
  </article>
}

function Provenance({ label, value }: { label: string; value: string | null }) {
  return <div className="min-w-0"><dt className="text-xs text-muted-foreground">{label}</dt><dd className="break-all text-sm font-medium">{value?.trim() ? value : 'Unbekannt'}</dd></div>
}

function SourceMetrics({ value }: { value: Pick<PlayerTournamentContribution, 'gamesPlayed' | 'totalPointsCents' | 'pointsPerGameCents' | 'goalDifference'> }) {
  return <><Metric label="Spiele" value={value.gamesPlayed ?? '—'} /><Metric label="Punkte" value={value.totalPointsCents === null ? '—' : formatDecimal(value.totalPointsCents / 100)} /><Metric label="Punkte/Spiel" value={value.pointsPerGameCents === null ? '—' : formatDecimal(value.pointsPerGameCents / 100)} /><Metric label="Tordifferenz" value={value.goalDifference ?? '—'} /></>
}
function Metric({ label, value }: { label: string; value: string | number }) { return <div><dt className="text-sm text-muted-foreground">{label}</dt><dd className="tabular font-medium">{value}</dd></div> }
function signed(value: number, decimal = false) { return (value > 0 ? '+' : '') + (decimal ? formatDecimal(value) : String(value)) }
function date(value: string | null) { return value ? dateFormat.format(new Date(value)) : '—' }
function safeUrl(value: string) {
  try { const url = new URL(value); return (url.protocol === 'https:' || url.protocol === 'http:') && !url.username && !url.password ? url.href : null } catch { return null }
}
