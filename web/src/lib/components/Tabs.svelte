<script lang="ts" module>
  export type TabDef = { id: string; label: string; count?: number };
</script>

<script lang="ts">
  // WAI-ARIA tabs: arrow keys / Home / End move and activate. Renders the
  // panel too when a `panel` snippet is given (it receives the active id).
  import type { Snippet } from 'svelte';

  let {
    tabs,
    value = $bindable(),
    label,
    onchange,
    panel
  }: {
    tabs: TabDef[];
    /** Active tab id (bindable). Defaults to the first tab. */
    value?: string;
    /** aria-label of the tab list. */
    label: string;
    onchange?: (id: string) => void;
    panel?: Snippet<[string]>;
  } = $props();

  const uid = $props.id();
  let list = $state<HTMLElement>();
  const active = $derived(tabs.some((t) => t.id === value) ? value! : tabs[0]?.id);

  function select(id: string, focus = false) {
    value = id;
    onchange?.(id);
    if (focus) list?.querySelector<HTMLElement>(`#${CSS.escape(`${uid}-tab-${id}`)}`)?.focus();
  }

  function onkeydown(e: KeyboardEvent) {
    const i = tabs.findIndex((t) => t.id === active);
    let next = -1;
    if (e.key === 'ArrowRight') next = (i + 1) % tabs.length;
    else if (e.key === 'ArrowLeft') next = (i - 1 + tabs.length) % tabs.length;
    else if (e.key === 'Home') next = 0;
    else if (e.key === 'End') next = tabs.length - 1;
    if (next < 0) return;
    e.preventDefault();
    select(tabs[next].id, true);
  }
</script>

<div class="tabs">
  <div class="list" role="tablist" aria-label={label} tabindex="-1" bind:this={list} {onkeydown}>
    {#each tabs as t (t.id)}
      <button
        type="button"
        role="tab"
        id="{uid}-tab-{t.id}"
        aria-selected={t.id === active}
        aria-controls="{uid}-panel"
        tabindex={t.id === active ? 0 : -1}
        onclick={() => select(t.id)}
      >
        {t.label}
        {#if t.count !== undefined}<span class="count">{t.count}</span>{/if}
      </button>
    {/each}
  </div>
  {#if panel && active}
    <div class="panel" role="tabpanel" id="{uid}-panel" aria-labelledby="{uid}-tab-{active}">
      {@render panel(active)}
    </div>
  {/if}
</div>

<style>
  .list {
    display: flex;
    gap: var(--space-1);
    overflow-x: auto;
    scrollbar-width: none;
    border-bottom: var(--border-width) solid var(--color-border);
  }
  .list::-webkit-scrollbar {
    display: none;
  }
  .list:focus {
    outline: none;
  }
  [role='tab'] {
    flex: none;
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    min-height: var(--tap-min);
    padding: 0 var(--space-3);
    border: 0;
    border-bottom: 2px solid transparent;
    margin-bottom: calc(var(--border-width) * -1);
    background: transparent;
    color: var(--color-text-muted);
    font-weight: var(--weight-medium);
    cursor: pointer;
    white-space: nowrap;
  }
  [role='tab']:hover {
    color: var(--color-text);
  }
  [role='tab'][aria-selected='true'] {
    color: var(--color-text);
    border-bottom-color: var(--color-accent);
  }
  [role='tab']:focus-visible {
    outline-offset: -2px;
  }
  .count {
    color: var(--color-text-faint);
    font-size: var(--text-xs);
    font-variant-numeric: tabular-nums;
  }
  .panel {
    padding-top: var(--space-3);
  }
  .panel:focus-visible {
    outline-offset: 4px;
  }
</style>
