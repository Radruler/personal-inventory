<script lang="ts">
  // Provenance for one attribute: the winning fact (basis, confidence,
  // sources resolved to observation text / reference / note, who, when), the
  // other facts for the key (history), and Correct / Reject.
  import { api, unwrap, type Fact, type ItemDetail, type Schemas } from '$lib/api/client';
  import { Badge, Button, ProvenanceBadge, Sheet, errorMessage, toast } from '$lib/components';
  import FactSources from './FactSources.svelte';
  import ValueInput from './ValueInput.svelte';
  import { actorLabel, factsForKey, formatDateTime, formatValue, keyLabel, parseValueInput, valueToInput, type FactKey } from './detail';

  let {
    open = $bindable(false),
    item,
    factKey,
    keys,
    sourceLabels,
    onwrite
  }: {
    open?: boolean;
    item: ItemDetail;
    /** The attribute key being inspected. */
    factKey: string | null;
    keys: Map<string, FactKey>;
    sourceLabels: Map<string, string>;
    onwrite: (res: Schemas['FactWrite']) => void;
  } = $props();

  const uid = $props.id();
  const src = $derived(factKey ? item.attr_sources?.[factKey] : undefined);
  const winnerId = $derived(src?.fact_id ?? null);
  const list = $derived(factKey ? factsForKey(item, factKey, winnerId) : []);
  const winner = $derived(list.find((f) => f.id === winnerId) ?? null);
  const others = $derived(list.filter((f) => f.id !== winnerId));
  const type = $derived(factKey ? (keys.get(factKey)?.value_type ?? null) : null);
  const label = $derived(factKey ? keyLabel(keys, factKey) : '');
  const value = $derived(factKey ? item.attrs?.[factKey] : undefined);
  const fromProduct = $derived(src?.subject_type === 'product');

  let mode = $state<'view' | 'correct' | 'reject'>('view');
  let text = $state('');
  let currency = $state('');
  let note = $state('');
  let error = $state('');
  let busy = $state(false);
  let rejectTarget = $state<Fact | null>(null);

  // Reset whenever the sheet opens or the key changes.
  $effect(() => {
    void factKey;
    if (open) {
      mode = 'view';
      error = '';
    }
  });

  function startCorrect() {
    text = valueToInput(value, type);
    currency = type === 'money' && value && typeof value === 'object' ? String((value as { currency?: string }).currency ?? '') : '';
    note = '';
    error = '';
    mode = 'correct';
  }

  function startReject(f: Fact) {
    rejectTarget = f;
    note = '';
    mode = 'reject';
  }

  async function correct(e: SubmitEvent) {
    e.preventDefault();
    if (!factKey) return;
    const parsed = parseValueInput(type, text, currency);
    if (!parsed.ok) {
      error = parsed.error;
      return;
    }
    busy = true;
    try {
      let res: Schemas['FactWrite'];
      const n = note.trim() || null;
      if (winner && winner.subject_type === 'item') {
        res = unwrap(
          await api.POST('/api/facts/{id}/supersede', {
            params: { path: { id: winner.id } },
            body: { value: parsed.value, basis: 'user', confidence: 1, note: n }
          })
        );
      } else {
        // Product values (or a missing winner) get an item-level user fact,
        // which wins by precedence without changing the product.
        res = unwrap(
          await api.POST('/api/items/{id}/facts', {
            params: { path: { id: item.id } },
            body: { key: factKey, value: parsed.value, basis: 'user', confidence: 1, note: n }
          })
        );
      }
      onwrite(res);
      toast(`${label} set to ${formatValue(res.fact.value, type)}`, { tone: 'success' });
      mode = 'view';
    } catch (err) {
      error = errorMessage(err);
    } finally {
      busy = false;
    }
  }

  async function reject(e: SubmitEvent) {
    e.preventDefault();
    if (!rejectTarget) return;
    busy = true;
    try {
      const res = unwrap(
        await api.POST('/api/facts/{id}/reject', { params: { path: { id: rejectTarget.id } }, body: { note: note.trim() || null } })
      );
      onwrite(res);
      toast(`Rejected ${formatValue(rejectTarget.value, type)}`, { tone: 'success' });
      mode = 'view';
      rejectTarget = null;
    } catch (err) {
      toast(`Couldn't reject: ${errorMessage(err)}`, { tone: 'error' });
    } finally {
      busy = false;
    }
  }

  const STATUS_TONE: Record<string, 'success' | 'neutral' | 'danger'> = { active: 'success', superseded: 'neutral', rejected: 'danger' };
</script>

