<script lang="ts">
  // Bulk upload to the Inbox from the camera roll: pick many photos, add an
  // optional note per photo, then upload them one at a time with progress.
  // accept includes HEIC/HEIF and image/* (never JPEG-only) so iOS can send
  // originals. Duplicates (same bytes) are reported, not re-stored.
  import { onDestroy } from 'svelte';
  import { uploadMedia, type Schemas } from '$lib/api/client';
  import Button from '$lib/components/Button.svelte';
  import Icon from '$lib/components/Icon.svelte';
  import { errorMessage } from '$lib/components/format';
  import { toast } from '$lib/components/toast.svelte';
  import ContextNote from '../../(locations)/shared/ContextNote.svelte';
  import { plural } from '../../(locations)/shared/places';

  type Status = 'queued' | 'uploading' | 'done' | 'dup' | 'error';
  type Entry = {
    key: number;
    file: File;
    preview: string;
    broken: boolean;
    context: string;
    status: Status;
    progress: number;
    error: string;
    media: Schemas['MediaUploadResult'] | null;
  };

  let { onuploaded }: { onuploaded?: () => void } = $props();

  let entries = $state<Entry[]>([]);
  let running = $state(false);
  let dragging = $state(false);
  let seq = 0;
  const ACCEPT = 'image/*,image/heic,image/heif,.heic,.heif';

  function add(files: Iterable<File>) {
    for (const file of files) {
      if (entries.some((e) => e.status === 'queued' && e.file.name === file.name && e.file.size === file.size)) continue;
      entries.push({
        key: ++seq,
        file,
        preview: URL.createObjectURL(file),
        broken: false,
        context: '',
        status: 'queued',
        progress: 0,
        error: '',
        media: null
      });
    }
  }

  function onpick(e: Event) {
    const input = e.currentTarget as HTMLInputElement;
    add(input.files ?? []);
    input.value = '';
  }

  function remove(key: number) {
    const i = entries.findIndex((e) => e.key === key);
    if (i < 0) return;
    URL.revokeObjectURL(entries[i].preview);
    entries.splice(i, 1);
  }

  function clearFinished() {
    for (const e of entries.filter((x) => x.status === 'done' || x.status === 'dup')) remove(e.key);
  }

  onDestroy(() => entries.forEach((e) => URL.revokeObjectURL(e.preview)));

  const queued = $derived(entries.filter((e) => e.status === 'queued' || e.status === 'error'));
  const finished = $derived(entries.filter((e) => e.status === 'done' || e.status === 'dup').length);

  async function uploadAll() {
    running = true;
    let stored = 0;
    let dups = 0;
    for (const e of entries) {
      if (e.status !== 'queued' && e.status !== 'error') continue;
      e.status = 'uploading';
      e.progress = 0;
      e.error = '';
      try {
        const res = await uploadMedia(e.file, { context: e.context.trim() || undefined, onProgress: (p) => (e.progress = p) });
        e.media = res;
        e.status = res.deduplicated ? 'dup' : 'done';
        if (res.deduplicated) dups++;
        else stored++;
      } catch (err) {
        e.status = 'error';
        e.error = errorMessage(err);
      }
    }
    running = false;
    const failed = entries.filter((e) => e.status === 'error').length;
    const parts = [stored && `${plural(stored, 'photo')} added`, dups && `${dups} already stored`, failed && `${failed} failed`].filter(Boolean);
    if (parts.length) toast(parts.join(' · '), { tone: failed ? 'error' : 'success' });
    onuploaded?.();
  }

  function statusText(e: Entry): string {
    switch (e.status) {
      case 'queued':
        return 'Ready';
      case 'uploading':
        return `Uploading ${Math.round(e.progress * 100)}%`;
      case 'done':
        return 'In the inbox';
      case 'dup':
        return e.media?.links.length ? 'Already stored (linked to something)' : 'Already in the inbox';
      case 'error':
        return e.error || 'Failed';
    }
  }

  function sizeText(bytes: number): string {
    return bytes > 1e6 ? `${(bytes / 1e6).toFixed(1)} MB` : `${Math.max(1, Math.round(bytes / 1e3))} KB`;
  }
</script>

<section
  class="queue"
  class:dragging
  aria-label="Upload photos"
  ondragover={(e) => {
    e.preventDefault();
    dragging = true;
  }}
  ondragleave={() => (dragging = false)}
  ondrop={(e) => {
    e.preventDefault();
    dragging = false;
    if (e.dataTransfer?.files) add(e.dataTransfer.files);
  }}
