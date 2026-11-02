<script module lang="ts">
  import type { LocationNode } from '$lib/api/client';
  // The tree is shared between opens (and items); refreshed on every open.
  let treeCache: LocationNode[] | null = null;
</script>

<script lang="ts">
  // Pick a destination (tree + search + create) and POST /api/moves:
  // mode "move" sets the location (or unknown), "stage" sets the target.
  import { api, createLocation, unwrap, type Schemas } from '$lib/api/client';
  import { Button, ErrorState, LoadingState, LocationPicker, Sheet, errorMessage, toast, type LocationCreateRequest } from '$lib/components';
  import { findLocation } from '../../home';

  type MoveResult = Schemas['MoveResult'];

  let {
    open = $bindable(false),
    mode,
    itemId,
    currentId = null,
    targetId = null,
    staged = false,
    ondone
  }: {
    open?: boolean;
    mode: 'move' | 'stage';
    itemId: string;
    /** The item's location (pre-selected for move). */
    currentId?: string | null;
    /** The item's target (pre-selected for stage). */
    targetId?: string | null;
    /** The item has a target, so "move" can offer to clear it. */
    staged?: boolean;
    ondone: (res: MoveResult, req: Schemas['MoveRequest']) => void;
  } = $props();

  let roots = $state<LocationNode[] | null>(treeCache);
  let loadError = $state<unknown>(null);
  let value = $state<string | null>(null);
  let note = $state('');
  let clearTarget = $state(false);
  let busy = $state(false);
  let creating = $state(false);
  let createError = $state<unknown>(null);

  async function loadTree() {
    loadError = null;
    try {
      roots = treeCache = unwrap(await api.GET('/api/locations')).roots;
    } catch (e) {
      if (!roots) loadError = e;
    }
  }

  // Reset the form each time the sheet opens.
  let wasOpen = false;
  $effect(() => {
    if (open && !wasOpen) {
      value = mode === 'stage' ? targetId : currentId;
      note = '';
      clearTarget = false;
      createError = null;
      loadTree();
    }
    wasOpen = open;
  });

  const picked = $derived(value && roots ? findLocation(roots, value) : null);
  const unchanged = $derived(mode === 'move' ? value === currentId : value === targetId);

  async function oncreate(req: LocationCreateRequest) {
    creating = true;
    createError = null;
    try {
      const loc = await createLocation(req);
      await loadTree();
      value = loc.id;
      toast(`Created ${loc.label}`, { tone: 'success' });
    } catch (e) {
      createError = e;
    } finally {
      creating = false;
    }
  }

  async function submit(to: string | null) {
    const req: Schemas['MoveRequest'] = {
      action: mode,
      item_ids: [itemId],
      to,
      note: note.trim() || null,
      clear_target: mode === 'move' && staged && clearTarget
    };
    busy = true;
    try {
      const res = unwrap(await api.POST('/api/moves', { body: req }));
      open = false;
      ondone(res, req);
    } catch (e) {
      toast(`Couldn't ${mode === 'stage' ? 'stage' : 'move'}: ${errorMessage(e)}`, { tone: 'error' });
    } finally {
      busy = false;
    }
  }
</script>

<Sheet bind:open title={mode === 'stage' ? 'Stage to go to…' : 'Move to…'} side="auto">
  {#if roots}
    <div class="body">
      <LocationPicker
        {roots}
        bind:value
        label={mode === 'stage' ? 'Destination' : 'Locations'}
        {oncreate}
        {creating}
        {createError}
      />
      <label class="field">
        <span>Note <span class="muted">(optional)</span></span>
        <input type="text" bind:value={note} placeholder={mode === 'stage' ? 'e.g. winter rotation' : 'e.g. packed Sunday'} />
      </label>
      {#if mode === 'move' && staged}
        <label class="check">
          <input type="checkbox" bind:checked={clearTarget} />
          <span>Also clear “going to” <span class="muted">(automatic when moving into the target)</span></span>
        </label>
      {/if}
    </div>
  {:else if loadError}
    <ErrorState compact error={loadError} title="Couldn't load locations" onretry={loadTree} />
  {:else}
    <LoadingState variant="list" count={6} label="Loading locations…" />
  {/if}

  {#snippet footer()}
    {#if mode === 'move'}
      <Button variant="ghost" disabled={busy || currentId === null} onclick={() => submit(null)}>Location unknown</Button>
    {/if}
    <Button variant="primary" loading={busy} disabled={!picked || unchanged} onclick={() => submit(picked!.id)}>
      {#if picked}
        {mode === 'stage' ? 'Stage for' : 'Move to'} {picked.code || picked.name}
      {:else}
        Pick a location
      {/if}
    </Button>
  {/snippet}
</Sheet>

<style>
  .body {
    display: grid;
    gap: var(--space-4);
  }
  .field {
    display: grid;
    gap: var(--space-1);
    font-size: var(--text-sm);
    font-weight: var(--weight-medium);
  }
  .field input {
    min-height: var(--tap-min);
    padding: 0 var(--space-3);
    border: var(--border-width) solid var(--color-border-strong);
    border-radius: var(--radius-md);
    background: var(--color-bg);
    font-weight: var(--weight-regular);
  }
  .check {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    min-height: var(--tap-min);
    font-size: var(--text-sm);
  }
  .check input {
    width: 20px;
    height: 20px;
    accent-color: var(--color-accent);
  }
</style>
