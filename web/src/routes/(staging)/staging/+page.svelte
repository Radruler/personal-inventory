<script lang="ts">
  // "I'm packing" (PLAN.md §8): staged items grouped by target. Select within
  // one group, then Pack into box… (existing or new box under the target) in
  // one bulk move, or Unstage. Undo puts everything back.
  import { api, unwrap, type ItemSummary, type LocationRef, type Schemas } from '$lib/api/client';
  import Button from '$lib/components/Button.svelte';
  import EmptyState from '$lib/components/EmptyState.svelte';
  import ErrorState from '$lib/components/ErrorState.svelte';
  import Icon from '$lib/components/Icon.svelte';
  import LoadingState from '$lib/components/LoadingState.svelte';
  import MultiSelectBar from '$lib/components/MultiSelectBar.svelte';
  import PhotoGrid from '$lib/components/PhotoGrid.svelte';
  import { plural } from '../../(locations)/shared/places';
  import { moveItems } from '../../(locations)/shared/store.svelte';
  import PackSheet from './PackSheet.svelte';

  let data = $state<Schemas['Staging'] | null>(null);
  let loading = $state(true);
  let error = $state<unknown>(null);

  // selection lives in one group at a time
  let groupId = $state<string | null>(null);
  let selected = $state<string[]>([]);
  let packOpen = $state(false);
  let unstaging = $state(false);
  /** Items hidden optimistically while a move is in flight. */
  let hidden = $state(new Set<string>());

  async function load() {
    error = null;
    try {
      data = unwrap(await api.GET('/api/staging'));
    } catch (e) {
      error = e;
    } finally {
      loading = false;
    }
  }
  $effect(() => {
    void load();
  });

  const groups = $derived(
    (data?.groups ?? []).map((g) => ({ ...g, items: g.items.filter((i) => !hidden.has(i.id)) })).filter((g) => g.items.length)
  );
  const group = $derived(groups.find((g) => g.target.id === groupId) ?? null);
  const selectedItems = $derived(group ? group.items.filter((i) => selected.includes(i.id)) : []);
  const total = $derived(groups.reduce((n, g) => n + g.items.length, 0));

  function startSelect(id: string) {
    if (groupId !== id) selected = [];
    groupId = id;
  }
  function stopSelect() {
    groupId = null;
    selected = [];
  }

  async function withHidden(ids: string[], run: () => Promise<unknown>) {
    hidden = new Set([...hidden, ...ids]);
    stopSelect();
    try {
      await run();
    } catch {
      // error toast already shown by moveItems
    } finally {
      const next = new Set(hidden);
      ids.forEach((id) => next.delete(id));
      hidden = next;
    }
  }

  async function pack(boxId: string, box: { code: string | null; name: string }) {
    const prev = selectedItems;
    const ids = prev.map((i) => i.id);
    packOpen = false;
    await withHidden(ids, () =>
      moveItems({ action: 'move', item_ids: ids, to: boxId, note: 'packed' }, prev, `Packed ${plural(ids.length, 'item')} into ${box.code ?? box.name}`, load)
    );
  }

  async function unstage() {
    const prev = selectedItems;
    const ids = prev.map((i) => i.id);
    unstaging = true;
    await withHidden(ids, () => moveItems({ action: 'unstage', item_ids: ids }, prev, `Unstaged ${plural(ids.length, 'item')}`, load));
    unstaging = false;
  }

  /** Items whose location is already inside the target (e.g. their box moved there): nothing to pack. */
  function arrived(i: ItemSummary, t: LocationRef): boolean {
    return !!i.placement.location?.path.some((c) => c.id === t.id);
  }
</script>

<svelte:head><title>Packing · Personal Inventory</title></svelte:head>

