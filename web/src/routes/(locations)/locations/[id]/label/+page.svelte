<script lang="ts">
  // Printable box label: code in very large type, name, path and a QR code
  // linking to the box page. Sizes: 4×6 in, A6, or a sheet of six on
  // A4/Letter (this label repeated, or every coded place inside this one).
  import { page } from '$app/state';
  import { api, ApiError, unwrap, type LocationDetail, type LocationNode } from '$lib/api/client';
  import Button from '$lib/components/Button.svelte';
  import ErrorState from '$lib/components/ErrorState.svelte';
  import LoadingState from '$lib/components/LoadingState.svelte';
  import Tabs from '$lib/components/Tabs.svelte';
  import { encodeQr, qrPath } from '../../../shared/qr';
  import { locationParam } from '../../../shared/places';
  import { readPref, writePref } from '../../../shared/store.svelte';

  type Size = '4x6' | 'a6' | 'sheet';
  type LabelData = { id: string; code: string | null; name: string; path: string };

  const ref = $derived(locationParam(page.params.id ?? ''));
  let loc = $state<LocationDetail | null>(null);
  let error = $state<unknown>(null);
  let loading = $state(true);

  let size = $state<Size>((readPref('inv.label.size') as Size | null) ?? '4x6');
  let base = $state(readPref('inv.label.base') ?? (typeof location !== 'undefined' ? location.origin : ''));
  let sheetMode = $state<'repeat' | 'inside'>('repeat');

  async function load() {
    error = null;
    try {
      loc = unwrap(await api.GET('/api/locations/{id}', { params: { path: { id: ref } } }));
    } catch (e) {
      error = e;
    } finally {
      loading = false;
    }
  }
  $effect(() => {
    void ref;
    void load();
  });

  $effect(() => writePref('inv.label.size', size));

  function saveBase() {
    base = base.trim().replace(/\/+$/, '');
    writePref('inv.label.base', base && base !== location.origin ? base : null);
  }

  function pathText(l: { label: string }, name: string): string {
    // label is "Unit 2 › B-014"; drop the trailing self part for the path line
    const parts = l.label.split(' › ');
    if (parts.length > 1) parts.pop();
    const p = parts.join(' › ');
    return p === name ? '' : p;
  }

  const self = $derived<LabelData | null>(
    loc ? { id: loc.id, code: loc.code, name: loc.name, path: loc.path.slice(0, -1).map((c) => (c.kind === 'container' || c.kind === 'shelf' ? (c.code ?? c.name) : c.name)).join(' › ') } : null
  );

  const inside = $derived.by<LabelData[]>(() => {
    const out: LabelData[] = [];
    const walk = (ns: LocationNode[]) => {
      for (const n of ns) {
        if (n.code && !n.archived_at) out.push({ id: n.id, code: n.code, name: n.name, path: pathText(n, n.name) });
        walk(n.children);
      }
    };
    walk(loc?.children ?? []);
    return out;
  });

  const labels = $derived.by<LabelData[]>(() => {
    if (!self) return [];
    if (size !== 'sheet') return [self];
    if (sheetMode === 'inside' && inside.length) return inside;
    return Array.from({ length: 6 }, () => self);
  });

  function urlFor(id: string): string {
    return `${(base || location.origin).replace(/\/+$/, '')}/locations/${id}`;
  }

  function qrFor(id: string): { d: string; n: number } | null {
    try {
      const qr = encodeQr(urlFor(id));
      return { d: qrPath(qr, 4), n: qr.size + 8 };
    } catch {
      return null;
    }
  }

  function codeSize(code: string | null, small: boolean): string {
    const len = Math.max(3, (code ?? '').length);
    return `${Math.min(small ? 22 : 34, (small ? 92 : 150) / len)}cqw`;
  }

  const pageCss = $derived(
    size === '4x6'
      ? '@page { size: 4in 6in; margin: 0; }'
      : size === 'a6'
        ? '@page { size: A6 portrait; margin: 0; }'
        : '@page { size: auto; margin: 10mm; }'
  );

  const localhostLink = $derived(/\/\/(localhost|127\.|\[::1\])/.test(base || (typeof location !== 'undefined' ? location.origin : '')));
