import { type ReactNode } from 'react'
import { cn } from '@/lib/utils'

export function Tooltip({ content, children, className }: { content: string; children: ReactNode; className?: string }) {
  return <span className={cn('group relative inline-flex focus-within:z-10', className)}><span tabIndex={0} aria-describedby="ui-tooltip" className="inline-flex">{children}</span><span id="ui-tooltip" role="tooltip" className="pointer-events-none absolute bottom-full left-1/2 z-20 mb-2 hidden -translate-x-1/2 whitespace-nowrap rounded-md bg-foreground px-2 py-1 text-xs text-background shadow-md group-hover:block group-focus-within:block">{content}</span></span>
}
