<script lang="ts">
  // Quick add (PLAN.md §8): the manual escape hatch. Photos (uploaded right
  // away, linked on save) + kind + title + location; category, quantity and
  // a staging target are optional. One-handed on a phone: big controls, a
  // sticky Save bar, and "Save + another" keeps kind and location.
  import { goto } from '$app/navigation';
  import { onDestroy } from 'svelte';
  import { api, unwrap, uploadMedia, type Kind, type Media, type Schemas } from '$lib/api/client';
  import Button from '$lib/components/Button.svelte';
  import Icon from '$lib/components/Icon.svelte';
  import { errorMessage } from '$lib/components/format';
  import { hasVariant } from '$lib/components/gallery';
  import { toast } from '$lib/components/toast.svelte';
  import KindPicker from '../../(locations)/shared/KindPicker.svelte';
  import LocationField from '../../(locations)/shared/LocationField.svelte';
  import PhotoRoles from '../../(locations)/shared/PhotoRoles.svelte';
  import { defaultRole } from '../../(locations)/shared/places';
  import { lastLocation, readPref, rememberLocation, writePref } from '../../(locations)/shared/store.svelte';

  type Photo = { key: string; file: File; preview: string; broken: boolean; progress: number | null; media: Media | null; error: string };

  let kind = $state<Kind>((readPref('inv.add.kind') as Kind | null) ?? 'clothing');
  let title = $state('');
  let location = $state<string | null>(lastLocation());
  let category = $state('');
  let quantity = $state(1);
  let target = $state<string | null>(null);
  let photos = $state<Photo[]>([]);
  let roles = $state<Record<string, string>>({});
  let saving = $state(false);
  let error = $state('');
  let titleInput = $state<HTMLInputElement>();
  let seq = 0;

  // ---- photos: upload in the background, one at a time
  let runner: Promise<void> = Promise.resolve();

  function onfiles(e: Event) {
    const input = e.currentTarget as HTMLInputElement;
    const files = [...(input.files ?? [])];
    input.value = '';
    for (const file of files) {
      const p: Photo = { key: `p${++seq}`, file, preview: URL.createObjectURL(file), broken: false, progress: 0, media: null, error: '' };
      roles[p.key] = defaultRole(photos.length);
      photos.push(p);
      const proxy = photos[photos.length - 1];
      runner = runner.then(() => upload(proxy));
    }
  }

  async function upload(p: Photo) {
    if (!photos.includes(p)) return; // removed before its turn
    p.progress = 0;
    p.error = '';
    try {
      p.media = await uploadMedia(p.file, { onProgress: (f) => (p.progress = f) });
    } catch (err) {
      p.error = errorMessage(err);
    } finally {
      p.progress = null;
    }
  }

  function removePhoto(key: string) {
    const i = photos.findIndex((p) => p.key === key);
    if (i < 0) return;
    URL.revokeObjectURL(photos[i].preview);
    photos.splice(i, 1);
    delete roles[key];
  }

  function retry() {
    for (const p of photos.filter((x) => x.error)) runner = runner.then(() => upload(p));
  }

  onDestroy(() => photos.forEach((p) => URL.revokeObjectURL(p.preview)));

  const thumbs = $derived(
    photos.map((p) => ({
      id: p.key,
      thumb: p.media && hasVariant(p.media, 'thumb') ? p.media.thumb_url : p.broken ? null : p.preview,
      name: p.file.name,
      busy: p.progress
    }))
  );
  const failed = $derived(photos.filter((p) => p.error).length);
  const uploading = $derived(photos.some((p) => p.progress !== null || (!p.media && !p.error)));

  // ---- save
  async function save(another: boolean) {
    error = '';
    if (!title.trim()) {
      error = 'Give it a title';
      titleInput?.focus();
      return;
    }
    saving = true;
    try {
      await runner;
      if (photos.some((p) => p.error)) {
        error = 'Some photos failed to upload. Retry or remove them.';
        return;
      }
      const media = photos.filter((p) => p.media).map((p, i) => ({ media_id: p.media!.id, role: roles[p.key] ?? defaultRole(i), sort: i }));
      const body: Schemas['ItemCreate'] = {
        kind,
        title: title.trim(),
        category: category.trim() || null,
        quantity: Math.max(1, Math.floor(quantity) || 1),
        location: location ?? null,
        target_location: target ?? null,
        review_state: 'ok',
        media
      } as Schemas['ItemCreate'];
      const item = unwrap(await api.POST('/api/items', { body }));
      writePref('inv.add.kind', kind);
      rememberLocation(location);
      if (another) {
        toast(`Added ${item.title}`, { tone: 'success', action: { label: 'Open', run: () => goto(`/items/${item.id}`) } });
        photos.forEach((p) => URL.revokeObjectURL(p.preview));
        photos = [];
        roles = {};
        title = '';
        category = '';
        quantity = 1;
        target = null;
        window.scrollTo({ top: 0 });
        titleInput?.focus();
      } else {
        await goto(`/items/${item.id}`);
      }
    } catch (err) {
      error = errorMessage(err);
    } finally {
      saving = false;
    }
  }
