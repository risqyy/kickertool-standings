import { Routes, Route } from 'react-router-dom'
import { AdminGuard } from '@/app/admin-guard'
import { AdminSessionProvider } from '@/app/providers'
import { AdminLayout } from './admin-layout'
import { DashboardPage } from './dashboard-page'
import { TournamentManagementPage } from './tournament-management-page'
import { PlayerMergePage } from '../player-merge/player-merge-page'
import { ManualCorrectionsPage } from './manual-corrections-page'

export default function AdminRoutes() {
  return <AdminSessionProvider><Routes><Route element={<AdminGuard />}><Route element={<AdminLayout />}><Route index element={<DashboardPage />} /><Route path="tournaments" element={<TournamentManagementPage />} /><Route path="players/merge" element={<PlayerMergePage />} /><Route path="players/corrections" element={<ManualCorrectionsPage />} /></Route></Route></Routes></AdminSessionProvider>
}
