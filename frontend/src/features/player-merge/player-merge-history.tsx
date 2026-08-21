import { useEffect, useState } from 'react'
import { CheckCircle2, History, RotateCcw, ShieldAlert } from 'lucide-react'
import { confirmPlayerMergeUndo, listPlayerMerges, previewPlayerMergeUndo } from '@/api/client'
import type { MergeAggregate, PlayerMergeAudit, PlayerMergeUndoPreview } from '@/api/types'
import { Alert } from '@/components/ui/alert'
import { AlertDialog } from '@/components/ui/alert-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { formatDate, formatDecimal } from '@/lib/utils'

export function PlayerMergeHistory({ csrf, refreshKey }: { csrf: string; refreshKey: number }) {
  const [items, setItems] = useState<PlayerMergeAudit[]>([])
  const [loading, setLoading] = useState(true)
  const [reloadVersion, setReloadVersion] = useState(0)
  const [error, setError] = useState('')
  const [success, setSuccess] = useState('')
  const [preview, setPreview] = useState<PlayerMergeUndoPreview | null>(null)
  const [token, setToken] = useState('')
  const [reason, setReason] = useState('')
  const [checked, setChecked] = useState(false)
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    let active = true
    setLoading(true)
    listPlayerMerges()
      .then(value => { if (active) { setItems(value); setError('') } })
      .catch(value => { if (active) setError(value instanceof Error ? value.message : 'Zusammenführungsverlauf konnte nicht geladen werden.') })
      .finally(() => { if (active) setLoading(false) })
    return () => { active = false }
  }, [refreshKey, reloadVersion])

  const openPreview = async (merge: PlayerMergeAudit) => {
    setError('')
    setSuccess('')
    try {
      const value = await previewPlayerMergeUndo(csrf, merge.id)
      setPreview(value.preview)
      setToken(value.token)
      setReason('')
      setChecked(false)
    } catch (value) {
      setError(value instanceof Error ? value.message : 'Die Wiederherstellung konnte nicht geprüft werden.')
    }
  }

  const confirmUndo = async () => {
    if (!preview || !token || !checked || submitting) return
    setSubmitting(true)
    setError('')
    try {
      const result = await confirmPlayerMergeUndo(csrf, preview.merge.id, token, reason.trim())
      setSuccess(`${result.merge.sourceDisplayName} und ${result.merge.targetDisplayName} wurden wieder als getrennte Spieler hergestellt.`)
      setPreview(null)
      setReloadVersion(value => value + 1)
    } catch (value) {
      setError(value instanceof Error ? value.message : 'Zusammenführung konnte nicht rückgängig gemacht werden.')
    } finally {
      setSubmitting(false)
    }
  }

  return <Card className="mt-10">
    <CardHeader>
      <div className="flex items-start gap-3">
        <History className="mt-0.5 text-primary" aria-hidden="true" />
        <div>
          <CardTitle>Zusammenführungsverlauf</CardTitle>
          <CardDescription className="mt-1">Neuere Zusammenführungen lassen sich exakt auf ihren vorherigen Stand zurücksetzen, solange die beteiligten Daten danach nicht verändert wurden.</CardDescription>
        </div>
      </div>
    </CardHeader>
    <CardContent>
      {error && <Alert variant="destructive" className="mb-4"><div className="flex items-start gap-3"><ShieldAlert className="shrink-0" aria-hidden="true" /><span>{error}</span></div></Alert>}
      {success && <Alert variant="success" className="mb-4"><div className="flex items-start gap-3"><CheckCircle2 className="shrink-0" aria-hidden="true" /><span>{success}</span></div></Alert>}
      {loading ? <div className="space-y-3" aria-label="Zusammenführungsverlauf wird geladen"><Skeleton className="h-28" /><Skeleton className="h-28" /></div> : items.length === 0 ? <p className="text-sm text-muted-foreground">Noch keine Spieler-Zusammenführungen vorhanden.</p> : <div className="space-y-3">
        {items.map(merge => <article key={merge.id} className="rounded-lg border p-4">
          <div className="flex flex-col justify-between gap-4 sm:flex-row sm:items-start">
            <div>
              <div className="flex flex-wrap items-center gap-2">
                <h3 className="font-semibold">{merge.sourceDisplayName} <span className="font-normal text-muted-foreground">→</span> {merge.targetDisplayName}</h3>
                <MergeUndoBadge merge={merge} />
              </div>
              <p className="mt-2 text-sm text-muted-foreground">Zusammengeführt am {formatDate(merge.mergedAt)}{merge.actor ? ` von ${merge.actor}` : ''}</p>
              {merge.reason && <p className="mt-1 text-sm">Grund: {merge.reason}</p>}
              {merge.undoneAt && <p className="mt-1 text-sm">Rückgängig gemacht am {formatDate(merge.undoneAt)}{merge.undoneBy ? ` von ${merge.undoneBy}` : ''}{merge.undoReason ? ` – ${merge.undoReason}` : ''}</p>}
              {!merge.undoAvailable && !merge.undoneAt && merge.undoUnavailableReason && <p className="mt-2 text-sm text-muted-foreground">{merge.undoUnavailableReason}</p>}
            </div>
            {merge.undoAvailable && <Button variant="outline" className="shrink-0" onClick={() => openPreview(merge)}><RotateCcw aria-hidden="true" />Rückgängigmachen prüfen</Button>}
          </div>
        </article>)}
      </div>}
    </CardContent>
    <AlertDialog
      open={preview !== null}
      title="Zusammenführung rückgängig machen?"
      description={preview ? `${preview.merge.sourceDisplayName} und ${preview.merge.targetDisplayName} werden mit ihren Daten vor der Zusammenführung wiederhergestellt.` : ''}
      confirmLabel={submitting ? 'Wird wiederhergestellt…' : 'Rückgängig machen'}
      destructive
      confirmDisabled={!checked || submitting}
      onCancel={() => { if (!submitting) setPreview(null) }}
      onConfirm={confirmUndo}
    >
      {preview && <div>
        <div className="grid gap-3 sm:grid-cols-2">
          <UndoAggregate label={preview.merge.sourceDisplayName} value={preview.sourceBefore} />
          <UndoAggregate label={preview.merge.targetDisplayName} value={preview.targetBefore} />
        </div>
        <label className="mt-4 block text-sm font-medium" htmlFor="merge-undo-reason">Grund für das Rückgängigmachen (optional)</label>
        <Input id="merge-undo-reason" className="mt-2" value={reason} onChange={event => setReason(event.target.value)} maxLength={500} />
        <label className="mt-4 flex min-h-11 items-center gap-3 text-sm">
          <Checkbox checked={checked} onChange={event => setChecked(event.target.checked)} />
          Ich habe geprüft, dass diese Zusammenführung rückgängig gemacht werden soll.
        </label>
      </div>}
    </AlertDialog>
  </Card>
}