</script>

<svelte:head>
  <title>Label {loc?.code ?? ''} · Personal Inventory</title>
  <!-- eslint-disable-next-line svelte/no-at-html-tags -->
  {@html `<style>${pageCss}</style>`}
</svelte:head>

<div class="label-root">
  {#if loading}
    <div class="page"><LoadingState variant="spinner" label="Loading label…" /></div>
  {:else if error || !loc}
    <div class="page">
      <ErrorState {error} title={error instanceof ApiError && error.status === 404 ? 'No such place' : 'Could not load'} onretry={load} />
    </div>
  {:else}
    <div class="controls page no-print">
      <div class="top">
        <Button variant="ghost" icon="chevron-left" href="/locations/{loc.id}">{loc.code ?? loc.name}</Button>
        <Button variant="primary" onclick={() => window.print()}>Print</Button>
      </div>
      <Tabs
        label="Label size"
        tabs={[
          { id: '4x6', label: '4×6 in' },
          { id: 'a6', label: 'A6' },
          { id: 'sheet', label: 'Sheet of 6' }
        ]}
        bind:value={size}
      />
      {#if size === 'sheet' && inside.length}
        <fieldset class="mode">
          <legend class="visually-hidden">Sheet contents</legend>
          <label><input type="radio" bind:group={sheetMode} value="repeat" /> This label ×6</label>
          <label><input type="radio" bind:group={sheetMode} value="inside" /> Everything inside ({inside.length})</label>
        </fieldset>
      {/if}
      <label class="base">
        <span>QR links to</span>
        <input bind:value={base} onchange={saveBase} inputmode="url" autocomplete="off" spellcheck="false" placeholder={location.origin} />
        <small class="muted mono">{urlFor(loc.id)}</small>
        {#if localhostLink}
          <small class="warn">This address only works on this computer. Use the one your phone opens (e.g. your Tailscale name).</small>
        {/if}
      </label>
      <p class="muted hint">In the print dialog choose the matching paper size and 100% scale (no “fit to page”).</p>
    </div>

    <div class="sheet size-{size}">
      {#each labels as l, i (l.id + i)}
        {@const qr = qrFor(l.id)}
        <article class="label" aria-label="Label {l.code ?? l.name}">
          <div class="in">
          {#if l.code}<div class="code mono" style:font-size={codeSize(l.code, size === 'sheet')}>{l.code}</div>{/if}
          <div class="name" class:big={!l.code}>{l.name}</div>
          {#if l.path}<div class="path">{l.path}</div>{/if}
          {#if qr}
            <svg class="qr" viewBox="0 0 {qr.n} {qr.n}" shape-rendering="crispEdges" role="img" aria-label="QR code for {urlFor(l.id)}">
              <rect class="paper" width={qr.n} height={qr.n} />
              <path class="ink" d={qr.d} />
            </svg>
          {/if}
          </div>
        </article>
      {/each}
    </div>
  {/if}
</div>

<style>
  .controls {
    display: grid;
    gap: var(--space-4);
    max-width: 640px;
    padding-bottom: var(--space-4);
  }
  .top {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }
  .mode {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-4);
    margin: 0;
    padding: 0;
    border: 0;
  }
  .mode label {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    min-height: var(--tap-min);
  }
  .base {
    display: grid;
    gap: var(--space-1);
    font-size: var(--text-sm);
    font-weight: var(--weight-medium);
  }
  .base input {
    min-height: var(--tap-min);
    padding: 0 var(--space-3);
    border: var(--border-width) solid var(--color-border-strong);
    border-radius: var(--radius-md);
    background: var(--color-surface);
    font-weight: var(--weight-regular);
  }
  .base small {
    font-weight: var(--weight-regular);
    word-break: break-all;
  }
  .warn {
    color: var(--color-warning);
  }
  .hint {
    font-size: var(--text-sm);
  }

  /* ---- the label itself: paper-like in both themes (viewer tokens are fixed light/dark) */
  .sheet {
    --paper: var(--color-viewer-fg);
    --ink: var(--color-viewer-bg);
    display: grid;
    justify-content: center;
    gap: var(--space-4);
    padding: 0 var(--gutter) var(--space-7);
  }
  .label {
    container-type: inline-size;
    box-sizing: border-box;
    overflow: hidden;
    background: var(--paper);
    color: var(--ink);
    border-radius: var(--radius-sm);
    box-shadow: var(--shadow-md);
    text-align: center;
    font-family: var(--font-sans);
  }
  .size-4x6 .label {
    width: min(4in, calc(100vw - 2 * var(--gutter)));
    aspect-ratio: 4 / 6;
  }
  .size-a6 .label {
    width: min(105mm, calc(100vw - 2 * var(--gutter)));
    aspect-ratio: 105 / 148;
  }
  /* cqw on the container itself would resolve against the viewport, so layout lives on .in */
  .in {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 2cqw;
    height: 100%;
    padding: 6cqw;
  }
  .code {
    font-weight: var(--weight-bold);
    line-height: 0.95;
    letter-spacing: -0.02em;
    white-space: nowrap;
  }
  .name {
    font-size: 8cqw;
    font-weight: var(--weight-bold);
    line-height: 1.15;
    overflow-wrap: anywhere;
  }
  .name.big {
    font-size: 13cqw;
    margin-top: 6cqw;
  }
  .path {
    font-size: 5cqw;
    opacity: 0.75;
  }
  .qr {
    width: 72cqw;
    height: auto;
    margin-top: auto;
  }
  .paper {
    fill: var(--paper);
  }
  .ink {
    fill: var(--ink);
  }

  /* sheet of six: two columns, small horizontal labels */
  .size-sheet {
    grid-template-columns: repeat(2, minmax(0, 92mm));
  }
  .size-sheet .label {
    height: 84mm;
    text-align: left;
  }
  .size-sheet .in {
    display: grid;
    grid-template-columns: 1fr 42cqw;
    grid-template-rows: auto auto auto;
    align-content: center;
    align-items: center;
    padding: 5cqw;
    column-gap: 4cqw;
  }
  .size-sheet .code,
  .size-sheet .name,
  .size-sheet .path {
    grid-column: 1;
  }
  .size-sheet .name {
    font-size: 6.5cqw;
  }
  .size-sheet .path {
    font-size: 4cqw;
  }
  .size-sheet .qr {
    grid-column: 2;
    grid-row: 1 / span 3;
    align-self: center;
    width: 100%;
    margin: 0;
  }
  @media (max-width: 719px) {
    .size-sheet {
      grid-template-columns: minmax(0, 92mm);
    }
  }

  /* ---- print: only the labels, exact physical size, pure black on white */
  @media print {
    :global(body:has(.label-root)) {
      background: #fff;
    }
    :global(body:has(.label-root) .main) {
      padding: 0 !important;
    }
    .sheet {
      --paper: #fff;
      --ink: #000;
      padding: 0;
      gap: 0;
    }
    .label {
      box-shadow: none;
      border-radius: 0;
      break-inside: avoid;
    }
    .size-4x6 .label {
      width: 4in;
      height: 6in;
      break-after: page;
    }
    .size-a6 .label {
      width: 105mm;
      height: 148mm;
      break-after: page;
    }
    .size-sheet {
      grid-template-columns: repeat(2, 92mm);
      gap: 4mm;
    }
    .size-sheet .label {
      outline: 0.2mm dashed #999;
    }
    .size-sheet .label:nth-child(6n) {
      break-after: page;
    }
  }
</style>
