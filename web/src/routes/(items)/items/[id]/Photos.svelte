<script lang="ts">
  // Item photos: the role-tabbed Gallery, "Set as cover", and "Add photos"
  // (uploads linked to this item with the chosen role; HEIC allowed).
  import { api, unwrap, uploadMedia, type ItemDetail } from '$lib/api/client';
  import { Button, Gallery, errorMessage, toast } from '$lib/components';
  import type { GalleryTabId } from '$lib/components/gallery';
  import { roleForTab } from './detail';

  let {
    item,
    roles,
    onchange,
    onrefresh
  }: {
    item: ItemDetail;
    /** Media roles for the upload picker. */
    roles: { value: string; label: string }[];
    /** PATCH result (cover change). */
    onchange: (item: ItemDetail) => void;
    /** Refetch after uploads. */
    onrefresh: () => Promise<void> | void;
  } = $props();

  let tab = $state<GalleryTabId>();
  let role = $state('flat_lay');
  let fileInput = $state<HTMLInputElement>();
  let uploading = $state<{ done: number; total: number; frac: number } | null>(null);
  let coverBusy = $state(false);

  // Default the upload role to the tab being looked at.
  $effect(() => {
    role = roleForTab(tab);
  });

  const coverId = $derived(item.cover_media_id ?? item.cover?.id ?? null);

  async function setCover(mediaId: string) {
    coverBusy = true;
    try {
      onchange(unwrap(await api.PATCH('/api/items/{id}', { params: { path: { id: item.id } }, body: { cover_media_id: mediaId } })));
      toast('Cover updated', { tone: 'success' });
    } catch (e) {
      toast(`Couldn't set the cover: ${errorMessage(e)}`, { tone: 'error' });
    } finally {
      coverBusy = false;
    }
  }

  async function upload(files: FileList | null) {
    if (!files?.length) return;
    const list = [...files];
    uploading = { done: 0, total: list.length, frac: 0 };
    let failed = 0;
    let dup = 0;
    for (const f of list) {
      try {
        const res = await uploadMedia(f, {
          link: { subject_type: 'item', subject_id: item.id, role },
          onProgress: (frac) => uploading && (uploading.frac = frac)
        });
        if (res.deduplicated) dup++;
      } catch (e) {
        failed++;
        toast(`${f.name}: ${errorMessage(e)}`, { tone: 'error' });
      }
      if (uploading) uploading = { done: uploading.done + 1, total: list.length, frac: 0 };
    }
    uploading = null;
    if (fileInput) fileInput.value = '';
    const ok = list.length - failed;
    if (ok) toast(`Added ${ok} ${ok === 1 ? 'photo' : 'photos'}${dup ? ` (${dup} already stored)` : ''}`, { tone: 'success' });
    await onrefresh();
  }
</script>

<div class="photos">
  <Gallery media={item.media} title={item.title} bind:tab {coverId} emptyMessage="No photos of this item yet.">
    {#snippet actions(p)}
      {#if item.cover_media_id === p.media.id}
        <span class="muted small">Chosen as cover</span>
      {:else}
        <Button size="sm" variant="ghost" icon="check" loading={coverBusy} onclick={() => setCover(p.media.id)}>Set as cover</Button>
      {/if}
      <Button size="sm" variant="ghost" icon="external" href={p.media.orig_url} target="_blank" rel="noopener">Original</Button>
    {/snippet}
  </Gallery>

  <div class="add">
    <label class="role">
      <span class="visually-hidden">Role for new photos</span>
      <select bind:value={role} disabled={!!uploading}>
        {#each roles as r (r.value)}<option value={r.value}>{r.label}</option>{/each}
      </select>
    </label>
    <Button icon="plus" loading={!!uploading} onclick={() => fileInput?.click()}>
      {#if uploading}
        Uploading {uploading.done + 1}/{uploading.total}{uploading.frac ? ` · ${Math.round(uploading.frac * 100)}%` : ''}
      {:else}
        Add photos
      {/if}
    </Button>
    <input
      bind:this={fileInput}
      class="visually-hidden"
      type="file"
      accept="image/*,image/heic,image/heif,.heic,.heif"
      multiple
      tabindex="-1"
      aria-hidden="true"
      onchange={(e) => upload(e.currentTarget.files)}
    />
  </div>
</div>

<style>
  .photos {
    display: grid;
    gap: var(--space-3);
    min-width: 0;
  }
  .add {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
    align-items: center;
  }
  .role select {
    min-height: var(--tap-min);
    padding: 0 var(--space-3);
    border: var(--border-width) solid var(--color-border);
    border-radius: var(--radius-md);
    background: var(--color-surface);
  }
  .small {
    font-size: var(--text-sm);
  }
</style>
