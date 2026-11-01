<script lang="ts">
  // Placeholder from Phase 0 (owner: thread E). Shows the API is wired up.
  import { api, unwrap, type Schemas } from '$lib/api/client';

  let health = $state<Schemas['Health'] | null>(null);
  let error = $state<string | null>(null);

  $effect(() => {
    api
      .GET('/api/health')
      .then((r) => (health = unwrap(r)))
      .catch((e) => (error = String(e)));
  });
</script>

<svelte:head><title>Items · Personal Inventory</title></svelte:head>

<div class="page">
  <h1>All items</h1>
  {#if health}
    <p class="muted">Server {health.version} · schema v{health.schema_version} · vips {health.vips}</p>
  {:else if error}
    <p class="muted">API unreachable: {error}</p>
  {:else}
    <p class="muted">Connecting…</p>
  {/if}
</div>
