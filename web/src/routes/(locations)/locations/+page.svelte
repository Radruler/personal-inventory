<script lang="ts">
  // Places: the location tree (sites → rooms/units → shelves/boxes) with
  // item counts, codes up front, search by code or name, archived toggle,
  // "New location" and the unknown-location row (PLAN.md §8 Locations).
  import { goto } from '$app/navigation';
  import { api, unwrap, type LocationNode } from '$lib/api/client';
  import Button from '$lib/components/Button.svelte';
  import Chip from '$lib/components/Chip.svelte';
  import EmptyState from '$lib/components/EmptyState.svelte';
  import ErrorState from '$lib/components/ErrorState.svelte';
  import Icon from '$lib/components/Icon.svelte';
  import LoadingState from '$lib/components/LoadingState.svelte';
  import SearchBox from '$lib/components/SearchBox.svelte';
  import { LOCATION_KIND_LABEL } from '$lib/components/format';
  import { LOCATION_KIND_ICON } from '$lib/components/icons';
  import { indexTree, searchTree, visibleRows } from '$lib/components/locationTree';
  import LocationFormSheet from '../shared/LocationFormSheet.svelte';
  import { plural } from '../shared/places';
  import { readPref, writePref } from '../shared/store.svelte';

  let roots = $state<LocationNode[]>([]);
  let unknown = $state(0);
  let loading = $state(true);
  let error = $state<unknown>(null);
  let query = $state('');
  let archived = $state(readPref('inv.places.archived') === '1');
  let collapsed = $state(new Set<string>());
  let createOpen = $state(false);

  async function load() {
    error = null;
    try {
      const t = unwrap(await api.GET('/api/locations', { params: { query: { include_archived: archived } } }));
      roots = t.roots;
      unknown = t.unknown_count;
    } catch (e) {
      error = e;
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    void archived;
    void load();
  });

  const idx = $derived(indexTree(roots));
  const expanded = $derived(new Set([...idx.values()].filter((e) => e.node.children.length && !collapsed.has(e.node.id)).map((e) => e.node.id)));
  const search = $derived(searchTree(roots, query));
  const rows = $derived(visibleRows(roots, expanded, search));
  const boxCount = $derived([...idx.values()].filter((e) => e.node.kind === 'container' && !e.node.archived_at).length);

  function toggle(id: string) {
    const next = new Set(collapsed);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    collapsed = next;
  }

  function setArchived(v: boolean) {
    archived = v;
    writePref('inv.places.archived', v ? '1' : null);
  }

  function onsearchkey(e: KeyboardEvent) {
    // Enter on an exact code match opens it (handy with a keyboard at the desk).
    if (e.key !== 'Enter' || !search) return;
    const q = query.trim().toLowerCase();
    const exact = [...idx.values()].find((x) => x.node.code?.toLowerCase() === q);
    const only = search.matches.size === 1 ? [...search.matches][0] : null;
    const id = exact?.node.id ?? only;
    if (id) goto(`/locations/${id}`);
  }
</script>

<svelte:head><title>Places · Personal Inventory</title></svelte:head>

