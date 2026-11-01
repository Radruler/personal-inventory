<script lang="ts">
  // UI kit showcase (thread D). Every component in its states, driven by
  // typed fixtures; "Live data" swaps in GET /api/items, /api/facets and
  // /api/locations wherever they answer 200.
  import { onMount, type Snippet } from 'svelte';
  import { api, ApiError, getJSON, type Facets, type ItemSummary, type LocationNode } from '$lib/api/client';
  import {
    ActiveFilters,
    Badge,
    Button,
    Chip,
    EmptyState,
    ErrorState,
    FilterSheet,
    Gallery,
    ItemCard,
    LoadingState,
    LocationChip,
    LocationPicker,
    MultiSelectBar,
    PhotoGrid,
    ProvenanceBadge,
    SearchBox,
    Sheet,
    Spinner,
    Tabs,
    filterParams,
    toast,
    type FilterState,
    type LocationCreateRequest
  } from '$lib/components';
  import { fixtureFacets, fixtureGalleryMedia, fixtureItems, fixtureLocationTree, placements } from './fixtures';

  // ---------------------------------------------------------------- theme
  type Theme = 'system' | 'light' | 'dark';
  let theme = $state<Theme>('system');
  let prevTheme: string | undefined;
  onMount(() => {
    prevTheme = document.documentElement.dataset.theme;
    if (prevTheme === 'light' || prevTheme === 'dark') theme = prevTheme;
    return () => {
      if (prevTheme) document.documentElement.dataset.theme = prevTheme;
      else delete document.documentElement.dataset.theme;
    };
  });
  $effect(() => {
    const el = document.documentElement;
    if (theme === 'system') delete el.dataset.theme;
    else el.dataset.theme = theme;
  });

  // ---------------------------------------------------------------- data (fixtures or live)
  type Source = 'fixtures' | 'live' | 'loading' | `fixtures (${string})`;
  let live = $state(false);
  let items = $state<ItemSummary[]>(fixtureItems);
  let facets = $state<Facets>(fixtureFacets);
  let roots = $state<LocationNode[]>(structuredClone(fixtureLocationTree.roots));
  let sources = $state<Record<'items' | 'facets' | 'locations', Source>>({ items: 'fixtures', facets: 'fixtures', locations: 'fixtures' });

  function why(e: unknown): Source {
    return `fixtures (${e instanceof ApiError ? e.status || 'offline' : 'error'})`;
  }

  async function loadLive() {
    sources = { items: 'loading', facets: 'loading', locations: 'loading' };
    const [i, f, l] = await Promise.allSettled([
      api.GET('/api/items', { params: { query: { limit: 60 } } }),
      api.GET('/api/facets'),
      api.GET('/api/locations')
    ]);
    if (!live) return;
    if (i.status === 'fulfilled' && i.value.response.ok && i.value.data) {
      items = i.value.data.items;
      sources.items = 'live';
    } else {
      items = fixtureItems;
      sources.items = i.status === 'fulfilled' ? why(new ApiError(i.value.response.status)) : why(i.reason);
    }
    if (f.status === 'fulfilled' && f.value.response.ok && f.value.data) {
      facets = f.value.data;
      sources.facets = 'live';
    } else {
      facets = fixtureFacets;
      sources.facets = f.status === 'fulfilled' ? why(new ApiError(f.value.response.status)) : why(f.reason);
    }
    if (l.status === 'fulfilled' && l.value.response.ok && l.value.data) {
      roots = l.value.data.roots;
      sources.locations = 'live';
    } else {
      roots = structuredClone(fixtureLocationTree.roots);
      sources.locations = l.status === 'fulfilled' ? why(new ApiError(l.value.response.status)) : why(l.reason);
    }
  }

  function setLive(on: boolean) {
    live = on;
    if (on) loadLive();
    else {
      items = fixtureItems;
      facets = fixtureFacets;
      roots = structuredClone(fixtureLocationTree.roots);
      sources = { items: 'fixtures', facets: 'fixtures', locations: 'fixtures' };
    }
  }

  // ---------------------------------------------------------------- component demo state
  let chipOn = $state<Record<string, boolean>>({ clothing: true });
  let q = $state('');
  let lastSearch = $state('');
  let tab = $state('one');

  type GridState = 'data' | 'loading' | 'empty' | 'error';
  let gridState = $state<GridState>('data');
  let selecting = $state(false);
  let selected = $state<string[]>([]);

  let filterOpen = $state(false);
  let filters = $state<FilterState>({});
  let facetsLoading = $state(false);
  async function refreshFacets(next: FilterState) {
    if (sources.facets !== 'live') return;
    facetsLoading = true;
    try {
      facets = await getJSON<Facets>('/api/facets', filterParams(next));
    } catch (e) {
      toast(`Facets: ${(e as Error).message}`, { tone: 'error' });
    } finally {
      facetsLoading = false;
    }
  }

  let pickedId = $state<string | null>('L-b014');
  let packOpen = $state(false);
  let packTarget = $state<string | null>(null);
  let creating = $state(false);
  let createError = $state<unknown>(null);
  let lastCreate = $state<LocationCreateRequest | null>(null);

  function findNode(nodes: LocationNode[], id: string): LocationNode | null {
    for (const n of nodes) {
      if (n.id === id) return n;
      const c = findNode(n.children, id);
      if (c) return c;
    }
    return null;
  }

  async function createLocation(req: LocationCreateRequest, after: (id: string) => void) {
    lastCreate = req;
    creating = true;
    createError = null;
    try {
      if (sources.locations === 'live') {
        const res = await api.POST('/api/locations', { body: req });
        if (!res.response.ok || !res.data) throw new ApiError(res.response.status, (res.error as { error?: { code?: string; message?: string } })?.error as never);
        const tree = await api.GET('/api/locations');
        if (tree.data) roots = tree.data.roots;
        after(res.data.id);
      } else {
        await new Promise((r) => setTimeout(r, 500));
        const all: LocationNode[] = [];
        const walk = (ns: LocationNode[]) => ns.forEach((n) => (all.push(n), walk(n.children)));
        walk(roots);
        if (req.code && all.some((n) => n.code?.toLowerCase() === req.code!.toLowerCase())) {
          throw new ApiError(409, { code: 'conflict', message: `Code ${req.code} is already used` });
        }
        const parent = req.parent ? findNode(roots, req.parent) : null;
        const id = `L-new-${Date.now()}`;
        const now = new Date().toISOString();
        const node: LocationNode = {
          id,
          code: req.code ?? null,
          name: req.name,
          kind: req.kind,
          parent_id: parent?.id ?? null,
          cover_media_id: null,
          notes: null,
          layout: null,
          archived_at: null,
          created_at: now,
          updated_at: now,
          label: parent ? `${parent.label} › ${req.code ?? req.name}` : req.name,
          cover: null,
          item_count: 0,
          total_count: 0,
          staged_count: 0,
          children: []
        };
        if (parent) parent.children.push(node);
        else roots.push(node);
        after(id);
      }
      toast(`Created ${req.code ?? req.name}`, { tone: 'success' });
    } catch (e) {
      createError = e;
    } finally {
      creating = false;
    }
  }

  let sheetSide = $state<'auto' | 'bottom' | 'right' | 'center' | null>(null);
  let sheetOpen = $state(false);

  let retrying = $state(false);
  const demoError = new ApiError(501, { code: 'not_implemented', message: 'GET /api/items is not implemented yet' });

  // ---------------------------------------------------------------- previews
  let phoneW = $state(0);
  let deskW = $state(0);
  const previewSrc = $derived(`/dev/kit/preview?theme=${theme}${live ? '&live=1' : ''}`);
  const phoneScale = $derived(phoneW ? Math.min(1, phoneW / 390) : 1);
  const deskScale = $derived(deskW ? Math.min(1, deskW / 1280) : 1);

  const sections = [
    ['buttons', 'Buttons'],
    ['chips', 'Chips & badges'],
    ['inputs', 'Search & tabs'],
    ['location-chip', 'Location chip'],
    ['cards', 'Item card'],
    ['grid', 'Photo grid'],
    ['gallery', 'Gallery'],
    ['filters', 'Filter sheet'],
    ['picker', 'Location picker'],
    ['sheets', 'Sheets'],
    ['states', 'States & toasts'],
    ['previews', 'Phone & desktop']
  ] as const;

  const statusList = [
    ['placed', placements.placed()],
    ['placed (room)', placements.room()],
    ['approximate', placements.approximate()],
    ['staged', placements.staged()],
    ['staged (from unknown)', placements.stagedFromUnknown()],
    ['unknown', placements.unknown()],
    ['gone', placements.gone()]
  ] as const;

  const cardCases: [string, ItemSummary][] = [
    ['photo', fixtureItems[0]],
    ['no photo', fixtureItems[5]],
    ['quantity ×4', fixtureItems[3]],
    ['needs review', fixtureItems[4]],
    ['gone', fixtureItems[8]],
    ['variant pending', fixtureItems[9]],
    ['variant failed', fixtureItems[11]],
    ['long title', fixtureItems[13]]
  ];
  let cardSel = $state(true);
