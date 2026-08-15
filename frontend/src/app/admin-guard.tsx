import { Link, Outlet } from 'react-router-dom'
import { AlertCircle, LoaderCircle, LockKeyhole, RefreshCw } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { useAdminSession } from './providers'

export function AdminGuard() {
  const session = useAdminSession()
  if (session.status === 'loading') return <main className="flex min-h-dvh items-center justify-center"><div className="flex items-center gap-3 text-muted-foreground" role="status"><LoaderCircle className="animate-spin" aria-hidden="true" />Adminbereich wird geprüft …</div></main>
  if (session.status === 'unauthorized') return <main className="page-shell"><div className="mx-auto mt-16 max-w-lg"><div className="flex items-start gap-3 rounded-lg border border-amber-300 bg-amber-50 p-5 text-amber-950"><LockKeyhole className="mt-0.5 shrink-0" aria-hidden="true" /><div><h1 className="font-semibold">Admin-Anmeldung erforderlich</h1><p className="mt-2 text-sm">Der Browser erwartet HTTP Basic Auth. Bitte melden Sie sich über die Browser-Abfrage an und versuchen Sie es anschließend erneut.</p><Button className="mt-4" onClick={() => session.retry()}><RefreshCw aria-hidden="true" />Erneut prüfen</Button></div></div><Link className="mt-5 inline-flex min-h-11 items-center text-sm text-primary underline" to="/">Zur öffentlichen Rangliste</Link></div></main>
  if (session.status === 'error') return <main className="page-shell"><div className="mx-auto mt-16 max-w-lg"><div className="flex items-start gap-3 rounded-lg border border-red-300 bg-red-50 p-5 text-red-950"><AlertCircle className="mt-0.5 shrink-0" aria-hidden="true" /><div><h1 className="font-semibold">Adminbereich nicht erreichbar</h1><p className="mt-2 text-sm">Bitte prüfen Sie die Verbindung und versuchen Sie es erneut.</p><Button variant="outline" className="mt-4" onClick={() => session.retry()}><RefreshCw aria-hidden="true" />Erneut laden</Button></div></div></div></main>
  return <Outlet />
}
