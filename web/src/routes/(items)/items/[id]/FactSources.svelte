<script lang="ts">
  // A fact's sources, resolved: observation (source label + verbatim text),
  // reference (link + archived copy), photo, URL or note.
  import type { Fact, ItemDetail } from '$lib/api/client';
  import { Icon } from '$lib/components';
  import { hasVariant } from '$lib/components/gallery';
  import Verbatim from './Verbatim.svelte';
  import { resolveSources } from './detail';

  let {
    fact,
    item,
    sourceLabels,
    compact = false
  }: {
    fact: Pick<Fact, 'sources'>;
    item: ItemDetail;
    sourceLabels: Map<string, string>;
    compact?: boolean;
  } = $props();

  const sources = $derived(resolveSources(fact, item));
</script>

{#if sources.length}
  <ul class="sources" class:compact aria-label="Sources">
    {#each sources as s, i (i)}
      <li>
        {#if s.type === 'observation'}
          <span class="kind"><Icon name="review" size={14} /> Read from {s.observation ? (sourceLabels.get(s.observation.source) ?? s.observation.source).toLowerCase() : 'an observation'}</span>
          {#if s.observation?.text && !compact}
            <Verbatim text={s.observation.text} />
          {:else if !s.observation}
            <span class="muted mono">{s.id}</span>
          {/if}
        {:else if s.type === 'reference'}
          <span class="kind"><Icon name="external" size={14} /> Reference</span>
          {#if s.reference?.url}
            <a href={s.reference.url} target="_blank" rel="noopener noreferrer">{s.reference.title || s.reference.url}</a>
          {:else}
            <span>{s.reference?.title ?? s.id}</span>
          {/if}
          {#if s.reference?.archive}
            <a class="small" href={s.reference.archive.orig_url} target="_blank" rel="noopener">Archived copy</a>
          {/if}
        {:else if s.type === 'media'}
          <span class="kind"><Icon name="image" size={14} /> Photo</span>
          {#if s.media && hasVariant(s.media, 'thumb')}
            <a href={s.media.orig_url} target="_blank" rel="noopener"><img src={s.media.thumb_url} alt="The source" width="64" height="64" /></a>
          {/if}
        {:else if s.type === 'url'}
          <span class="kind"><Icon name="external" size={14} /> Link</span>
          <a href={s.url} target="_blank" rel="noopener noreferrer" class="url">{s.url}</a>
        {:else}
          <span class="kind"><Icon name="info" size={14} /> Note</span>
          <span>{s.note}</span>
        {/if}
      </li>
    {/each}
  </ul>
{/if}

<style>
  .sources {
    display: grid;
    gap: var(--space-2);
    margin: 0;
    padding: 0;
    list-style: none;
    font-size: var(--text-sm);
  }
  .sources li {
    display: grid;
    gap: var(--space-1);
    min-width: 0;
  }
  .compact li {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
  }
  .kind {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    color: var(--color-text-muted);
    font-size: var(--text-xs);
    font-weight: var(--weight-medium);
  }
  .url {
    overflow-wrap: anywhere;
  }
  .small {
    font-size: var(--text-xs);
  }
  img {
    width: 64px;
    height: 64px;
    object-fit: cover;
    border-radius: var(--radius-sm);
  }
</style>
