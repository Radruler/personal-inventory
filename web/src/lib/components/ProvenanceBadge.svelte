<script lang="ts">
  // Where a fact came from: user / observed / looked_up / inferred, colored
  // with --basis-* tokens, plus a confidence meter. A button when onclick is set.
  import type { Schemas } from '$lib/api/client';
  import { BASIS_LABEL, formatConfidence } from './format';

  let {
    basis,
    confidence = null,
    compact = false,
    onclick
  }: {
    basis: Schemas['FactBasis'];
    /** 0..1; hidden when null. */
    confidence?: number | null;
    /** Dot + percentage only (label kept for screen readers). */
    compact?: boolean;
    onclick?: (e: MouseEvent) => void;
  } = $props();

  const pct = $derived(formatConfidence(confidence));
  const aria = $derived(`Source: ${BASIS_LABEL[basis] ?? basis}${pct ? `, confidence ${pct}` : ''}`);
</script>

{#snippet body()}
  <span class="dot" aria-hidden="true"></span>
  {#if !compact}<span class="basis" aria-hidden="true">{BASIS_LABEL[basis] ?? basis}</span>{/if}
  {#if pct}
    <span class="meter" aria-hidden="true"><span style:width={pct}></span></span>
    <span class="pct" aria-hidden="true">{pct}</span>
  {/if}
  <span class="visually-hidden">{aria}</span>
{/snippet}

{#if onclick}
  <button type="button" class="prov b-{basis}" class:compact {onclick}>{@render body()}</button>
{:else}
  <span class="prov b-{basis}" class:compact>{@render body()}</span>
{/if}

<style>
  .prov {
    --prov-color: var(--basis-inferred);
    position: relative;
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    min-height: 22px;
    padding: 0 var(--space-2);
    border: var(--border-width) solid color-mix(in srgb, var(--prov-color) 35%, transparent);
    border-radius: var(--radius-pill);
    background: color-mix(in srgb, var(--prov-color) 10%, var(--color-surface));
    color: var(--prov-color);
    font-size: var(--text-xs);
    font-weight: var(--weight-medium);
    line-height: 1;
    white-space: nowrap;
  }
  button.prov {
    cursor: pointer;
  }
  button.prov::after {
    content: '';
    position: absolute;
    inset: -11px -4px;
  }
  button.prov:hover {
    background: color-mix(in srgb, var(--prov-color) 18%, var(--color-surface));
  }
  .b-user {
    --prov-color: var(--basis-user);
  }
  .b-observed {
    --prov-color: var(--basis-observed);
  }
  .b-looked_up {
    --prov-color: var(--basis-looked-up);
  }
  .b-inferred {
    --prov-color: var(--basis-inferred);
  }
  .dot {
    width: 8px;
    height: 8px;
    border-radius: var(--radius-pill);
    background: currentColor;
  }
  .meter {
    position: relative;
    width: 24px;
    height: 4px;
    border-radius: var(--radius-pill);
    background: color-mix(in srgb, currentColor 22%, transparent);
    overflow: hidden;
  }
  .meter span {
    position: absolute;
    inset: 0 auto 0 0;
    background: currentColor;
  }
  .pct {
    font-variant-numeric: tabular-nums;
    color: var(--color-text-muted);
  }
  .compact {
    gap: var(--space-1);
  }
</style>
