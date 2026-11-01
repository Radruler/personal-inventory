<script lang="ts">
  // Facet filters in a Sheet (bottom sheet on phone, side panel on desktop).
  // Each Facets group renders as toggle chips with counts. `value` is the
  // selection (bindable); `onchange` fires on every toggle so the caller can
  // refetch /api/facets for live counts; "Show N items" applies and closes.
  import type { Facets } from '$lib/api/client';
  import Button from './Button.svelte';
  import Chip from './Chip.svelte';
  import Sheet from './Sheet.svelte';
  import Spinner from './Spinner.svelte';
  import { activeCount, clearField, groupValues, isSelected, toggleValue, type FilterState } from './filters';

  let {
    facets,
    value = $bindable({}),
    open = $bindable(false),
    loading = false,
    title = 'Filters',
    collapseAfter = 8,
    hideFields = [],
    onchange,
    onapply
  }: {
    facets: Facets | null;
    value?: FilterState;
    open?: boolean;
    /** Counts are being refreshed. */
    loading?: boolean;
    title?: string;
    /** Long groups show this many values plus "Show all". */
    collapseAfter?: number;
    /** Facet fields not to show (e.g. 'location' on a box page). */
    hideFields?: string[];
    onchange?: (state: FilterState) => void;
    /** "Show N items" pressed. */
    onapply?: (state: FilterState) => void;
  } = $props();

  let expanded = $state<Record<string, boolean>>({});
  const groups = $derived((facets?.groups ?? []).filter((g) => g.values.length && !hideFields.includes(g.field)));
  const n = $derived(activeCount(value));
  const total = $derived(facets?.total ?? 0);

  function set(next: FilterState) {
    value = next;
    onchange?.(next);
  }
</script>

<Sheet bind:open {title} side="auto">
  {#snippet header()}
    {#if loading}<Spinner size={16} label="Updating counts" />{/if}
  {/snippet}

  {#if !groups.length}
    <p class="muted">No filters available.</p>
  {/if}
  <div class="groups">
    {#each groups as g (g.field)}
      {@const shown = groupValues(g, value, expanded[g.field] ? Infinity : collapseAfter)}
      {@const sel = value[g.field]?.length ?? 0}
      <fieldset>
        <legend>
          <span>{g.label}</span>
          {#if sel}
            <button type="button" class="reset" onclick={() => set(clearField(value, g.field))}>
              Clear<span class="visually-hidden"> {g.label}</span>
            </button>
          {/if}
        </legend>
        <div class="chips">
          {#each shown.values as v (v.value)}
            <Chip selected={isSelected(value, g.field, v.value)} count={v.count} onclick={() => set(toggleValue(value, g.field, v.value))}>
              {v.label}
            </Chip>
          {/each}
          {#if shown.hidden > 0}
            <button type="button" class="more" onclick={() => (expanded[g.field] = true)}>Show all ({shown.hidden} more)</button>
          {:else if expanded[g.field]}
            <button type="button" class="more" onclick={() => (expanded[g.field] = false)}>Show fewer</button>
          {/if}
        </div>
      </fieldset>
    {/each}
  </div>

  {#snippet footer()}
    <Button variant="ghost" disabled={n === 0} onclick={() => set({})}>Clear{n ? ` (${n})` : ''}</Button>
    <Button
      variant="primary"
      loading={loading}
      onclick={() => {
        onapply?.(value);
        open = false;
      }}
    >
      Show {total} {total === 1 ? 'item' : 'items'}
    </Button>
  {/snippet}
</Sheet>

<style>
  .groups {
    display: grid;
    gap: var(--space-5);
  }
  fieldset {
    margin: 0;
    padding: 0;
    border: 0;
    min-width: 0;
  }
  legend {
    display: flex;
    align-items: center;
    justify-content: space-between;
    width: 100%;
    padding: 0;
    margin-bottom: var(--space-2);
    font-weight: var(--weight-bold);
    font-size: var(--text-sm);
  }
  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
  }
  .reset,
  .more {
    min-height: calc(var(--tap-min) - var(--space-2));
    padding: 0 var(--space-2);
    border: 0;
    border-radius: var(--radius-sm);
    background: none;
    color: var(--color-accent);
    font-size: var(--text-sm);
    font-weight: var(--weight-medium);
    cursor: pointer;
  }
  .reset:hover,
  .more:hover {
    background: var(--color-accent-soft);
  }
</style>
