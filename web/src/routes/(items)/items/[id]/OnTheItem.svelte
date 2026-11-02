<script lang="ts">
  // "On the item": what is literally written on it. Observations verbatim
  // (line breaks kept, [?] marked) with the source photo (cropped to the
  // region when there is one), plus identifiers grouped by scheme.
  import type { Identifier, Observation } from '$lib/api/client';
  import { Icon, MediaViewer, type ViewerPhoto } from '$lib/components';
  import { hasVariant } from '$lib/components/gallery';
  import Verbatim from './Verbatim.svelte';
  import { actorLabel, formatDateTime, groupIdentifiers, regionStyle, type SchemaLite } from './detail';

  let {
    observations,
    identifiers,
    sourceLabels,
    schemes,
    title
  }: {
    observations: Observation[];
    identifiers: Identifier[];
    sourceLabels: Map<string, string>;
    schemes: Map<string, SchemaLite['identifier_schemes'][number]>;
    title: string;
  } = $props();

  const groups = $derived(groupIdentifiers(identifiers));
  const obsIndex = $derived(new Map(observations.map((o, i) => [o.id, i + 1])));

  let viewerOpen = $state(false);
  let viewerPhotos = $state<ViewerPhoto[]>([]);
  function view(o: Observation) {
    if (!o.media) return;
    viewerPhotos = [{ media: o.media, caption: sourceLabels.get(o.source) ?? o.source }];
    viewerOpen = true;
  }
</script>

{#if observations.length}
  <ol class="obs">
    {#each observations as o, i (o.id)}
      <li class="ob">
        {#if o.media}
          {@const r = o.region ? regionStyle(o.region, o.media.width, o.media.height) : null}
          <button type="button" class="src" onclick={() => view(o)} aria-label="Open the source photo full screen" style:aspect-ratio={r?.aspect ?? undefined}>
            {#if hasVariant(o.media, 'thumb')}
              {#if r}
                <img class="crop" src={o.media.thumb_url} alt="" style={r.img} loading="lazy" />
              {:else}
                <img src={o.media.thumb_url} alt="" loading="lazy" />
              {/if}
            {:else}
              <Icon name="image" size={24} />
            {/if}
          </button>
        {/if}
        <div class="obody">
          <p class="ohead">
            <strong>{sourceLabels.get(o.source) ?? o.source}</strong>
            {#if observations.length > 1}<span class="muted">#{i + 1}</span>{/if}
          </p>
          {#if o.text}
            <Verbatim text={o.text} />
          {:else}
            <p class="muted">No text transcribed.</p>
          {/if}
          {#if o.data && Object.keys(o.data).length}
            <details>
              <summary>As read (structured)</summary>
              <pre class="data">{JSON.stringify(o.data, null, 2)}</pre>
            </details>
          {/if}
          <p class="meta muted">{actorLabel(o.recorded_by)} · {formatDateTime(o.recorded_at)}</p>
        </div>
      </li>
    {/each}
  </ol>
{/if}

{#if groups.length}
  <dl class="ids" aria-label="Identifiers">
    {#each groups as g (g.scheme)}
      {@const sch = schemes.get(g.scheme)}
      <div class="id">
        <dt title={sch?.description ?? undefined}>{sch?.label ?? g.scheme}</dt>
        <dd>
          {#each g.identifiers as id (id.id)}
            <span class="code">
              <span class="mono">{id.value}</span>
              {#if id.observation_id && obsIndex.has(id.observation_id) && observations.length > 1}
                <span class="muted from">from #{obsIndex.get(id.observation_id)}</span>
              {/if}
              {#if id.note}<span class="muted from">{id.note}</span>{/if}
            </span>
          {/each}
        </dd>
      </div>
    {/each}
  </dl>
{/if}

{#if !observations.length && !groups.length}
  <p class="muted empty">Nothing transcribed from labels or tags yet.</p>
{/if}

<MediaViewer photos={viewerPhotos} bind:open={viewerOpen} {title} />

<style>
  .obs {
    display: grid;
    gap: var(--space-3);
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .ob {
    display: grid;
    grid-template-columns: 88px 1fr;
    gap: var(--space-3);
    align-items: start;
  }
  .ob:not(:has(.src)) {
    grid-template-columns: 1fr;
  }
  .src {
    position: relative;
    display: grid;
    place-items: center;
    width: 88px;
    max-height: 160px;
    aspect-ratio: 3 / 4;
    padding: 0;
    border: var(--border-width) solid var(--color-border);
    border-radius: var(--radius-sm);
    overflow: hidden;
    background: var(--color-surface-2);
    color: var(--color-text-faint);
    cursor: zoom-in;
  }
  .src img {
    width: 100%;
    height: 100%;
    object-fit: cover;
  }
  .src img.crop {
    position: absolute;
    max-width: none;
    object-fit: fill;
  }
  .obody {
    display: grid;
    gap: var(--space-1);
    min-width: 0;
  }
  .ohead {
    display: flex;
    gap: var(--space-2);
    align-items: baseline;
  }
  .meta {
    font-size: var(--text-xs);
  }
  details {
    font-size: var(--text-sm);
  }
  summary {
    min-height: var(--tap-min);
    display: flex;
    align-items: center;
    color: var(--color-text-muted);
    cursor: pointer;
  }
  .data {
    margin: 0;
    padding: var(--space-2);
    border-radius: var(--radius-sm);
    background: var(--color-surface-2);
    overflow-x: auto;
    font-size: var(--text-xs);
  }
  .ids {
    display: grid;
    gap: 0;
    margin: var(--space-3) 0 0;
    border-top: var(--border-width) solid var(--color-border);
  }
  .id {
    display: grid;
    grid-template-columns: minmax(7rem, 35%) 1fr;
    gap: var(--space-2);
    padding: var(--space-2) 0;
    border-bottom: var(--border-width) solid var(--color-border);
  }
  dt {
    color: var(--color-text-muted);
    font-size: var(--text-sm);
  }
  dd {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-1) var(--space-3);
    margin: 0;
    min-width: 0;
  }
  .code {
    display: inline-flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: var(--space-1);
    overflow-wrap: anywhere;
  }
  .from {
    font-size: var(--text-xs);
  }
  .empty {
    font-size: var(--text-sm);
  }
</style>
