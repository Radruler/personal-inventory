<script lang="ts">
  // Tree + search over GET /api/locations roots, keyboard navigable (WAI-ARIA
  // tree), with "Create new location…" that emits a create request. The
  // caller performs POST /api/locations, then passes the new tree (and the
  // new id as `value`). Works inline or inside a Sheet.
  import type { LocationNode } from '$lib/api/client';
  import Button from './Button.svelte';
  import EmptyState from './EmptyState.svelte';
  import Icon from './Icon.svelte';
  import SearchBox from './SearchBox.svelte';
  import { LOCATION_KIND_ICON } from './icons';
  import { errorMessage, LOCATION_KIND_LABEL } from './format';
  import {
    ancestorsOf,
    buildCreateRequest,
    indexTree,
    parentOptions,
    searchTree,
    suggestChildKind,
    treeKey,
    visibleRows,
    type LocationCreateRequest,
    type LocationKind
  } from './locationTree';

  let {
    roots,
    value = $bindable(null),
    onselect,
    canSelect,
    oncreate,
    creating = false,
    createError = null,
    label = 'Locations',
    autofocus = false,
    showCounts = true
  }: {
    roots: LocationNode[];
    /** Selected location id (bindable). */
    value?: string | null;
    onselect?: (node: LocationNode) => void;
    /** Rows that may be picked (others stay navigable, e.g. only containers for "Pack into box"). */
    canSelect?: (node: LocationNode) => boolean;
    /** Enables "Create new location…"; the caller does the API call. */
    oncreate?: (req: LocationCreateRequest) => void;
    /** The caller's create request is in flight. */
    creating?: boolean;
    /** Error from the caller's create request (e.g. duplicate code). */
    createError?: unknown;
    label?: string;
    autofocus?: boolean;
    showCounts?: boolean;
  } = $props();

  const uid = $props.id();
  let query = $state('');
  // Top-level nodes (sites) start open so units are one tap away.
  // svelte-ignore state_referenced_locally
  let expanded = $state(new Set<string>(roots.map((r) => r.id)));
  let focusedId = $state<string | null>(null);
  let tree = $state<HTMLElement>();
  let showCreate = $state(false);

  const idx = $derived(indexTree(roots));
  const search = $derived(searchTree(roots, query));
  const rows = $derived(visibleRows(roots, expanded, search));

  // Open the path to the selected value once it is known.
  let opened: string | null = null;
  $effect(() => {
    if (!value || value === opened || !idx.has(value)) return;
    opened = value;
    const next = new Set(expanded);
    for (const a of ancestorsOf(idx, value)) next.add(a);
    expanded = next;
  });

  // Keep the roving focus on a visible row.
  const tabId = $derived(
    rows.some((r) => r.node.id === focusedId)
      ? focusedId
      : (rows.find((r) => r.node.id === value)?.node.id ?? rows[0]?.node.id ?? null)
  );

  function selectable(n: LocationNode) {
    return canSelect ? canSelect(n) : true;
  }
  function choose(n: LocationNode) {
    focusedId = n.id;
    if (!selectable(n)) {
      toggle(n.id);
      return;
    }
    value = n.id;
    onselect?.(n);
  }
  function toggle(id: string, force?: boolean) {
    const next = new Set(expanded);
    const open = force ?? !next.has(id);
    if (open) next.add(id);
    else next.delete(id);
    expanded = next;
  }
  function focusRow(id: string) {
    focusedId = id;
    queueMicrotask(() => tree?.querySelector<HTMLElement>(`[data-id="${CSS.escape(id)}"]`)?.focus());
  }

  function onkeydown(e: KeyboardEvent) {
    const r = treeKey(rows, tabId, e.key);
    if (!r) return;
    e.preventDefault();
    if (r.type === 'focus') focusRow(r.id);
    else if (r.type === 'expand') toggle(r.id, true);
    else if (r.type === 'collapse') toggle(r.id, false);
    else if (r.type === 'select') {
      const n = idx.get(r.id)?.node;
      if (n) choose(n);
    }
  }

  // ---- create form
  let cName = $state('');
  let cCode = $state('');
  let cKind = $state<LocationKind>('container');
  let cParent = $state<string>('');
  let cError = $state('');
  const kinds: LocationKind[] = ['site', 'room', 'unit', 'shelf', 'container'];
  const parents = $derived(parentOptions(roots));

  function startCreate() {
    const parentId = tabId && idx.has(tabId) ? tabId : (value ?? '');
    cName = query.trim();
    cCode = '';
    cParent = parentId ?? '';
    cKind = suggestChildKind(parentId ? idx.get(parentId)?.node.kind : null);
    cError = '';
    showCreate = true;
  }
  function submitCreate(e: SubmitEvent) {
    e.preventDefault();
    const req = buildCreateRequest({ name: cName, code: cCode, kind: cKind, parent: cParent || null });
    if (typeof req === 'string') {
      cError = req;
      return;
    }
    cError = '';
    oncreate?.(req);
  }
  // Close the form when the caller reports success (creating flips back to false without an error).
  let wasCreating = false;
  $effect(() => {
    if (wasCreating && !creating && !createError) showCreate = false;
    wasCreating = creating;
  });
  const createErrorText = $derived(cError || (createError ? errorMessage(createError) : ''));
