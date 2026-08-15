import { forwardRef, type ButtonHTMLAttributes, type HTMLAttributes, type InputHTMLAttributes } from 'react'
import { cn } from '@/lib/utils'

export function Command({ className, ...props }: HTMLAttributes<HTMLDivElement>) { return <div role="listbox" className={cn('flex w-full flex-col overflow-hidden rounded-md bg-popover text-popover-foreground', className)} {...props} /> }
export const CommandInput = forwardRef<HTMLInputElement, InputHTMLAttributes<HTMLInputElement>>(({ className, ...props }, ref) => <input ref={ref} className={cn('flex h-11 w-full border-b bg-transparent px-3 text-sm outline-none placeholder:text-muted-foreground', className)} {...props} />)
CommandInput.displayName = 'CommandInput'
export function CommandList({ className, ...props }: HTMLAttributes<HTMLDivElement>) { return <div className={cn('max-h-64 overflow-y-auto p-1', className)} {...props} /> }
export const CommandItem = forwardRef<HTMLButtonElement, ButtonHTMLAttributes<HTMLButtonElement>>(({ className, ...props }, ref) => <button ref={ref} role="option" className={cn('flex min-h-11 w-full items-center rounded-sm px-3 text-left text-sm hover:bg-accent hover:text-accent-foreground', className)} {...props} />)
CommandItem.displayName = 'CommandItem'