</script>

<svelte:head><title>Add · Personal Inventory</title></svelte:head>

<form
  class="page add"
  onsubmit={(e) => {
    e.preventDefault();
    void save(false);
  }}
>
  <h1>Add an item</h1>

  <section class="photos" aria-label="Photos">
    {#if photos.length}
      <PhotoRoles photos={thumbs} bind:roles onremove={removePhoto} />
    {/if}
    <div class="photo-btns">
      <label class="pick">
        <input type="file" multiple accept="image/*,image/heic,image/heif,.heic,.heif" onchange={onfiles} />
        <Icon name="image" size={22} />
        <span>{photos.length ? 'More photos' : 'Add photos'}</span>
      </label>
      {#if failed}
        <Button size="sm" variant="ghost" icon="refresh" onclick={retry}>Retry {failed}</Button>
      {/if}
      <span class="muted small">{uploading ? 'Uploading…' : photos.length ? '' : 'Optional'}</span>
    </div>
  </section>

  <KindPicker bind:value={kind} />

  <label class="field">
    <span>Title</span>
    <input bind:this={titleInput} bind:value={title} required autocomplete="off" enterkeyhint="done" placeholder="Navy shell jacket" />
  </label>

  <LocationField bind:value={location} label="Where it is" title="Where is it?" />

  <details class="more">
    <summary>More: category, quantity, staging</summary>
    <div class="more-body">
      <label class="field">
        <span>Category <small class="muted">(e.g. outerwear/shell, cable/usb-c)</small></span>
        <input bind:value={category} autocomplete="off" autocapitalize="none" />
      </label>
      <label class="field qty">
        <span>Quantity</span>
        <span class="stepper">
          <button type="button" aria-label="Fewer" onclick={() => (quantity = Math.max(1, quantity - 1))}>−</button>
          <input type="number" min="1" inputmode="numeric" bind:value={quantity} />
          <button type="button" aria-label="More" onclick={() => (quantity = quantity + 1)}>+</button>
        </span>
      </label>
      <LocationField bind:value={target} label="Stage it to go to" title="Going to…" noneText="Not staged" noneLabel="Not staged" />
    </div>
  </details>

  {#if error}<p class="err" role="alert">{error}</p>{/if}

  <div class="savebar">
    <Button variant="secondary" size="lg" loading={saving} onclick={() => save(true)}>Save + another</Button>
    <Button variant="primary" size="lg" type="submit" loading={saving}>Save</Button>
  </div>
</form>

<style>
  .add {
    display: grid;
    gap: var(--space-5);
    max-width: 640px;
  }
  .photos {
    display: grid;
    gap: var(--space-3);
  }
  .photo-btns {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-3);
  }
  .pick {
    position: relative;
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    min-height: calc(var(--tap-min) + var(--space-2));
    padding: 0 var(--space-5);
    border: var(--border-width) dashed var(--color-border-strong);
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
    width: 100%;
    opacity: 0;
    cursor: pointer;
  }
  .small {
    font-size: var(--text-sm);
  }
  .field {
    display: grid;
    gap: var(--space-1);
    font-size: var(--text-sm);
    font-weight: var(--weight-medium);
  }
  .field input {
    width: 100%;
    min-height: calc(var(--tap-min) + var(--space-1));
    padding: 0 var(--space-3);
    border: var(--border-width) solid var(--color-border-strong);
    border-radius: var(--radius-md);
    background: var(--color-surface);
    font-size: var(--text-lg);
    font-weight: var(--weight-regular);
  }
  .more {
    border-top: var(--border-width) solid var(--color-border);
    padding-top: var(--space-2);
  }
  summary {
    display: flex;
    align-items: center;
    min-height: var(--tap-min);
    color: var(--color-accent);
    font-weight: var(--weight-medium);
    cursor: pointer;
  }
  .more-body {
    display: grid;
    gap: var(--space-4);
    padding-top: var(--space-2);
  }
  .stepper {
    display: flex;
    gap: var(--space-2);
    max-width: 220px;
  }
  .stepper button {
    flex: none;
    width: var(--tap-min);
    min-height: var(--tap-min);
    border: var(--border-width) solid var(--color-border-strong);
    border-radius: var(--radius-md);
    background: var(--color-surface);
    font-size: var(--text-lg);
    cursor: pointer;
  }
  .stepper input {
    text-align: center;
  }
  .err {
    color: var(--color-danger);
    font-size: var(--text-sm);
  }
  .savebar {
    position: sticky;
    bottom: calc(var(--tabbar-height) + env(safe-area-inset-bottom));
    z-index: var(--z-sticky);
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: var(--space-2);
    margin: 0 calc(var(--gutter) * -1);
    padding: var(--space-3) var(--gutter);
    background: color-mix(in srgb, var(--color-bg) 92%, transparent);
    backdrop-filter: blur(8px);
    border-top: var(--border-width) solid var(--color-border);
  }
  @media (min-width: 720px) {
    .savebar {
      bottom: 0;
      grid-template-columns: auto auto;
      justify-content: end;
    }
  }
</style>
