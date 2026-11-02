<script lang="ts">
  // "What's in this box?" (PLAN.md §8): big label code, name, path, photos,
  // children, contents grid (deep toggle, multi-select → move / stage),
  // staged-to-here, recent movements, edit, archive, Move box, Print label.
  import { page } from '$app/state';
  import { api, ApiError, unwrap, uploadMedia, type ItemSummary, type LocationDetail, type LocationNode } from '$lib/api/client';
  import Button from '$lib/components/Button.svelte';
  import Chip from '$lib/components/Chip.svelte';
  import EmptyState from '$lib/components/EmptyState.svelte';
  import ErrorState from '$lib/components/ErrorState.svelte';
  import Icon from '$lib/components/Icon.svelte';
  import LoadingState from '$lib/components/LoadingState.svelte';
  import MediaViewer from '$lib/components/MediaViewer.svelte';
  import MultiSelectBar from '$lib/components/MultiSelectBar.svelte';
  import PhotoGrid from '$lib/components/PhotoGrid.svelte';
  import Sheet from '$lib/components/Sheet.svelte';
  import { errorMessage, LOCATION_KIND_LABEL, roleLabel } from '$lib/components/format';
  import { hasVariant } from '$lib/components/gallery';
  import { LOCATION_KIND_ICON } from '$lib/components/icons';
  import { toast } from '$lib/components/toast.svelte';
  import LocationFormSheet from '../../shared/LocationFormSheet.svelte';
  import PickLocationSheet from '../../shared/PickLocationSheet.svelte';
  import { archiveBlockers, canMoveInto, describeMovement, locationParam, plural, relTime, shortDateTime } from '../../shared/places';
  import { invalidateTree, loadTree, moveItems, postMove, tree } from '../../shared/store.svelte';

  const ref = $derived(locationParam(page.params.id ?? ''));

  let loc = $state<LocationDetail | null>(null);
  let loading = $state(true);
  let error = $state<unknown>(null);
  let deep = $state(false);

  async function load() {
    error = null;
    try {
      loc = unwrap(await api.GET('/api/locations/{id}', { params: { path: { id: ref }, query: { deep } } }));
    } catch (e) {
      error = e;
    } finally {
      loading = false;
    }
  }

  let lastRef = '';
  $effect(() => {
    const r = ref;
    if (r !== lastRef) {
      lastRef = r;
      loading = true;
      loc = null;
      deep = false;
      selecting = false;
      selected = [];
      showAllMoves = false;
    }
    void deep;
    void load();
  });

  const isBox = $derived(loc?.kind === 'container' || loc?.kind === 'shelf');
  const crumbs = $derived(loc ? loc.path.slice(0, -1) : []);
  const titles = $derived(new Map([...(loc?.items ?? []), ...(loc?.staged_items ?? [])].map((i) => [i.id, i.title])));
  let showAllMoves = $state(false);
  const moves = $derived(loc ? (showAllMoves ? loc.movements : loc.movements.slice(0, 6)) : []);

  // ---- photos
  const photos = $derived((loc?.media ?? []).map((l) => ({ media: l.media, role: l.role, caption: l.caption })));
  let viewerOpen = $state(false);
  let viewerIndex = $state(0);
  let fileInput = $state<HTMLInputElement>();
  let uploadRole = $state('box_exterior');
  let uploading = $state<{ i: number; n: number; p: number } | null>(null);

  function pickPhotos(role: string) {
    uploadRole = role;
    fileInput?.click();
  }
  async function onfiles(e: Event) {
    const input = e.currentTarget as HTMLInputElement;
    const files = [...(input.files ?? [])];
    input.value = '';
    if (!loc || !files.length) return;
    const id = loc.id;
    let ok = 0;
    for (let i = 0; i < files.length; i++) {
      uploading = { i: i + 1, n: files.length, p: 0 };
      try {
        await uploadMedia(files[i], {
          link: { subject_type: 'location', subject_id: id, role: uploadRole },
          onProgress: (p) => uploading && (uploading = { ...uploading, p })
        });
        ok++;
      } catch (err) {
        toast(`${files[i].name}: ${errorMessage(err)}`, { tone: 'error' });
      }
    }
    uploading = null;
    if (ok) toast(`Added ${plural(ok, 'photo')}`, { tone: 'success' });
    await load();
    // variants are generated in the background; refresh once more shortly
    if (loc?.media.some((m) => m.media.variant_status === 'pending')) setTimeout(load, 2500);
  }

  // ---- selection
  let selecting = $state(false);
  let selected = $state<string[]>([]);
  const selectedItems = $derived((loc?.items ?? []).filter((i) => selected.includes(i.id)));

  // ---- sheets
  let editOpen = $state(false);
  let newChildOpen = $state(false);
  let moveBoxOpen = $state(false);
  let moveItemsOpen = $state(false);
  let stageOpen = $state(false);
  let archiveOpen = $state(false);
  let archiveBusy = $state(false);
  let archiveError = $state<string>('');

  function openMoveBox() {
    void loadTree(true);
    moveBoxOpen = true;
  }

  async function moveBox(to: string | null, node: LocationNode | null) {
    if (!loc) return;
    const prevParent = loc.parent_id;
    const id = loc.id;
    const res = await postMove({ action: 'move', location_id: id, to });
    toast(res.moved ? `Moved ${loc.code ?? loc.name} to ${node?.label ?? 'top level'}` : 'Already there', {
      tone: 'success',
      timeout: 8000,
      action: res.moved
        ? {
            label: 'Undo',
            run: async () => {
              try {
                await postMove({ action: 'move', location_id: id, to: prevParent, note: 'undo' });
                toast('Undone');
              } catch (e) {
                toast(errorMessage(e), { tone: 'error' });
              }
              await load();
            }
          }
        : undefined
    });
    await load();
  }

  async function moveSelected(to: string | null, node: LocationNode | null) {
    const prev = selectedItems;
    await moveItems({ action: 'move', item_ids: prev.map((i) => i.id), to }, prev, `Moved ${plural(prev.length, 'item')} to ${node?.label ?? 'unknown'}`, load);
    selected = [];
    selecting = false;
  }

  async function stageSelected(to: string | null, node: LocationNode | null) {
    if (!to) return;
    const prev = selectedItems;
    await moveItems({ action: 'stage', item_ids: prev.map((i) => i.id), to }, prev, `Staged ${plural(prev.length, 'item')} to go to ${node?.label ?? ''}`, load);
    selected = [];
    selecting = false;
  }

  async function archive() {
    if (!loc) return;
    archiveBusy = true;
    archiveError = '';
    try {
      unwrap(await api.DELETE('/api/locations/{id}', { params: { path: { id: loc.id } } }));
      invalidateTree();
      archiveOpen = false;
      toast(`Archived ${loc.code ?? loc.name}`, { tone: 'success' });
      await load();
    } catch (e) {
      const why = e instanceof ApiError && e.status === 409 ? archiveBlockers(e.details) : '';
      archiveError = why ? `It still has ${why}. Move or unstage them first.` : errorMessage(e);
    } finally {
      archiveBusy = false;
    }
  }

  async function unarchive() {
    if (!loc) return;
    try {
      unwrap(await api.PATCH('/api/locations/{id}', { params: { path: { id: loc.id } }, body: { archived: false } }));
      invalidateTree();
      toast('Restored', { tone: 'success' });
      await load();
    } catch (e) {
      toast(errorMessage(e), { tone: 'error' });
    }
  }

  async function setCover(mediaId: string) {
    if (!loc) return;
    try {
      unwrap(await api.PATCH('/api/locations/{id}', { params: { path: { id: loc.id } }, body: { cover_media_id: mediaId } }));
      toast('Cover set', { tone: 'success' });
      await load();
    } catch (e) {
      toast(errorMessage(e), { tone: 'error' });
    }
  }
