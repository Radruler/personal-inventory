<script lang="ts">
  // Create (POST /api/locations, with a parent picker) or edit (PATCH name,
  // code, kind, notes) a location. Re-parenting is a move, not an edit.
  import { untrack } from 'svelte';
  import { api, ApiError, unwrap, type LocationDetail, type LocationNode } from '$lib/api/client';
  import Button from '$lib/components/Button.svelte';
  import Sheet from '$lib/components/Sheet.svelte';
  import { errorMessage, LOCATION_KIND_LABEL } from '$lib/components/format';
  import { parentOptions, suggestChildKind, type LocationKind } from '$lib/components/locationTree';
  import { findNode, nextCode } from './places';
  import { createAndReload, invalidateTree, loadTree, tree } from './store.svelte';

  let {
    open = $bindable(false),
    location = null,
    parent = null,
    onsaved
  }: {
    open?: boolean;
    /** Edit this location; null = create. */
    location?: Pick<LocationDetail, 'id' | 'code' | 'name' | 'kind' | 'notes'> | null;
    /** Default parent id for create. */
    parent?: string | null;
    onsaved?: (id: string) => void;
  } = $props();

  const uid = $props.id();
  const kinds: LocationKind[] = ['site', 'room', 'unit', 'shelf', 'container'];

  let name = $state('');
  let code = $state('');
  let kind = $state<LocationKind>('container');
  let notes = $state('');
  let parentId = $state('');
  let busy = $state(false);
  let error = $state('');

  $effect(() => {
    if (!open) return;
    untrack(() => {
      error = '';
      void loadTree();
      if (location) {
        name = location.name;
        code = location.code ?? '';
        kind = location.kind;
        notes = location.notes ?? '';
      } else {
        parentId = parent ?? '';
        const p = findNode(tree.roots, parent);
        kind = suggestChildKind(p?.kind ?? null);
        name = '';
        notes = '';
        code = kind === 'container' ? nextCode(tree.roots) : '';
      }
    });
  });

  // When the tree arrives after opening a create form, fill the code suggestion.
  $effect(() => {
    if (open && !location && tree.loaded && !code) untrack(() => kind === 'container' && (code = nextCode(tree.roots)));
  });

  const parents = $derived(parentOptions(tree.roots));

  function onkind(k: LocationKind) {
    kind = k;
    if (!location && k === 'container' && !code) code = nextCode(tree.roots);
  }

  function friendly(e: unknown): string {
    if (e instanceof ApiError && e.status === 409) return errorMessage(e).replace(/^location code/, 'Code') + '. Pick another code.';
    return errorMessage(e);
  }

  async function submit(e: SubmitEvent) {
    e.preventDefault();
    if (!name.trim()) {
      error = 'Name is required';
      return;
    }
    busy = true;
    error = '';
    try {
      let id: string;
      if (location) {
        const body = {
          name: name.trim(),
          code: code.trim() || null,
          kind,
          notes: notes.trim() || null
        };
        const res = unwrap(await api.PATCH('/api/locations/{id}', { params: { path: { id: location.id } }, body }));
        id = res.id;
        invalidateTree();
      } else {
        id = await createAndReload({
          name: name.trim(),
          kind,
          code: code.trim() || null,
          parent: parentId || null,
          notes: notes.trim() || null
        });
      }
      open = false;
      onsaved?.(id);
    } catch (err) {
      error = friendly(err);
    } finally {
      busy = false;
    }
  }

  const parentNode = $derived<LocationNode | null>(findNode(tree.roots, parentId));
</script>

<Sheet bind:open title={location ? 'Edit location' : 'New location'} side="auto">
  <form id="{uid}-f" class="form" onsubmit={submit}>
    <label>
      <span>Name</span>
      <input bind:value={name} required autocomplete="off" placeholder="Winter coats" />
    </label>
    <div class="two">
      <label>
        <span>Code <small class="muted">(on the label)</small></span>
        <input
          bind:value={code}
          class="mono"
          autocomplete="off"
          autocapitalize="characters"
          spellcheck="false"
          placeholder="B-021"
        />
      </label>
      <label>
        <span>Kind</span>
        <select value={kind} onchange={(e) => onkind(e.currentTarget.value as LocationKind)}>
          {#each kinds as k (k)}<option value={k}>{LOCATION_KIND_LABEL[k]}</option>{/each}
        </select>
      </label>
    </div>
    {#if !location}
      <label>
        <span>Inside</span>
        <select bind:value={parentId}>
          <option value="">Nothing (top level)</option>
          {#each parents as p (p.id)}
            <option value={p.id}>{' '.repeat(p.depth * 2)}{p.label}</option>
          {/each}
        </select>
        {#if parentNode}<small class="muted">{parentNode.label}</small>{/if}
      </label>
    {/if}
    <label>
      <span>Notes <small class="muted">(optional)</small></span>
      <textarea bind:value={notes} rows="3"></textarea>
    </label>
    {#if error}<p class="err" role="alert">{error}</p>{/if}
  </form>
  {#snippet footer()}
    <Button variant="ghost" onclick={() => (open = false)}>Cancel</Button>
    <Button variant="primary" type="submit" form="{uid}-f" loading={busy}>{location ? 'Save' : 'Create'}</Button>
  {/snippet}
</Sheet>

<style>
  .form {
    display: grid;
    gap: var(--space-4);
  }
  label {
    display: grid;
    gap: var(--space-1);
    font-size: var(--text-sm);
    font-weight: var(--weight-medium);
    min-width: 0;
  }
  input,
  select,
  textarea {
    width: 100%;
    min-height: var(--tap-min);
    padding: var(--space-2) var(--space-3);
    border: var(--border-width) solid var(--color-border-strong);
    border-radius: var(--radius-md);
    background: var(--color-surface);
    font-weight: var(--weight-regular);
    font-size: var(--text-md);
  }
  textarea {
    resize: vertical;
  }
  .two {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: var(--space-3);
  }
  .err {
    color: var(--color-danger);
    font-size: var(--text-sm);
  }
</style>
