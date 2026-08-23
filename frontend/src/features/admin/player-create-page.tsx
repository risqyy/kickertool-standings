import { useRef, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { AlertCircle, CheckCircle2, ShieldAlert, UserPlus } from 'lucide-react'
import { ApiError, createPlayer } from '@/api/client'
import type { Player } from '@/api/types'
import { useAdminSession } from '@/app/providers'
import { Alert } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'

export const MAX_PLAYER_DISPLAY_NAME_LENGTH = 120
export const MIN_PLAYER_DISPLAY_NAME_LENGTH = 2

export type PlayerNameFieldErrors = {
  displayName?: string
  confirmDisplayName?: string
}

/** The player key is name-based, so reject control and punctuation-only input. */
export function validatePlayerDisplayName(displayName: string): PlayerNameFieldErrors {
  const value = displayName.trim()
  if (!value) return { displayName: 'Bitte einen Anzeigenamen eingeben.' }
  if ([...value].length < MIN_PLAYER_DISPLAY_NAME_LENGTH) return { displayName: `Der Anzeigename muss mindestens ${MIN_PLAYER_DISPLAY_NAME_LENGTH} Zeichen lang sein.` }
  if (value.length > MAX_PLAYER_DISPLAY_NAME_LENGTH) return { displayName: `Der Anzeigename darf höchstens ${MAX_PLAYER_DISPLAY_NAME_LENGTH} Zeichen lang sein.` }
  if (!/^[\p{L}\p{M}\p{N}][\p{L}\p{M}\p{N} .,'’\-_/()&+]*$/u.test(value)) {
    return { displayName: 'Bitte nur Buchstaben, Zahlen, Leerzeichen und übliche Namenszeichen verwenden.' }
  }
  return {}
}

export function PlayerCreatePage() {
  const session = useAdminSession()
  const [searchParams] = useSearchParams()
  const [displayName, setDisplayName] = useState(() => searchParams.get('name') ?? '')
  const [confirmDisplayName, setConfirmDisplayName] = useState('')
  const [fieldErrors, setFieldErrors] = useState<PlayerNameFieldErrors>({})
  const [error, setError] = useState('')
  const [conflictPlayer, setConflictPlayer] = useState<Player | null>(null)
  const [createdPlayer, setCreatedPlayer] = useState<Player | null>(null)
  const [busy, setBusy] = useState(false)
  const displayNameRef = useRef<HTMLInputElement>(null)
  const confirmDisplayNameRef = useRef<HTMLInputElement>(null)

  const submit = async () => {
    const nameErrors = validatePlayerDisplayName(displayName)
    const nextErrors: PlayerNameFieldErrors = { ...nameErrors }
    if (!nameErrors.displayName && confirmDisplayName.trim() !== displayName.trim()) {
      nextErrors.confirmDisplayName = 'Bitte den Anzeigenamen exakt wiederholen.'
    }
    setFieldErrors(nextErrors)
    setError('')
    setConflictPlayer(null)
    if (Object.keys(nextErrors).length > 0) {
      if (nextErrors.displayName) displayNameRef.current?.focus()
      else confirmDisplayNameRef.current?.focus()
      return
    }

    setBusy(true)
    try {
      const value = await createPlayer(session.csrf, displayName.trim())
      setCreatedPlayer(value.player)
      setDisplayName(value.player.displayName)
      setConfirmDisplayName(value.player.displayName)
      setFieldErrors({})
    } catch (value) {
      if (value instanceof ApiError && value.status === 409) {
        const existing = conflictPlayerFrom(value.payload)
        setConflictPlayer(existing)
        setError(existing ? `Ein aktiver Spieler mit diesem Namen existiert bereits.` : 'Dieser Anzeigename ist bereits vergeben. Bitte prüfe den aktiven Spieler in der Spielersuche.')
      } else {
        setError(value instanceof Error ? value.message : 'Spieler konnte nicht angelegt werden. Bitte versuche es erneut.')
      }
    } finally {
      setBusy(false)
    }
  }

  const clearError = (field: keyof PlayerNameFieldErrors) => {
    setFieldErrors(current => {
      if (!current[field]) return current
      const next = { ...current }
      delete next[field]
      return next
    })
    setError('')
    setConflictPlayer(null)
    setCreatedPlayer(null)
  }

  const playerQuery = (player: Player) => `?playerId=${encodeURIComponent(String(player.id))}&name=${encodeURIComponent(player.displayName)}`

  return <div>
    <div className="mb-6">
      <p className="text-sm font-medium text-primary">Datenpflege</p>
      <h1 className="mt-1 text-3xl font-bold tracking-tight">Spieler anlegen</h1>
      <p className="mt-2 max-w-2xl text-muted-foreground">Lege einen aktiven Spieler für manuelle Ranking-Korrekturen oder eine spätere Zusammenführung an. Rankingwerte werden erst in den jeweiligen Folgeprozessen erfasst.</p>
    </div>

    {error && <Alert variant="destructive" className="mb-4"><div className="flex items-start gap-3"><ShieldAlert className="mt-0.5 shrink-0" aria-hidden="true" /><p>{error}</p></div></Alert>}
    {conflictPlayer && <Alert variant="warning" className="mb-4"><div className="flex items-start gap-3"><AlertCircle className="mt-0.5 shrink-0" aria-hidden="true" /><div><p className="font-medium">Aktiver Spieler bereits vorhanden</p><p className="mt-1">Der bestehende Spieler <strong>{conflictPlayer.displayName}</strong> ist der aktive Datensatz für diesen Namen.</p><div className="mt-3 flex flex-wrap gap-2"><Link className="inline-flex min-h-11 items-center rounded-md border border-input bg-background px-4 text-sm font-medium hover:bg-accent" to={`/admin/players/corrections${playerQuery(conflictPlayer)}`}>Ranking korrigieren</Link><Link className="inline-flex min-h-11 items-center rounded-md border border-input bg-background px-4 text-sm font-medium hover:bg-accent" to={`/admin/players/merge${playerQuery(conflictPlayer)}`}>Zur Zusammenführung</Link></div></div></div></Alert>}
    {createdPlayer && <Alert variant="success" className="mb-4"><div className="flex items-start gap-3"><CheckCircle2 className="mt-0.5 shrink-0" aria-hidden="true" /><div><p className="font-medium">Spieler „{createdPlayer.displayName}“ wurde angelegt.</p><p className="mt-1">Der Spieler ist jetzt in der Suche für Korrekturen und Zusammenführungen verfügbar.</p><div className="mt-3 flex flex-wrap gap-2"><Link className="inline-flex min-h-11 items-center gap-2 rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground hover:bg-primary/90" to={`/admin/players/corrections${playerQuery(createdPlayer)}`}><UserPlus size={16} aria-hidden="true" />Ranking korrigieren</Link><Link className="inline-flex min-h-11 items-center rounded-md border border-input bg-background px-4 text-sm font-medium hover:bg-accent" to={`/admin/players/merge${playerQuery(createdPlayer)}`}>Zur Zusammenführung</Link></div></div></div></Alert>}

    <Card className="max-w-2xl">
      <CardHeader><CardTitle>Neuen aktiven Spieler erfassen</CardTitle><CardDescription>Der Anzeigename wird als kanonischer Name verwendet. Bestätige ihn deshalb noch einmal exakt.</CardDescription></CardHeader>
      <CardContent>
        <form className="space-y-5" onSubmit={event => { event.preventDefault(); void submit() }} noValidate>
          <div>
            <label htmlFor="player-display-name" className="text-sm font-medium">Anzeigename <span aria-hidden="true">*</span></label>
            <Input ref={displayNameRef} id="player-display-name" className="mt-2" value={displayName} onChange={event => { setDisplayName(event.target.value); clearError('displayName') }} aria-invalid={Boolean(fieldErrors.displayName)} aria-describedby={`player-display-name-help${fieldErrors.displayName ? ' player-display-name-error' : ''}`} autoComplete="off" maxLength={MAX_PLAYER_DISPLAY_NAME_LENGTH} autoFocus />
            <p id="player-display-name-help" className="mt-2 text-sm text-muted-foreground">{MIN_PLAYER_DISPLAY_NAME_LENGTH}–{MAX_PLAYER_DISPLAY_NAME_LENGTH} Zeichen, z. B. „Alex Müller“.</p>
            {fieldErrors.displayName && <p id="player-display-name-error" className="mt-1 text-sm text-red-700" role="alert">{fieldErrors.displayName}</p>}
          </div>
          <div>
            <label htmlFor="player-display-name-confirm" className="text-sm font-medium">Anzeigename bestätigen <span aria-hidden="true">*</span></label>
            <Input ref={confirmDisplayNameRef} id="player-display-name-confirm" className="mt-2" value={confirmDisplayName} onChange={event => { setConfirmDisplayName(event.target.value); clearError('confirmDisplayName') }} aria-invalid={Boolean(fieldErrors.confirmDisplayName)} aria-describedby={fieldErrors.confirmDisplayName ? 'player-display-name-confirm-error' : undefined} autoComplete="off" />
            {fieldErrors.confirmDisplayName && <p id="player-display-name-confirm-error" className="mt-1 text-sm text-red-700" role="alert">{fieldErrors.confirmDisplayName}</p>}
          </div>
          <Button type="submit" disabled={busy}>{busy ? 'Spieler wird angelegt …' : 'Spieler anlegen'}</Button>
          {busy && <p className="text-sm text-muted-foreground" role="status" aria-live="polite">Speichere den Spieler …</p>}
        </form>
      </CardContent>
    </Card>
  </div>
}

function conflictPlayerFrom(payload: unknown): Player | null {
  if (!payload || typeof payload !== 'object') return null
  const value = payload as Record<string, unknown>
  const nested = value.conflict && typeof value.conflict === 'object' ? value.conflict as Record<string, unknown> : null
  const candidate = value.player ?? value.existingPlayer ?? value.activePlayer ?? nested?.player ?? nested?.existingPlayer ?? nested?.activePlayer
  if (!candidate || typeof candidate !== 'object') return null
  const player = candidate as Partial<Player>
  return typeof player.id === 'number' && typeof player.displayName === 'string' ? player as Player : null
}
