import { type SelectHTMLAttributes } from 'react'
import { cn } from '@/lib/utils'
export function Select({ className, ...props }: SelectHTMLAttributes<HTMLSelectElement>) { return <select className={cn('min-h-11 rounded-md border border-input bg-background px-3 py-2 text-sm', className)} {...props} /> }
