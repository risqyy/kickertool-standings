import { type HTMLAttributes } from 'react'
import { cn } from '@/lib/utils'

export function Alert({ className, variant = 'default', ...props }: HTMLAttributes<HTMLDivElement> & { variant?: 'default' | 'destructive' | 'warning' | 'success' }) {
  return <div role={variant === 'destructive' ? 'alert' : 'status'} className={cn('rounded-lg border p-4 text-sm', {
    'bg-card text-card-foreground': variant === 'default',
    'border-red-300 bg-red-50 text-red-950': variant === 'destructive',
    'border-amber-300 bg-amber-50 text-amber-950': variant === 'warning',
    'border-emerald-300 bg-emerald-50 text-emerald-950': variant === 'success'
  }, className)} {...props} />
}
