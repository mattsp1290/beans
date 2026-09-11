<script lang="ts">
  import { api, type PlanDetail, type PlanListItem } from '../../lib/api'
  import ErrorState from '../../lib/components/ErrorState.svelte'
  import LoadingState from '../../lib/components/LoadingState.svelte'
	import PlanGraph from '../../lib/plan-graph/PlanGraph.svelte'
  interface Props { project:string; id:string|null; reloadKey:number; navigate:(path:string)=>void }
  let { project, id, reloadKey, navigate }:Props=$props()
  let plans=$state<PlanListItem[]>([]), detail=$state<PlanDetail|null>(null), error=$state(''), loading=$state(true)
  $effect(()=>{ project; id; reloadKey; let cancelled=false;loading=true;error='';(id?api.getPlan(id):api.listPlans(project)).then((v)=>{if(cancelled)return;if(id)detail=v as PlanDetail;else plans=v as PlanListItem[]}).catch((e)=>{if(!cancelled)error=e instanceof Error?e.message:'Could not load plans'}).finally(()=>{if(!cancelled)loading=false});return()=>{cancelled=true}})
</script>

{#if loading}<LoadingState label="Loading plans…" message="Fetching plans from the hub." />
{:else if error}<ErrorState title="Could not load plans" message={error} />
{:else if detail}
  <article class="plan-detail">
    <p class="muted">{detail.project} · <strong>{detail.summary.status}</strong></p><h2>{detail.title}</h2>
    <section><h3>Outcome</h3>{@html detail.summary.outcome_html}</section>
    <section><h3>Affected areas</h3>{@html detail.summary.affected_areas_html}</section>
    <section><h3>Execution order</h3>{@html detail.summary.execution_order_html}</section>
    <section><h3>Risks</h3>{@html detail.summary.risks_html}</section>
		<section><h3>Change graph</h3><PlanGraph graph={detail.summary.graph} /><button type="button" onclick={(e)=>{const n=(e.currentTarget.nextElementSibling as HTMLElement);n.hidden=!n.hidden}}>View graph as text</button><ul hidden>{#each detail.summary.graph.nodes as node}<li><strong>{node.label}</strong> — {node.kind}{#if node.ref}: {node.ref}{/if}</li>{/each}{#each detail.summary.graph.edges as edge}<li>{edge.from} → {edge.to} ({edge.kind})</li>{/each}</ul></section>
    {#each detail.sections as section}<section><h3>{section.path}</h3>{@html section.html}</section>{/each}
  </article>
{:else}
  <div class="cards">{#each plans as plan}<button class="card" type="button" onclick={()=>navigate('/plans/'+encodeURIComponent(plan.id))}><strong>{plan.title}</strong><span>{plan.status} · {plan.project}</span></button>{:else}<p class="muted">No plans found.</p>{/each}</div>
{/if}

<style>.cards{display:grid;gap:10px}.card{text-align:left;display:grid;gap:4px}.plan-detail section{margin:24px 0}.plan-detail h3{margin-bottom:8px}</style>
