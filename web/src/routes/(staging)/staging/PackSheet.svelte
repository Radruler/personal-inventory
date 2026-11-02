<script lang="ts">
  // "Pack into box…": pick an existing box under the target, or create a new
  // one (code + name, inside the target) in the same flow; the caller then
  // does one POST /api/moves.
  import { untrack } from 'svelte';
  import type { LocationNode, LocationRef } from '$lib/api/client';
  import Button from '$lib/components/Button.svelte';
  import Icon from '$lib/components/Icon.svelte';
  import LoadingState from '$lib/components/LoadingState.svelte';
  import Sheet from '$lib/components/Sheet.svelte';
  import { errorMessage } from '$lib/components/format';
  import { boxesUnder, boxParents, nextCode, plural } from '../../(locations)/shared/places';
  import { createAndReload, loadTree, tree } from '../../(locations)/shared/store.svelte';

  let {
    open = $bindable(false),
    target,
    count,
    onpack
  }: {
    open?: boolean;
    /** The staging target (Unit 2). */
    target: LocationRef;
    /** Number of selected items. */
    count: number;
    /** Does the move into the chosen box id. Throw to keep the sheet open. */
    onpack: (boxId: string, box: { code: string | null; name: string; label: string }) => Promise<void>;
  } = $props();

  const uid = $props.id();
  let mode = $state<'pick' | 'new'>('pick');
  let picked = $state<string | null>(null);
  let code = $state('');
  let name = $state('');
  let parent = $state('');
  let busy = $state(false);
  let error = $state('');

  const boxes = $derived(boxesUnder(tree.roots, target.id));
  const parents = $derived(boxParents(tree.roots, target.id));

  $effect(() => {
    if (!open) return;
    untrack(() => {
      error = '';
      picked = null;
      name = '';
      parent = target.id;
      loadTree(true).then(() => {
        code = nextCode(tree.roots);
        mode = boxesUnder(tree.roots, target.id).length ? 'pick' : 'new';
      });
    });
  });

  async function submit(e?: SubmitEvent) {
    e?.preventDefault();
    error = '';
    busy = true;
    try {
      if (mode === 'pick') {
        const b = boxes.find((x) => x.id === picked);
        if (!b) {
          error = 'Pick a box';
          return;
        }
        await onpack(b.id, b);
      } else {
        if (!code.trim() && !name.trim()) {
          error = 'Give the box a code or a name';
          return;
        }
        const c = code.trim().toUpperCase() || null;
        const n = name.trim() || c || 'Box';
        const id = await createAndReload({ code: c, name: n, kind: 'container', parent: parent || target.id });
        const node = boxesUnder(tree.roots, target.id).find((x) => x.id === id);
        await onpack(id, node ?? { code: c, name: n, label: c ?? n });
      }
      open = false;
    } catch (err) {
      error = errorMessage(err);
    } finally {
      busy = false;
    }
  }

  function boxLine(b: LocationNode): string {
    const where = b.label.split(' › ').slice(0, -1).join(' › ');
    return [b.code && b.name !== b.code ? b.name : '', where !== target.name ? where : ''].filter(Boolean).join(' · ');
  }
</script>

<Sheet bind:open title="Pack {plural(count, 'item')} into…" side="auto">
  {#if !tree.loaded}
    <LoadingState variant="list" count={4} />
  {:else}
    <div class="modes" role="radiogroup" aria-label="Box">
      <button type="button" role="radio" aria-checked={mode === 'pick'} class:on={mode === 'pick'} disabled={!boxes.length} onclick={() => (mode = 'pick')}>
        Existing box <span class="n">{boxes.length}</span>
      </button>
      <button type="button" role="radio" aria-checked={mode === 'new'} class:on={mode === 'new'} onclick={() => (mode = 'new')}>New box</button>
    </div>

    {#if mode === 'pick'}
      <ul class="boxes" role="listbox" aria-label="Boxes in {target.name}">
        {#each boxes as b (b.id)}
          <li role="option" aria-selected={picked === b.id}>
            <button type="button" class:on={picked === b.id} onclick={() => (picked = b.id)} ondblclick={() => submit()}>
              <Icon name="container" size={22} />
              <span class="bt">
                <span class="bc mono">{b.code ?? b.name}</span>
                <span class="bs muted">{boxLine(b)}</span>
              </span>
              <span class="bn muted">{b.total_count}</span>
              {#if picked === b.id}<Icon name="check" size={20} />{/if}
            </button>
          </li>
        {/each}
      </ul>
    {:else}
      <form id="{uid}-new" class="new" onsubmit={submit}>
        <div class="two">
          <label>
            <span>Code <small class="muted">(write it on the box)</small></span>
            <input bind:value={code} class="mono big" autocomplete="off" autocapitalize="characters" spellcheck="false" placeholder="B-021" />
          </label>
          <label>
            <span>Name <small class="muted">(optional)</small></span>
            <input bind:value={name} autocomplete="off" placeholder="Winter boots" />
          </label>
        </div>
        <label>
          <span>Goes in</span>
          <select bind:value={parent}>
            {#each parents as p (p.id)}<option value={p.id}>{p.label}</option>{/each}
          </select>
        </label>
      </form>
    {/if}
    {#if error}<p class="err" role="alert">{error}</p>{/if}
  {/if}
  {#snippet footer()}
    <Button variant="ghost" onclick={() => (open = false)}>Cancel</Button>
    <Button variant="primary" loading={busy} disabled={mode === 'pick' && !picked} onclick={() => submit()}>
      {mode === 'new' ? `Create ${code.trim() || 'box'} and pack` : 'Pack'}
    </Button>
  {/snippet}
</Sheet>

<style>
  .modes {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: var(--space-1);
    padding: var(--space-1);
    margin-bottom: var(--space-4);
    border-radius: var(--radius-md);
    background: var(--color-surface-2);
  }
  .modes button {
    min-height: var(--tap-min);
    border: 0;
    border-radius: var(--radius-sm);
    background: none;
    color: var(--color-text-muted);
    font-weight: var(--weight-medium);
    cursor: pointer;
  }
  .modes button.on {
    background: var(--color-surface);
    color: var(--color-text);
    box-shadow: var(--shadow-sm);
  }
  .modes button:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .n {
    color: var(--color-text-faint);
  }
  .boxes {
    display: grid;
    gap: var(--space-2);
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .boxes button {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    width: 100%;
    min-height: calc(var(--tap-min) + var(--space-3));
    padding: var(--space-2) var(--space-3);
    border: var(--border-width) solid var(--color-border);
    border-radius: var(--radius-md);
    background: var(--color-surface);
    text-align: left;
    cursor: pointer;
  }
  .boxes button.on {
    border-color: var(--color-accent);
    background: var(--color-accent-soft);
    color: var(--color-accent);
  }
  .bt {
    flex: 1;
    min-width: 0;
    display: grid;
  }
  .bc {
    font-size: var(--text-lg);
    font-weight: var(--weight-bold);
    color: var(--color-text);
  }
  .bs {
    font-size: var(--text-sm);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .bn {
    font-variant-numeric: tabular-nums;
  }
  .new {
    display: grid;
    gap: var(--space-4);
  }
  .two {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: var(--space-3);
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
    font-size: var(--text-md);
  }
  .big {
    font-size: var(--text-lg);
    font-weight: var(--weight-bold);
  }
  .err {
    margin-top: var(--space-3);
    color: var(--color-danger);
    font-size: var(--text-sm);
  }
</style>
