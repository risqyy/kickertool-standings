import { createContext, useContext, useEffect, useState, type ReactNode } from 'react'
import { getAdminSession } from '@/api/client'

interface AdminSessionValue { status: 'loading' | 'authenticated' | 'unauthorized' | 'error'; csrf: string; retry: () => void }
const AdminSessionContext = createContext<AdminSessionValue | null>(null)

export function AdminSessionProvider({ children }: { children: ReactNode }) {
  const [retryKey, setRetryKey] = useState(0)
  const [state, setState] = useState<AdminSessionValue['status']>('loading')
  const [csrf, setCsrf] = useState('')
  useEffect(() => {
    let active = true
    setState('loading')
    getAdminSession().then(value => {
      if (active) { setCsrf(value.csrf_token); setState('authenticated') }
    }).catch(error => {
      if (active) setState(error?.status === 401 ? 'unauthorized' : 'error')
    })
    return () => { active = false }
  }, [retryKey])
  return <AdminSessionContext.Provider value={{ status: state, csrf, retry: () => setRetryKey(value => value + 1) }}>{children}</AdminSessionContext.Provider>
}

export function useAdminSession() {
  const value = useContext(AdminSessionContext)
  if (!value) throw new Error('useAdminSession must be used inside AdminSessionProvider')
  return value
}
