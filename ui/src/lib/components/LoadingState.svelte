<script lang="ts">
  interface Props {
    label?: string
    message: string
  }

  let { label = 'Loading', message }: Props = $props()
</script>

<div class="state-panel loading-state" role="status" aria-label={label}>
  <span aria-hidden="true"></span>
  <p>{message}</p>
</div>
