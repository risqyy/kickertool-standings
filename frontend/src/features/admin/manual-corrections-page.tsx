import { useEffect, useState, type ReactNode } from 'react'
import { CheckCircle2, History, Search, ShieldAlert, Undo2 } from 'lucide-react'
import { ApiError, confirmManualCorrection, listManualCorrections, previewManualCorrection, revokeManualCorrection, searchPlayers, type ManualCorrectionInput } from '@/api/client'
import type { ManualRankingCorrection, ManualRankingCorrectionPreview, Player, RankingAggregate } from '@/api/types'
import { useAdminSession } from '@/app/providers'
import { Alert } from '@/components/ui/alert'
import { AlertDialog } from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { formatDecimal, formatDate } from '@/lib/utils'

const today = berlinDate(new Date())
type FieldName = 'date' | 'year' | 'reason' | 'tournaments' | 'games' | 'points' | 'goals' | 'revokeReason'
type FieldErrors = Partial<Record<FieldName, string>>

export function ManualCorrectionsPage() {
  const session = useAdminSession()
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<Player[]>([])
  const [activeResultIndex, setActiveResultIndex] = useState(0)
  const [player, setPlayer] = useState<Player | null>(null)
  const [corrections, setCorrections] = useState<ManualRankingCorrection[]>([])
  const [version, setVersion] = useState(0)
  const [date, setDate] = useState(today)
  const [year, setYear] = useState(String(Number(today.slice(0, 4))))
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
  const [fieldErrors, setFieldErrors] = useState<FieldErrors>({})
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
    setPlayer(value)
    setQuery(value.displayName)
    setResults([])
    setActiveResultIndex(0)
    setPreview(null)
    setError('')
    setSuccess('')
    try {
      const history = await listManualCorrections(value.id)
      setCorrections(history.items)
      setVersion(history.version)
    } catch {
      setCorrections([])
      setVersion(value.rankingCorrectionVersion ?? 0)
    }
  }

  const loadHistory = async () => {
    if (!player) return
    const history = await listManualCorrections(player.id)
    setCorrections(history.items)
    setVersion(history.version)
  }

  const createPreview = async () => {
    if (!player) {
      setError('Bitte zuerst einen Spieler auswählen.')
      return
    }
    const input = getCorrectionInput({ date, year, tournaments, games, points, goals, reason, replaceTarget })
    const validation = validateCorrectionInput(input, { date, year, tournaments, games, points, goals, reason })
    setFieldErrors(validation)
    if (Object.keys(validation).length > 0) {
      setError('Bitte korrigiere die markierten Eingaben.')
      return
    }

    setBusy(true)
    setError('')
    setSuccess('')
    try {
      setPreview(await previewManualCorrection(session.csrf, player.id, input))
    } catch (value) {
      setError(manualCorrectionError(value, 'Vorschau konnte nicht erstellt werden.'))
    } finally {
      setBusy(false)
    }
  }

  const confirm = async () => {
    if (!preview) return
    setBusy(true)
    setError('')
    try {
      await confirmManualCorrection(session.csrf, preview.token, preview.expectedVersion)
      setSuccess('Korrektur gespeichert. Die Ranglisten wurden neu berechnet.')
      setPreview(null)
      setConfirmOpen(false)
      setReplaceTarget(null)
      await loadHistory()
    } catch (value) {
      setError(manualCorrectionError(value, 'Korrektur konnte nicht gespeichert werden.'))
    } finally {
      setBusy(false)
    }
  }

  const revoke = async () => {
    if (!player || !revokeTarget) return
    const trimmedReason = revokeReason.trim()
    if (trimmedReason.length < 3) {
      setFieldErrors(current => ({ ...current, revokeReason: 'Bitte einen Aufhebungsgrund mit mindestens 3 Zeichen eingeben.' }))
      setError('Bitte korrigiere die markierten Eingaben.')
      return
    }
    setBusy(true)
    setError('')
    try {
      await revokeManualCorrection(session.csrf, player.id, revokeTarget.id, version, trimmedReason)
      setSuccess('Korrektur revisionssicher aufgehoben.')
      setRevokeTarget(null)
      setRevokeReason('')
      clearFieldError('revokeReason')
      await loadHistory()
    } catch (value) {
      setError(manualCorrectionError(value, 'Aufhebung konnte nicht gespeichert werden.'))
    } finally {
      setBusy(false)
    }
  }

  const startReplace = (item: ManualRankingCorrection) => {
    setReplaceTarget(item)
    setDate(item.effectiveDate)
    setYear(String(item.effectiveYear))
    setTournaments(String(item.tournamentCountDelta))
    setGames(String(item.gamesPlayedDelta))
    setPoints((item.pointsCentsDelta / 100).toFixed(2))
    setGoals(String(item.goalDifferenceDelta))
    setReason(item.reason)
    setPreview(null)
    setFieldErrors({})
    setError('')
    setSuccess('')
  }

  const clearFieldError = (field: FieldName) => {
    setFieldErrors(current => {
      if (!current[field]) return current
      const next = { ...current }
      delete next[field]
      return next
    })
  }

  return <div>
    <div className="mb-6"><p className="text-sm font-medium text-primary">Datenpflege</p><h1 className="mt-1 text-3xl font-bold tracking-tight">Manuelle Ranking-Korrekturen</h1><p className="mt-2 max-w-3xl text-muted-foreground">Korrekturen werden als separate Buchungen mit Kalenderjahr und Wirksamkeitsdatum protokolliert. Quelldaten bleiben unverändert.</p></div>
    {error && <Alert variant="destructive" className="mb-4"><div className="flex items-center gap-3"><ShieldAlert aria-hidden="true" />{error}</div></Alert>}
    {success && <Alert variant="success" className="mb-4"><div className="flex items-center gap-3"><CheckCircle2 aria-hidden="true" />{success}</div></Alert>}
    <Card><CardHeader><CardTitle>Spieler auswählen</CardTitle><CardDescription>Suche umfasst kanonische Namen und Namensvarianten.</CardDescription></CardHeader><CardContent><div className="relative max-w-xl"><label htmlFor="correction-player-search" className="sr-only">Spieler suchen</label><Search className="absolute left-3 top-3.5 text-muted-foreground" size={17} aria-hidden="true" /><Input id="correction-player-search" className="pl-10" value={query} onChange={event => { setQuery(event.target.value); setPlayer(null); setActiveResultIndex(0) }} onKeyDown={event => { if (results.length === 0) return; if (event.key === 'ArrowDown') { event.preventDefault(); setActiveResultIndex(index => Math.min(index + 1, results.length - 1)) } else if (event.key === 'ArrowUp') { event.preventDefault(); setActiveResultIndex(index => Math.max(index - 1, 0)) } else if (event.key === 'Enter') { event.preventDefault(); void choose(results[activeResultIndex]) } else if (event.key === 'Escape') { setResults([]) } }} placeholder="Mindestens 2 Zeichen" role="combobox" aria-autocomplete="list" aria-expanded={results.length > 0} aria-controls="correction-player-options" aria-activedescendant={results[activeResultIndex] ? `correction-player-option-${results[activeResultIndex].id}` : undefined} />{results.length > 0 && <div id="correction-player-options" className="absolute z-10 mt-2 w-full rounded-md border bg-card p-1 shadow-lg" role="listbox" aria-label="Spielergebnisse">{results.map((value, index) => <button type="button" role="option" id={`correction-player-option-${value.id}`} aria-selected={index === activeResultIndex} key={value.id} className={`flex min-h-11 w-full items-center justify-between rounded px-3 text-left text-sm hover:bg-muted ${index === activeResultIndex ? 'bg-muted' : ''}`} onClick={() => choose(value)}><span className="font-medium">{value.displayName}</span><span className="text-xs text-muted-foreground">{value.tournamentCount} Turniere</span></button>)}</div>}</div>{player && <p className="mt-3 text-sm text-muted-foreground">Ausgewählt: <strong className="text-foreground">{player.displayName}</strong> · Korrekturversion {version}</p>}</CardContent></Card>
    <div className="mt-6 grid gap-6 xl:grid-cols-[minmax(0,1fr)_minmax(20rem,0.8fr)]">
      <Card><CardHeader><CardTitle>{replaceTarget ? 'Korrektur ändern' : 'Additive Buchung'}</CardTitle><CardDescription>{replaceTarget ? `Buchung #${replaceTarget.id} wird revisionssicher ersetzt; die alte Buchung bleibt verknüpft in der Historie.` : 'Positive und negative Werte sind zulässig; Punkte/Spiel wird aus den resultierenden Punkten und Spielen berechnet.'}</CardDescription></CardHeader><CardContent>{replaceTarget && <Alert className="mb-4" variant="warning">Ersetze Buchung #{replaceTarget.id}. <Button variant="ghost" size="sm" onClick={() => setReplaceTarget(null)}>Änderung abbrechen</Button></Alert>}<div className="grid gap-4 sm:grid-cols-2"><Field id="correction-effective-date" label="Wirksam ab (Datum)" error={fieldErrors.date} required><Input id="correction-effective-date" type="date" required value={date} aria-invalid={Boolean(fieldErrors.date)} aria-describedby={fieldErrors.date ? 'correction-effective-date-error' : undefined} onChange={event => { setDate(event.target.value); const selectedYear = dateYear(event.target.value); if (selectedYear) { setYear(String(selectedYear)); clearFieldError('year') } clearFieldError('date') }} /></Field><Field id="correction-effective-year" label="Kalenderjahr" error={fieldErrors.year} required><Input id="correction-effective-year" type="number" inputMode="numeric" min={1900} max={2200} step="1" required value={year} aria-invalid={Boolean(fieldErrors.year)} aria-describedby={fieldErrors.year ? 'correction-effective-year-error' : undefined} onChange={event => { setYear(event.target.value); clearFieldError('year') }} /></Field><Field id="correction-reason" label="Grund" error={fieldErrors.reason} required><Input id="correction-reason" required value={reason} aria-invalid={Boolean(fieldErrors.reason)} aria-describedby={fieldErrors.reason ? 'correction-reason-error' : undefined} onChange={event => { setReason(event.target.value); clearFieldError('reason') }} maxLength={500} placeholder="z. B. Nachtrag aus Turnierprotokoll" /></Field><Field id="correction-tournaments" label="Turniere (Delta)" error={fieldErrors.tournaments} required><Input id="correction-tournaments" type="number" step="1" required value={tournaments} aria-invalid={Boolean(fieldErrors.tournaments)} aria-describedby={fieldErrors.tournaments ? 'correction-tournaments-error' : undefined} onChange={event => { setTournaments(event.target.value); clearFieldError('tournaments') }} /></Field><Field id="correction-games" label="Spiele (Delta)" error={fieldErrors.games} required><Input id="correction-games" type="number" step="1" required value={games} aria-invalid={Boolean(fieldErrors.games)} aria-describedby={fieldErrors.games ? 'correction-games-error' : undefined} onChange={event => { setGames(event.target.value); clearFieldError('games') }} /></Field><Field id="correction-points" label="Punkte (Delta)" error={fieldErrors.points} required><Input id="correction-points" type="number" step="0.01" required value={points} aria-invalid={Boolean(fieldErrors.points)} aria-describedby={fieldErrors.points ? 'correction-points-error' : undefined} onChange={event => { setPoints(event.target.value); clearFieldError('points') }} /></Field><Field id="correction-goals" label="Tordifferenz (Delta)" error={fieldErrors.goals} required><Input id="correction-goals" type="number" step="1" required value={goals} aria-invalid={Boolean(fieldErrors.goals)} aria-describedby={fieldErrors.goals ? 'correction-goals-error' : undefined} onChange={event => { setGoals(event.target.value); clearFieldError('goals') }} /></Field></div><Button className="mt-5" disabled={!player || busy} onClick={createPreview}>{busy ? 'Wird geprüft …' : 'Vorschau erstellen'}</Button></CardContent></Card>
      <Card><CardHeader><CardTitle>Historie</CardTitle><CardDescription>Aufhebungen und Änderungen erzeugen neue Revisionen; Einträge werden nicht gelöscht.</CardDescription></CardHeader><CardContent>{!player ? <p className="text-sm text-muted-foreground">Wähle einen Spieler, um Korrekturen und Revisionen zu sehen.</p> : corrections.length === 0 ? <p className="text-sm text-muted-foreground">Keine manuellen Korrekturen.</p> : <div className="space-y-3">{corrections.map(item => <CorrectionRow key={item.id} item={item} disabled={busy || item.status !== 'active'} onRevoke={() => { setRevokeTarget(item); setRevokeReason(''); clearFieldError('revokeReason'); setError('') }} onReplace={() => startReplace(item)} />)}</div>}</CardContent></Card>
    </div>
    {preview && <PreviewCard preview={preview} onConfirm={() => setConfirmOpen(true)} />}
    {preview && <AlertDialog open={confirmOpen} title="Korrektur endgültig speichern?" description="Bitte prüfe Spieler, Jahr, Deltas und die resultierende Gesamtwertung. Die Quelldaten werden nicht verändert." confirmLabel="Korrektur speichern" confirmDisabled={busy} onCancel={() => setConfirmOpen(false)} onConfirm={confirm}><CorrectionImpact preview={preview} /></AlertDialog>}
    {revokeTarget && player && <AlertDialog open title="Korrektur rückgängig machen?" description="Die aktive Korrektur wird revisionssicher aufgehoben und bleibt in der Historie sichtbar." confirmLabel="Rückgängig machen" destructive confirmDisabled={busy || revokeReason.trim().length < 3} onCancel={() => { setRevokeTarget(null); setRevokeReason(''); clearFieldError('revokeReason') }} onConfirm={revoke}><RevokeImpact player={player} correction={revokeTarget} /><Field id="correction-revoke-reason" label="Pflichtgrund für das Rückgängigmachen" error={fieldErrors.revokeReason} required><Input id="correction-revoke-reason" required autoFocus value={revokeReason} aria-invalid={Boolean(fieldErrors.revokeReason)} aria-describedby={fieldErrors.revokeReason ? 'correction-revoke-reason-error' : undefined} onChange={event => { setRevokeReason(event.target.value); clearFieldError('revokeReason') }} maxLength={500} placeholder="Warum wird diese Buchung aufgehoben?" /></Field></AlertDialog>}
  </div>
}