function MergeUndoBadge({ merge }: { merge: PlayerMergeAudit }) {
  if (merge.undoneAt) return <Badge variant="success">Rückgängig gemacht</Badge>
  if (merge.undoAvailable) return <Badge variant="warning">Kann rückgängig gemacht werden</Badge>
  return <Badge variant="secondary">Nicht rückgängig machbar</Badge>
}

function UndoAggregate({ label, value }: { label: string; value: MergeAggregate }) {
  return <section className="rounded-md border bg-muted/30 p-3">
    <h3 className="font-medium">{label} vorher</h3>
    <dl className="mt-2 grid grid-cols-2 gap-x-3 gap-y-1 text-xs">
      <dt className="text-muted-foreground">Turniere</dt><dd className="text-right tabular-nums">{value.tournamentCount}</dd>
      <dt className="text-muted-foreground">Spiele</dt><dd className="text-right tabular-nums">{value.gamesPlayed ?? '—'}</dd>
      <dt className="text-muted-foreground">Punkte</dt><dd className="text-right tabular-nums">{cents(value.totalPointsCents)}</dd>
      <dt className="text-muted-foreground">Tordifferenz</dt><dd className="text-right tabular-nums">{value.goalDifference ?? '—'}</dd>
    </dl>
  </section>
}

function cents(value: number | null | undefined) {
  return value === null || value === undefined ? '—' : formatDecimal(value / 100)
}
