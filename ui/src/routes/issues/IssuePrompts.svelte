<script lang="ts">
  import type { PromptSummary } from '../../lib/api'
  import { handleNavigationClick } from '../../lib/nav'
  import { promptHref } from './prompts'

  interface Props {
    prompts: PromptSummary[]
    navigate: (path: string) => void
  }

  let { prompts, navigate }: Props = $props()

  function goTo(id: string, event: MouseEvent) {
    handleNavigationClick(event, navigate, promptHref(id))
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
          <span>
            <a href={promptHref(prompt.id)} onclick={(event) => goTo(prompt.id, event)}>{prompt.title}</a>
            <span class="project-pill">{prompt.project}</span>
            <span class="status-pill">{prompt.status}</span>
            <span>P{prompt.priority}</span>
          </span>
        </li>
      {/each}
    </ul>
  {/if}
</div>
