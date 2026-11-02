<script lang="ts">
  // A form field showing the chosen location; tapping opens the picker sheet
  // (tree + search + create). `null` means "unknown" / not set.
  import Icon from '$lib/components/Icon.svelte';
  import { LOCATION_KIND_ICON } from '$lib/components/icons';
  import PickLocationSheet from './PickLocationSheet.svelte';
  import { findNode } from './places';
  import { loadTree, tree } from './store.svelte';

  let {
    value = $bindable(null),
    label,
    title = label,
    noneText = 'Unknown',
    noneLabel = 'Location unknown',
    onchange
  }: {
    value?: string | null;
    label: string;
    /** Sheet title. */
    title?: string;
    /** Shown when value is null. */
    noneText?: string;
    /** Button in the sheet that sets null. */
    noneLabel?: string;
    onchange?: (id: string | null) => void;
  } = $props();

  const uid = $props.id();
  let open = $state(false);

  $effect(() => {
    if (value) void loadTree();
  });

  const node = $derived(findNode(tree.roots, value));
</script>

<div class="field">
  <span class="lbl" id="{uid}-l">{label}</span>
  <button type="button" class="val" aria-labelledby="{uid}-l {uid}-v" onclick={() => (open = true)}>
    <Icon name={node ? (LOCATION_KIND_ICON[node.kind] ?? 'placed') : 'unknown'} size={20} />
    <span id="{uid}-v" class="txt" class:none={!value}>
      {#if node}{node.label}{:else if value && !tree.loaded}…{:else}{noneText}{/if}
    </span>
    <Icon name="chevron-right" size={18} />
  </button>
</div>

<PickLocationSheet
  bind:open
  {title}
  {value}
  confirmLabel="Choose"
  {noneLabel}
  canSelect={(n) => !n.archived_at}
  onpick={(id) => {
    value = id;
    onchange?.(id);
  }}
/>

<style>
  .field {
    display: grid;
    gap: var(--space-1);
    min-width: 0;
  }
  .lbl {
    font-size: var(--text-sm);
    font-weight: var(--weight-medium);
  }
  .val {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    width: 100%;
    min-height: var(--tap-min);
    padding: 0 var(--space-3);
    border: var(--border-width) solid var(--color-border-strong);
    border-radius: var(--radius-md);
    background: var(--color-surface);
    color: var(--color-text-muted);
    text-align: left;
    cursor: pointer;
  }
  .txt {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--color-text);
  }
  .txt.none {
    color: var(--color-text-muted);
  }
</style>
