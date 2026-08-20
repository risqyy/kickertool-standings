import { useEffect, useState, type ReactNode } from 'react'
import { CheckCircle2, History, Search, ShieldAlert, Undo2 } from 'lucide-react'
import { ApiError, confirmManualCorrection, listManualCorrections, previewManualCorrection, revokeManualCorrection, searchPlayers } from '@/api/client'
import type { ManualRankingCorrection, ManualRankingCorrectionPreview, Player } from '@/api/types'
import { useAdminSession } from '@/app/providers'
import { Alert } from '@/components/ui/alert'
import { AlertDialog } from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { formatDecimal, formatDate } from '@/lib/utils'

const today = berlinDate(new Date())

export function ManualCorrectionsPage() {
  const session = useAdminSession()
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<Player[]>([])
  const [activeResultIndex, setActiveResultIndex] = useState(0)
  const [player, setPlayer] = useState<Player | null>(null)
  const [corrections, setCorrections] = useState<ManualRankingCorrection[]>([])
  const [version, setVersion] = useState(0)
  const [date, setDate] = useState(today)
  const [tournaments, setTournaments] = useState('0')
  const [games, setGames] = useState('0')
  const [points, setPoints] = useState('0.00')
  const [goals, setGoals] = useState('0')
  const [reason, setReason] = useState('')
  const [preview, setPreview] = useState<ManualRankingCorrectionPreview | null>(null)
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [revokeTarget, setRevokeTarget] = useState<ManualRankingCorrection | null>(null)
  const [revokeReason, setRevokeReason] = useState('')
  const [replaceTarget, setReplaceTarget] = useState<ManualRankingCorrection | null>(null)
  const [error, setError] = useState('')
  const [success, setSuccess] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    const timer = setTimeout(() => {
      if (query.trim().length < 2) { setResults([]); return }
      searchPlayers(query).then(values => { setResults(values); setActiveResultIndex(0) }).catch(() => setResults([]))
    }, 250)
    return () => clearTimeout(timer)
  }, [query])

  const choose = async (value: Player) => {
    setPlayer(value); setQuery(value.displayName); setResults([]); setActiveResultIndex(0); setPreview(null); setError(''); setSuccess('')
    try { const history = await listManualCorrections(value.id); setCorrections(history.items); setVersion(history.version) } catch { setCorrections([]); setVersion(value.rankingCorrectionVersion ?? 0) }
  }

  const loadHistory = async () => {
    if (!player) return
    const history = await listManualCorrections(player.id)
    setCorrections(history.items); setVersion(history.version)
  }

  const createPreview = async () => {
    if (!player) { setError('Bitte zuerst einen Spieler auswählen.'); return }
    const values = { effectiveDate: date, effectiveYear: Number(date.slice(0, 4)), tournamentCountDelta: Number(tournaments), gamesPlayedDelta: Number(games), pointsCentsDelta: Math.round(Number(points.replace(',', '.')) * 100), goalDifferenceDelta: Number(goals), reason, ...(replaceTarget ? { replaceCorrectionId: replaceTarget.id } : {}) }
    setBusy(true); setError(''); setSuccess('')
    try { setPreview(await previewManualCorrection(session.csrf, player.id, values)) } catch (value) { setError(value instanceof Error ? value.message : 'Vorschau konnte nicht erstellt werden.') } finally { setBusy(false) }
  }

  const confirm = async () => {
    if (!preview) return
    setBusy(true); setError('')
    try { await confirmManualCorrection(session.csrf, preview.token, preview.expectedVersion); setSuccess('Korrektur gespeichert. Die Ranglisten wurden neu berechnet.'); setPreview(null); setConfirmOpen(false); setReplaceTarget(null); await loadHistory() } catch (value) { setError(value instanceof ApiError && value.status === 409 ? 'Die Vorschau ist veraltet. Bitte eine neue Vorschau erstellen.' : value instanceof Error ? value.message : 'Korrektur konnte nicht gespeichert werden.') } finally { setBusy(false) }
  }

  const revoke = async () => {
    if (!player || !revokeTarget) return
    setBusy(true); setError('')
    if (revokeReason.trim().length < 3) { setError('Bitte einen Aufhebungsgrund mit mindestens 3 Zeichen eingeben.'); setBusy(false); return }
    try { await revokeManualCorrection(session.csrf, player.id, revokeTarget.id, version, revokeReason); setSuccess('Korrektur revisionssicher aufgehoben.'); setRevokeTarget(null); setRevokeReason(''); await loadHistory() } catch (value) { setError(value instanceof ApiError && value.status === 409 ? 'Der Spieler wurde zwischenzeitlich geändert. Bitte Historie neu laden.' : value instanceof Error ? value.message : 'Aufhebung fehlgeschlagen.') } finally { setBusy(false) }
  }

  const startReplace = (item: ManualRankingCorrection) => {
    setReplaceTarget(item); setDate(item.effectiveDate); setTournaments(String(item.tournamentCountDelta)); setGames(String(item.gamesPlayedDelta)); setPoints((item.pointsCentsDelta / 100).toFixed(2)); setGoals(String(item.goalDifferenceDelta)); setReason(item.reason); setPreview(null); setError(''); setSuccess('')
  }

  return <div>
    <div className="mb-6"><p className="text-sm font-medium text-primary">Datenpflege</p><h1 className="mt-1 text-3xl font-bold tracking-tight">Manuelle Ranking-Korrekturen</h1><p className="mt-2 max-w-3xl text-muted-foreground">Korrekturen werden als separate Buchungen mit Wirksamkeitsdatum und Administrator protokolliert. Quelldaten bleiben unverändert.</p></div>
    {error && <Alert variant="destructive" className="mb-4"><div className="flex items-center gap-3"><ShieldAlert aria-hidden="true" />{error}</div></Alert>}
    {success && <Alert variant="success" className="mb-4"><div className="flex items-center gap-3"><CheckCircle2 aria-hidden="true" />{success}</div></Alert>}
    <Card><CardHeader><CardTitle>Spieler auswählen</CardTitle><CardDescription>Suche umfasst kanonische Namen und Namensvarianten.</CardDescription></CardHeader><CardContent><div className="relative max-w-xl"><label htmlFor="correction-player-search" className="sr-only">Spieler suchen</label><Search className="absolute left-3 top-3.5 text-muted-foreground" size={17} aria-hidden="true" /><Input id="correction-player-search" className="pl-10" value={query} onChange={event => { setQuery(event.target.value); setPlayer(null); setActiveResultIndex(0) }} onKeyDown={event => { if (results.length === 0) return; if (event.key === 'ArrowDown') { event.preventDefault(); setActiveResultIndex(index => Math.min(index + 1, results.length - 1)) } else if (event.key === 'ArrowUp') { event.preventDefault(); setActiveResultIndex(index => Math.max(index - 1, 0)) } else if (event.key === 'Enter') { event.preventDefault(); void choose(results[activeResultIndex]) } else if (event.key === 'Escape') { setResults([]) } }} placeholder="Mindestens 2 Zeichen" role="combobox" aria-autocomplete="list" aria-expanded={results.length > 0} aria-controls="correction-player-options" aria-activedescendant={results[activeResultIndex] ? `correction-player-option-${results[activeResultIndex].id}` : undefined} />{results.length > 0 && <div id="correction-player-options" className="absolute z-10 mt-2 w-full rounded-md border bg-card p-1 shadow-lg" role="listbox" aria-label="Spielergebnisse">{results.map((value, index) => <button type="button" role="option" id={`correction-player-option-${value.id}`} aria-selected={index === activeResultIndex} key={value.id} className={`flex min-h-11 w-full items-center justify-between rounded px-3 text-left text-sm hover:bg-muted ${index === activeResultIndex ? 'bg-muted' : ''}`} onClick={() => choose(value)}><span className="font-medium">{value.displayName}</span><span className="text-xs text-muted-foreground">{value.tournamentCount} Turniere</span></button>)}</div>}</div>{player && <p className="mt-3 text-sm text-muted-foreground">Ausgewählt: <strong className="text-foreground">{player.displayName}</strong> · Korrekturversion {version}</p>}</CardContent></Card>
    <div className="mt-6 grid gap-6 xl:grid-cols-[minmax(0,1fr)_minmax(20rem,0.8fr)]">
      <Card><CardHeader><CardTitle>{replaceTarget ? 'Korrektur ändern' : 'Additive Buchung'}</CardTitle><CardDescription>{replaceTarget ? `Buchung #${replaceTarget.id} wird revisionssicher ersetzt; die alte Buchung bleibt verknüpft in der Historie.` : 'Positive und negative Werte sind zulässig; Punkte/Spiel wird aus den resultierenden Punkten und Spielen berechnet.'}</CardDescription></CardHeader><CardContent>{replaceTarget && <Alert className="mb-4" variant="warning">Ersetze Buchung #{replaceTarget.id}. <Button variant="ghost" size="sm" onClick={() => setReplaceTarget(null)}>Änderung abbrechen</Button></Alert>}<div className="grid gap-4 sm:grid-cols-2"><Field id="correction-effective-date" label="Wirksam ab (Datum)"><Input id="correction-effective-date" type="date" value={date} onChange={event => setDate(event.target.value)} /></Field><Field id="correction-reason" label="Grund"><Input id="correction-reason" value={reason} onChange={event => setReason(event.target.value)} maxLength={500} placeholder="z. B. Nachtrag aus Turnierprotokoll" /></Field><Field id="correction-tournaments" label="Turniere (Delta)"><Input id="correction-tournaments" type="number" step="1" value={tournaments} onChange={event => setTournaments(event.target.value)} /></Field><Field id="correction-games" label="Spiele (Delta)"><Input id="correction-games" type="number" step="1" value={games} onChange={event => setGames(event.target.value)} /></Field><Field id="correction-points" label="Punkte (Delta)"><Input id="correction-points" type="number" step="0.01" value={points} onChange={event => setPoints(event.target.value)} /></Field><Field id="correction-goals" label="Tordifferenz (Delta)"><Input id="correction-goals" type="number" step="1" value={goals} onChange={event => setGoals(event.target.value)} /></Field></div><Button className="mt-5" disabled={!player || busy} onClick={createPreview}>Vorschau erstellen</Button></CardContent></Card>
      <Card><CardHeader><CardTitle>Historie</CardTitle><CardDescription>Aufhebungen und Änderungen erzeugen neue Revisionen; Einträge werden nicht gelöscht.</CardDescription></CardHeader><CardContent>{!player ? <p className="text-sm text-muted-foreground">Wähle einen Spieler, um Korrekturen und Revisionen zu sehen.</p> : corrections.length === 0 ? <p className="text-sm text-muted-foreground">Keine manuellen Korrekturen.</p> : <div className="space-y-3">{corrections.map(item => <CorrectionRow key={item.id} item={item} disabled={busy || item.status !== 'active'} onRevoke={() => setRevokeTarget(item)} onReplace={() => startReplace(item)} />)}</div>}</CardContent></Card>
    </div>
    {preview && <PreviewCard preview={preview} onConfirm={() => setConfirmOpen(true)} />}
    <AlertDialog open={confirmOpen} title="Korrektur endgültig speichern?" description={preview ? `${preview.correction.effectiveDate}: ${preview.correction.reason}. Die Rohdaten werden nicht verändert.` : ''} confirmLabel="Korrektur speichern" onCancel={() => setConfirmOpen(false)} onConfirm={confirm} />
    <AlertDialog open={revokeTarget !== null} title="Korrektur revisionssicher aufheben?" description={revokeTarget ? `Buchung vom ${revokeTarget.effectiveDate} wird aufgehoben und bleibt in der Historie sichtbar.` : ''} confirmLabel="Aufhebung speichern" destructive onCancel={() => { setRevokeTarget(null); setRevokeReason('') }} onConfirm={revoke}><Field id="correction-revoke-reason" label="Aufhebungsgrund"><Input id="correction-revoke-reason" value={revokeReason} onChange={event => setRevokeReason(event.target.value)} maxLength={500} placeholder="Warum wird diese Buchung aufgehoben?" /></Field></AlertDialog>
  </div>
}