>
  <div class="bar">
    <label class="pick">
      <input type="file" multiple accept={ACCEPT} onchange={onpick} disabled={running} />
      <Icon name="plus" size={20} />
      <span>{entries.length ? 'Add more photos' : 'Choose photos'}</span>
    </label>
    {#if queued.length}
      <Button variant="primary" icon="inbox" loading={running} onclick={uploadAll}>Upload {queued.length}</Button>
    {/if}
    {#if finished && !running}
      <Button variant="ghost" size="sm" onclick={clearFinished}>Clear finished</Button>
    {/if}
  </div>

  {#if !entries.length}
    <p class="muted hint">Pick photos from the camera roll (HEIC is fine). Add a note to any photo, like “bought in Tokyo 2019, the grey one”; agents read it.</p>
  {:else}
    <ul class="entries">
      {#each entries as e (e.key)}
        <li class="entry s-{e.status}">
          <div class="pv">
            {#if e.media?.thumb_url}
              <img src={e.media.thumb_url} alt="" />
            {:else if !e.broken}
              <img src={e.preview} alt="" onerror={() => (e.broken = true)} />
            {:else}
              <span class="ph"><Icon name="image" size={24} /><small>{e.file.name.split('.').pop()?.toUpperCase()}</small></span>
            {/if}
            {#if e.status === 'uploading'}<span class="prog" style:--p={e.progress}></span>{/if}
          </div>
          <div class="meta">
            <div class="line">
              <span class="name" title={e.file.name}>{e.file.name}</span>
              <span class="muted size">{sizeText(e.file.size)}</span>
            </div>
            <span class="st" role="status">
              {#if e.status === 'done' || e.status === 'dup'}<Icon name="check" size={14} />{:else if e.status === 'error'}<Icon name="alert" size={14} />{/if}
              {statusText(e)}
            </span>
            {#if e.media}
              <ContextNote mediaId={e.media.id} value={e.media.context} compact />
            {:else}
              <textarea
                bind:value={e.context}
                rows="1"
                placeholder="Note (optional)"
                aria-label="Note for {e.file.name}"
                disabled={e.status === 'uploading'}
              ></textarea>
            {/if}
          </div>
          {#if e.status === 'queued' || e.status === 'error'}
            <button type="button" class="rm" aria-label="Remove {e.file.name}" onclick={() => remove(e.key)} disabled={running}><Icon name="close" size={18} /></button>
          {/if}
        </li>
      {/each}
    </ul>
  {/if}
</section>

<style>
  .queue {
    display: grid;
    gap: var(--space-3);
    padding: var(--space-4);
    border: var(--border-width) dashed var(--color-border-strong);
    border-radius: var(--radius-lg);
    background: var(--color-surface);
  }
  .queue.dragging {
    border-color: var(--color-accent);
    background: var(--color-accent-soft);
  }
  .bar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
  }
  .pick {
    position: relative;
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    min-height: var(--tap-min);
    padding: 0 var(--space-4);
    border: var(--border-width) solid var(--color-border-strong);
    border-radius: var(--radius-md);
    background: var(--color-surface);
    font-weight: var(--weight-medium);
    cursor: pointer;
  }
  .pick:focus-within {
    outline: 2px solid var(--color-focus);
    outline-offset: 2px;
  }
  .pick input {
    position: absolute;
    inset: 0;
    opacity: 0;
    width: 100%;
    cursor: pointer;
  }
  .hint {
    font-size: var(--text-sm);
  }
  .entries {
    display: grid;
    gap: var(--space-3);
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .entry {
    display: flex;
    align-items: flex-start;
    gap: var(--space-3);
  }
  .pv {
    position: relative;
    flex: none;
    width: 72px;
    height: 72px;
    border-radius: var(--radius-sm);
    overflow: hidden;
    background: var(--color-surface-2);
  }
  .pv img {
    width: 100%;
    height: 100%;
    object-fit: cover;
  }
  .ph {
    display: grid;
    place-items: center;
    align-content: center;
    height: 100%;
    color: var(--color-text-faint);
    font-size: var(--text-xs);
  }
  .prog {
    position: absolute;
    inset: auto 0 0 0;
    height: 4px;
    background: linear-gradient(to right, var(--color-accent) calc(var(--p) * 100%), var(--color-surface-3) 0);
  }
  .meta {
    flex: 1;
    min-width: 0;
    display: grid;
    gap: var(--space-1);
  }
  .line {
    display: flex;
    gap: var(--space-2);
    align-items: baseline;
  }
  .name {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--text-sm);
    font-weight: var(--weight-medium);
  }
  .size {
    flex: none;
    font-size: var(--text-xs);
  }
  .st {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    font-size: var(--text-xs);
    color: var(--color-text-muted);
  }
  .s-done .st {
    color: var(--color-success);
  }
  .s-dup .st {
    color: var(--color-warning);
  }
  .s-error .st {
    color: var(--color-danger);
  }
  textarea {
    width: 100%;
    min-height: var(--tap-min);
    padding: var(--space-2);
    border: var(--border-width) solid var(--color-border-strong);
    border-radius: var(--radius-sm);
    background: var(--color-bg);
    font-size: var(--text-md);
    resize: vertical;
  }
  .rm {
    display: grid;
    place-items: center;
    flex: none;
    width: var(--tap-min);
    height: var(--tap-min);
    padding: 0;
    border: 0;
    background: none;
    color: var(--color-text-muted);
    cursor: pointer;
  }
</style>
