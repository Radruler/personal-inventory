<script lang="ts" module>
  import type { MediaRef } from '$lib/api/client';</script>

<script lang="ts">
  // Full-screen photo viewer: display variant, swipe / arrow keys / buttons
  // to navigate, Esc to close, and a link to the original file.
  import Icon from './Icon.svelte';
  import Spinner from './Spinner.svelte';
  import { trapTab } from './focus';
  import { hasVariant, swipeDirection, wrapIndex } from './gallery';
  import { roleLabel } from './format';

  export type ViewerPhoto = { media: MediaRef; role?: string; caption?: string | null };

  let {
    photos,
    index = $bindable(0),
    open = $bindable(false),
    title = 'Photo',
    onclose
  }: {
    photos: ViewerPhoto[];
    index?: number;
    open?: boolean;
    /** Accessible name prefix, e.g. the item title. */
    title?: string;
    onclose?: () => void;
  } = $props();

  let dialog = $state<HTMLDialogElement>();
  let restoreTo: HTMLElement | null = null;
  let loaded = $state<string | null>(null);

  const n = $derived(photos.length);
  const i = $derived(wrapIndex(index, n));
  const cur = $derived(photos[i]);
  const src = $derived(cur && hasVariant(cur.media, 'display') ? cur.media.display_url : null);

  function close() {
    open = false;
    onclose?.();
  }
  function go(delta: number) {
    if (n > 1) index = wrapIndex(i + delta, n);
  }

  $effect(() => {
    const d = dialog;
    if (!d) return;
    if (open && !d.open) {
      restoreTo = document.activeElement as HTMLElement | null;
      d.showModal();
    } else if (!open && d.open) {
      d.close();
      if (restoreTo?.isConnected) restoreTo.focus();
    }
  });
  $effect(() => () => {
    if (dialog?.open) dialog.close();
  });

  // Warm the neighbours so swiping feels instant (immutable-cached GETs).
  $effect(() => {
    if (!open || n < 2) return;
    for (const d of [-1, 1]) {
      const p = photos[wrapIndex(i + d, n)];
      if (p && hasVariant(p.media, 'display')) {
        const img = new Image();
        img.decoding = 'async';
        img.src = p.media.display_url!;
      }
    }
  });

  function onkeydown(e: KeyboardEvent) {
    if (e.key === 'ArrowRight') {
      e.preventDefault();
      go(1);
    } else if (e.key === 'ArrowLeft') {
      e.preventDefault();
      go(-1);
    } else if (e.key === 'Home') {
      index = 0;
    } else if (e.key === 'End') {
      index = n - 1;
    } else if (dialog) trapTab(e, dialog);
  }

  let start: { x: number; y: number; id: number } | null = null;
  function onpointerdown(e: PointerEvent) {
    if (e.pointerType === 'mouse' && e.button !== 0) return;
    start = { x: e.clientX, y: e.clientY, id: e.pointerId };
  }
  function onpointerup(e: PointerEvent) {
    if (!start || start.id !== e.pointerId) return;
    const dir = swipeDirection(e.clientX - start.x, e.clientY - start.y);
    start = null;
    if (dir) go(dir);
  }
</script>

<dialog
  bind:this={dialog}
  class="viewer"
  aria-label="{title}: photo {i + 1} of {n}"
  oncancel={(e) => {
    e.preventDefault();
    close();
  }}
  onclose={() => {
    if (open) close();
  }}
  {onkeydown}
