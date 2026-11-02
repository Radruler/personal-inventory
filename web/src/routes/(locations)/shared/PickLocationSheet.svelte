<script lang="ts">
  // Sheet + LocationPicker (with "create new") for choosing a destination:
  // Move box, Move selected to…, Stage for…, quick-add location. The caller
  // does the actual write in `onpick`; the sheet shows busy/error state.
  import { untrack } from 'svelte';
  import type { LocationNode } from '$lib/api/client';
  import Button from '$lib/components/Button.svelte';
  import ErrorState from '$lib/components/ErrorState.svelte';
  import LoadingState from '$lib/components/LoadingState.svelte';
  import LocationPicker from '$lib/components/LocationPicker.svelte';
  import Sheet from '$lib/components/Sheet.svelte';
  import { errorMessage } from '$lib/components/format';
  import type { LocationCreateRequest } from '$lib/components/locationTree';
  import { createAndReload, loadTree, tree } from './store.svelte';

  let {
    open = $bindable(false),
    title,
    value = null,
    canSelect,
    confirmLabel = 'Move here',
    noneLabel,
    hint,
    onpick
  }: {
    open?: boolean;
    title: string;
    /** Initially selected location id. */
    value?: string | null;
    canSelect?: (n: LocationNode) => boolean;
    confirmLabel?: string;
    /** Offers a "null" choice (e.g. "Location unknown", "Top level"). */
    noneLabel?: string;
    /** Short text above the tree. */
    hint?: string;
    /** Performs the write; the sheet closes when it resolves, stays open with the error when it throws. */
    onpick: (id: string | null, node: LocationNode | null) => Promise<void> | void;
  } = $props();

  let picked = $state<string | null>(null);
  let busy = $state(false);
  let error = $state<unknown>(null);
  let creating = $state(false);
  let createError = $state<unknown>(null);

  $effect(() => {
    if (!open) return;
    untrack(() => {
      picked = value ?? null;
      error = null;
      void loadTree();
    });
  });

  async function oncreate(req: LocationCreateRequest) {
    creating = true;
    createError = null;
    try {
      picked = await createAndReload(req);
    } catch (e) {
      createError = e;
    } finally {
      creating = false;
    }
  }

  function nodeFor(id: string | null): LocationNode | null {
    if (!id) return null;
    const stack = [...tree.roots];
    while (stack.length) {
      const n = stack.pop()!;
      if (n.id === id) return n;
      stack.push(...n.children);
    }
    return null;
  }

  async function confirm(id: string | null) {
    busy = true;
    error = null;
    try {
      await onpick(id, nodeFor(id));
      open = false;
    } catch (e) {
      error = e;
    } finally {
      busy = false;
    }
  }

  const pickedNode = $derived(nodeFor(picked));
  const ok = $derived(!!pickedNode && (!canSelect || canSelect(pickedNode)));
</script>

<Sheet bind:open {title} side="auto">
  <div class="body" style:--picker-max-height="none">
    {#if hint}<p class="muted hint">{hint}</p>{/if}
    {#if tree.error && !tree.loaded}
      <ErrorState compact error={tree.error} onretry={() => loadTree(true)} />
    {:else if !tree.loaded}
      <LoadingState variant="list" count={6} />
    {:else}
      <LocationPicker roots={tree.roots} bind:value={picked} {canSelect} {oncreate} {creating} {createError} />
    {/if}
    {#if error}<p class="err" role="alert">{errorMessage(error)}</p>{/if}
  </div>
  {#snippet footer()}
    {#if noneLabel}
      <Button variant="ghost" disabled={busy} onclick={() => confirm(null)}>{noneLabel}</Button>
    {/if}
    <span class="grow"></span>
    <Button variant="primary" disabled={!ok} loading={busy} onclick={() => confirm(picked)}>
      {confirmLabel}{pickedNode ? ` · ${pickedNode.code ?? pickedNode.name}` : ''}
    </Button>
  {/snippet}
</Sheet>

<style>
  .body {
    display: grid;
    gap: var(--space-3);
  }
  .hint {
    font-size: var(--text-sm);
  }
  .err {
    color: var(--color-danger);
    font-size: var(--text-sm);
  }
  .grow {
    flex: 1;
  }
</style>
