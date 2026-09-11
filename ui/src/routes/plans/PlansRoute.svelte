<script lang="ts">
  import { api, type PlanDetail, type PlanListItem } from '../../lib/api'
  import ErrorState from '../../lib/components/ErrorState.svelte'
  import LoadingState from '../../lib/components/LoadingState.svelte'
  import PlanGraph from '../../lib/plan-graph/PlanGraph.svelte'

  interface Props {
    project: string
    id: string | null
    reloadKey: number
    navigate: (path: string) => void
  }

  let { project, id, reloadKey, navigate }: Props = $props()
  let plans = $state<PlanListItem[]>([])
  let detail = $state<PlanDetail | null>(null)
  let error = $state('')
  let loading = $state(true)
  let showGraphText = $state(false)

  $effect(() => {
    project
    id
    reloadKey
    let cancelled = false
    loading = true
    error = ''
    const request = id ? api.getPlan(id) : api.listPlans(project)
    request
      .then((value) => {
        if (cancelled) return
        if (id) detail = value as PlanDetail
        else plans = value as PlanListItem[]
      })
      .catch((cause) => {
        if (!cancelled) error = cause instanceof Error ? cause.message : 'Could not load plans'
      })
      .finally(() => {
        if (!cancelled) loading = false
      })
    return () => {
      cancelled = true
    }
  })
</script>

{#if loading}
  <LoadingState label="Loading plans…" message="Fetching plans from the hub." />
{:else if error}
  <ErrorState title="Could not load plans" message={error} />
{:else if detail}
  <article class="plan-detail">
    <p class="muted">{detail.project} · lifecycle <strong>{detail.execution.lifecycle_status}</strong> · execution <strong>{detail.execution.execution_state}</strong></p>
    <h2>{detail.title}</h2>
    {#if detail.execution.lifecycle_mismatch}<p role="alert">Execution and lifecycle differ. Change plan lifecycle explicitly after validating the artifact.</p>{/if}
    <section aria-label="Execution counts"><h3>Execution</h3><p>Runnable {detail.execution.counts.runnable} · In progress {detail.execution.counts.in_progress} · Held {detail.execution.counts.held} · Blocked {detail.execution.counts.blocked} · Done {detail.execution.counts.done} · Missing {detail.execution.counts.missing}</p>
      <ul>
      {#each detail.execution.nodes as node}
        <li><strong>{node.label}</strong>: {node.binding}{#if node.work_state} — {node.work_state}{/if}{#if node.issue} — <a href={'/issues/' + encodeURIComponent(node.issue.id)}>{node.issue.id}</a> ({node.issue.status}){/if}{#if node.hold_reason} — {node.hold_reason}{/if}{#if node.binding === 'reference'} — {node.ref}{/if}
        {#if node.blockers.length}<ul>{#each node.blockers as blocker}<li>Blocked by {blocker.missing ? blocker.target + ' (missing)' : blocker.id + ' (' + blocker.status + ')'}</li>{/each}</ul>{/if}</li>
      {/each}
      </ul>
    </section>
    <section><h3>Outcome</h3>{@html detail.summary.outcome_html}</section>
    <section><h3>Affected areas</h3>{@html detail.summary.affected_areas_html}</section>
    <section><h3>Execution order</h3>{@html detail.summary.execution_order_html}</section>
    <section><h3>Risks</h3>{@html detail.summary.risks_html}</section>
    <section>
      <h3>Change graph</h3>
      <PlanGraph graph={detail.summary.graph} />
      <button type="button" aria-expanded={showGraphText} onclick={() => (showGraphText = !showGraphText)}>
        View graph as text
      </button>
      <ul hidden={!showGraphText}>
        {#each detail.summary.graph.nodes as node}
          <li><strong>{node.label}</strong> — {node.kind}{#if node.ref}: {node.ref}{/if}</li>
        {/each}
        {#each detail.summary.graph.edges as edge}
          <li>{edge.from} → {edge.to} ({edge.kind})</li>
        {/each}
      </ul>
    </section>
    {#each detail.sections as section}
      <section><h3>{section.path}</h3>{@html section.html}</section>
    {/each}
  </article>
{:else}
  <div class="cards">
    {#each plans as plan}
      <button class="card" type="button" onclick={() => navigate('/plans/' + encodeURIComponent(plan.id))}>
        <strong>{plan.title}</strong><span>{plan.status} · {plan.project}</span>
      </button>
    {:else}
      <p class="muted">No plans found.</p>
    {/each}
  </div>
{/if}

<style>
  .cards { display: grid; gap: 10px }
  .card { text-align: left; display: grid; gap: 4px }
  .plan-detail section { margin: 24px 0 }
  .plan-detail h3 { margin-bottom: 8px }
</style>
