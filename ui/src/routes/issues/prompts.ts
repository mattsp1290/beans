import type { Backlink, PromptSummary } from '../../lib/api'

export function promptHref(id: string): string {
  return `/prompts/${encodeURIComponent(id)}`
}

export function withoutPromptBacklinkDuplicates(backlinks: Backlink[], prompts: PromptSummary[]): Backlink[] {
  const represented = new Set(prompts.map((prompt) => prompt.id))
  return backlinks.filter((link) => link.note_kind !== 'prompt' || !represented.has(link.from))
}