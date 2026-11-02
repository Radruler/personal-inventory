<script lang="ts">
  // Verbatim tag text: line breaks kept, [?] uncertainty marks highlighted.
  import { splitUncertain } from './detail';

  let { text }: { text: string } = $props();
  const parts = $derived(splitUncertain(text));
</script>

<p class="verbatim text">{#each parts as p, i (i)}{#if p.uncertain}<mark title="Uncertain reading">{p.text}</mark>{:else}{p.text}{/if}{/each}</p>

<style>
  .text {
    margin: 0;
    padding: var(--space-2) var(--space-3);
    border-radius: var(--radius-sm);
    background: var(--color-bg);
    border: var(--border-width) dashed var(--color-border-strong);
    overflow-wrap: anywhere;
  }
  mark {
    padding: 0 var(--space-0);
    border-radius: var(--radius-sm);
    background: var(--color-warning-soft);
    color: var(--color-warning);
    font-weight: var(--weight-bold);
  }
</style>
