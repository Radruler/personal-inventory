<script module lang="ts">
  import type { SchemaLite } from './detail';
  // /api/schema rarely changes: fetched once per app session.
  let schemaCache: SchemaLite | null = null;
</script>

<script lang="ts">
  // Item detail (PLAN.md §8): "Remember it, then find it". Where it is first,
  // then photos, what's literally on the item, what we know (with
  // provenance), product, references, purchase, sets and movement history.
  import { untrack } from 'svelte';
  import { afterNavigate } from '$app/navigation';
  import { page } from '$app/state';
  import { api, ApiError, unwrap, type ItemDetail, type ItemSummary, type Schemas } from '$lib/api/client';
  import {
    Badge,
    Button,
    Chip,
    EmptyState,
    ErrorState,
    Gallery,
    ItemCard,
    KIND_LABEL,
    LoadingState,
    ProvenanceBadge,
    Sheet,
    errorMessage,
    formatCategory,
    toast
  } from '$lib/components';
  import AddFactSheet from './AddFactSheet.svelte';
  import AttrList from './AttrList.svelte';
  import FactSheet from './FactSheet.svelte';
  import OnTheItem from './OnTheItem.svelte';
  import Photos from './Photos.svelte';
  import WherePanel from './WherePanel.svelte';
  import {
    PURCHASE_KEYS,
    actorLabel,
    attrRows,
    formatDay,
    itemRoles,
    movementSentence,
    normalizeTag,
    productAttrRows,
    type FactKey
  } from './detail';

  const id = $derived(page.params.id ?? '');

  // Where "‹ All items" leads: back to the grid we came from (with its query).
  let backHref = $state('/');
  let cameFromGrid = $state(false);
  afterNavigate(({ from, type }) => {
    cameFromGrid = type !== 'enter' && from?.url.pathname === '/';
    backHref = cameFromGrid ? `/${from!.url.search}` : '/';
  });

  // ---------------------------------------------------------------- data
  let item = $state<ItemDetail | null>(null);
  let status = $state<'loading' | 'ready' | 'notfound' | 'error'>('loading');
  let error = $state<unknown>(null);
  let schema = $state<SchemaLite | null>(schemaCache);
  let seq = 0;

  async function load(target: string, silent = false) {
    const my = ++seq;
    if (!silent) {
      status = 'loading';
      item = null;
    }
    try {
      const res = await api.GET('/api/items/{id}', { params: { path: { id: target } } });
      if (my !== seq) return;
      if (res.response.status === 404) {
        status = 'notfound';
        item = null;
        return;
      }
      item = unwrap(res);
      status = 'ready';
      error = null;
    } catch (e) {
      if (my !== seq) return;
      if (silent && item) toast(`Couldn't refresh: ${errorMessage(e)}`, { tone: 'error' });
      else {
        error = e;
        status = e instanceof ApiError && e.status === 404 ? 'notfound' : 'error';
      }
    }
  }
  const refresh = () => load(id, true);

  $effect(() => {
    const target = id;
    untrack(() => load(target));
  });

  $effect(() => {
    if (schemaCache) return;
    api
      .GET('/api/schema')
      .then((r) => (schema = schemaCache = unwrap(r) as unknown as SchemaLite))
      .catch(() => {});
  });

  // New uploads are processed in the background: poll until variants are ready.
  let polls = 0;
  $effect(() => {
    if (!item) return;
    const pending = item.media.some((l) => l.media.variant_status === 'pending');
    if (!pending) {
      polls = 0;
      return;
    }
    if (polls >= 15) return;
    const t = setTimeout(() => {
      polls++;
      refresh();
    }, 2000);
    return () => clearTimeout(t);
  });

  const keys = $derived(new Map<string, FactKey>((schema?.fact_keys ?? []).map((k) => [k.key, k])));
  const sourceLabels = $derived(new Map((schema?.observation_sources ?? []).map((s) => [s.value, s.label])));
  const schemes = $derived(new Map((schema?.identifier_schemes ?? []).map((s) => [s.scheme, s])));
  const roles = $derived(
    schema
      ? itemRoles(schema.media_roles)
      : [
          { value: 'flat_lay', label: 'Flat lay' },
          { value: 'on_body', label: 'On body' },
          { value: 'label', label: 'Label' },
          { value: 'detail', label: 'Detail' }
        ]
  );
  const knownRows = $derived(item ? attrRows(item, keys, { except: PURCHASE_KEYS }) : []);
  const purchaseRows = $derived(item ? attrRows(item, keys, { only: PURCHASE_KEYS }) : []);
  const productRows = $derived(item?.product ? productAttrRows(item.product, keys) : []);
  const references = $derived(item ? [...item.references, ...(item.product?.references ?? [])] : []);

  // ---------------------------------------------------------------- updates
  function applyCard(card: ItemSummary | null) {
    if (card && item) {
      item = {
        ...item,
        placement: card.placement,
        updated_at: card.updated_at,
        location_id: card.placement.location?.id ?? null,
        target_location_id: card.placement.target?.id ?? null
      };
    }
    refresh();
  }

  async function patch(body: Schemas['ItemPatch'], ok?: string) {
    if (!item) return false;
    try {
      item = unwrap(await api.PATCH('/api/items/{id}', { params: { path: { id: item.id } }, body }));
      if (ok) toast(ok, { tone: 'success' });
      return true;
    } catch (e) {
      toast(`Couldn't save: ${errorMessage(e)}`, { tone: 'error' });
      return false;
    }
  }

  let reviewing = $state(false);
  async function markReviewed() {
    reviewing = true;
    await patch({ review_state: 'ok' }, 'Marked reviewed');
    reviewing = false;
  }

  let restoring = $state(false);
  async function restore() {
    restoring = true;
    await patch({ lifecycle: 'owned' }, 'Restored');
    restoring = false;
  }

  // facts
  let factOpen = $state(false);
  let factKey = $state<string | null>(null);
  let addOpen = $state(false);
  let addKey = $state('');
  function openFact(key: string) {
    factKey = key;
    factOpen = true;
  }
  function openAdd(key = '') {
    addKey = key;
    addOpen = true;
  }
  function onFactWrite(res: Schemas['FactWrite']) {
    if (item) item = { ...item, attrs: res.attrs };
    refresh();
  }

  // tags
  let tagInput = $state('');
  let tagBusy = $state(false);
  async function editTags(add: string[], remove: string[]) {
    if (!item) return;
    tagBusy = true;
    try {
      const res = unwrap(await api.POST('/api/items/{id}/tags', { params: { path: { id: item.id } }, body: { add, remove } }));
      item = { ...item, tags: res.tags };
    } catch (e) {
      toast(`Couldn't change tags: ${errorMessage(e)}`, { tone: 'error' });
    } finally {
      tagBusy = false;
    }
  }
  async function addTag(e: SubmitEvent) {
    e.preventDefault();
    const tags = tagInput
      .split(',')
      .map(normalizeTag)
      .filter((t) => t && !item?.tags.includes(t));
    if (!tags.length) {
      tagInput = '';
      return;
    }
    await editTags(tags, []);
    tagInput = '';
  }

  // edit sheet
  let editOpen = $state(false);
  let eTitle = $state('');
  let eKind = $state<Schemas['Kind']>('clothing');
  let eCategory = $state('');
  let eQuantity = $state(1);
  let eNotes = $state('');
  let eError = $state('');
  let eBusy = $state(false);
  function openEdit() {
    if (!item) return;
    eTitle = item.title;
    eKind = item.kind;
    eCategory = item.category ?? '';
    eQuantity = item.quantity;
    eNotes = item.notes ?? '';
    eError = '';
    editOpen = true;
  }
  async function saveEdit(e: SubmitEvent) {
    e.preventDefault();
    if (!item) return;
    const body: Schemas['ItemPatch'] = {};
    const title = eTitle.trim();
    if (!title) {
      eError = 'A title is required.';
      return;
    }
    const q = Math.floor(Number(eQuantity));
    if (!Number.isFinite(q) || q < 1) {
      eError = 'Quantity is 1 or more.';
      return;
    }
    if (title !== item.title) body.title = title;
    if (eKind !== item.kind) body.kind = eKind;
    const cat = eCategory.trim().replace(/\s*\/\s*/g, '/') || null;
    if (cat !== (item.category ?? null)) body.category = cat;
    if (q !== item.quantity) body.quantity = q;
    const notes = eNotes.trim() ? eNotes : null;
    if (notes !== (item.notes ?? null)) body.notes = notes;
    if (!Object.keys(body).length) {
      editOpen = false;
      return;
    }
    eBusy = true;
    if (await patch(body, 'Saved')) editOpen = false;
    eBusy = false;
  }

  // gone sheet
  let goneOpen = $state(false);
  let goneReason = $state('sold');
  let goneBusy = $state(false);
  async function markGone(e: SubmitEvent) {
    e.preventDefault();
    if (!item) return;
    goneBusy = true;
    try {
      item = unwrap(await api.DELETE('/api/items/{id}', { params: { path: { id: item.id }, query: { reason: goneReason || undefined } } }));
      goneOpen = false;
      toast('Marked as gone', { tone: 'success', action: { label: 'Undo', run: restore } });
    } catch (err) {
      toast(`Couldn't mark as gone: ${errorMessage(err)}`, { tone: 'error' });
    } finally {
      goneBusy = false;
    }
  }

  const kindLabel = $derived(item ? ((KIND_LABEL as Record<string, string>)[item.kind] ?? item.kind) : '');
  const goneReasons = $derived(schema?.gone_reasons ?? ['sold', 'donated', 'discarded', 'lost', 'other']);
  const cap = (s: string) => (s ? s[0].toUpperCase() + s.slice(1) : s);
