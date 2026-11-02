<script module lang="ts">
  // The last result set survives navigation to an item and back, so the grid
  // renders at once and the browser restores the scroll position; it is
  // refreshed silently right after.
  import type { Facets, ItemList, ItemSummary, LocationNode } from '$lib/api/client';
  import type { FacetGroup } from '$lib/components/filters';

  type Cache = {
    key: string;
    items: ItemSummary[];
    next: string | null;
    total: number;
    facetsKey: string;
    facets: Facets | null;
    lifecycle: FacetGroup | null;
  };
  let cache: Cache | null = null;
  let treeCache: LocationNode[] | null = null;
</script>

<script lang="ts">
  import { untrack } from 'svelte';
  import { goto } from '$app/navigation';
  import { page } from '$app/state';
  import { api, getJSON, unwrap } from '$lib/api/client';
  import {
    ActiveFilters,
    Button,
    EmptyState,
    ErrorState,
    FilterSheet,
    Icon,
    LoadingState,
    PhotoGrid,
    SearchBox,
    activeCount,
    errorMessage,
    filterParams,
    toast,
    type FilterState
  } from '$lib/components';
  import {
    SORTS,
    decorateFacets,
    effectiveSort,
    facetsParams,
    findLocation,
    homeSearch,
    itemsParams,
    lifecycleGroup,
    parseHome,
    resultSummary,
    withField,
    type HomeQuery,
    type SortKey
  } from './home';

  const PAGE = 60;

  const query = $derived(parseHome(page.url.searchParams));
  const key = $derived(itemsParams(query).toString());
  const fKey = $derived(facetsParams(query).toString());

  // ---------------------------------------------------------------- search box ↔ URL
  // svelte-ignore state_referenced_locally
  let input = $state(query.q);
  // svelte-ignore state_referenced_locally
  let pushedQ = query.q;
  let searchEl = $state<HTMLInputElement>();
  $effect(() => {
    const uq = query.q;
    // A back/forward or link changed q: show it. Our own pushes are skipped
    // so typing that continued after the debounce is not clobbered.
    if (uq !== pushedQ) {
      pushedQ = uq;
      input = uq;
    }
  });

  function navigate(next: HomeQuery, replace = false) {
    pushedQ = next.q;
    const search = homeSearch(next);
    if (search === page.url.search) return;
    goto(`/${search}`, { replaceState: replace, keepFocus: true, noScroll: true });
  }

  function onsearch(raw: string) {
    const q = raw.trim();
    if (q === query.q) return;
    // Refining a search replaces the history entry; starting or clearing one pushes.
    navigate({ ...query, q }, !!q && !!query.q);
  }

  function setFilters(filters: FilterState) {
    navigate({ ...query, filters });
  }

  function setSort(sort: SortKey) {
    navigate({ ...query, sort });
  }

  // ---------------------------------------------------------------- results
  type Status = 'loading' | 'ready' | 'error';
  // svelte-ignore state_referenced_locally
  const warm = cache && cache.key === key ? cache : null;
  let status = $state<Status>(warm ? 'ready' : 'loading');
  let items = $state<ItemSummary[]>(warm?.items ?? []);
  let next = $state<string | null>(warm?.next ?? null);
  let total = $state(warm?.total ?? 0);
  let error = $state<unknown>(null);
  let loadingMore = $state(false);
  let refreshing = $state(false);
  let seq = 0;

  function save() {
    cache = {
      key,
      items: $state.snapshot(items) as ItemSummary[],
      next,
      total,
      facetsKey: fKey,
      facets: facets ? ($state.snapshot(facets) as Facets) : null,
      lifecycle: lifecycle ? ($state.snapshot(lifecycle) as FacetGroup) : null
    };
  }

  async function load(h: HomeQuery, k: string) {
    const my = ++seq;
    const reuse = cache && cache.key === k ? cache : null;
    if (reuse) {
      items = reuse.items;
      next = reuse.next;
      total = reuse.total;
      status = 'ready';
    } else if (status !== 'ready' || !items.length) {
      status = 'loading';
    }
    refreshing = true;
    try {
      // A warm refresh fetches as many cards as were showing (one request).
      const limit = reuse ? Math.min(200, Math.max(PAGE, reuse.items.length)) : PAGE;
      const res = await getJSON<ItemList>('/api/items', itemsParams(h, null, limit));
      if (my !== seq) return;
      items = res.items;
      next = res.next_cursor;
      total = res.total;
      status = 'ready';
      error = null;
      save();
    } catch (e) {
      if (my !== seq) return;
      error = e;
      if (!reuse) status = 'error';
      else toast(`Couldn't refresh: ${errorMessage(e)}`, { tone: 'error' });
    } finally {
      if (my === seq) refreshing = false;
    }
  }

  async function loadMore() {
    if (!next || loadingMore) return;
    const my = seq;
    loadingMore = true;
    try {
      const res = await getJSON<ItemList>('/api/items', itemsParams(query, next, PAGE));
      if (my !== seq) return;
      const seen = new Set(items.map((i) => i.id));
      items = [...items, ...res.items.filter((i) => !seen.has(i.id))];
      next = res.next_cursor;
      total = res.total;
      save();
    } catch (e) {
      if (my === seq) toast(`Couldn't load more: ${errorMessage(e)}`, { tone: 'error' });
    } finally {
      loadingMore = false;
    }
  }

  // Keyed on the query strings, so URL changes that don't change the result set refetch nothing.
  $effect(() => {
    const k = key;
    untrack(() => load(query, k));
  });

  // ---------------------------------------------------------------- facets
  // svelte-ignore state_referenced_locally
  const warmFacets = cache && cache.facetsKey === fKey ? cache : null;
  let facets = $state<Facets | null>(warmFacets?.facets ?? null);
  let lifecycle = $state<FacetGroup | null>(warmFacets?.lifecycle ?? null);
  let fseq = 0;

  /** Facets plus the lifecycle counts (owned / gone) for a filter state. */
  async function fetchFacets(q: string, filters: FilterState): Promise<{ facets: Facets; lifecycle: FacetGroup }> {
    const count = async (lc: string) =>
      (await getJSON<ItemList>('/api/items', filterParams(withField(filters, 'lifecycle', [lc]), { q: q || undefined, limit: 1 }))).total;
    const [f, owned, gone] = await Promise.all([
      getJSON<Facets>('/api/facets', filterParams(filters, { q: q || undefined })),
      count('owned'),
      count('gone')
    ]);
    return { facets: f, lifecycle: lifecycleGroup(owned, gone) };
  }

  $effect(() => {
    const fk = fKey;
    untrack(async () => {
      const h = query;
      const my = ++fseq;
      try {
        const r = await fetchFacets(h.q, h.filters);
        if (my !== fseq) return;
        facets = r.facets;
        lifecycle = r.lifecycle;
        if (cache) {
          cache.facetsKey = fk;
          cache.facets = r.facets;
          cache.lifecycle = r.lifecycle;
        }
      } catch {
        /* the grid shows its own error; chips fall back to raw values */
      }
    });
  });

  // A deep link may select a box the location facet doesn't list: label it from the tree.
  let locationLabel = $state<string | null>(null);
  $effect(() => {
    const ref = query.filters.location?.[0];
    if (!ref || ref === 'unknown') {
      locationLabel = null;
      return;
    }
    const fromTree = (roots: LocationNode[]) => (locationLabel = findLocation(roots, ref)?.label ?? null);
    if (treeCache) {
      fromTree(treeCache);
      return;
    }
    api
      .GET('/api/locations')
      .then((r) => {
        treeCache = unwrap(r).roots;
        fromTree(treeCache);
      })
      .catch(() => {});
  });

  const shownFacets = $derived(decorateFacets(facets, query.filters, { lifecycle, locationLabel }));

  // ---------------------------------------------------------------- filter sheet (draft until "Show N items")
  let sheetOpen = $state(false);
  let draft = $state<FilterState>({});
  let draftFacets = $state<Facets | null>(null);
  let draftLifecycle = $state<FacetGroup | null>(null);
  let draftLoading = $state(false);
  let dseq = 0;

  function openSheet() {
    draft = { ...query.filters };
    draftFacets = facets;
    draftLifecycle = lifecycle;
    sheetOpen = true;
  }
  async function refreshDraft(state: FilterState) {
    const my = ++dseq;
    draftLoading = true;
    try {
      const r = await fetchFacets(query.q, state);
      if (my !== dseq) return;
      draftFacets = r.facets;
      draftLifecycle = r.lifecycle;
    } catch (e) {
      if (my === dseq) toast(`Couldn't update counts: ${errorMessage(e)}`, { tone: 'error' });
    } finally {
      if (my === dseq) draftLoading = false;
    }
  }
  const sheetFacets = $derived(decorateFacets(draftFacets, draft, { lifecycle: draftLifecycle, locationLabel }));

  // ---------------------------------------------------------------- misc
  const sort = $derived(effectiveSort(query));
  const nFilters = $derived(activeCount(query.filters));
  const showingGone = $derived(!!query.filters.lifecycle);

  function onwindowkey(e: KeyboardEvent) {
    if (e.key !== '/' || e.metaKey || e.ctrlKey || e.altKey) return;
    const t = e.target as HTMLElement | null;
    if (t && (t.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(t.tagName))) return;
    if (document.querySelector('dialog[open]')) return;
    e.preventDefault();
    searchEl?.focus();
  }
