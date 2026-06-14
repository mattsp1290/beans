<script lang="ts">
  const queueMetrics = [
    { label: 'Open', value: '0' },
    { label: 'Ready', value: '0' },
    { label: 'Blocked', value: '0' },
  ]

  const views = ['Issues', 'Ready', 'Dependencies', 'Graph']
</script>

<svelte:head>
  <title>Bean Counter</title>
</svelte:head>

<div class="shell">
  <aside class="sidebar" aria-label="Primary navigation">
    <div class="brand">
      <span class="brand-mark" aria-hidden="true">bc</span>
      <div>
        <p>Bean Counter</p>
        <span>Local tracker</span>
      </div>
    </div>

    <nav>
      {#each views as view}
        <a class:active={view === 'Issues'} href="/" aria-current={view === 'Issues' ? 'page' : undefined}>
          {view}
        </a>
      {/each}
    </nav>
  </aside>

  <main>
    <header class="topbar">
      <div>
        <h1>Issues</h1>
        <p>Project work queue</p>
      </div>
      <button type="button">New issue</button>
    </header>

    <section class="metrics" aria-label="Queue totals">
      {#each queueMetrics as metric}
        <div class="metric">
          <span>{metric.label}</span>
          <strong>{metric.value}</strong>
        </div>
      {/each}
    </section>

    <section class="workspace" aria-label="Issues workspace">
      <div class="toolbar">
        <label>
          <span>Filter</span>
          <input type="search" placeholder="Title, label, or id" />
        </label>
        <select aria-label="State">
          <option>All states</option>
          <option>Open</option>
          <option>In progress</option>
          <option>Blocked</option>
          <option>Closed</option>
        </select>
      </div>

      <div class="empty-state">
        <h2>No issues loaded</h2>
        <p>The API client will populate this workspace in the next frontend slice.</p>
      </div>
    </section>
  </main>
</div>
