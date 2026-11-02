<script lang="ts">
  // A row of photo thumbnails, each with a role select (flat lay, label, …).
  import Icon from '$lib/components/Icon.svelte';
  import { roleLabel } from '$lib/components/format';
  import { ITEM_ROLES } from './places';

  let {
    photos,
    roles = $bindable({}),
    onremove
  }: {
    photos: { id: string; thumb: string | null; name?: string | null; busy?: number | null }[];
    /** media id → role (bindable). */
    roles?: Record<string, string>;
    /** Shows a remove button per photo. */
    onremove?: (id: string) => void;
  } = $props();

  // Local previews of HEIC files don't decode outside Safari: fall back to an icon.
  let broken = $state<Record<string, string>>({});
</script>

<ul class="roles">
  {#each photos as p (p.id)}
    <li>
      <div class="thumb">
        {#if p.thumb && broken[p.id] !== p.thumb}
          <img src={p.thumb} alt={p.name ?? ''} onerror={() => (broken[p.id] = p.thumb ?? '')} />
        {:else}
          <span class="ph"><Icon name="image" size={24} /></span>
        {/if}
        {#if p.busy !== undefined && p.busy !== null}
          <span class="bar" style:--p={p.busy} aria-label="Uploading {Math.round(p.busy * 100)}%"></span>
        {/if}
        {#if onremove}
          <button type="button" class="rm" aria-label="Remove photo" onclick={() => onremove?.(p.id)}><Icon name="close" size={16} /></button>
        {/if}
      </div>
      <select bind:value={roles[p.id]} aria-label="Role of photo {p.name ?? ''}">
        {#each ITEM_ROLES as r (r)}<option value={r}>{roleLabel(r)}</option>{/each}
      </select>
    </li>
  {/each}
</ul>

<style>
  .roles {
    display: flex;
    gap: var(--space-3);
    margin: 0;
    padding: 0 0 var(--space-1);
    list-style: none;
    overflow-x: auto;
  }
  li {
    flex: none;
    display: grid;
    gap: var(--space-1);
    width: 112px;
  }
  .thumb {
    position: relative;
    aspect-ratio: 1;
    border-radius: var(--radius-sm);
    overflow: hidden;
    background: var(--color-surface-2);
  }
  img {
    width: 100%;
    height: 100%;
    object-fit: cover;
  }
  .ph {
    display: grid;
    place-items: center;
    height: 100%;
    color: var(--color-text-faint);
  }
  .bar {
    position: absolute;
    inset: auto 0 0 0;
    height: 4px;
    background: linear-gradient(to right, var(--color-accent) calc(var(--p) * 100%), var(--color-surface-3) 0);
  }
  .rm {
    position: absolute;
    top: 0;
    right: 0;
    display: grid;
    place-items: center;
    width: var(--tap-min);
    height: var(--tap-min);
    padding: 0;
    border: 0;
    background: none;
    color: var(--color-viewer-fg);
    cursor: pointer;
  }
  .rm :global(svg) {
    padding: 2px;
    border-radius: var(--radius-pill);
    background: var(--color-overlay);
    width: 22px;
    height: 22px;
  }
  select {
    width: 100%;
    min-height: var(--tap-min);
    padding: 0 var(--space-2);
    border: var(--border-width) solid var(--color-border-strong);
    border-radius: var(--radius-sm);
    background: var(--color-surface);
    font-size: var(--text-sm);
  }
</style>
