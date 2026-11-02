<script lang="ts">
  // Projected attributes as a definition list; each value has its
  // provenance badge, and tapping the row opens the provenance sheet.
  import { Badge, Icon, ProvenanceBadge } from '$lib/components';
  import type { AttrRow } from './detail';

  let { rows, onopen }: { rows: AttrRow[]; onopen: (key: string) => void } = $props();
</script>

<dl class="attrs">
  {#each rows as r (r.key)}
    <div class="attr">
      <dt>{r.label}</dt>
      <dd>
        <button type="button" class="val" onclick={() => onopen(r.key)} aria-label="{r.label}: {r.text}. Show where this came from">
          <span class="text">{r.text}</span>
          <span class="badges">
            {#if r.fromProduct}<Badge tone="info">Product</Badge>{/if}
            {#if r.fact}<ProvenanceBadge basis={r.fact.basis} confidence={r.fact.confidence} compact />{/if}
            <Icon name="chevron-right" size={16} />
          </span>
        </button>
      </dd>
    </div>
  {/each}
</dl>

<style>
  .attrs {
    display: grid;
    margin: 0;
    border-top: var(--border-width) solid var(--color-border);
  }
  .attr {
    display: grid;
    grid-template-columns: minmax(7rem, 35%) 1fr;
    align-items: center;
    gap: var(--space-2);
    border-bottom: var(--border-width) solid var(--color-border);
  }
  dt {
    color: var(--color-text-muted);
    font-size: var(--text-sm);
  }
  dd {
    margin: 0;
    min-width: 0;
  }
  .val {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-2);
    width: 100%;
    min-height: var(--tap-min);
    padding: var(--space-1) 0;
    border: 0;
    background: none;
    text-align: left;
    cursor: pointer;
  }
  .val:hover .text {
    color: var(--color-accent);
  }
  .text {
    min-width: 0;
    overflow-wrap: anywhere;
    font-weight: var(--weight-medium);
  }
  .badges {
    flex: none;
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    color: var(--color-text-faint);
  }
</style>
