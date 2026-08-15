import { type HTMLAttributes, type ReactNode } from 'react'
import { cn } from '@/lib/utils'

export function Popover({ children, className }: { children: ReactNode; className?: string }) { return <div className={cn('relative', className)}>{children}</div> }
export function PopoverTrigger({ children, className }: { children: ReactNode; className?: string }) { return <div className={cn('w-full', className)}>{children}</div> }
export function PopoverContent({ children, className, ...props }: HTMLAttributes<HTMLDivElement>) { return <div role="dialog" className={cn('absolute left-0 right-0 top-full z-30 mt-2 rounded-md border bg-popover p-1 text-popover-foreground shadow-md', className)} {...props}>{children}</div> }