function Field({ id, label, children, error, required = false }: { id: string; label: string; children: ReactNode; error?: string; required?: boolean }) {
  return <div><label htmlFor={id} className="text-sm font-medium">{label}{required && <span className="ml-1 text-destructive" aria-hidden="true">*</span>}</label><div className="mt-2">{children}</div>{error && <p id={`${id}-error`} role="alert" className="mt-1 text-sm text-destructive">{error}</p>}</div>
}

function CorrectionRow({ item, disabled, onRevoke, onReplace }: { item: ManualRankingCorrection; disabled: boolean; onRevoke: () => void; onReplace: () => void }) {
  const statusLabel = item.status === 'active' ? 'Aktiv' : item.status === 'replaced' ? 'Ersetzt' : item.status === 'revoked' ? 'Aufgehoben' : item.status
  return <article className="rounded-md border p-3 text-sm"><div className="flex items-start justify-between gap-3"><div className="min-w-0"><p className="font-medium">{item.effectiveYear} · {item.effectiveDate} · {statusLabel}</p><p className="mt-1 break-words text-muted-foreground">{item.reason}</p>{item.status === 'revoked' && <p className="mt-1 break-words text-xs text-muted-foreground">Aufgehoben am {item.revokedAt ? formatDate(item.revokedAt) : '—'} von {item.revokedBy || '—'}: {item.revocationReason || '—'}</p>}{item.replacedByCorrectionId && <p className="mt-1 text-xs text-muted-foreground">Ersetzt durch Buchung #{item.replacedByCorrectionId}</p>}{item.supersedesCorrectionId && <p className="mt-1 text-xs text-muted-foreground">Ersetzt Buchung #{item.supersedesCorrectionId}</p>}</div>{item.status === 'active' && <div className="flex flex-wrap justify-end gap-2"><Button size="sm" variant="outline" disabled={disabled} onClick={onReplace}>Ändern</Button><Button size="sm" variant="outline" disabled={disabled} onClick={onRevoke}><Undo2 size={15} aria-hidden="true" />Rückgängig machen</Button></div>}</div><dl className="mt-3 grid grid-cols-2 gap-2 text-xs text-muted-foreground sm:grid-cols-4"><Metric label="Turniere" value={signed(item.tournamentCountDelta)} /><Metric label="Spiele" value={signed(item.gamesPlayedDelta)} /><Metric label="Punkte" value={signed(item.pointsCentsDelta / 100)} /><Metric label="Tordiff." value={signed(item.goalDifferenceDelta)} /></dl><p className="mt-3 flex items-center gap-2 text-xs text-muted-foreground"><History size={13} aria-hidden="true" />{item.administrator} · Revision {item.revision} · {formatDate(item.createdAt)}</p></article>
}

function PreviewCard({ preview, onConfirm }: { preview: ManualRankingCorrectionPreview; onConfirm: () => void }) {
  return <Card className="mt-6 border-2 border-primary/40"><CardHeader><CardTitle>Vorschau prüfen</CardTitle><CardDescription>{preview.player.displayName} · Kalenderjahr {preview.correction.effectiveYear} · Wirksam ab {preview.correction.effectiveDate} · {preview.correction.reason}{preview.superseded ? ` · ersetzt Buchung #${preview.superseded.id}` : ''}</CardDescription></CardHeader><CardContent><CorrectionSummary correction={preview.correction} /><p className="mt-4 text-sm text-muted-foreground">Auswirkung: Gesamtwertung und die Jahresrangliste {preview.correction.effectiveYear} werden aktualisiert. Andere Kalenderjahre bleiben unverändert.</p><div className="mt-4 grid gap-4 sm:grid-cols-2"><Aggregate label="Gesamtwertung – Vorher" value={preview.before} /><Aggregate label="Gesamtwertung – Nachher" value={preview.after} highlight /></div><Button className="mt-5" onClick={onConfirm}>Prüfen und bestätigen</Button></CardContent></Card>
}

function CorrectionImpact({ preview }: { preview: ManualRankingCorrectionPreview }) {
  return <div className="space-y-4"><div><p className="font-medium">{preview.player.displayName}</p><p className="text-sm text-muted-foreground">Kalenderjahr {preview.correction.effectiveYear} · Wirksam ab {preview.correction.effectiveDate}</p></div><CorrectionSummary correction={preview.correction} /><p className="text-sm text-muted-foreground">Auswirkung: Gesamtwertung sowie Jahresrangliste {preview.correction.effectiveYear}. Die Quelldaten und andere Jahresranglisten bleiben unverändert.</p><div className="grid gap-3 sm:grid-cols-2"><Aggregate label="Gesamtwertung – Vorher" value={preview.before} /><Aggregate label="Gesamtwertung – Nachher" value={preview.after} highlight /></div><p className="text-sm"><span className="font-medium">Grund:</span> {preview.correction.reason}</p></div>
}

function CorrectionSummary({ correction }: { correction: ManualRankingCorrection }) {
  return <dl className="grid grid-cols-2 gap-2 rounded-md border bg-muted/30 p-3 text-sm sm:grid-cols-4"><Metric label="Turniere (Delta)" value={signed(correction.tournamentCountDelta)} /><Metric label="Spiele (Delta)" value={signed(correction.gamesPlayedDelta)} /><Metric label="Punkte (Delta)" value={signed(correction.pointsCentsDelta / 100)} /><Metric label="Tordiff. (Delta)" value={signed(correction.goalDifferenceDelta)} /></dl>
}

function RevokeImpact({ player, correction }: { player: Player; correction: ManualRankingCorrection }) {
  const before = playerAggregate(player)
  const after = applyCorrection(before, correction, -1)
  return <div className="space-y-4"><div><p className="font-medium">{player.displayName}</p><p className="text-sm text-muted-foreground">Kalenderjahr {correction.effectiveYear} · Wirksam ab {correction.effectiveDate}</p></div><CorrectionSummary correction={{ ...correction, tournamentCountDelta: -correction.tournamentCountDelta, gamesPlayedDelta: -correction.gamesPlayedDelta, pointsCentsDelta: -correction.pointsCentsDelta, goalDifferenceDelta: -correction.goalDifferenceDelta }} /><p className="text-sm text-muted-foreground">Auswirkung: Die Gesamtwertung wird auf den Stand vor dieser Korrektur zurückgesetzt. Die Jahresrangliste {correction.effectiveYear} verliert genau diese Buchung; andere Jahre bleiben unverändert.</p><div className="grid gap-3 sm:grid-cols-2"><Aggregate label="Gesamtwertung – Vorher" value={before} /><Aggregate label="Gesamtwertung – Nachher" value={after} highlight /></div></div>
}

function Aggregate({ label, value, highlight = false }: { label: string; value: RankingAggregate; highlight?: boolean }) {
  return <article className={highlight ? 'rounded-md border-2 border-primary/40 bg-primary/5 p-4' : 'rounded-md border p-4'}><h3 className="font-medium">{label}</h3><dl className="mt-3 grid grid-cols-2 gap-2 text-sm"><Metric label="Turniere" value={value.tournamentCount} /><Metric label="Spiele" value={value.gamesPlayed ?? '—'} /><Metric label="Punkte" value={value.totalPointsCents === null ? '—' : formatDecimal(value.totalPointsCents / 100)} /><Metric label="Punkte/Spiel" value={value.pointsPerGameCents === null ? '—' : formatDecimal(value.pointsPerGameCents / 100)} /><Metric label="Tordifferenz" value={value.goalDifference ?? '—'} /></dl></article>
}

function Metric({ label, value }: { label: string; value: string | number }) { return <div><dt className="text-muted-foreground">{label}</dt><dd className="tabular font-medium text-foreground">{value}</dd></div> }
function signed(value: number) { return value > 0 ? `+${value}` : String(value) }
function berlinDate(value: Date) { const parts = new Intl.DateTimeFormat('en-CA', { timeZone: 'Europe/Berlin', year: 'numeric', month: '2-digit', day: '2-digit' }).formatToParts(value); const get = (type: string) => parts.find(part => part.type === type)?.value ?? ''; return `${get('year')}-${get('month')}-${get('day')}` }
function dateYear(value: string) { const match = /^(\d{4})-\d{2}-\d{2}$/.exec(value); return match ? Number(match[1]) : null }

function getCorrectionInput(values: { date: string; year: string; tournaments: string; games: string; points: string; goals: string; reason: string; replaceTarget: ManualRankingCorrection | null }): ManualCorrectionInput {
  return { effectiveDate: values.date, effectiveYear: Number(values.year), tournamentCountDelta: Number(values.tournaments), gamesPlayedDelta: Number(values.games), pointsCentsDelta: Math.round(Number(values.points.replace(',', '.')) * 100), goalDifferenceDelta: Number(values.goals), reason: values.reason.trim(), ...(values.replaceTarget ? { replaceCorrectionId: values.replaceTarget.id } : {}) }
}

function validateCorrectionInput(input: ManualCorrectionInput, raw: { date: string; year: string; tournaments: string; games: string; points: string; goals: string; reason: string }): FieldErrors {
  const errors: FieldErrors = {}
  const selectedYear = dateYear(raw.date)
  if (!selectedYear) errors.date = 'Bitte ein gültiges Datum eingeben.'
  if (!/^\d{4}$/.test(raw.year) || !Number.isInteger(input.effectiveYear) || input.effectiveYear < 1900 || input.effectiveYear > 2200) errors.year = 'Bitte ein gültiges Kalenderjahr eingeben.'
  else if (selectedYear !== null && selectedYear !== input.effectiveYear) errors.year = `Das Kalenderjahr muss zum Datum passen (${selectedYear}).`
  if (!raw.reason.trim()) errors.reason = 'Ein Grund ist erforderlich.'
  if (!isInteger(raw.tournaments)) errors.tournaments = 'Bitte eine ganze Zahl eingeben.'
  if (!isInteger(raw.games)) errors.games = 'Bitte eine ganze Zahl eingeben.'
  if (!isFiniteNumber(raw.points.replace(',', '.'))) errors.points = 'Bitte eine gültige Punktzahl eingeben.'
  if (!isInteger(raw.goals)) errors.goals = 'Bitte eine ganze Zahl eingeben.'
  return errors
}

function isInteger(value: string) { return value.trim() !== '' && Number.isInteger(Number(value)) }
function isFiniteNumber(value: string) { return value.trim() !== '' && Number.isFinite(Number(value)) }

function manualCorrectionError(value: unknown, fallback: string) {
  if (!(value instanceof ApiError)) return value instanceof Error ? value.message : fallback
  if (value.status === 409) return 'Die Daten wurden zwischenzeitlich geändert (Versionskonflikt). Bitte Historie neu laden und die Vorschau erneut erstellen.'
  if (value.status === 400 || value.status === 422) return value.message && !value.message.startsWith('HTTP ') ? `Eingaben abgelehnt: ${value.message}` : 'Die Eingaben wurden vom Server abgelehnt. Bitte prüfe Jahr, Datum und Deltas.'
  return value.message || fallback
}

function playerAggregate(player: Player): RankingAggregate {
  return { tournamentCount: player.tournamentCount, gamesPlayed: player.gamesPlayed, totalPointsCents: player.totalPointsCents, pointsPerGameCents: player.pointsPerGameCents, goalDifference: player.goalDifference }
}

function applyCorrection(value: RankingAggregate, correction: ManualRankingCorrection, multiplier: 1 | -1): RankingAggregate {
  const gamesPlayed = value.gamesPlayed === null ? null : value.gamesPlayed + multiplier * correction.gamesPlayedDelta
  const totalPointsCents = value.totalPointsCents === null ? null : value.totalPointsCents + multiplier * correction.pointsCentsDelta
  return { tournamentCount: value.tournamentCount + multiplier * correction.tournamentCountDelta, gamesPlayed, totalPointsCents, pointsPerGameCents: gamesPlayed === null || gamesPlayed <= 0 || totalPointsCents === null ? null : Math.round(totalPointsCents / gamesPlayed), goalDifference: value.goalDifference === null ? null : value.goalDifference + multiplier * correction.goalDifferenceDelta }
}
