<script lang="ts">
  // "Where it is" (PLAN.md §8): placement with a linked path, the target when
  // staged, and Move / Stage / Unstage with undo. Results show immediately
  // from the MoveResult card; the caller refetches for history.
  import { api, unwrap, type ItemDetail, type ItemSummary, type LocationRef, type Schemas } from '$lib/api/client';
  import { Button, Icon, LocationChip, errorMessage, toast } from '$lib/components';
  import { STATUS_ICON } from '$lib/components/icons';
  import MoveSheet from './MoveSheet.svelte';
  import { formatDay } from './detail';

  let {
    item,
    onchange,
    onrestore,
    restoring = false
  }: {
    item: ItemDetail;
    /** A move changed the item: the updated card (placement etc.). */
    onchange: (card: ItemSummary | null) => void;
    onrestore: () => void;
    restoring?: boolean;
  } = $props();

  let sheetOpen = $state(false);
  let mode = $state<'move' | 'stage'>('move');
  let busy = $state(false);

  const p = $derived(item.placement);
  const gone = $derived(item.lifecycle === 'gone');
  const staged = $derived(!!item.target_location_id);

  function open(m: 'move' | 'stage') {
    mode = m;
    sheetOpen = true;
  }

  async function post(req: Schemas['MoveRequest']) {
    return unwrap(await api.POST('/api/moves', { body: req }));
  }

  function describe(req: Schemas['MoveRequest'], card: ItemSummary | null): string {
    if (req.action === 'unstage') return 'No longer staged';
    if (req.action === 'stage') return `Staged to go to ${card?.placement.target?.label ?? 'there'}`;
    return req.to ? `Moved to ${card?.placement.location?.label ?? 'there'}` : 'Marked location unknown';
  }

  /** Puts location and target back the way they were. */
  async function undo(prev: { location: string | null; target: string | null }) {
    try {
      const reqs: Schemas['MoveRequest'][] = [];
      const loc = item.location_id;
      const tgt = item.target_location_id;
      if (loc !== prev.location) reqs.push({ action: 'move', item_ids: [item.id], to: prev.location, note: 'undo', clear_target: false });
      if (tgt !== prev.target) {
        reqs.push(prev.target ? { action: 'stage', item_ids: [item.id], to: prev.target, note: 'undo', clear_target: false } : { action: 'unstage', item_ids: [item.id], note: 'undo', clear_target: false });
      }
      let card: ItemSummary | null = null;
      for (const r of reqs) card = (await post(r)).items[0] ?? card;
      onchange(card);
      toast('Undone', { tone: 'success' });
    } catch (e) {
      toast(`Couldn't undo: ${errorMessage(e)}`, { tone: 'error' });
    }
  }

  function done(res: Schemas['MoveResult'], req: Schemas['MoveRequest'], prev: { location: string | null; target: string | null }) {
    const card = res.items[0] ?? null;
    onchange(card);
    if (!res.moved) {
      toast('Nothing changed');
      return;
    }
    toast(describe(req, card), { tone: 'success', action: { label: 'Undo', run: () => undo(prev) } });
  }

  async function unstage() {
    const prev = { location: item.location_id, target: item.target_location_id };
    const req: Schemas['MoveRequest'] = { action: 'unstage', item_ids: [item.id], clear_target: false };
    busy = true;
    try {
      done(await post(req), req, prev);
    } catch (e) {
      toast(`Couldn't unstage: ${errorMessage(e)}`, { tone: 'error' });
    } finally {
      busy = false;
    }
  }

  // Snapshot taken when the sheet opens, for undo.
  let prevForSheet = { location: null as string | null, target: null as string | null };
  $effect(() => {
    if (sheetOpen) prevForSheet = { location: item.location_id, target: item.target_location_id };
  });
</script>

