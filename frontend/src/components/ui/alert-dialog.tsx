import { useEffect, useId, useRef, type ReactNode } from 'react'
import { AlertTriangle, X } from 'lucide-react'
import { Button } from './button'

export function AlertDialog({ open, title, description, confirmLabel, onConfirm, onCancel, destructive = false, children }: { open: boolean; title: string; description: string; confirmLabel: string; onConfirm: () => void; onCancel: () => void; destructive?: boolean; children?: ReactNode }) {
  const cancelRef = useRef<HTMLButtonElement>(null)
  const dialogRef = useRef<HTMLDivElement>(null)
  const onCancelRef = useRef(onCancel)
  const titleId = useId()
  const descriptionId = useId()
  useEffect(() => { onCancelRef.current = onCancel }, [onCancel])
  useEffect(() => {
    if (!open) return
    const previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
    cancelRef.current?.focus()
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') { event.preventDefault(); onCancelRef.current(); return }
      if (event.key !== 'Tab' || !dialogRef.current) return
      const focusable = Array.from(dialogRef.current.querySelectorAll<HTMLElement>('button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), a[href], [tabindex]:not([tabindex="-1"])'))
      if (focusable.length === 0) { event.preventDefault(); return }
      const first = focusable[0]
      const last = focusable[focusable.length - 1]
      if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus() }
      else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus() }
    }
    document.addEventListener('keydown', handleKeyDown)
    return () => {
      document.removeEventListener('keydown', handleKeyDown)
      if (previousFocus?.isConnected) previousFocus.focus()
    }
  }, [open])
  if (!open) return null
  return <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-950/40 p-4" role="presentation"><div ref={dialogRef} className="w-full max-w-md rounded-lg border bg-card p-6 shadow-xl" role="alertdialog" aria-modal="true" aria-labelledby={titleId} aria-describedby={descriptionId}><div className="flex items-start justify-between gap-4"><div className="flex items-start gap-3"><AlertTriangle className={destructive ? 'text-destructive' : 'text-amber-600'} aria-hidden="true" /><div><h2 id={titleId} className="font-semibold">{title}</h2><p id={descriptionId} className="mt-2 text-sm text-muted-foreground">{description}</p></div></div><button type="button" className="inline-flex min-h-11 min-w-11 items-center justify-center rounded-md hover:bg-muted" aria-label="Dialog schließen" onClick={onCancel}><X aria-hidden="true" /></button></div>{children && <div className="mt-4">{children}</div>}<div className="mt-6 flex justify-end gap-3"><Button ref={cancelRef} variant="outline" onClick={onCancel}>Abbrechen</Button><Button variant={destructive ? 'destructive' : 'default'} onClick={onConfirm}>{confirmLabel}</Button></div></div></div>
}
