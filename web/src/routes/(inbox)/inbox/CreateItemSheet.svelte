<script lang="ts">
  // "Create item from these": quick-create (kind, title, category, location)
  // with the selected inbox photos linked by role; review_state ok.
  import { untrack } from 'svelte';
  import { api, unwrap, type ItemDetail, type Kind, type Media, type Schemas } from '$lib/api/client';
  import Button from '$lib/components/Button.svelte';
  import Sheet from '$lib/components/Sheet.svelte';
  import { errorMessage } from '$lib/components/format';
  import { hasVariant } from '$lib/components/gallery';
  import KindPicker from '../../(locations)/shared/KindPicker.svelte';
  import LocationField from '../../(locations)/shared/LocationField.svelte';
  import PhotoRoles from '../../(locations)/shared/PhotoRoles.svelte';
  import { defaultRole, plural } from '../../(locations)/shared/places';
  import { lastLocation, readPref, rememberLocation, writePref } from '../../(locations)/shared/store.svelte';

  let {
    open = $bindable(false),
    photos,
    oncreated
  }: {
    open?: boolean;
    photos: Media[];
    oncreated: (item: ItemDetail) => void;
  } = $props();

  const uid = $props.id();
  let kind = $state<Kind>((readPref('inv.add.kind') as Kind | null) ?? 'clothing');
  let title = $state('');
  let category = $state('');
  let location = $state<string | null>(null);
  let roles = $state<Record<string, string>>({});
  let busy = $state(false);
  let error = $state('');

  $effect(() => {
    if (!open) return;
    untrack(() => {
      title = '';
      category = '';
      error = '';
      location = lastLocation();
      roles = Object.fromEntries(photos.map((p, i) => [p.id, defaultRole(i)]));
    });
  });

  const notes = $derived(photos.map((p) => p.context).filter((c): c is string => !!c));
  const thumbs = $derived(photos.map((p) => ({ id: p.id, thumb: hasVariant(p, 'thumb') ? p.thumb_url : null, name: p.original_name })));

  async function submit(e: SubmitEvent) {
    e.preventDefault();
    if (!title.trim()) {
      error = 'Give it a title';
      return;
    }
    busy = true;
    error = '';
    try {
      const item = unwrap(
        await api.POST('/api/items', {
          body: {
            kind,
            title: title.trim(),
            category: category.trim() || null,
            location: location ?? null,
            review_state: 'ok',
            media: photos.map((p, i) => ({ media_id: p.id, role: roles[p.id] ?? defaultRole(i), sort: i }))
          } as Schemas['ItemCreate']
        })
      );
      writePref('inv.add.kind', kind);
      rememberLocation(location);
      open = false;
      oncreated(item);
    } catch (err) {
      error = errorMessage(err);
    } finally {
      busy = false;
    }
  }
</script>

<Sheet bind:open title="New item from {plural(photos.length, 'photo')}" side="auto" size="lg">
  <form id="{uid}-f" class="form" onsubmit={submit}>
    <PhotoRoles photos={thumbs} bind:roles />
    {#if notes.length}
      <div class="notes">
        <span class="muted">Your notes:</span>
        {#each notes as n, i (i)}<q>{n}</q>{/each}
      </div>
    {/if}
    <KindPicker bind:value={kind} />
    <label>
      <span>Title</span>
      <input bind:value={title} required autocomplete="off" placeholder="Navy shell jacket" />
    </label>
    <label>
      <span>Category <small class="muted">(optional, e.g. outerwear/shell)</small></span>
      <input bind:value={category} autocomplete="off" autocapitalize="none" />
    </label>
    <LocationField bind:value={location} label="Where it is" />
    {#if error}<p class="err" role="alert">{error}</p>{/if}
  </form>
  {#snippet footer()}
    <Button variant="ghost" onclick={() => (open = false)}>Cancel</Button>
    <Button variant="primary" type="submit" form="{uid}-f" loading={busy}>Create item</Button>
  {/snippet}
</Sheet>

<style>
  .form {
    display: grid;
    gap: var(--space-4);
  }
  .notes {
    display: grid;
    gap: var(--space-1);
    font-size: var(--text-sm);
  }
  q {
    font-style: italic;
  }
  label {
    display: grid;
    gap: var(--space-1);
    font-size: var(--text-sm);
    font-weight: var(--weight-medium);
  }
  input {
    width: 100%;
    min-height: var(--tap-min);
    padding: 0 var(--space-3);
    border: var(--border-width) solid var(--color-border-strong);
    border-radius: var(--radius-md);
    background: var(--color-surface);
    font-weight: var(--weight-regular);
    font-size: var(--text-md);
  }
  .err {
    color: var(--color-danger);
    font-size: var(--text-sm);
  }
</style>