>
  {#if open && cur}
    <div class="top">
      <span class="counter" aria-live="polite">{i + 1} / {n}</span>
      <a class="tool" href={cur.media.orig_url} target="_blank" rel="noopener" download>
        <Icon name="download" size={20} /><span>Original</span>
      </a>
      <!-- svelte-ignore a11y_autofocus -->
      <button type="button" class="tool icon" aria-label="Close viewer" onclick={close} autofocus>
        <Icon name="close" size={22} />
      </button>
    </div>

    <div
      class="stage"
      role="group"
      aria-roledescription="carousel"
      aria-label="Photos"
      {onpointerdown}
      {onpointerup}
      onpointercancel={() => (start = null)}
    >
      {#if src}
        {#key cur.media.id}
          {#if loaded !== cur.media.id}<span class="loading"><Spinner size={28} /></span>{/if}
          <img
            {src}
            alt={cur.caption || `${title}, ${roleLabel(cur.role ?? 'other').toLowerCase()} photo`}
            width={cur.media.width ?? undefined}
            height={cur.media.height ?? undefined}
            decoding="async"
            draggable="false"
            onload={() => (loaded = cur.media.id)}
          />
        {/key}
      {:else}
        <div class="missing">
          <Icon name="image" size={40} />
          <p>{cur.media.variant_status === 'pending' ? 'This photo is still being processed.' : 'No preview for this file.'}</p>
          <a href={cur.media.orig_url} target="_blank" rel="noopener">Open the original</a>
        </div>
      {/if}

      {#if n > 1}
        <button type="button" class="nav prev" aria-label="Previous photo" onclick={() => go(-1)}>
          <Icon name="chevron-left" size={28} />
        </button>
        <button type="button" class="nav next" aria-label="Next photo" onclick={() => go(1)}>
          <Icon name="chevron-right" size={28} />
        </button>
      {/if}
    </div>

    {#if cur.caption || cur.role}
      <div class="caption">
        {#if cur.role}<span class="role">{roleLabel(cur.role)}</span>{/if}
        {#if cur.caption}<span>{cur.caption}</span>{/if}
      </div>
    {/if}
  {/if}
</dialog>

<style>
  /* Follows the theme today; --color-viewer-bg/fg (proposed tokens) can pin
     it dark later without touching this file. */
  .viewer {
    --viewer-bg: var(--color-viewer-bg, var(--color-bg));
    --viewer-fg: var(--color-viewer-fg, var(--color-text));
    --viewer-tool: color-mix(in srgb, var(--viewer-fg) 10%, transparent);
    --viewer-tool-hover: color-mix(in srgb, var(--viewer-fg) 18%, transparent);
    position: fixed;
    inset: 0;
    width: 100%;
    height: 100%;
    max-width: none;
    max-height: none;
    margin: 0;
    padding: env(safe-area-inset-top) env(safe-area-inset-right) env(safe-area-inset-bottom) env(safe-area-inset-left);
    border: 0;
    background: var(--viewer-bg);
    color: var(--viewer-fg);
    overflow: hidden;
  }
  .viewer[open] {
    display: flex;
    flex-direction: column;
  }
  .viewer::backdrop {
    background: var(--viewer-bg);
  }
  :global(html:has(dialog.viewer[open])) {
    overflow: hidden;
  }
  .top {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-2);
  }
  .counter {
    flex: 1;
    padding-left: var(--space-2);
    font-size: var(--text-sm);
    font-variant-numeric: tabular-nums;
    opacity: 0.8;
  }
  .tool {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    min-height: var(--tap-min);
    padding: 0 var(--space-3);
    border: 0;
    border-radius: var(--radius-pill);
    background: var(--viewer-tool);
    color: inherit;
    font-size: var(--text-sm);
    cursor: pointer;
  }
  .tool:hover {
    background: var(--viewer-tool-hover);
    text-decoration: none;
  }
  .tool.icon {
    width: var(--tap-min);
    padding: 0;
    justify-content: center;
  }
  .stage {
    position: relative;
    flex: 1;
    min-height: 0;
    display: grid;
    place-items: center;
    touch-action: pan-y pinch-zoom;
    user-select: none;
  }
  .stage img {
    grid-area: 1 / 1;
    max-width: 100%;
    max-height: 100%;
    width: auto;
    height: auto;
    object-fit: contain;
  }
  .loading {
    grid-area: 1 / 1;
    opacity: 0.7;
  }
  .missing {
    display: grid;
    justify-items: center;
    gap: var(--space-2);
    padding: var(--space-5);
    text-align: center;
    opacity: 0.85;
  }
  .missing a {
    color: inherit;
    text-decoration: underline;
  }
  .nav {
    position: absolute;
    top: 50%;
    display: grid;
    place-items: center;
    width: calc(var(--tap-min) + var(--space-2));
    height: calc(var(--tap-min) + var(--space-2));
    margin-top: calc((var(--tap-min) + var(--space-2)) / -2);
    border: 0;
    border-radius: var(--radius-pill);
    background: color-mix(in srgb, var(--viewer-bg) 70%, transparent);
    box-shadow: var(--shadow-sm);
    color: inherit;
    cursor: pointer;
  }
  .nav:hover {
    background: var(--viewer-bg);
  }
  .prev {
    left: var(--space-2);
  }
  .next {
    right: var(--space-2);
  }
  .caption {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
    justify-content: center;
    padding: var(--space-3) var(--space-4) var(--space-4);
    font-size: var(--text-sm);
    text-align: center;
  }
  .role {
    opacity: 0.7;
  }
  .viewer :focus-visible {
    outline-color: var(--viewer-fg);
  }
</style>
