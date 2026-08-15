import { forwardRef, type InputHTMLAttributes } from 'react'
import { cn } from '@/lib/utils'

export const Checkbox = forwardRef<HTMLInputElement, InputHTMLAttributes<HTMLInputElement>>(({ className, ...props }, ref) => (
  <input ref={ref} type="checkbox" className={cn('h-5 w-5 rounded border-input text-primary accent-primary', className)} {...props} />
))
Checkbox.displayName = 'Checkbox'