</script>

<div class="picker">
  <SearchBox
    bind:value={query}
    debounce={0}
    placeholder="Search name or code"
    label="Search locations"
    {autofocus}
    onkeydown={(e) => {
      if (e.key === 'ArrowDown' && tabId) {
        e.preventDefault();
        focusRow(tabId);
      }
    }}
  />

  {#if rows.length}
    <ul class="tree" role="tree" aria-label={label} bind:this={tree}>
      {#each rows as r (r.node.id)}
        {@const n = r.node}
        {@const isSel = n.id === value}
        <li
          role="treeitem"
          data-id={n.id}
          aria-level={r.depth}
          aria-setsize={r.setsize}
          aria-posinset={r.posinset}
          aria-expanded={r.hasChildren ? r.expanded : undefined}
          aria-selected={isSel}
          aria-disabled={selectable(n) ? undefined : 'true'}
          tabindex={n.id === tabId ? 0 : -1}
          class:selected={isSel}
          class:match={r.match}
          class:dim={!selectable(n)}
          style:--depth={r.depth - 1}
          onclick={() => choose(n)}
          {onkeydown}
          onfocus={() => (focusedId = n.id)}
        >
          <span
            class="twisty"
            class:open={r.expanded}
            class:leaf={!r.hasChildren}
            aria-hidden="true"
            onclick={(e) => {
              e.stopPropagation();
              if (r.hasChildren) toggle(n.id);
            }}
            role="presentation"
          >
            {#if r.hasChildren}<Icon name="chevron-right" size={18} />{/if}
          </span>
          <Icon name={LOCATION_KIND_ICON[n.kind] ?? 'container'} size={18} class="kind" />
          <span class="name">
            {n.name}
            {#if n.code}<span class="code mono">{n.code}</span>{/if}
          </span>
          {#if showCounts}
            <span class="counts">
              {#if n.staged_count}<span class="staged" title="{n.staged_count} staged to go here">+{n.staged_count}</span>{/if}
              <span class="total" title="{n.total_count} items inside">{n.total_count}</span>
            </span>
          {/if}
          {#if isSel}<Icon name="check" size={18} class="tick" />{/if}
        </li>
      {/each}
    </ul>
  {:else if query}
    <EmptyState compact icon="search" title="No match" message={'Nothing called “' + query + '”.'} />
  {:else}
    <EmptyState compact icon="placed" title="No locations yet" />
  {/if}

  {#if oncreate}
    {#if !showCreate}
      <Button variant="ghost" icon="plus" onclick={startCreate} block>
        {query.trim() ? `Create “${query.trim()}”…` : 'Create new location…'}
      </Button>
    {:else}
      <form class="create" onsubmit={submitCreate} aria-labelledby="{uid}-ct">
        <h3 id="{uid}-ct">New location</h3>
        <label>
          <span>Name</span>
          <!-- svelte-ignore a11y_autofocus -->
          <input bind:value={cName} required autocomplete="off" autofocus />
        </label>
        <div class="two">
          <label>
            <span>Code <small class="muted">(printed label, optional)</small></span>
            <input bind:value={cCode} autocomplete="off" autocapitalize="characters" spellcheck="false" class="mono" placeholder="B-021" />
          </label>
          <label>
            <span>Kind</span>
            <select bind:value={cKind}>
              {#each kinds as k (k)}<option value={k}>{LOCATION_KIND_LABEL[k]}</option>{/each}
            </select>
          </label>
        </div>
        <label>
          <span>Inside</span>
          <select bind:value={cParent}>
            <option value="">Nothing (top level)</option>
            {#each parents as p (p.id)}
              <option value={p.id}>{' '.repeat(p.depth)}{p.label}</option>
            {/each}
          </select>
        </label>
        {#if createErrorText}<p class="err" role="alert">{createErrorText}</p>{/if}
        <div class="actions">
          <Button variant="ghost" onclick={() => (showCreate = false)}>Cancel</Button>
          <Button variant="primary" type="submit" loading={creating}>Create</Button>
        </div>
      </form>
    {/if}
  {/if}
</div>

<style>
  .picker {
    display: grid;
    gap: var(--space-3);
    min-width: 0;
  }
  .tree {
    margin: 0;
    padding: 0;
    list-style: none;
    max-height: var(--picker-max-height, none);
    overflow-y: auto;
  }
  [role='treeitem'] {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    min-height: var(--tap-min);
    padding: 0 var(--space-2) 0 calc(var(--space-1) + var(--depth) * var(--space-5));
    border-radius: var(--radius-sm);
    cursor: pointer;
    -webkit-tap-highlight-color: transparent;
  }
  [role='treeitem']:hover {
    background: var(--color-surface-2);
  }
  [role='treeitem']:focus-visible {
    outline-offset: -2px;
  }
  .selected,
  .selected:hover {
    background: var(--color-accent-soft);
    color: var(--color-accent);
  }
  .dim .name {
    color: var(--color-text-muted);
  }
  .match .name {
    font-weight: var(--weight-bold);
  }
  .twisty {
    display: grid;
    place-items: center;
    flex: none;
    width: 28px;
    height: var(--tap-min);
    margin-right: calc(var(--space-1) * -1);
    color: var(--color-text-muted);
  }
  .twisty :global(svg) {
    transition: transform var(--duration-fast) var(--ease);
  }
  .twisty.open :global(svg) {
    transform: rotate(90deg);
  }
  .twisty.leaf {
    cursor: inherit;
  }
  [role='treeitem'] :global(.kind) {
    color: var(--color-text-muted);
  }
  .selected :global(.kind) {
    color: inherit;
  }
  .name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .code {
    margin-left: var(--space-1);
    padding: 0 var(--space-1);
    border-radius: var(--space-1);
    background: var(--color-surface-2);
    color: var(--color-text-muted);
    font-size: var(--text-xs);
  }
  .counts {
    display: inline-flex;
    gap: var(--space-2);
    font-size: var(--text-xs);
    font-variant-numeric: tabular-nums;
    color: var(--color-text-faint);
  }
  .staged {
    color: var(--status-staged);
  }
  .create {
    display: grid;
    gap: var(--space-3);
    padding: var(--space-4);
    border: var(--border-width) solid var(--color-border);
    border-radius: var(--radius-md);
    background: var(--color-bg);
  }
  .create h3 {
    font-size: var(--text-md);
  }
  label {
    display: grid;
    gap: var(--space-1);
    font-size: var(--text-sm);
    font-weight: var(--weight-medium);
    min-width: 0;
  }
  input,
  select {
    width: 100%;
    min-height: var(--tap-min);
    padding: 0 var(--space-3);
    border: var(--border-width) solid var(--color-border-strong);
    border-radius: var(--radius-md);
    background: var(--color-surface);
    font-weight: var(--weight-regular);
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
  .actions {
    display: flex;
    justify-content: flex-end;
    gap: var(--space-2);
  }
  @media (prefers-reduced-motion: reduce) {
    .twisty :global(svg) {
      transition: none;
    }
  }
</style>
