import { readFile } from 'node:fs/promises'

const contract = await readFile(new URL('../backend/api/openapi.yaml', import.meta.url), 'utf8')
const client = await readFile(new URL('../frontend/src/api/client.ts', import.meta.url), 'utf8')

for (const path of ['/api/v1/public/rankings', '/api/v1/admin/session', '/api/v1/admin/tournaments', '/api/v1/admin/players/merge/preview', '/api/v1/admin/players/merge/confirm']) {
  if (!contract.includes(path)) throw new Error(`contract missing ${path}`)
}
if (!client.includes('/api/v1/public/rankings') || !client.includes('/api/v1/admin/')) {
  throw new Error('frontend API client is not using the versioned API paths')
}
console.log('API contract/client paths are aligned')
