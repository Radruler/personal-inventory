<script lang="ts">
  // A row with the "Filters" button and the active filters as removable chips.
  import type { Facets } from '$lib/api/client';
  import Button from './Button.svelte';
  import Chip from './Chip.svelte';
  import { activeCount, activeFilters, toggleValue, type FilterState } from './filters';

  let {
    value = $bindable({}),
    facets,
    onopen,
    onchange
  }: {
    value?: FilterState;
    facets: Facets | null;
    /** Opens the FilterSheet. */
    onopen: () => void;
    onchange?: (state: FilterState) => void;
  } = $props();

  const list = $derived(activeFilters(value, facets));
  const n = $derived(activeCount(value));
</script>

<div class="row">
  <Button size="sm" icon="filter" variant={n ? 'primary' : 'secondary'} onclick={onopen} aria-haspopup="dialog">
    Filters{n ? ` · ${n}` : ''}
  </Button>
  {#each list as f (f.field + '\u0000' + f.value)}
    <Chip
      onremove={() => {
        value = toggleValue(value, f.field, f.value);
        onchange?.(value);
      }}
      removeLabel="Remove filter {f.group}: {f.label}"
    >
      {f.label}
    </Chip>
  {/each}
</div>

<style>
  .row {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    overflow-x: auto;
    padding: var(--space-1) 0;
    scrollbar-width: none;
  }
  .row::-webkit-scrollbar {
    display: none;
  }
  .row > :global(*) {
    flex: none;
  }
</style>
