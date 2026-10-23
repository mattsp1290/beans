import type { Backlink, IssuePromptSummary } from '../../lib/api'

export function promptHref(id: string): string {
  return `/prompts/${encodeURIComponent(id)}`
}

export function isMissingPrompt(prompt: IssuePromptSummary): prompt is { id:string; missing:true } {
  return 'missing' in prompt && prompt.missing === true
}

export function withoutPromptBacklinkDuplicates(backlinks: Backlink[], prompts: IssuePromptSummary[]): Backlink[] {
  const represented = new Set(prompts.map((prompt) => prompt.id))
  return backlinks.filter((link) => link.note_kind !== 'prompt' || !represented.has(link.from))
}