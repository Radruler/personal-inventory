<script lang="ts">
  // Assign inbox photos to an existing item: search items (GET /api/items?q=),
  // pick one, choose a role per photo, then POST /api/media/{id}/links.
  import { untrack } from 'svelte';
  import { api, getJSON, itemQuery, unwrap, type ItemList, type ItemSummary, type Media } from '$lib/api/client';
  import Button from '$lib/components/Button.svelte';
  import EmptyState from '$lib/components/EmptyState.svelte';
  import Icon from '$lib/components/Icon.svelte';
  import LocationChip from '$lib/components/LocationChip.svelte';
  import SearchBox from '$lib/components/SearchBox.svelte';
  import Sheet from '$lib/components/Sheet.svelte';
  import { errorMessage } from '$lib/components/format';
  import { hasVariant } from '$lib/components/gallery';
  import { kindIcon } from '$lib/components/icons';
  import PhotoRoles from '../../(locations)/shared/PhotoRoles.svelte';
  import { defaultRole, plural } from '../../(locations)/shared/places';

  let {
    open = $bindable(false),
    photos,
    onassigned
  }: {
    open?: boolean;
    photos: Media[];
    onassigned: (item: ItemSummary, linked: number) => void;
  } = $props();

  let q = $state('');
  let results = $state<ItemSummary[]>([]);
  let searching = $state(false);
  let searchError = $state<unknown>(null);
  let picked = $state<ItemSummary | null>(null);
  let roles = $state<Record<string, string>>({});
  let busy = $state(false);
  let error = $state('');
  let seq = 0;

  $effect(() => {
    if (!open) return;
    untrack(() => {
      picked = null;
      error = '';
      roles = Object.fromEntries(photos.map((p, i) => [p.id, defaultRole(i)]));
      void search(q);
    });
  });

  async function search(text: string) {
    const my = ++seq;
    searching = true;
    searchError = null;
    try {
      const res = await getJSON<ItemList>('/api/items', itemQuery({ q: text.trim() || undefined, sort: text.trim() ? 'relevance' : 'updated', limit: 30 }));
      if (my === seq) results = res.items;
    } catch (e) {
      if (my === seq) searchError = e;
    } finally {
      if (my === seq) searching = false;
    }
  }

  async function assign() {
    if (!picked) return;
    busy = true;
    error = '';
    let linked = 0;
    try {
      for (const p of photos) {
        unwrap(
          await api.POST('/api/media/{id}/links', {
            params: { path: { id: p.id } },
            body: { subject_type: 'item', subject_id: picked.id, role: roles[p.id] ?? 'other' }
          })
        );
        linked++;
      }
      open = false;
      onassigned(picked, linked);
    } catch (e) {
      error = `${linked ? `Linked ${linked}, then: ` : ''}${errorMessage(e)}`;
      if (linked) onassigned(picked, linked);
    } finally {
      busy = false;
    }
  }

  const thumbs = $derived(photos.map((p) => ({ id: p.id, thumb: hasVariant(p, 'thumb') ? p.thumb_url : null, name: p.original_name })));
</script>

<Sheet bind:open title="Assign {plural(photos.length, 'photo')} to an item" side="auto" size="lg">
  <div class="body">
    <PhotoRoles photos={thumbs} bind:roles />
    <SearchBox bind:value={q} placeholder="Search items: title, brand, code…" label="Search items" busy={searching} onsearch={search} />
    {#if searchError}
      <p class="err">{errorMessage(searchError)}</p>
    {:else if !results.length && !searching}
      <EmptyState compact icon="search" title="No items found" message="Try other words, or create a new item from these photos instead." />
    {:else}
      <ul class="results" role="listbox" aria-label="Items">
        {#each results as it (it.id)}
          <li role="option" aria-selected={picked?.id === it.id}>
            <button type="button" class:on={picked?.id === it.id} onclick={() => (picked = it)}>
              <span class="th">
                {#if it.cover && hasVariant(it.cover, 'thumb')}
                  <img src={it.cover.thumb_url} alt="" loading="lazy" />
                {:else}
                  <Icon name={kindIcon(it.kind)} size={22} />
                {/if}
              </span>
              <span class="tt">
                <span class="title">{it.title}</span>
                <LocationChip placement={it.placement} />
              </span>
              {#if picked?.id === it.id}<Icon name="check" size={20} />{/if}
            </button>
          </li>
        {/each}
      </ul>
    {/if}
    {#if error}<p class="err" role="alert">{error}</p>{/if}
  </div>
  {#snippet footer()}
    <Button variant="ghost" onclick={() => (open = false)}>Cancel</Button>
    <Button variant="primary" disabled={!picked} loading={busy} onclick={assign}>
      {picked ? `Link to ${picked.title.length > 24 ? picked.title.slice(0, 23) + '…' : picked.title}` : 'Pick an item'}
    </Button>
  {/snippet}
</Sheet>

<style>
  .body {
    display: grid;
    gap: var(--space-4);
  }
  .results {
    display: grid;
    gap: var(--space-1);
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .results button {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    width: 100%;
    min-height: calc(var(--tap-min) + var(--space-4));
    padding: var(--space-2);
    border: var(--border-width) solid transparent;
    border-radius: var(--radius-md);
    background: none;
    text-align: left;
    cursor: pointer;
  }
  .results button:hover {
    background: var(--color-surface-2);
  }
  .results button.on {
    border-color: var(--color-accent);
    background: var(--color-accent-soft);
  }
  .th {
    display: grid;
    place-items: center;
    flex: none;
    width: 52px;
    height: 52px;
    border-radius: var(--radius-sm);
    overflow: hidden;
    background: var(--color-surface-2);
    color: var(--color-text-faint);
  }
  .th img {
    width: 100%;
    height: 100%;
    object-fit: cover;
  }
  .tt {
    flex: 1;
    min-width: 0;
    display: grid;
    gap: var(--space-1);
    justify-items: start;
  }
  .title {
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-weight: var(--weight-medium);
  }
  .err {
    color: var(--color-danger);
    font-size: var(--text-sm);
  }
</style>
