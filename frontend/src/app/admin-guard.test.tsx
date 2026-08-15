import { render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import { AdminGuard } from './admin-guard'
import { AdminSessionProvider } from './providers'

describe('AdminGuard', () => {
  it('does not flash protected content and explains a 401', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('unauthorized', { status: 401 })))
    render(<MemoryRouter initialEntries={['/admin']}><AdminSessionProvider><Routes><Route element={<AdminGuard />}><Route path="/admin" element={<div>geheim</div>} /></Route></Routes></AdminSessionProvider></MemoryRouter>)
    expect(screen.getByRole('status')).toHaveTextContent('Adminbereich wird geprüft')
    expect(await screen.findByText('Admin-Anmeldung erforderlich')).toBeInTheDocument()
    expect(screen.queryByText('geheim')).not.toBeInTheDocument()
  })
})