<div class="page places">
  <header class="head">
    <div>
      <h1>Places</h1>
      {#if !loading && !error}
        <p class="muted sub">{plural(idx.size, 'location')} · {plural(boxCount, 'box', 'boxes')}</p>
      {/if}
    </div>
    <Button variant="primary" icon="plus" onclick={() => (createOpen = true)}>New location</Button>
  </header>

  <div class="tools">
    <div class="search"><SearchBox bind:value={query} debounce={0} placeholder="Code or name, e.g. B-014" label="Search places" onkeydown={onsearchkey} /></div>
    <Chip selected={archived} onclick={() => setArchived(!archived)}>Archived</Chip>
  </div>

  {#if loading}
    <LoadingState variant="list" count={8} />
  {:else if error}
    <ErrorState {error} onretry={load} />
  {:else}
    {#if unknown > 0 && !query}
      <a class="unknown" href="/?status=unknown">
        <Icon name="unknown" size={22} />
        <span class="u-text"><strong>Location unknown</strong><span class="muted">{plural(unknown, 'item')} with no known place</span></span>
        <Icon name="chevron-right" size={20} />
      </a>
    {/if}

    {#if rows.length}
      <ul class="tree" aria-label="Places">
        {#each rows as r (r.node.id)}
          {@const n = r.node}
          <li class="row" class:match={r.match} class:archived={!!n.archived_at} style:--depth={r.depth - 1}>
            {#if r.hasChildren && !search}
              <button
                type="button"
                class="twisty"
                class:open={r.expanded}
                aria-expanded={r.expanded}
                aria-label="{r.expanded ? 'Collapse' : 'Expand'} {n.name}"
                onclick={() => toggle(n.id)}
              >
                <Icon name="chevron-right" size={18} />
              </button>
            {:else}
              <span class="twisty leaf" aria-hidden="true"></span>
            {/if}
            <a class="link" href="/locations/{n.id}">
              <span class="kind" title={LOCATION_KIND_LABEL[n.kind]}><Icon name={LOCATION_KIND_ICON[n.kind] ?? 'container'} size={20} /></span>
              <span class="names">
                {#if n.code}<span class="code mono">{n.code}</span>{/if}
                <span class="name">{n.name}</span>
                {#if n.archived_at}<span class="tag">Archived</span>{/if}
              </span>
              <span class="counts">
                {#if n.staged_count}<span class="staged" title="{n.staged_count} staged to go here">+{n.staged_count}</span>{/if}
                <span class="total" title="{n.total_count} items inside ({n.item_count} directly here)">
                  {n.total_count}{#if n.item_count !== n.total_count && n.item_count > 0}<small>&nbsp;· {n.item_count} here</small>{/if}
                </span>
              </span>
            </a>
          </li>
        {/each}
      </ul>
    {:else if query}
      <EmptyState icon="search" title="No match" message={'No place called “' + query + '”.'}>
        <Button icon="plus" onclick={() => (createOpen = true)}>New location</Button>
      </EmptyState>
    {:else}
      <EmptyState icon="placed" title="No places yet" message="Start with a site (Home, the storage facility), then its rooms, units and boxes.">
        <Button variant="primary" icon="plus" onclick={() => (createOpen = true)}>New location</Button>
      </EmptyState>
    {/if}

    <p class="legend muted">Numbers are items inside (including boxes within); <span class="staged">+N</span> are staged to go there.</p>
  {/if}
</div>

<LocationFormSheet bind:open={createOpen} onsaved={(id) => goto(`/locations/${id}`)} />

<style>
  .places {
    max-width: 860px;
  }
  .head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: var(--space-3);
    margin-bottom: var(--space-4);
  }
  .sub {
    font-size: var(--text-sm);
  }
  .tools {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    margin-bottom: var(--space-4);
  }
  .search {
    flex: 1;
    min-width: 0;
  }
  .unknown {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    min-height: var(--tap-min);
    margin-bottom: var(--space-3);
    padding: var(--space-3) var(--space-4);
    border-radius: var(--radius-md);
    background: var(--status-unknown-soft);
    color: var(--status-unknown);
  }
  .unknown:hover {
    text-decoration: none;
  }
  .u-text {
    flex: 1;
    display: grid;
    color: var(--color-text);
  }
  .u-text .muted {
    font-size: var(--text-sm);
  }
  .tree {
    margin: 0;
    padding: 0;
    list-style: none;
    border-top: var(--border-width) solid var(--color-border);
  }
  .row {
    display: flex;
    align-items: center;
    padding-left: calc(var(--depth) * var(--space-5));
    border-bottom: var(--border-width) solid var(--color-border);
  }
  .twisty {
    display: grid;
    place-items: center;
    flex: none;
    width: 36px;
    height: var(--tap-min);
    padding: 0;
    border: 0;
    background: none;
    color: var(--color-text-muted);
    cursor: pointer;
  }
  .twisty :global(svg) {
    transition: transform var(--duration-fast) var(--ease);
  }
  .twisty.open :global(svg) {
    transform: rotate(90deg);
  }
  .link {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: center;
    gap: var(--space-3);
    min-height: calc(var(--tap-min) + var(--space-2));
    padding: var(--space-1) var(--space-2) var(--space-1) 0;
    color: var(--color-text);
  }
  .link:hover {
    text-decoration: none;
    background: var(--color-surface-2);
  }
  .kind {
    flex: none;
    color: var(--color-text-muted);
  }
  .names {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: baseline;
    flex-wrap: wrap;
    column-gap: var(--space-2);
  }
  .code {
    font-weight: var(--weight-bold);
    font-size: var(--text-md);
    letter-spacing: 0.02em;
  }
  .name {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .code + .name {
    color: var(--color-text-muted);
  }
  .match .code,
  .match .name {
    color: var(--color-accent);
  }
  .archived .names {
    opacity: 0.6;
  }
  .tag {
    padding: 0 var(--space-2);
    border-radius: var(--radius-pill);
    background: var(--color-surface-3);
    color: var(--color-text-muted);
    font-size: var(--text-xs);
  }
  .counts {
    display: inline-flex;
    align-items: baseline;
    gap: var(--space-2);
    flex: none;
    font-variant-numeric: tabular-nums;
  }
  .total {
    font-weight: var(--weight-medium);
  }
  .total small {
    color: var(--color-text-faint);
    font-weight: var(--weight-regular);
  }
  .staged {
    color: var(--status-staged);
    font-weight: var(--weight-medium);
  }
  .legend {
    margin-top: var(--space-4);
    font-size: var(--text-xs);
  }
  @media (prefers-reduced-motion: reduce) {
    .twisty :global(svg) {
      transition: none;
    }
  }
</style>
