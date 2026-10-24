import type { SearchResult } from '../../lib/api'

export function searchResultPath(result: SearchResult): string {
  if (result.kind === 'issue') return `/issues/${encodeURIComponent(result.id)}`
  if (result.kind === 'request') return `/requests/${encodeURIComponent(result.id)}`
  if (result.kind === 'prompt') return `/prompts/${encodeURIComponent(result.id)}`
  if (result.kind === 'memory') return `/search?q=${encodeURIComponent(result.basename)}`
  return `/wiki/${result.path.replace(/\.md$/i, '').split('/').map(encodeURIComponent).join('/')}`
}