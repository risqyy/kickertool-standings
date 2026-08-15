import { lazy, Suspense } from 'react'
import { BrowserRouter, Route, Routes } from 'react-router-dom'
import { LoaderCircle } from 'lucide-react'
import { PublicLayout } from '@/layouts/public-layout'
import { RankingPage } from '@/features/ranking/ranking-page'

const AdminRoutes = lazy(() => import('@/features/admin/admin-routes'))

function AdminLoading() {
  return <main className="flex min-h-dvh items-center justify-center"><div className="flex items-center gap-3 text-muted-foreground" role="status"><LoaderCircle className="animate-spin" aria-hidden="true" />Adminbereich wird geladen …</div></main>
}

export function AppRouter() {
  return <BrowserRouter><Routes><Route element={<PublicLayout />}><Route path="/" element={<RankingPage />} /><Route path="/standings" element={<RankingPage />} /></Route><Route path="/admin/*" element={<Suspense fallback={<AdminLoading />}><AdminRoutes /></Suspense>} /><Route path="*" element={<RankingPage />} /></Routes></BrowserRouter>
}
