import { type InputHTMLAttributes } from 'react'
import { cn } from '@/lib/utils'

export function Switch({ className, ...props }: InputHTMLAttributes<HTMLInputElement>) {
  return <label className="relative inline-flex min-h-11 min-w-[3.5rem] cursor-pointer items-center"><input type="checkbox" className="peer sr-only" {...props} /><span className={cn('h-6 w-11 rounded-full bg-muted transition-colors peer-checked:bg-primary peer-focus-visible:ring-2 peer-focus-visible:ring-ring', className)} /><span className="absolute left-1 h-4 w-4 rounded-full bg-white transition-transform peer-checked:translate-x-5" /></label>
}
