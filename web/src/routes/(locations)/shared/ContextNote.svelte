<script lang="ts">
  // The owner's context note on a photo, editable in place
  // (PATCH /api/media/{id}). Agents treat it as user-asserted.
  import { api, unwrap } from '$lib/api/client';
  import { errorMessage } from '$lib/components/format';
  import { toast } from '$lib/components/toast.svelte';

  let {
    mediaId,
    value = $bindable(null),
    compact = false
  }: {
    mediaId: string;
    value?: string | null;
    compact?: boolean;
  } = $props();

  let editing = $state(false);
  let draft = $state('');
  let busy = $state(false);
  let area = $state<HTMLTextAreaElement>();

  function start() {
    draft = value ?? '';
    editing = true;
    queueMicrotask(() => area?.focus());
  }

  async function save() {
    const next = draft.trim() || null;
    if (next === (value ?? null)) {
      editing = false;
      return;
    }
    busy = true;
    try {
      const m = unwrap(await api.PATCH('/api/media/{id}', { params: { path: { id: mediaId } }, body: { context: next } }));
      value = m.context;
      editing = false;
    } catch (e) {
      toast(`Note not saved: ${errorMessage(e)}`, { tone: 'error' });
    } finally {
      busy = false;
    }
  }

  function onkeydown(e: KeyboardEvent) {
    if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
      e.preventDefault();
      void save();
    } else if (e.key === 'Escape') {
      e.stopPropagation();
      editing = false;
    }
  }
</script>

{#if editing}
  <div class="edit">
    <textarea
      bind:this={area}
      bind:value={draft}
      rows={compact ? 2 : 3}
      placeholder="e.g. bought in Tokyo 2019, the grey one"
      aria-label="Note about this photo"
      {onkeydown}
      disabled={busy}
    ></textarea>
    <div class="btns">
      <button type="button" class="link" onclick={() => (editing = false)} disabled={busy}>Cancel</button>
      <button type="button" class="link strong" onclick={save} disabled={busy}>{busy ? 'Saving…' : 'Save'}</button>
    </div>
  </div>
{:else}
  <button type="button" class="note" class:empty={!value} class:compact onclick={start} title="Edit note">
    {value || '+ Add note'}
  </button>
{/if}

<style>
  .note {
    display: block;
    width: 100%;
    min-height: var(--tap-min);
    padding: var(--space-1) 0;
    border: 0;
    background: none;
    color: var(--color-text);
    font-size: var(--text-sm);
    text-align: left;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    cursor: text;
  }
  .note.compact {
    display: -webkit-box;
    -webkit-line-clamp: 3;
    line-clamp: 3;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }
  .note.empty {
    color: var(--color-accent);
  }
  .edit {
    display: grid;
    gap: var(--space-1);
  }
  textarea {
    width: 100%;
    padding: var(--space-2);
    border: var(--border-width) solid var(--color-border-strong);
    border-radius: var(--radius-sm);
    background: var(--color-surface);
    font-size: var(--text-md);
    resize: vertical;
  }
  .btns {
    display: flex;
    justify-content: flex-end;
    gap: var(--space-3);
  }
  .link {
    min-height: var(--tap-min);
    padding: 0 var(--space-1);
    border: 0;
    background: none;
    color: var(--color-text-muted);
    font-size: var(--text-sm);
    cursor: pointer;
  }
  .link.strong {
    color: var(--color-accent);
    font-weight: var(--weight-medium);
  }
</style>
