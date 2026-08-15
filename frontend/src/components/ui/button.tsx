import { forwardRef, type ButtonHTMLAttributes } from 'react'
import { cn } from '@/lib/utils'

type Variant = 'default' | 'secondary' | 'outline' | 'destructive' | 'ghost'

export const Button = forwardRef<HTMLButtonElement, ButtonHTMLAttributes<HTMLButtonElement> & { variant?: Variant; size?: 'default' | 'sm' }>(
  ({ className, variant = 'default', size = 'default', ...props }, ref) => (
    <button ref={ref} className={cn('inline-flex min-h-11 items-center justify-center gap-2 rounded-md px-4 text-sm font-medium transition-colors disabled:pointer-events-none disabled:opacity-50', {
      'bg-primary text-primary-foreground hover:bg-primary/90': variant === 'default',
      'bg-secondary text-secondary-foreground hover:bg-secondary/80': variant === 'secondary',
      'border border-input bg-background hover:bg-accent hover:text-accent-foreground': variant === 'outline',
      'bg-destructive text-destructive-foreground hover:bg-destructive/90': variant === 'destructive',
      'hover:bg-accent hover:text-accent-foreground': variant === 'ghost',
      'min-h-9 px-3 text-xs': size === 'sm'
    }, className)} {...props} />
  )
)
Button.displayName = 'Button'
