<script lang="ts">
  // "Add fact": a key from the registry (or a new dotted key), a value shaped
  // by its value_type, basis and confidence. POST /api/items/{id}/facts.
  import { api, unwrap, type Schemas } from '$lib/api/client';
  import { BASIS_LABEL, Button, Sheet, errorMessage, toast } from '$lib/components';
  import ValueInput from './ValueInput.svelte';
  import { formatValue, keyLabel, parseValueInput, type FactKey } from './detail';

  let {
    open = $bindable(false),
    itemId,
    kind,
    keys,
    initialKey = '',
    onwrite
  }: {
    open?: boolean;
    itemId: string;
    /** Item kind: registry keys for other kinds are listed last. */
    kind: string;
    keys: Map<string, FactKey>;
    initialKey?: string;
    onwrite: (res: Schemas['FactWrite']) => void;
  } = $props();

  const uid = $props.id();
  let key = $state('');
  let text = $state('');
  let currency = $state('');
  let basis = $state<Schemas['FactBasis']>('user');
  let confidence = $state(100);
  let note = $state('');
  let error = $state('');
  let keyError = $state('');
  let busy = $state(false);

  let wasOpen = false;
  $effect(() => {
    if (open && !wasOpen) {
      key = initialKey;
      text = '';
      currency = '';
      basis = 'user';
      confidence = 100;
      note = '';
      error = keyError = '';
    }
    wasOpen = open;
  });

  const options = $derived(
    [...keys.values()].sort((a, b) => {
      const ak = !a.kinds.length || a.kinds.includes(kind as never) ? 0 : 1;
      const bk = !b.kinds.length || b.kinds.includes(kind as never) ? 0 : 1;
      return ak - bk || a.sort - b.sort;
    })
  );
  const k = $derived(key.trim());
  const reg = $derived(keys.get(k));
  const type = $derived(reg?.value_type ?? 'string');

  async function submit(e: SubmitEvent) {
    e.preventDefault();
    keyError = error = '';
    if (!/^[a-z0-9_]+(\.[a-z0-9_]+)*$/.test(k)) {
      keyError = 'Use lower-case words with dots or underscores, e.g. shell.technology.';
      return;
    }
    const parsed = parseValueInput(type, text, currency);
    if (!parsed.ok) {
      error = parsed.error;
      return;
    }
    busy = true;
    try {
      const res = unwrap(
        await api.POST('/api/items/{id}/facts', {
          params: { path: { id: itemId } },
          body: { key: k, value: parsed.value, basis, confidence: confidence / 100, note: note.trim() || null }
        })
      );
      onwrite(res);
      toast(`Added ${keyLabel(keys, k)}: ${formatValue(res.fact.value, type)}`, { tone: 'success' });
      open = false;
    } catch (err) {
      error = errorMessage(err);
    } finally {
      busy = false;
    }
  }
</script>

<Sheet bind:open title="Add a fact" side="auto">
  <form id="{uid}-f" class="form" onsubmit={submit}>
    <label for="{uid}-k">What</label>
    <input id="{uid}-k" type="text" list="{uid}-keys" bind:value={key} placeholder="color, size, purchase_price…" autocapitalize="off" spellcheck="false" aria-invalid={keyError ? 'true' : undefined} />
    <datalist id="{uid}-keys">
      {#each options as o (o.key)}<option value={o.key}>{o.label}</option>{/each}
    </datalist>
    {#if reg}<p class="hint muted">{reg.label}{reg.description ? ` — ${reg.description}` : ''}</p>{/if}
    {#if keyError}<p class="err" role="alert">{keyError}</p>{/if}

    <label for="{uid}-v">Value</label>
    <ValueInput id="{uid}-v" {type} bind:text bind:currency {error} />

    <div class="two">
      <label>
        <span>Source</span>
        <select bind:value={basis}>
          {#each Object.entries(BASIS_LABEL) as [b, l] (b)}<option value={b}>{l}</option>{/each}
        </select>
      </label>
      <label>
        <span>Confidence {confidence}%</span>
        <input type="range" min="0" max="100" step="5" bind:value={confidence} />
      </label>
    </div>

    <label for="{uid}-n">Note <span class="muted">(optional)</span></label>
    <input id="{uid}-n" type="text" bind:value={note} placeholder="How do you know?" />
  </form>

  {#snippet footer()}
    <Button variant="ghost" onclick={() => (open = false)}>Cancel</Button>
    <Button variant="primary" type="submit" form="{uid}-f" loading={busy}>Add fact</Button>
  {/snippet}
</Sheet>

<style>
  .form {
    display: grid;
    gap: var(--space-2);
  }
  label,
  .two span {
    font-size: var(--text-sm);
    font-weight: var(--weight-medium);
  }
  .two {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: var(--space-3);
  }
  .two label {
    display: grid;
    gap: var(--space-1);
  }
  input[type='text'],
  select {
    width: 100%;
    min-height: var(--tap-min);
    padding: 0 var(--space-3);
    border: var(--border-width) solid var(--color-border-strong);
    border-radius: var(--radius-md);
    background: var(--color-bg);
    font-weight: var(--weight-regular);
  }
  input[type='range'] {
    min-height: var(--tap-min);
    accent-color: var(--color-accent);
  }
  [aria-invalid='true'] {
    border-color: var(--color-danger);
  }
  .hint,
  .err {
    font-size: var(--text-xs);
    font-weight: var(--weight-regular);
  }
  .err {
    color: var(--color-danger);
  }
</style>