</script>

<svelte:head><title>UI kit · Personal Inventory</title></svelte:head>

{#snippet section(id: string, title: string, body: Snippet, note?: string)}
  <section {id} aria-labelledby="{id}-h">
    <h2 id="{id}-h">{title}</h2>
    {#if note}<p class="muted note">{note}</p>{/if}
    {@render body()}
  </section>
{/snippet}

{#snippet segmented(name: string, options: readonly (readonly [string, string])[], current: string, set: (v: string) => void)}
  <div class="seg" role="radiogroup" aria-label={name}>
    {#each options as [v, label] (v)}
      <button type="button" role="radio" aria-checked={current === v} onclick={() => set(v)}>{label}</button>
    {/each}
  </div>
{/snippet}

<div class="page kit">
  <header class="head">
    <div class="title">
      <h1>UI kit</h1>
      <p class="muted">Thread D components on tokens.css. Fixtures unless “Live data” finds the endpoints.</p>
    </div>
    <div class="controls">
      {@render segmented('Theme', [['system', 'System'], ['light', 'Light'], ['dark', 'Dark']] as const, theme, (v) => (theme = v as Theme))}
      <label class="switch">
        <input type="checkbox" checked={live} onchange={(e) => setLive(e.currentTarget.checked)} />
        <span>Live data</span>
      </label>
    </div>
    <dl class="sources">
      {#each Object.entries(sources) as [k, v] (k)}
        <div><dt>{k}</dt><dd class:live={v === 'live'}>{#if v === 'loading'}<Spinner size={12} />{/if} {v}</dd></div>
      {/each}
    </dl>
    <nav class="toc" aria-label="Sections">
      {#each sections as [id, label] (id)}<a href="#{id}">{label}</a>{/each}
    </nav>
  </header>

  <!-- ------------------------------------------------------------ buttons -->
  {#snippet buttons()}
    <div class="stack">
      {#each ['primary', 'secondary', 'ghost', 'danger'] as const as v (v)}
        <div class="row">
          <span class="lbl">{v}</span>
          <Button variant={v} size="sm">Small</Button>
          <Button variant={v}>Medium</Button>
          <Button variant={v} size="lg">Large</Button>
          <Button variant={v} icon="plus">With icon</Button>
          <Button variant={v} icon="filter" label="Icon only" />
          <Button variant={v} loading>Saving</Button>
          <Button variant={v} disabled>Disabled</Button>
        </div>
      {/each}
      <div class="row">
        <span class="lbl">link</span>
        <Button href="#buttons" variant="secondary" iconEnd="chevron-right">As a link</Button>
        <Button block variant="primary">Block (full width)</Button>
      </div>
    </div>
  {/snippet}
  {@render section('buttons', 'Buttons', buttons, 'sm is 36px visually but keeps a --tap-min hit area.')}

  <!-- ------------------------------------------------------------ chips -->
  {#snippet chips()}
    <div class="stack">
      <div class="row">
        <span class="lbl">toggle</span>
        {#each [['clothing', 'Clothing', 40], ['object', 'Object', 11], ['utility', 'Utility', 0]] as const as [v, l, c] (v)}
          <Chip selected={!!chipOn[v]} count={c} onclick={() => (chipOn[v] = !chipOn[v])}>{l}</Chip>
        {/each}
      </div>
      <div class="row">
        <span class="lbl">static/link</span>
        <Chip>outerwear</Chip>
        <Chip icon="placed" href="#chips">Unit 2</Chip>
        <Chip onremove={() => toast('Removed filter')} removeLabel="Remove filter color: navy">navy</Chip>
      </div>
      <div class="row">
        <span class="lbl">badges</span>
        <Badge>neutral</Badge>
        <Badge tone="accent">accent</Badge>
        <Badge tone="info">info</Badge>
        <Badge tone="success" icon="check">success</Badge>
        <Badge tone="warning" icon="review">Review</Badge>
        <Badge tone="danger" icon="alert">danger</Badge>
        <Badge label="Quantity 4">×4</Badge>
      </div>
      <div class="row">
        <span class="lbl">provenance</span>
        <ProvenanceBadge basis="user" />
        <ProvenanceBadge basis="observed" confidence={1} />
        <ProvenanceBadge basis="looked_up" confidence={0.6} />
        <ProvenanceBadge basis="inferred" confidence={0.8} />
        <ProvenanceBadge basis="inferred" confidence={0.35} compact />
        <ProvenanceBadge basis="looked_up" confidence={0.6} onclick={() => toast('Open provenance')} />
      </div>
    </div>
  {/snippet}
  {@render section('chips', 'Chips & badges', chips)}

  <!-- ------------------------------------------------------------ inputs -->
  {#snippet inputs()}
    <div class="stack narrow">
      <SearchBox bind:value={q} placeholder="Search items, tags, codes…" onsearch={(v) => (lastSearch = v)} />
      <p class="muted small">onsearch (debounced 250 ms, Enter, clear, Esc): “{lastSearch}”</p>
      <SearchBox value="navy" busy label="Busy search" />
      <Tabs
        label="Demo tabs"
        bind:value={tab}
        tabs={[
          { id: 'one', label: 'Flat lay', count: 2 },
          { id: 'two', label: 'Labels', count: 3 },
          { id: 'three', label: 'Detail' }
        ]}
      >
        {#snippet panel(id)}<p class="muted">Panel for “{id}”. Arrow keys, Home and End move between tabs.</p>{/snippet}
      </Tabs>
    </div>
  {/snippet}
  {@render section('inputs', 'Search & tabs', inputs)}

  <!-- ------------------------------------------------------------ location chip -->
  {#snippet locchip()}
    <div class="table" role="table" aria-label="Location chip variants">
      <div class="tr th" role="row"><span role="columnheader">status</span><span role="columnheader">compact</span><span role="columnheader">full</span></div>
      {#each statusList as [name, p] (name)}
        <div class="tr" role="row">
          <span class="lbl" role="cell">{name}</span>
          <span role="cell"><LocationChip placement={p} /></span>
          <span role="cell"><LocationChip placement={p} variant="full" /></span>
        </div>
      {/each}
      <div class="tr" role="row">
        <span class="lbl" role="cell">link / button</span>
        <span role="cell"><LocationChip placement={placements.placed()} href="/locations/L-b014" /></span>
        <span role="cell"><LocationChip placement={placements.staged()} variant="full" onclick={() => toast('Open move sheet')} /></span>
      </div>
    </div>
  {/snippet}
  {@render section('location-chip', 'Location chip', locchip)}

  <!-- ------------------------------------------------------------ cards -->
  {#snippet cards()}
    <label class="switch"><input type="checkbox" bind:checked={cardSel} /><span>Show selected card</span></label>
    <div class="cards">
      {#each cardCases as [name, it] (name)}
        <figure>
          <ItemCard item={it} href="#cards" />
          <figcaption class="lbl">{name}</figcaption>
        </figure>
      {/each}
      {#if cardSel}
        <figure>
          <ItemCard item={fixtureItems[1]} selectable selected ontoggle={() => (cardSel = false)} />
          <figcaption class="lbl">selected</figcaption>
        </figure>
        <figure>
          <ItemCard item={fixtureItems[6]} selectable />
          <figcaption class="lbl">selectable</figcaption>
        </figure>
      {/if}
    </div>
  {/snippet}
  {@render section('cards', 'Item card', cards, 'Placeholder (kind icon + category) when cover is null, the variant is not ready, or the thumb fails to load.')}

  <!-- ------------------------------------------------------------ grid -->
  {#snippet grid()}
    <div class="row">
      {@render segmented('Grid state', [['data', 'Data'], ['loading', 'Loading'], ['empty', 'Empty'], ['error', 'Error']] as const, gridState, (v) => (gridState = v as GridState))}
      <Button
        size="sm"
        variant={selecting ? 'primary' : 'secondary'}
        icon="check"
        onclick={() => {
          selecting = !selecting;
          if (!selecting) selected = [];
        }}>{selecting ? 'Selecting' : 'Select'}</Button
      >
    </div>
    <div class="gridwrap">
      {#if gridState === 'loading'}
        <LoadingState variant="grid" count={6} label="Loading items" />
      {:else if gridState === 'error'}
        <ErrorState error={demoError} onretry={() => (gridState = 'data')} />
      {:else}
        <PhotoGrid items={gridState === 'empty' ? [] : items} selectable={selecting} bind:selected hrefFor={() => '#grid'}>
          {#snippet empty()}
            <EmptyState title="No items match" message="Try fewer filters or a different search.">
              <Button onclick={() => (gridState = 'data')}>Clear filters</Button>
            </EmptyState>
          {/snippet}
        </PhotoGrid>
      {/if}
    </div>
    {#if selecting}
      <MultiSelectBar
        count={selected.length}
        total={items.length}
        alwaysShow
        onselectall={() => (selected = items.map((i) => i.id))}
        onclear={() => {
          if (selected.length) selected = [];
          else selecting = false;
        }}
      >
        {#snippet actions()}
          <Button size="sm" variant="secondary" disabled={!selected.length} onclick={() => toast(`Stage ${selected.length} items…`)}>Stage…</Button>
          <Button size="sm" variant="primary" disabled={!selected.length} onclick={() => (packOpen = true)}>Pack into box…</Button>
        {/snippet}
      </MultiSelectBar>
    {/if}
  {/snippet}
  {@render section('grid', 'Photo grid', grid, 'Selection mode: tap toggles, shift-click selects a range. The bar sits above the phone tab bar.')}

  <!-- ------------------------------------------------------------ gallery -->
  {#snippet gallery()}
    <div class="two-col">
      <div>
        <h3 class="sub">All roles</h3>
        <Gallery media={fixtureGalleryMedia} title="Navy shell jacket" />
      </div>
      <div class="stack">
        <h3 class="sub">One photo</h3>
        <Gallery media={fixtureGalleryMedia.slice(3, 4)} title="Stock only" />
        <h3 class="sub">No photos</h3>
        <Gallery media={[]} />
      </div>
    </div>
  {/snippet}
  {@render section('gallery', 'Gallery', gallery, 'Tap the photo for the full-screen viewer: swipe or ←/→, Esc closes, “Original” opens orig_url.')}

  <!-- ------------------------------------------------------------ filters -->
  {#snippet filtersDemo()}
    <ActiveFilters bind:value={filters} {facets} onopen={() => (filterOpen = true)} onchange={refreshFacets} />
    <pre class="out">state: {JSON.stringify(filters)}
query: {filterParams(filters, { q: q || undefined }).toString() || '(none)'}</pre>
    <FilterSheet
      {facets}
      bind:value={filters}
      bind:open={filterOpen}
      loading={facetsLoading}
      onchange={refreshFacets}
      onapply={(s) => toast(`Apply ${Object.keys(s).length} filter groups`)}
    />
  {/snippet}
  {@render section('filters', 'Filter sheet', filtersDemo, 'Bottom sheet on phone, side panel from 720px. Location and review are single-valued.')}

  <!-- ------------------------------------------------------------ picker -->
  {#snippet picker()}
    <div class="two-col">
      <div class="card-box picker-box">
        <LocationPicker
          {roots}
          bind:value={pickedId}
          oncreate={(req) => createLocation(req, (id) => (pickedId = id))}
          {creating}
          {createError}
        />
      </div>
      <div class="stack">
        <p>Selected: <code>{pickedId ?? 'none'}</code></p>
        {#if lastCreate}<pre class="out">oncreate: {JSON.stringify(lastCreate, null, 1)}</pre>{/if}
        <Button icon="container" onclick={() => (packOpen = true)}>Pack into box… (sheet, boxes only)</Button>
        {#if packTarget}<p class="muted">Packed into <code>{packTarget}</code></p>{/if}
      </div>
    </div>
    <Sheet bind:open={packOpen} title="Pack into box" side="auto">
      <LocationPicker
        {roots}
        bind:value={packTarget}
        canSelect={(n) => n.kind === 'container'}
        oncreate={(req) => createLocation({ ...req, kind: req.kind }, (id) => (packTarget = id))}
        {creating}
        {createError}
        label="Boxes"
      />
      {#snippet footer()}
        <Button variant="ghost" onclick={() => (packOpen = false)}>Cancel</Button>
        <Button
          variant="primary"
          disabled={!packTarget}
          onclick={() => {
            packOpen = false;
            toast(`Packed ${selected.length || 1} item(s) into ${packTarget}`, { tone: 'success', action: { label: 'Undo', run: () => toast('Undone') } });
          }}>Pack</Button
        >
      {/snippet}
    </Sheet>
  {/snippet}
  {@render section('picker', 'Location picker', picker, 'Arrow keys move, → expands, ← collapses or goes to the parent, Enter selects; ↓ from the search box enters the tree. Try creating a duplicate code (B-014).')}

  <!-- ------------------------------------------------------------ sheets -->
  {#snippet sheets()}
    <div class="row">
      {#each ['auto', 'bottom', 'right', 'center'] as const as s (s)}
        <Button
          onclick={() => {
            sheetSide = s;
            sheetOpen = true;
          }}>{s}</Button
        >
      {/each}
    </div>
    {#if sheetSide}
      <Sheet bind:open={sheetOpen} side={sheetSide} title="Sheet · {sheetSide}" size={sheetSide === 'center' ? 'sm' : 'md'}>
        <div class="stack">
          <p>Focus is trapped here; Esc or the backdrop closes; focus returns to the button.</p>
          <label class="field"><span>A field</span><input placeholder="Type here" /></label>
          {#each { length: 6 }, i (i)}<p class="muted">Scrollable content line {i + 1}.</p>{/each}
        </div>
        {#snippet footer()}
          <Button variant="ghost" onclick={() => (sheetOpen = false)}>Cancel</Button>
          <Button variant="primary" onclick={() => (sheetOpen = false)}>Done</Button>
        {/snippet}
      </Sheet>
    {/if}
  {/snippet}
  {@render section('sheets', 'Sheets', sheets)}

  <!-- ------------------------------------------------------------ states -->
  {#snippet states()}
    <div class="row">
      <Button onclick={() => toast('Saved')}>Toast</Button>
      <Button onclick={() => toast('Moved 3 items to B-014', { tone: 'success', action: { label: 'Undo', run: () => toast('Undone') } })}>Success + action</Button>
      <Button variant="danger" onclick={() => toast('Could not reach the server', { tone: 'error' })}>Error toast</Button>
    </div>
    <div class="states">
      <div class="card-box"><EmptyState title="Nothing here yet" message="Add photos from your camera roll to get started."><Button variant="primary" icon="plus">Add item</Button></EmptyState></div>
      <div class="card-box"><EmptyState compact icon="search" title="No results" message="Compact variant." /></div>
      <div class="card-box"><LoadingState variant="spinner" /></div>
      <div class="card-box pad"><LoadingState variant="list" count={3} /><p><LoadingState variant="inline" label="Refreshing…" /></p></div>
      <div class="card-box">
        <ErrorState
          error={demoError}
          {retrying}
          onretry={() => {
            retrying = true;
            setTimeout(() => (retrying = false), 900);
          }}
        />
      </div>
      <div class="card-box"><ErrorState compact title="Could not load facets" error={new Error('network error')} /></div>
    </div>
  {/snippet}
  {@render section('states', 'States & toasts', states)}

  <!-- ------------------------------------------------------------ previews -->
  {#snippet previews()}
    <div class="previews">
      <figure class="frame phone" bind:clientWidth={phoneW}>
        <div class="viewport" style:height="{780 * phoneScale}px">
          <iframe title="Phone preview (390 × 780)" src={previewSrc} width="390" height="780" style:transform="scale({phoneScale})"></iframe>
        </div>
        <figcaption class="lbl">Phone · 390 × 780</figcaption>
      </figure>
      <figure class="frame desk" bind:clientWidth={deskW}>
        <div class="viewport" style:height="{800 * deskScale}px">
          <iframe title="Desktop preview (1280 × 800)" src={previewSrc} width="1280" height="800" style:transform="scale({deskScale})"></iframe>
        </div>
        <figcaption class="lbl">Desktop · 1280 × 800 (scaled)</figcaption>
      </figure>
    </div>
  {/snippet}
  {@render section('previews', 'Phone & desktop', previews, 'A small “All items” screen built from the kit, inside the real app shell, at both widths.')}
</div>


<style>
  .kit {
    display: grid;
    gap: var(--space-7);
  }
  .head {
    display: grid;
    gap: var(--space-3);
  }
  .controls {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-3);
  }
  .sources {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-1) var(--space-4);
    margin: 0;
    font-size: var(--text-xs);
  }
  .sources div {
    display: flex;
    gap: var(--space-1);
  }
  .sources dt {
    color: var(--color-text-muted);
  }
  .sources dd {
    margin: 0;
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    color: var(--status-unknown);
  }
  .sources dd.live {
    color: var(--color-success);
  }
  .toc {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-1) var(--space-3);
    font-size: var(--text-sm);
  }
  .toc a {
    display: inline-flex;
    align-items: center;
    min-height: var(--tap-min);
  }
  section {
    display: grid;
    gap: var(--space-4);
    scroll-margin-top: calc(var(--topbar-height) + var(--space-4));
    min-width: 0;
  }
  .note {
    margin-top: calc(var(--space-3) * -1);
    font-size: var(--text-sm);
  }
  .sub {
    font-size: var(--text-md);
    margin-bottom: var(--space-2);
  }
  .stack {
    display: grid;
    gap: var(--space-3);
    min-width: 0;
  }
  .narrow {
    max-width: 560px;
  }
  .row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
  }
  .lbl {
    min-width: 84px;
    color: var(--color-text-muted);
    font-size: var(--text-xs);
    font-family: var(--font-mono);
  }
  .small {
    font-size: var(--text-sm);
  }
  .seg {
    display: inline-flex;
    padding: var(--space-0);
    border-radius: var(--radius-md);
    background: var(--color-surface-2);
  }
  .seg button {
    min-height: calc(var(--tap-min) - var(--space-1));
    padding: 0 var(--space-3);
    border: 0;
    border-radius: var(--radius-sm);
    background: transparent;
    color: var(--color-text-muted);
    font-size: var(--text-sm);
    font-weight: var(--weight-medium);
    cursor: pointer;
  }
  .seg button[aria-checked='true'] {
    background: var(--color-surface);
    color: var(--color-text);
    box-shadow: var(--shadow-sm);
  }
  .switch {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    min-height: var(--tap-min);
    font-size: var(--text-sm);
    font-weight: var(--weight-medium);
    cursor: pointer;
  }
  .switch input {
    width: 20px;
    height: 20px;
    accent-color: var(--color-accent);
  }
  .table {
    display: grid;
    gap: var(--space-2);
  }
  .tr {
    display: grid;
    grid-template-columns: minmax(84px, 140px) minmax(0, 1fr) minmax(0, 1.4fr);
    align-items: center;
    gap: var(--space-3);
  }
  .tr > span {
    min-width: 0;
  }
  .th {
    color: var(--color-text-faint);
    font-size: var(--text-xs);
  }
  .cards {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(min(var(--grid-card-min), calc(50% - var(--space-3) / 2)), 1fr));
    gap: var(--space-4) var(--space-3);
  }
  figure {
    margin: 0;
    display: grid;
    gap: var(--space-1);
    min-width: 0;
  }
  .gridwrap {
    min-height: 200px;
  }
  .two-col {
    display: grid;
    gap: var(--space-5);
  }
  @media (min-width: 720px) {
    .two-col {
      grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
    }
  }
  .card-box {
    border: var(--border-width) solid var(--color-border);
    border-radius: var(--radius-lg);
    background: var(--color-surface);
    min-width: 0;
  }
  .card-box.pad,
  .picker-box {
    padding: var(--space-4);
  }
  .picker-box {
    --picker-max-height: 420px;
  }
  .states {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(min(100%, 300px), 1fr));
    gap: var(--space-3);
  }
  .out {
    margin: 0;
    padding: var(--space-3);
    border-radius: var(--radius-md);
    background: var(--color-surface-2);
    font-size: var(--text-xs);
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  .field {
    display: grid;
    gap: var(--space-1);
    font-size: var(--text-sm);
  }
  .field input {
    min-height: var(--tap-min);
    padding: 0 var(--space-3);
    border: var(--border-width) solid var(--color-border-strong);
    border-radius: var(--radius-md);
    background: var(--color-surface);
  }
  .previews {
    display: grid;
    gap: var(--space-5);
    align-items: start;
  }
  @media (min-width: 1100px) {
    .previews {
      grid-template-columns: 390px minmax(0, 1fr);
    }
  }
  .frame .viewport {
    position: relative;
    overflow: hidden;
    border: var(--border-width) solid var(--color-border-strong);
    border-radius: var(--radius-lg);
    background: var(--color-bg);
    box-shadow: var(--shadow-md);
  }
  .phone {
    max-width: 390px;
  }
  .frame iframe {
    position: absolute;
    top: 0;
    left: 0;
    border: 0;
    transform-origin: 0 0;
  }
</style>
