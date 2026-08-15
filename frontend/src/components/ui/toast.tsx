import { type ReactNode } from 'react'
import { cn } from '@/lib/utils'

export function Toast({ title, children, variant = 'default', className }: { title?: string; children?: ReactNode; variant?: 'default' | 'success' | 'destructive'; className?: string }) {
  return <div role="status" className={cn('rounded-md border bg-card p-4 text-sm shadow-md', { 'border-green-600/40': variant === 'success', 'border-destructive/50 text-destructive': variant === 'destructive' }, className)}>{title && <p className="font-medium">{title}</p>}{children && <div className={title ? 'mt-1 text-muted-foreground' : ''}>{children}</div>}</div>
}
