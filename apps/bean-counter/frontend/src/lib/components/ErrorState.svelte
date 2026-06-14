<script lang="ts">
  interface Props {
    title: string
    message: string
  }

  let { title, message }: Props = $props()
</script>

<div class="state-panel error-state" role="status">
  <h2>{title}</h2>
  <p>{message}</p>
</div>
