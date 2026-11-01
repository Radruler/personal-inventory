<script lang="ts">
  // A small "All items" screen assembled from the kit, framed by /dev/kit at
  // phone and desktop widths. ?theme=light|dark|system, ?live=1 for the API.
  import { page } from '$app/state';
  import { api, type Facets, type ItemSummary } from '$lib/api/client';
  import {
    ActiveFilters,
    Button,
    EmptyState,
    FilterSheet,
    LoadingState,
    MultiSelectBar,
    PhotoGrid,
    SearchBox,
    toast,
    type FilterState
  } from '$lib/components';
  import { fixtureFacets, fixtureItems } from '../fixtures';

  $effect(() => {
    const t = page.url.searchParams.get('theme');
    const el = document.documentElement;
    if (t === 'light' || t === 'dark') el.dataset.theme = t;
    else delete el.dataset.theme;
  });

  let all = $state<ItemSummary[]>(fixtureItems);
  let baseFacets = $state<Facets>(fixtureFacets);
  let loading = $state(page.url.searchParams.get('live') === '1');

  $effect(() => {
    if (page.url.searchParams.get('live') !== '1') return;
    Promise.allSettled([api.GET('/api/items', { params: { query: { limit: 60 } } }), api.GET('/api/facets')]).then(([i, f]) => {
      if (i.status === 'fulfilled' && i.value.response.ok && i.value.data) all = i.value.data.items;
      if (f.status === 'fulfilled' && f.value.response.ok && f.value.data) baseFacets = f.value.data;
      loading = false;
    });
  });

  let q = $state('');
  let applied = $state('');
  let filters = $state<FilterState>({});
  let open = $state(false);
  let selecting = $state(false);
  let selected = $state<string[]>([]);

  // Local filtering stands in for the server while the endpoints are stubs.
  function matches(i: ItemSummary, f: FilterState, text: string) {
    const t = text.trim().toLowerCase();
    if (t && !`${i.title} ${i.category ?? ''} ${i.tags.join(' ')}`.toLowerCase().includes(t)) return false;
    if (f.kind && !f.kind.includes(i.kind)) return false;
    if (f.status && !f.status.includes(i.placement.status)) return false;
    if (f.review && !f.review.includes(i.review_state)) return false;
    if (f.category && !f.category.some((c) => i.category === c || i.category?.startsWith(c + '/'))) return false;
    if (f.tag && !f.tag.some((x) => i.tags.includes(x))) return false;
    for (const [k, vs] of Object.entries(f)) {
      if (k.startsWith('attr.') && !vs.includes(String(i.attrs[k.slice(5)] ?? ''))) return false;
    }
    return true;
  }
  const items = $derived(all.filter((i) => matches(i, filters, applied)));
  const facets = $derived({ ...baseFacets, total: items.length });
</script>

<svelte:head><title>Kit preview · Personal Inventory</title></svelte:head>

<div class="page screen">
  <div class="top">
    <SearchBox bind:value={q} placeholder="Search items, tags, codes…" onsearch={(v) => (applied = v)} />
    <div class="bar">
      <ActiveFilters bind:value={filters} {facets} onopen={() => (open = true)} />
      <Button
        size="sm"
        variant={selecting ? 'primary' : 'ghost'}
        onclick={() => {
          selecting = !selecting;
          selected = [];
        }}>{selecting ? 'Done' : 'Select'}</Button
      >
    </div>
    <p class="muted count">{items.length} {items.length === 1 ? 'item' : 'items'}</p>
  </div>

  {#if loading}
    <LoadingState variant="grid" count={6} />
  {:else}
    <PhotoGrid {items} selectable={selecting} bind:selected hrefFor={() => '#'}>
      {#snippet empty()}
        <EmptyState icon="search" title="No items match" message="Try fewer filters or another search.">
          <Button
            onclick={() => {
              filters = {};
              q = '';
              applied = '';
            }}>Clear all</Button
          >
        </EmptyState>
      {/snippet}
    </PhotoGrid>
  {/if}
</div>

{#if selecting}
  <MultiSelectBar count={selected.length} alwaysShow onclear={() => (selected.length ? (selected = []) : (selecting = false))}>
    {#snippet actions()}
      <Button size="sm" variant="primary" disabled={!selected.length} onclick={() => toast(`Stage ${selected.length}…`)}>Stage…</Button>
    {/snippet}
  </MultiSelectBar>
{/if}

<FilterSheet {facets} bind:value={filters} bind:open />

<style>
  .screen {
    display: grid;
    gap: var(--space-4);
  }
  .top {
    display: grid;
    gap: var(--space-2);
  }
  .bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-2);
    min-width: 0;
  }
  .bar > :global(:first-child) {
    min-width: 0;
  }
  .count {
    font-size: var(--text-sm);
  }
</style>
