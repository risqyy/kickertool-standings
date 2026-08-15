type ClassValue = string | false | null | undefined | Record<string, boolean>

export function cn(...values: ClassValue[]) {
  return values.flatMap(value => {
    if (!value) return []
    if (typeof value === 'string') return [value]
    return Object.entries(value).filter(([, enabled]) => enabled).map(([name]) => name)
  }).join(' ')
}

export function formatDecimal(value: string | number | null | undefined) {
  if (value === null || value === undefined || value === '') return '—'
  return typeof value === 'number' ? value.toFixed(2) : Number(value).toFixed(2)
}

export function formatDate(value: string | null | undefined) {
  if (!value) return '—'
  return new Intl.DateTimeFormat('de-DE', { dateStyle: 'medium' }).format(new Date(value))
}