</script>

<svelte:head><title>{loc ? `${loc.code ? loc.code + ' · ' : ''}${loc.name}` : 'Place'} · Personal Inventory</title></svelte:head>

<input bind:this={fileInput} type="file" accept="image/*,image/heic,image/heif" multiple hidden onchange={onfiles} />

<div class="page loc">
  {#if loading}
    <LoadingState variant="spinner" label="Loading place…" />
  {:else if error || !loc}
    <ErrorState {error} title={error instanceof ApiError && error.status === 404 ? 'No such place' : 'Could not load this place'} onretry={load}>
      <Button href="/locations">All places</Button>
    </ErrorState>
  {:else}
    <nav class="crumbs" aria-label="Path">
      <a href="/locations">Places</a>
      {#each crumbs as c (c.id)}
        <span aria-hidden="true">›</span>
        <a href="/locations/{c.id}">{c.code ?? c.name}</a>
      {/each}
    </nav>

    <header class="hero">
      <div class="ident">
        {#if loc.code}<div class="code mono" aria-label="Label code {loc.code}">{loc.code}</div>{/if}
        <h1 class:solo={!loc.code}>{loc.name}</h1>
        <p class="meta muted">
          <Icon name={LOCATION_KIND_ICON[loc.kind] ?? 'container'} size={16} />
          {LOCATION_KIND_LABEL[loc.kind]}
          {#if crumbs.length}· in {crumbs[crumbs.length - 1].name}{/if}
          · {plural(loc.total_count, 'item')}{#if loc.staged_count}<span class="staged"> · +{loc.staged_count} staged</span>{/if}
        </p>
        {#if loc.notes}<p class="notes">{loc.notes}</p>{/if}
      </div>
      {#if loc.cover && hasVariant(loc.cover, 'thumb')}
        <button type="button" class="cover" onclick={() => ((viewerIndex = Math.max(0, photos.findIndex((p) => p.media.id === loc?.cover?.id))), (viewerOpen = true))}>
          <img src={loc.cover.thumb_url} alt="Photo of {loc.name}" />
        </button>
      {/if}
    </header>

    <div class="actions">
      {#if !loc.archived_at}
        <Button variant="primary" icon="staged" onclick={openMoveBox}>Move {isBox ? 'box' : 'this'}</Button>
      {/if}
      <Button icon="download" href="/locations/{loc.id}/label">Print label</Button>
      {#if loc.kind !== 'container' && !loc.archived_at}
        <Button variant="ghost" icon="plus" onclick={() => (newChildOpen = true)}>New inside</Button>
      {/if}
      <Button variant="ghost" onclick={() => (editOpen = true)}>Edit</Button>
      {#if loc.archived_at}
        <Button variant="ghost" onclick={unarchive}>Restore</Button>
      {:else}
        <Button variant="ghost" onclick={() => ((archiveError = ''), (archiveOpen = true))}>Archive</Button>
      {/if}
    </div>

    {#if loc.archived_at}
      <p class="banner" role="status"><Icon name="info" size={18} /> Archived {relTime(loc.archived_at)}. It is hidden from pickers and the tree.</p>
    {/if}

    <!-- photos -->
    <section aria-labelledby="ph">
      <div class="sec-head">
        <h2 id="ph">Photos</h2>
        <div class="row-btns">
          {#if isBox}
            <Button size="sm" icon="image" disabled={!!uploading} onclick={() => pickPhotos('box_exterior')}>Outside</Button>
            <Button size="sm" icon="image" disabled={!!uploading} onclick={() => pickPhotos('box_contents')}>Contents</Button>
          {:else}
            <Button size="sm" icon="image" disabled={!!uploading} onclick={() => pickPhotos('location')}>Add photo</Button>
          {/if}
        </div>
      </div>
      {#if uploading}
        <p class="muted progress" role="status">Uploading {uploading.i} of {uploading.n} · {Math.round(uploading.p * 100)}%</p>
      {/if}
      {#if photos.length}
        <ul class="strip">
          {#each photos as p, i (p.media.id + p.role)}
            <li>
              <button type="button" class="thumb" onclick={() => ((viewerIndex = i), (viewerOpen = true))} aria-label="Open {roleLabel(p.role)} photo">
                {#if hasVariant(p.media, 'thumb')}
                  <img src={p.media.thumb_url} alt="" loading="lazy" />
                {:else}
                  <span class="ph-pending"><Icon name="image" size={24} /></span>
                {/if}
              </button>
              <span class="cap">
                {roleLabel(p.role)}
                {#if loc.cover_media_id === p.media.id}· cover{:else if hasVariant(p.media, 'thumb')}<button type="button" class="link" onclick={() => setCover(p.media.id)}>Make cover</button>{/if}
              </span>
            </li>
          {/each}
        </ul>
      {:else}
        <p class="muted small">No photos yet. A shot of the outside helps you spot the box; one of the contents helps you remember what's in it.</p>
      {/if}
    </section>

    <!-- children -->
    {#if loc.children.length}
      <section aria-labelledby="ch">
        <h2 id="ch">Inside {loc.code ?? loc.name} <span class="count">{loc.children.length}</span></h2>
        <ul class="children">
          {#each loc.children as c (c.id)}
            <li>
              <a href="/locations/{c.id}" class:archived={!!c.archived_at}>
                <Icon name={LOCATION_KIND_ICON[c.kind] ?? 'container'} size={20} />
                {#if c.code}<span class="ccode mono">{c.code}</span>{/if}
                <span class="cname">{c.name}</span>
                {#if c.staged_count}<span class="staged">+{c.staged_count}</span>{/if}
                <span class="ccount">{c.total_count}</span>
              </a>
            </li>
          {/each}
        </ul>
      </section>
    {/if}

    <!-- contents -->
    <section aria-labelledby="ct">
      <div class="sec-head">
        <h2 id="ct">{isBox ? "What's in it" : 'Items here'} <span class="count">{loc.items.length}</span></h2>
        <div class="row-btns">
          {#if loc.children.length}
            <Chip selected={deep} onclick={() => (deep = !deep)}>Include {loc.kind === 'container' ? 'boxes' : 'places'} inside</Chip>
          {/if}
          {#if loc.items.length}
            <Button size="sm" variant={selecting ? 'secondary' : 'ghost'} onclick={() => ((selecting = !selecting), (selected = []))}>{selecting ? 'Done' : 'Select'}</Button>
          {/if}
        </div>
      </div>
      <PhotoGrid items={loc.items} selectable={selecting} bind:selected label="Items in {loc.name}" chip={deep ? 'compact' : false}>
        {#snippet empty()}
          <EmptyState compact icon="container" title="Empty" message={loc?.children.length && !deep ? 'Nothing directly here. Turn on “include inside” to see the boxes’ contents.' : 'Nothing recorded in here yet.'}>
            <Button href="/add" icon="plus">Add an item</Button>
          </EmptyState>
        {/snippet}
      </PhotoGrid>
    </section>

    <!-- staged to here -->
    {#if loc.staged_items.length}
      <section aria-labelledby="st">
        <div class="sec-head">
          <h2 id="st">Staged to go here <span class="count">{loc.staged_items.length}</span></h2>
          <Button size="sm" variant="ghost" href="/staging" iconEnd="chevron-right">Pack</Button>
        </div>
        <PhotoGrid items={loc.staged_items} label="Staged to go to {loc.name}" />
      </section>
    {/if}

    <!-- movements -->
    {#if loc.movements.length}
      <section aria-labelledby="mv">
        <h2 id="mv">Recent moves</h2>
        <ol class="moves">
          {#each moves as m (m.id)}
            <li>
              <span class="mv-text">
                {#if m.item_id && !m.location_moved_id}<a href="/items/{m.item_id}">{describeMovement(m, loc.id, titles)}</a>{:else}{describeMovement(m, loc.id, titles)}{/if}
                {#if m.note}<span class="muted"> · {m.note}</span>{/if}
              </span>
              <time class="muted" datetime={m.at} title={shortDateTime(m.at)}>{relTime(m.at)}</time>
            </li>
          {/each}
        </ol>
        {#if loc.movements.length > moves.length}
          <Button variant="ghost" size="sm" onclick={() => (showAllMoves = true)}>Show all {loc.movements.length}</Button>
        {/if}
      </section>
    {/if}
  {/if}
</div>

{#if loc}
  {#if selecting}
    <MultiSelectBar
      count={selected.length}
      total={loc.items.length}
      alwaysShow
      onselectall={() => (selected = loc?.items.map((i) => i.id) ?? [])}
      onclear={() => {
        if (selected.length) selected = [];
        else selecting = false;
      }}
    >
      {#snippet actions()}
        <Button size="sm" variant="primary" disabled={!selected.length} onclick={() => (moveItemsOpen = true)}>Move to…</Button>
        <Button size="sm" disabled={!selected.length} onclick={() => (stageOpen = true)}>Stage for…</Button>
      {/snippet}
    </MultiSelectBar>
  {/if}

  <MediaViewer {photos} bind:index={viewerIndex} bind:open={viewerOpen} title={loc.name} />

  <LocationFormSheet bind:open={editOpen} location={loc} onsaved={() => load()} />
  <LocationFormSheet bind:open={newChildOpen} parent={loc.id} onsaved={() => load()} />

  <PickLocationSheet
    bind:open={moveBoxOpen}
    title="Move {loc.code ?? loc.name}"
    hint="Everything inside moves with it."
    value={loc.parent_id}
    canSelect={(n) => (loc ? canMoveInto(loc, n, tree.roots) : false)}
    noneLabel={loc.kind === 'site' ? 'Top level' : undefined}
    onpick={moveBox}
  />

  <PickLocationSheet bind:open={moveItemsOpen} title="Move {plural(selected.length, 'item')} to…" noneLabel="Location unknown" canSelect={(n) => !n.archived_at} onpick={moveSelected} />

  <PickLocationSheet
    bind:open={stageOpen}
    title="Stage {plural(selected.length, 'item')} for…"
    hint="They stay where they are and show up on the Packing list."
    confirmLabel="Stage"
    canSelect={(n) => !n.archived_at}
    onpick={stageSelected}
  />

  <Sheet bind:open={archiveOpen} title="Archive {loc.code ?? loc.name}?" side="center" size="sm">
    <p>Archived places are hidden from the tree and pickers. History is kept and you can restore it.</p>
    {#if archiveError}<p class="err" role="alert">{archiveError}</p>{/if}
    {#snippet footer()}
      <Button variant="ghost" onclick={() => (archiveOpen = false)}>Cancel</Button>
      <Button variant="danger" loading={archiveBusy} onclick={archive}>Archive</Button>
    {/snippet}
  </Sheet>
{/if}

<style>
  .loc {
    display: grid;
    gap: var(--space-5);
  }
  .crumbs {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-1) var(--space-2);
    font-size: var(--text-sm);
    color: var(--color-text-faint);
  }
  .crumbs a {
    display: inline-flex;
    align-items: center;
    min-height: var(--tap-min);
  }
  .hero {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: var(--space-4);
    margin-top: calc(var(--space-4) * -1);
  }
  .ident {
    min-width: 0;
    display: grid;
    gap: var(--space-1);
  }
  .code {
    font-size: clamp(2.75rem, 12vw, 4.5rem);
    font-weight: var(--weight-bold);
    line-height: 1;
    letter-spacing: 0.02em;
    word-break: break-all;
  }
  h1 {
    font-size: var(--text-xl);
  }
  h1.solo {
    font-size: var(--text-2xl);
  }
  .meta {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-1);
    font-size: var(--text-sm);
  }
  .notes {
    white-space: pre-wrap;
    font-size: var(--text-sm);
  }
  .staged {
    color: var(--status-staged);
  }
  .cover {
    flex: none;
    width: 96px;
    height: 96px;
    padding: 0;
    border: 0;
    border-radius: var(--radius-md);
    overflow: hidden;
    background: var(--color-surface-2);
    cursor: zoom-in;
  }
  .cover img {
    width: 100%;
    height: 100%;
    object-fit: cover;
  }
  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
  }
  .banner {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-3) var(--space-4);
    border-radius: var(--radius-md);
    background: var(--color-warning-soft);
    color: var(--color-warning);
    font-size: var(--text-sm);
  }
  section {
    display: grid;
    gap: var(--space-3);
    min-width: 0;
  }
  h2 {
    font-size: var(--text-lg);
  }
  .count {
    margin-left: var(--space-1);
    color: var(--color-text-faint);
    font-weight: var(--weight-regular);
  }
  .sec-head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-2);
  }
  .row-btns {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
  }
  .progress,
  .small {
    font-size: var(--text-sm);
  }
  .strip {
    display: flex;
    gap: var(--space-3);
    margin: 0;
    padding: 0 0 var(--space-1);
    list-style: none;
    overflow-x: auto;
    scroll-snap-type: x proximity;
  }
  .strip li {
    flex: none;
    display: grid;
    gap: var(--space-1);
    width: 132px;
    scroll-snap-align: start;
  }
  .thumb {
    width: 132px;
    aspect-ratio: 1;
    padding: 0;
    border: 0;
    border-radius: var(--radius-md);
    overflow: hidden;
    background: var(--color-surface-2);
    cursor: zoom-in;
  }
  .thumb img {
    width: 100%;
    height: 100%;
    object-fit: cover;
  }
  .ph-pending {
    display: grid;
    place-items: center;
    height: 100%;
    color: var(--color-text-faint);
  }
  .cap {
    font-size: var(--text-xs);
    color: var(--color-text-muted);
  }
  .link {
    padding: 0;
    border: 0;
    background: none;
    color: var(--color-accent);
    font-size: inherit;
    cursor: pointer;
  }
  .children {
    margin: 0;
    padding: 0;
    list-style: none;
    border-top: var(--border-width) solid var(--color-border);
  }
  .children a {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    min-height: calc(var(--tap-min) + var(--space-2));
    padding: 0 var(--space-2);
    border-bottom: var(--border-width) solid var(--color-border);
    color: var(--color-text);
  }
  .children a:hover {
    text-decoration: none;
    background: var(--color-surface-2);
  }
  .children a.archived {
    opacity: 0.6;
  }
  .children :global(svg) {
    color: var(--color-text-muted);
  }
  .ccode {
    font-weight: var(--weight-bold);
  }
  .cname {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--color-text-muted);
  }
  .ccode + .cname {
    color: var(--color-text-muted);
  }
  .ccount {
    font-variant-numeric: tabular-nums;
    font-weight: var(--weight-medium);
  }
  .moves {
    margin: 0;
    padding: 0;
    list-style: none;
    display: grid;
    gap: var(--space-2);
    font-size: var(--text-sm);
  }
  .moves li {
    display: flex;
    justify-content: space-between;
    gap: var(--space-3);
  }
  .moves time {
    flex: none;
    font-size: var(--text-xs);
  }
  .err {
    margin-top: var(--space-3);
    color: var(--color-danger);
    font-size: var(--text-sm);
  }
  @media (min-width: 720px) {
    .cover {
      width: 160px;
      height: 160px;
    }
  }
</style>
