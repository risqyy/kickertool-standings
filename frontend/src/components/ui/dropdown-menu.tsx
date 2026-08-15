import { type ButtonHTMLAttributes, type HTMLAttributes, type ReactNode } from 'react'
import { cn } from '@/lib/utils'

export function DropdownMenu({ children }: { children: ReactNode }) { return <div className="relative inline-flex">{children}</div> }
export function DropdownMenuTrigger({ children, className, ...props }: ButtonHTMLAttributes<HTMLButtonElement>) { return <button className={cn('inline-flex min-h-11 items-center rounded-md px-3 text-sm hover:bg-accent', className)} {...props}>{children}</button> }
export function DropdownMenuContent({ className, ...props }: HTMLAttributes<HTMLDivElement>) { return <div role="menu" className={cn('absolute right-0 top-full z-30 mt-2 min-w-44 rounded-md border bg-popover p-1 shadow-md', className)} {...props} /> }
export function DropdownMenuItem({ className, ...props }: ButtonHTMLAttributes<HTMLButtonElement>) { return <button role="menuitem" className={cn('flex min-h-11 w-full items-center rounded-sm px-3 text-left text-sm hover:bg-accent', className)} {...props} /> }