{#snippet factMeta(f: Fact)}
  <p class="meta muted">
    {actorLabel(f.recorded_by)} · {formatDateTime(f.recorded_at)}{#if f.subject_type === 'product'} · on the product{/if}
  </p>
  {#if f.note}<p class="note">“{f.note}”</p>{/if}
{/snippet}

<Sheet bind:open title={label || 'Fact'} side="auto">
  {#if factKey}
    <div class="body">
      <div class="current">
        <p class="value">{formatValue(value, type)}</p>
        {#if fromProduct}<Badge tone="info">From the product</Badge>{/if}
      </div>

      {#if winner}
        <section class="winner" aria-label="Why we believe this">
          <ProvenanceBadge basis={winner.basis} confidence={winner.confidence} />
          {@render factMeta(winner)}
          <FactSources fact={winner} {item} {sourceLabels} />
        </section>
      {:else}
        <p class="muted">The fact behind this value isn't in this item's history.</p>
      {/if}

      {#if mode === 'correct'}
        <form class="form" onsubmit={correct}>
          <label for="{uid}-v">Correct value</label>
          <ValueInput id="{uid}-v" {type} bind:text bind:currency {error} />
          <label for="{uid}-n">Why <span class="muted">(optional)</span></label>
          <input id="{uid}-n" type="text" bind:value={note} placeholder="e.g. checked the hang tag" />
          {#if fromProduct}<p class="hint muted">Saved on this item only; the product keeps its value.</p>{/if}
          <div class="row">
            <Button variant="ghost" onclick={() => (mode = 'view')}>Cancel</Button>
            <Button variant="primary" type="submit" loading={busy}>Save as yours</Button>
          </div>
        </form>
      {:else if mode === 'reject' && rejectTarget}
        <form class="form" onsubmit={reject}>
          <p>
            Reject <strong>{formatValue(rejectTarget.value, type)}</strong>? It stays in history.
            {#if rejectTarget.subject_type === 'product'}This changes the product, so every item of it.{/if}
          </p>
          <label for="{uid}-rn">Why <span class="muted">(optional)</span></label>
          <input id="{uid}-rn" type="text" bind:value={note} placeholder="e.g. not the same model" />
          <div class="row">
            <Button variant="ghost" onclick={() => (mode = 'view')}>Cancel</Button>
            <Button variant="danger" type="submit" loading={busy}>Reject</Button>
          </div>
        </form>
      {:else}
        <div class="row start">
          <Button variant="primary" onclick={startCorrect}>Correct this</Button>
          {#if winner}<Button variant="ghost" onclick={() => startReject(winner)}>Reject</Button>{/if}
        </div>
      {/if}

      {#if others.length}
        <section aria-labelledby="{uid}-hist">
          <h3 id="{uid}-hist">History</h3>
          <ul class="history">
            {#each others as f (f.id)}
              <li class:inactive={f.status !== 'active'}>
                <div class="hrow">
                  <span class="hval">{formatValue(f.value, type)}</span>
                  <Badge tone={STATUS_TONE[f.status] ?? 'neutral'}>{f.status}</Badge>
                </div>
                <ProvenanceBadge basis={f.basis} confidence={f.confidence} compact />
                {@render factMeta(f)}
                <FactSources fact={f} {item} {sourceLabels} compact />
                {#if f.status === 'active' && mode === 'view'}
                  <div><Button size="sm" variant="ghost" onclick={() => startReject(f)}>Reject</Button></div>
                {/if}
              </li>
            {/each}
          </ul>
        </section>
      {/if}
    </div>
  {/if}
</Sheet>

<style>
  .body {
    display: grid;
    gap: var(--space-4);
  }
  .current {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
  }
  .value {
    font-size: var(--text-xl);
    font-weight: var(--weight-bold);
    overflow-wrap: anywhere;
  }
  .winner {
    display: grid;
    gap: var(--space-2);
    padding: var(--space-3);
    border-radius: var(--radius-md);
    background: var(--color-surface-2);
  }
  .meta {
    font-size: var(--text-sm);
  }
  .note {
    font-size: var(--text-sm);
    font-style: italic;
  }
  .form {
    display: grid;
    gap: var(--space-2);
  }
  .form label {
    font-size: var(--text-sm);
    font-weight: var(--weight-medium);
  }
  .form input {
    min-height: var(--tap-min);
    padding: 0 var(--space-3);
    border: var(--border-width) solid var(--color-border-strong);
    border-radius: var(--radius-md);
    background: var(--color-bg);
  }
  .hint {
    font-size: var(--text-xs);
  }
  .row {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: var(--space-2);
  }
  .row.start {
    justify-content: flex-start;
  }
  h3 {
    margin-bottom: var(--space-2);
    font-size: var(--text-md);
  }
  .history {
    display: grid;
    gap: var(--space-2);
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .history li {
    display: grid;
    gap: var(--space-1);
    padding: var(--space-3);
    border: var(--border-width) solid var(--color-border);
    border-radius: var(--radius-md);
  }
  .history li.inactive .hval {
    color: var(--color-text-muted);
  }
  .history li.inactive .hval {
    text-decoration: line-through;
  }
  .hrow {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-2);
  }
  .hval {
    font-weight: var(--weight-medium);
    overflow-wrap: anywhere;
  }
</style>