{#snippet crumbs(ref: LocationRef)}
  <ol class="crumbs" aria-label="Path">
    {#each ref.path as c, i (c.id)}
      <li>
        {#if i > 0}<span class="sep" aria-hidden="true">›</span>{/if}
        <a href="/locations/{c.id}" aria-current={c.id === ref.id ? 'location' : undefined}>
          {c.name}{#if c.code && c.code !== c.name && (c.kind === 'container' || c.kind === 'shelf')}<span class="code">{c.code}</span>{/if}
        </a>
      </li>
    {/each}
  </ol>
{/snippet}

<section class="where s-{p.status}" aria-labelledby="where-h">
  <div class="head">
    <h2 id="where-h">Where it is</h2>
    <LocationChip placement={p} variant="full" />
  </div>

  {#if gone}
    <p class="line">
      <Icon name={STATUS_ICON.gone} size={18} />
      <span title={item.gone_at ? `Since ${formatDay(item.gone_at)}` : undefined}>{p.label}</span>
    </p>
    {#if p.location}
      <div class="row"><span class="muted lbl">Last known</span>{@render crumbs(p.location)}</div>
    {/if}
  {:else}
    {#if p.location}
      <div class="row">
        <span class="muted lbl">{p.status === 'approximate' ? 'Somewhere in' : 'In'}</span>
        {@render crumbs(p.location)}
      </div>
      {#if p.status === 'approximate'}<p class="hint muted">Box unknown — move it to the box once you find it.</p>{/if}
    {:else}
      <p class="line unknown"><Icon name="unknown" size={18} /><span>Location unknown</span></p>
    {/if}
    {#if p.target}
      <div class="row target">
        <span class="lbl"><Icon name="staged" size={16} /> Going to</span>
        {@render crumbs(p.target)}
      </div>
    {/if}
  {/if}

  <div class="actions">
    {#if gone}
      <Button variant="primary" icon="refresh" loading={restoring} onclick={onrestore}>Restore to owned</Button>
    {:else}
      <Button variant="primary" icon="placed" onclick={() => open('move')}>Move…</Button>
      <Button icon="staged" onclick={() => open('stage')}>{staged ? 'Restage…' : 'Stage for…'}</Button>
      {#if staged}
        <Button variant="ghost" loading={busy} onclick={unstage}>Unstage</Button>
      {/if}
    {/if}
  </div>
</section>

<MoveSheet
  bind:open={sheetOpen}
  {mode}
  itemId={item.id}
  currentId={item.location_id}
  targetId={item.target_location_id}
  {staged}
  ondone={(res, req) => done(res, req, prevForSheet)}
/>

<style>
  .where {
    display: grid;
    gap: var(--space-2);
    padding: var(--space-3) var(--space-4) var(--space-4);
    border: var(--border-width) solid var(--color-border);
    border-left: 4px solid var(--status-placed);
    border-radius: var(--radius-lg);
    background: var(--color-surface);
  }
  .s-approximate { border-left-color: var(--status-approximate); }
  .s-staged { border-left-color: var(--status-staged); }
  .s-unknown { border-left-color: var(--status-unknown); }
  .s-gone { border-left-color: var(--status-gone); }
  .head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-2);
  }
  h2 {
    font-size: var(--text-md);
    color: var(--color-text-muted);
    font-weight: var(--weight-medium);
  }
  .row {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: var(--space-1) var(--space-2);
  }
  .lbl {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    font-size: var(--text-sm);
  }
  .target .lbl {
    color: var(--status-staged);
  }
  .crumbs {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: var(--space-1);
    margin: 0;
    padding: 0;
    list-style: none;
    font-size: var(--text-lg);
    font-weight: var(--weight-medium);
  }
  .crumbs li {
    display: inline-flex;
    align-items: baseline;
    gap: var(--space-1);
  }
  .crumbs a {
    display: inline-flex;
    align-items: baseline;
    gap: var(--space-1);
    color: var(--color-text);
  }
  .crumbs a[aria-current] {
    color: var(--color-accent);
  }
  .sep {
    color: var(--color-text-faint);
  }
  .code {
    padding: 0 var(--space-1);
    border-radius: var(--radius-sm);
    background: var(--color-surface-2);
    font-family: var(--font-mono);
    font-size: var(--text-sm);
  }
  .line {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    font-size: var(--text-lg);
    font-weight: var(--weight-medium);
  }
  .unknown {
    color: var(--status-unknown);
  }
  .hint {
    font-size: var(--text-sm);
  }
  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
    margin-top: var(--space-1);
  }
</style>