</script>

<svelte:head><title>{item ? item.title : status === 'notfound' ? 'Not found' : 'Item'} · Personal Inventory</title></svelte:head>

<div class="page detail">
  <nav class="back no-print" aria-label="Breadcrumb">
    <a
      href={backHref}
      onclick={(e) => {
        // Came from the grid: go back in history so its filters and scroll return.
        if (cameFromGrid) {
          e.preventDefault();
          history.back();
        }
      }}>‹ All items</a
    >
  </nav>

  {#if status === 'loading'}
    <LoadingState variant="spinner" label="Loading item…" />
  {:else if status === 'notfound'}
    <EmptyState icon="unknown" title="No such item" message="It may have been deleted, or the link is wrong.">
      <Button variant="primary" href="/">All items</Button>
    </EmptyState>
  {:else if status === 'error' || !item}
    <ErrorState {error} title="Couldn't load this item" onretry={() => load(id)} />
  {:else}
    <div class="layout">
      <header class="head">
        <div class="titles">
          <h1>{item.title}</h1>
          <p class="sub muted">
            <span>{kindLabel}</span>
            {#if item.category}<span>· {formatCategory(item.category)}</span>{/if}
            {#if item.quantity > 1}<span>· ×{item.quantity}</span>{/if}
            {#if item.attrs.brand}<span>· {String(item.attrs.brand)}</span>{/if}
          </p>
        </div>
        <div class="hactions">
          {#if item.review_state === 'needs_review'}
            <Badge tone="warning" icon="review">Needs review</Badge>
            <Button size="sm" variant="primary" icon="check" loading={reviewing} onclick={markReviewed}>Mark reviewed</Button>
          {/if}
          <Button size="sm" onclick={openEdit}>Edit</Button>
          {#if item.lifecycle === 'owned'}
            <Button size="sm" variant="ghost" onclick={() => (goneOpen = true)}>Mark as gone</Button>
          {/if}
        </div>
      </header>

      <div class="where">
        <WherePanel {item} onchange={applyCard} onrestore={restore} {restoring} />
      </div>

      <div class="media">
        <Photos {item} {roles} onchange={(d) => (item = d)} onrefresh={refresh} />
      </div>

      <div class="info">
        <section class="tags" aria-label="Tags">
          {#each item.tags as t (t)}
            <Chip href="/?tag={encodeURIComponent(t)}" onremove={() => editTags([], [t])} removeLabel="Remove tag {t}">{t}</Chip>
          {/each}
          <form class="tagform" onsubmit={addTag}>
            <label class="visually-hidden" for="tag-in">Add a tag</label>
            <input id="tag-in" type="text" bind:value={tagInput} placeholder="+ Add tag" autocapitalize="off" enterkeyhint="done" disabled={tagBusy} />
            {#if tagInput.trim()}<Button size="sm" type="submit" loading={tagBusy}>Add</Button>{/if}
          </form>
        </section>

        {#if item.notes}
          <p class="notes">{item.notes}</p>
        {/if}

        <section aria-labelledby="h-on">
          <h2 id="h-on">On the item</h2>
          <p class="lede muted">Exactly what's written on it.</p>
          <OnTheItem observations={item.observations} identifiers={item.identifiers} {sourceLabels} {schemes} title={item.title} />
        </section>

        <section aria-labelledby="h-know">
          <div class="shead">
            <h2 id="h-know">What we know</h2>
            <Button size="sm" variant="ghost" icon="plus" onclick={() => openAdd()}>Add fact</Button>
          </div>
          {#if knownRows.length}
            <AttrList rows={knownRows} onopen={openFact} />
          {:else}
            <p class="muted">Nothing yet. Add a fact, or let an agent enrich it.</p>
          {/if}
        </section>

        {#if purchaseRows.length}
          <section aria-labelledby="h-buy">
            <h2 id="h-buy">Purchase</h2>
            <AttrList rows={purchaseRows} onopen={openFact} />
          </section>
        {/if}

        {#if item.product}
          {@const p = item.product}
          <section aria-labelledby="h-prod">
            <h2 id="h-prod">Product</h2>
            <p class="pname"><strong>{p.name}</strong>{#if p.brand}<span class="muted"> · {p.brand}</span>{/if}</p>
            {#if p.item_count > 1}<p class="muted small"><a href="/?product={p.id}">{p.item_count} items</a> of this product</p>{/if}
            {#if p.media.length}
              <Gallery media={p.media} title={p.name} emptyMessage="No stock photos." />
            {/if}
            {#if productRows.length}
              <dl class="plain">
                {#each productRows as r (r.key)}
                  <div><dt>{r.label}</dt><dd>{r.text} {#if r.fact}<ProvenanceBadge basis={r.fact.basis} confidence={r.fact.confidence} compact />{/if}</dd></div>
                {/each}
              </dl>
            {/if}
            {#if p.identifiers.length}
              <dl class="plain">
                {#each p.identifiers as i (i.id)}
                  <div><dt>{schemes.get(i.scheme)?.label ?? i.scheme}</dt><dd class="mono">{i.value}</dd></div>
                {/each}
              </dl>
            {/if}
            {#if p.notes}<p class="notes">{p.notes}</p>{/if}
          </section>
        {/if}

        {#if references.length}
          <section aria-labelledby="h-refs">
            <h2 id="h-refs">References</h2>
            <ul class="refs">
              {#each references as r (r.id)}
                <li>
                  {#if r.url}
                    <a href={r.url} target="_blank" rel="noopener noreferrer">{r.title || r.url}</a>
                  {:else}
                    <span>{r.title || 'Untitled'}</span>
                  {/if}
                  <span class="muted small">
                    {cap(r.kind.replace(/_/g, ' '))}{#if r.subject_type === 'product'} · product{/if}{#if r.fetched_at} · fetched {formatDay(r.fetched_at)}{/if}
                  </span>
                  {#if r.archive}
                    <a class="small" href={r.archive.orig_url} target="_blank" rel="noopener">Archived snapshot</a>
                  {/if}
                  {#if r.notes}<span class="small">{r.notes}</span>{/if}
                </li>
              {/each}
            </ul>
          </section>
        {/if}

        {#if item.parent || item.children.length}
          <section aria-labelledby="h-set">
            <h2 id="h-set">{item.parent ? 'Part of a set' : 'In this set'}</h2>
            <ul class="set">
              {#if item.parent}<li><ItemCard item={item.parent} /></li>{/if}
              {#each item.children as c (c.id)}<li><ItemCard item={c} /></li>{/each}
            </ul>
          </section>
        {/if}

        <section aria-labelledby="h-hist">
          <h2 id="h-hist">History</h2>
          {#if item.movements.length}
            <ol class="moves">
              {#each item.movements as m (m.id)}
                {@const s = movementSentence(m)}
                <li>
                  <span>{s.text}</span>
                  <span class="muted small">
                    {['', s.date, m.actor && m.actor !== 'user' ? actorLabel(m.actor) : '', m.note ? `“${m.note}”` : ''].filter((x, i) => i === 0 || x).join(' · ')}
                  </span>
                </li>
              {/each}
            </ol>
          {:else}
            <p class="muted">No moves recorded.</p>
          {/if}
          <p class="muted small stamps">Added {formatDay(item.created_at)} · updated {formatDay(item.updated_at)}</p>
        </section>
      </div>
    </div>

    <FactSheet bind:open={factOpen} {item} {factKey} {keys} {sourceLabels} onwrite={onFactWrite} />
    <AddFactSheet bind:open={addOpen} itemId={item.id} kind={item.kind} {keys} initialKey={addKey} onwrite={onFactWrite} />

    <Sheet bind:open={editOpen} title="Edit item" side="auto">
      <form id="edit-f" class="form" onsubmit={saveEdit}>
        <label for="e-title">Title</label>
        <input id="e-title" type="text" bind:value={eTitle} required />
        <div class="two">
          <label>
            <span>Kind</span>
            <select bind:value={eKind}>
              {#each Object.entries(KIND_LABEL) as [k, l] (k)}<option value={k}>{l}</option>{/each}
            </select>
          </label>
          <label>
            <span>Quantity</span>
            <input type="number" min="1" step="1" inputmode="numeric" bind:value={eQuantity} />
          </label>
        </div>
        <label for="e-cat">Category <span class="muted">(e.g. outerwear/shell)</span></label>
        <input id="e-cat" type="text" bind:value={eCategory} autocapitalize="off" />
        <label for="e-notes">Notes</label>
        <textarea id="e-notes" rows="4" bind:value={eNotes}></textarea>
        {#if eError}<p class="err" role="alert">{eError}</p>{/if}
      </form>
      {#snippet footer()}
        <Button variant="ghost" onclick={() => (editOpen = false)}>Cancel</Button>
        <Button variant="primary" type="submit" form="edit-f" loading={eBusy}>Save</Button>
      {/snippet}
    </Sheet>

    <Sheet bind:open={goneOpen} title="Mark as gone" side="auto" size="sm">
      <form id="gone-f" class="form" onsubmit={markGone}>
        <p class="muted">It stays in the inventory, hidden from the grid unless you ask for gone items.</p>
        <fieldset class="reasons">
          <legend>Why</legend>
          {#each goneReasons as r (r)}
            <label class="radio"><input type="radio" name="reason" value={r} bind:group={goneReason} /> {cap(r)}</label>
          {/each}
        </fieldset>
      </form>
      {#snippet footer()}
        <Button variant="ghost" onclick={() => (goneOpen = false)}>Cancel</Button>
        <Button variant="danger" type="submit" form="gone-f" loading={goneBusy}>Mark as gone</Button>
      {/snippet}
    </Sheet>
  {/if}
</div>

<style>
  .detail {
    padding-top: var(--space-2);
  }
  .back {
    margin-bottom: var(--space-1);
    font-size: var(--text-sm);
  }
  .back a {
    display: inline-flex;
    align-items: center;
    min-height: var(--tap-min);
  }
  .layout {
    display: grid;
    gap: var(--space-4);
    grid-template-columns: minmax(0, 1fr);
    grid-template-areas: 'head' 'where' 'media' 'info';
  }
  .head {
    grid-area: head;
    display: grid;
    gap: var(--space-2);
  }
  .where {
    grid-area: where;
  }
  .media {
    grid-area: media;
    min-width: 0;
  }
  .info {
    grid-area: info;
    display: grid;
    gap: var(--space-6);
    min-width: 0;
  }
  h1 {
    font-size: var(--text-xl);
    overflow-wrap: anywhere;
  }
  .sub {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-1);
    font-size: var(--text-sm);
  }
  .hactions {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
  }
  section h2 {
    font-size: var(--text-lg);
    margin-bottom: var(--space-2);
  }
  .shead {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-2);
    margin-bottom: var(--space-2);
  }
  .shead h2 {
    margin: 0;
  }
  .lede {
    margin: calc(-1 * var(--space-1)) 0 var(--space-3);
    font-size: var(--text-sm);
  }
  .small {
    font-size: var(--text-sm);
  }
  .tags {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
  }
  .tagform {
    display: flex;
    gap: var(--space-1);
  }
  .tagform input {
    width: 8.5rem;
    min-height: var(--tap-min);
    padding: 0 var(--space-3);
    border: var(--border-width) dashed var(--color-border-strong);
    border-radius: var(--radius-pill);
    background: transparent;
    font-size: var(--text-sm);
  }
  .notes {
    white-space: pre-wrap;
    padding: var(--space-3);
    border-radius: var(--radius-md);
    background: var(--color-surface-2);
  }
  .pname {
    margin-bottom: var(--space-2);
  }
  .plain {
    display: grid;
    margin: var(--space-2) 0 0;
  }
  .plain div {
    display: grid;
    grid-template-columns: minmax(7rem, 35%) 1fr;
    gap: var(--space-2);
    padding: var(--space-2) 0;
    border-bottom: var(--border-width) solid var(--color-border);
  }
  .plain dt {
    color: var(--color-text-muted);
    font-size: var(--text-sm);
  }
  .plain dd {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
    margin: 0;
  }
  .refs,
  .moves {
    display: grid;
    gap: var(--space-2);
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .refs li {
    display: grid;
    gap: var(--space-0);
    padding: var(--space-2) 0;
    border-bottom: var(--border-width) solid var(--color-border);
    overflow-wrap: anywhere;
  }
  .moves li {
    padding-left: var(--space-3);
    border-left: 2px solid var(--color-border-strong);
  }
  .moves li:first-child {
    border-left-color: var(--color-accent);
  }
  .stamps {
    margin-top: var(--space-3);
  }
  .set {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(var(--grid-card-min), 1fr));
    gap: var(--space-3);
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .form {
    display: grid;
    gap: var(--space-2);
  }
  .form label,
  .two span,
  legend {
    font-size: var(--text-sm);
    font-weight: var(--weight-medium);
  }
  .two {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: var(--space-3);
  }
  .two label {
    display: grid;
    gap: var(--space-1);
  }
  .form input[type='text'],
  .form input[type='number'],
  .form select,
  .form textarea {
    width: 100%;
    min-height: var(--tap-min);
    padding: var(--space-2) var(--space-3);
    border: var(--border-width) solid var(--color-border-strong);
    border-radius: var(--radius-md);
    background: var(--color-bg);
    font-weight: var(--weight-regular);
  }
  .reasons {
    display: grid;
    gap: 0;
    margin: 0;
    padding: 0;
    border: 0;
  }
  .radio {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    min-height: var(--tap-min);
    font-weight: var(--weight-regular) !important;
  }
  .radio input {
    width: 20px;
    height: 20px;
    accent-color: var(--color-accent);
  }
  .err {
    color: var(--color-danger);
    font-size: var(--text-sm);
  }

  @media (min-width: 720px) {
    .detail {
      padding-top: var(--space-4);
    }
    .layout {
      column-gap: var(--space-6);
      grid-template-columns: minmax(0, 5fr) minmax(0, 6fr);
      grid-template-rows: auto auto 1fr;
      grid-template-areas:
        'media head'
        'media where'
        'media info';
    }
    .media {
      position: sticky;
      top: calc(var(--topbar-height) + var(--space-4));
      align-self: start;
    }
    h1 {
      font-size: var(--text-2xl);
    }
  }
</style>