<div class="page packing">
  <header class="head">
    <div>
      <h1>Packing</h1>
      {#if data}<p class="muted sub">{total ? `${plural(total, 'item')} staged to move` : 'Nothing staged'}</p>{/if}
    </div>
    <Button href="/" icon="plus" variant="ghost">Stage more</Button>
  </header>

  {#if data && data.unknown_count > 0}
    <a class="unknown" href="/?status=unknown">
      <Icon name="unknown" size={20} />
      <span>Unknown location <strong>({data.unknown_count})</strong></span>
      <Icon name="chevron-right" size={18} />
    </a>
  {/if}

  {#if loading}
    <LoadingState variant="grid" count={6} />
  {:else if error}
    <ErrorState {error} onretry={load} />
  {:else if !groups.length}
    <EmptyState icon="staged" title="Nothing to pack" message="Stage items from All items or an item's page (Move → Stage for…) and they'll show up here, grouped by where they're going.">
      <Button href="/" variant="primary">Browse items</Button>
    </EmptyState>
  {:else}
    {#each groups as g (g.target.id)}
      {@const sel = groupId === g.target.id}
      <section class="group" class:active={sel} aria-labelledby="g-{g.target.id}">
        <div class="g-head">
          <h2 id="g-{g.target.id}">
            <span class="muted to">Going to</span>
            <a href="/locations/{g.target.id}">{g.target.label}</a>
            <span class="count">{g.items.length}</span>
          </h2>
          {#if sel}
            <Button size="sm" variant="ghost" onclick={stopSelect}>Done</Button>
          {:else}
            <Button size="sm" variant="primary" onclick={() => startSelect(g.target.id)}>Select to pack</Button>
          {/if}
        </div>
        {#if g.items.some((i) => arrived(i, g.target))}
          <p class="muted note">Some of these are already in {g.target.name}; select them and Unstage to clear the target.</p>
        {/if}
        <PhotoGrid items={g.items} selectable={sel} bind:selected chip="compact" label="Staged for {g.target.label}" />
      </section>
    {/each}
  {/if}
</div>

{#if group}
  <MultiSelectBar
    count={selected.length}
    total={group.items.length}
    alwaysShow
    label="{selected.length} for {group.target.code ?? group.target.name}"
    onselectall={() => (selected = group?.items.map((i) => i.id) ?? [])}
    onclear={() => (selected.length ? (selected = []) : stopSelect())}
  >
    {#snippet actions()}
      <Button size="sm" variant="primary" icon="container" disabled={!selected.length} onclick={() => (packOpen = true)}>Pack into box…</Button>
      <Button size="sm" disabled={!selected.length} loading={unstaging} onclick={unstage}>Unstage</Button>
    {/snippet}
  </MultiSelectBar>
  <PackSheet bind:open={packOpen} target={group.target} count={selected.length} onpack={pack} />
{/if}

<style>
  .packing {
    display: grid;
    gap: var(--space-5);
  }
  .head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: var(--space-3);
  }
  .sub {
    font-size: var(--text-sm);
  }
  .unknown {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    min-height: var(--tap-min);
    padding: 0 var(--space-4);
    border-radius: var(--radius-md);
    background: var(--status-unknown-soft);
    color: var(--status-unknown);
  }
  .unknown span {
    flex: 1;
    color: var(--color-text);
  }
  .group {
    display: grid;
    gap: var(--space-3);
    padding: var(--space-4);
    border: var(--border-width) solid var(--color-border);
    border-radius: var(--radius-lg);
    background: var(--color-surface);
    min-width: 0;
  }
  .group.active {
    border-color: var(--color-accent);
  }
  .g-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-2);
    flex-wrap: wrap;
  }
  h2 {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: var(--space-2);
    font-size: var(--text-lg);
  }
  .to {
    font-size: var(--text-sm);
    font-weight: var(--weight-regular);
  }
  .count {
    color: var(--color-text-faint);
    font-weight: var(--weight-regular);
  }
  .note {
    font-size: var(--text-sm);
  }
  @media (max-width: 719px) {
    .group {
      padding: var(--space-3);
      margin: 0 calc(var(--space-2) * -1);
    }
  }
</style>