</script>

<svelte:window onkeydown={onwindowkey} />
<svelte:head><title>{query.q ? `“${query.q}” · ` : ''}Items · Personal Inventory</title></svelte:head>

<div class="page home">
  <h1 class="visually-hidden">All items</h1>

  <div class="toolbar">
    <SearchBox
      bind:value={input}
      bind:input={searchEl}
      placeholder="Search items, labels, codes…"
      label="Search items"
      debounce={300}
      busy={refreshing && status === 'ready'}
      {onsearch}
    />
    <div class="controls">
      <div class="filters">
        <ActiveFilters value={query.filters} facets={shownFacets} onopen={openSheet} onchange={setFilters} />
      </div>
      <label class="sort">
        <span class="visually-hidden">Sort by</span>
        <Icon name="chevron-down" size={16} />
        <select value={sort} onchange={(e) => setSort(e.currentTarget.value as SortKey)}>
          {#each SORTS as s (s.value)}
            <option value={s.value} disabled={s.value === 'relevance' && !query.q}>{s.label}</option>
          {/each}
        </select>
      </label>
    </div>
  </div>

  {#if status === 'ready'}
    <p class="summary muted" aria-live="polite">
      {resultSummary(total, query.q)}{#if showingGone}<span> · including gone</span>{/if}
    </p>
  {/if}

  {#if status === 'loading'}
    <LoadingState variant="grid" count={8} label="Loading items…" />
  {:else if status === 'error'}
    <ErrorState {error} title="Couldn't load items" onretry={() => load(query, key)} retrying={refreshing} />
  {:else}
    <PhotoGrid {items} label="Items" hasMore={!!next} {loadingMore} onloadmore={loadMore}>
      {#snippet empty()}
        {#if query.q || nFilters}
          <EmptyState icon="search" title="Nothing matches" message={query.q ? `No items match “${query.q}”${nFilters ? ' with these filters' : ''}.` : 'No items match these filters.'}>
            {#if nFilters}<Button onclick={() => setFilters({})}>Clear filters</Button>{/if}
            {#if query.q}<Button onclick={() => navigate({ ...query, q: '' })}>Clear search</Button>{/if}
          </EmptyState>
        {:else}
          <EmptyState icon="inbox" title="No items yet" message="Add something, or upload photos to the Inbox for an agent to sort.">
            <Button variant="primary" href="/add" icon="plus">Add an item</Button>
            <Button href="/inbox">Open Inbox</Button>
          </EmptyState>
        {/if}
      {/snippet}
    </PhotoGrid>
  {/if}
</div>

<FilterSheet
  facets={sheetFacets}
  bind:value={draft}
  bind:open={sheetOpen}
  loading={draftLoading}
  onchange={refreshDraft}
  onapply={(state) => setFilters(state)}
/>

<style>
  .home {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: var(--space-3);
    padding-top: var(--space-3);
  }
  .toolbar {
    position: sticky;
    top: 0;
    z-index: var(--z-sticky);
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: var(--space-2);
    margin: 0 calc(-1 * var(--gutter));
    padding: var(--space-2) var(--gutter);
    background: color-mix(in srgb, var(--color-bg) 92%, transparent);
    backdrop-filter: blur(12px);
  }
  .controls {
    display: flex;
    align-items: flex-start;
    gap: var(--space-2);
  }
  .filters {
    flex: 1;
    min-width: 0;
  }
  .sort {
    position: relative;
    flex: none;
    display: flex;
    align-items: center;
    color: var(--color-text-muted);
  }
  .sort :global(svg) {
    position: absolute;
    right: var(--space-2);
    pointer-events: none;
  }
  .sort select {
    appearance: none;
    min-height: var(--tap-min);
    max-width: 11rem;
    padding: 0 calc(var(--space-2) + 20px) 0 var(--space-3);
    border: var(--border-width) solid var(--color-border);
    border-radius: var(--radius-pill);
    background: var(--color-surface);
    color: var(--color-text);
    font-size: var(--text-sm);
    cursor: pointer;
  }
  .summary {
    font-size: var(--text-sm);
  }
  @media (min-width: 720px) {
    .toolbar {
      top: var(--topbar-height);
    }
  }
</style>