function Field({ id, label, children }: { id: string; label: string; children: ReactNode }) { return <div><label htmlFor={id} className="text-sm font-medium">{label}</label><div className="mt-2">{children}</div></div> }
function CorrectionRow({ item, disabled, onRevoke, onReplace }: { item: ManualRankingCorrection; disabled: boolean; onRevoke: () => void; onReplace: () => void }) { return <article className="rounded-md border p-3 text-sm"><div className="flex items-start justify-between gap-3"><div className="min-w-0"><p className="font-medium">{item.effectiveDate} · {item.status === 'active' ? 'Aktiv' : item.status === 'replaced' ? 'Ersetzt' : 'Aufgehoben'}</p><p className="mt-1 break-words text-muted-foreground">{item.reason}</p>{item.status === 'revoked' && <p className="mt-1 break-words text-xs text-muted-foreground">Aufgehoben am {item.revokedAt ? formatDate(item.revokedAt) : '—'} von {item.revokedBy || '—'}: {item.revocationReason || '—'}</p>}{item.replacedByCorrectionId && <p className="mt-1 text-xs text-muted-foreground">Ersetzt durch Buchung #{item.replacedByCorrectionId}</p>}{item.supersedesCorrectionId && <p className="mt-1 text-xs text-muted-foreground">Ersetzt Buchung #{item.supersedesCorrectionId}</p>}</div>{item.status === 'active' && <div className="flex flex-wrap justify-end gap-2"><Button size="sm" variant="outline" disabled={disabled} onClick={onReplace}>Ändern</Button><Button size="sm" variant="outline" disabled={disabled} onClick={onRevoke}><Undo2 size={15} aria-hidden="true" />Aufheben</Button></div>}</div><dl className="mt-3 grid grid-cols-2 gap-2 text-xs text-muted-foreground sm:grid-cols-4"><Metric label="Turniere" value={signed(item.tournamentCountDelta)} /><Metric label="Spiele" value={signed(item.gamesPlayedDelta)} /><Metric label="Punkte" value={signed(item.pointsCentsDelta / 100)} /><Metric label="Tordiff." value={signed(item.goalDifferenceDelta)} /></dl><p className="mt-3 flex items-center gap-2 text-xs text-muted-foreground"><History size={13} aria-hidden="true" />{item.administrator} · Revision {item.revision} · {formatDate(item.createdAt)}</p></article> }
function PreviewCard({ preview, onConfirm }: { preview: ManualRankingCorrectionPreview; onConfirm: () => void }) { return <Card className="mt-6 border-2 border-primary/40"><CardHeader><CardTitle>Vorschau prüfen</CardTitle><CardDescription>{preview.player.displayName} · {preview.correction.effectiveDate} · {preview.correction.reason}{preview.superseded ? ` · ersetzt Buchung #${preview.superseded.id}` : ''}</CardDescription></CardHeader><CardContent><div className="grid gap-4 sm:grid-cols-2"><Aggregate label="Vorher" value={preview.before} /><Aggregate label="Nachher" value={preview.after} highlight /></div><Button className="mt-5" onClick={onConfirm}>Prüfen und bestätigen</Button></CardContent></Card> }
function Aggregate({ label, value, highlight = false }: { label: string; value: ManualRankingCorrectionPreview['before']; highlight?: boolean }) { return <article className={highlight ? 'rounded-md border-2 border-primary/40 bg-primary/5 p-4' : 'rounded-md border p-4'}><h3 className="font-medium">{label}</h3><dl className="mt-3 grid grid-cols-2 gap-2 text-sm"><Metric label="Turniere" value={value.tournamentCount} /><Metric label="Spiele" value={value.gamesPlayed ?? '—'} /><Metric label="Punkte" value={value.totalPointsCents === null ? '—' : formatDecimal(value.totalPointsCents / 100)} /><Metric label="Punkte/Spiel" value={value.pointsPerGameCents === null ? '—' : formatDecimal(value.pointsPerGameCents / 100)} /><Metric label="Tordifferenz" value={value.goalDifference ?? '—'} /></dl></article> }
function Metric({ label, value }: { label: string; value: string | number }) { return <div><dt className="text-muted-foreground">{label}</dt><dd className="tabular font-medium text-foreground">{value}</dd></div> }
function signed(value: number) { return value > 0 ? `+${value}` : String(value) }
function berlinDate(value: Date) { const parts = new Intl.DateTimeFormat('en-CA', { timeZone: 'Europe/Berlin', year: 'numeric', month: '2-digit', day: '2-digit' }).formatToParts(value); const get = (type: string) => parts.find(part => part.type === type)?.value ?? ''; return `${get('year')}-${get('month')}-${get('day')}` }
