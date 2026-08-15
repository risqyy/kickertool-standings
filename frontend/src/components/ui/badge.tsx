import { type HTMLAttributes } from 'react'
import { cn } from '@/lib/utils'

export function Badge({ className, variant = 'default', ...props }: HTMLAttributes<HTMLSpanElement> & { variant?: 'default' | 'secondary' | 'outline' | 'destructive' | 'success' | 'warning' }) {
  return <span className={cn('inline-flex items-center rounded-full border px-2.5 py-1 text-xs font-medium', {
    'border-transparent bg-primary text-primary-foreground': variant === 'default',
    'border-transparent bg-secondary text-secondary-foreground': variant === 'secondary',
    'bg-background text-foreground': variant === 'outline',
    'border-transparent bg-destructive text-destructive-foreground': variant === 'destructive',
    'border-transparent bg-emerald-100 text-emerald-800': variant === 'success',
    'border-amber-300 bg-amber-50 text-amber-900': variant === 'warning'
  }, className)} {...props} />
}
