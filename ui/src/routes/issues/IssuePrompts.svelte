<script lang="ts">
  import type { IssuePromptSummary } from '../../lib/api'
  import { isMissingPrompt, promptHref } from './prompts'

  interface Props {
    prompts: IssuePromptSummary[]
    navigate: (path: string) => void
  }

  let { prompts, navigate }: Props = $props()

  function goTo(id: string, event: MouseEvent) {
    event.preventDefault()
    navigate(promptHref(id))
  }
</script>

<div class="detail-section">
  <h3>Prompts</h3>
  {#if prompts.length === 0}
    <p class="muted">No prompts link to this issue.</p>
  {:else}
    <ul class="dependency-list">
      {#each prompts as prompt (prompt.id)}
        <li>
          {#if isMissingPrompt(prompt)}
            <span><span class="status-pill missing">missing</span> {prompt.id}</span>
          {:else}
            <span>
              <a href={promptHref(prompt.id)} onclick={(event) => goTo(prompt.id, event)}>{prompt.title}</a>
              <span class="project-pill">{prompt.project}</span>
              <span class="status-pill">{prompt.status}</span>
              <span>P{prompt.priority}</span>
            </span>
          {/if}
        </li>
      {/each}
    </ul>
  {/if}
</div>
