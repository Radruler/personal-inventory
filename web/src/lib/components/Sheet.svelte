<script lang="ts">
  // Modal sheet / dialog on the native <dialog> (top layer, inert page,
  // Esc to close) plus a Tab focus trap, backdrop click and focus restore.
  //   side="auto"   bottom sheet on phone, side panel (right) on desktop
  //   side="bottom" always a bottom sheet
  //   side="right"  always a side panel
  //   side="center" a centered dialog
  import type { Snippet } from 'svelte';
  import Button from './Button.svelte';
  import { trapTab } from './focus';

  let {
    open = $bindable(false),
    title,
    side = 'auto',
    size = 'md',
    dismissible = true,
    hideTitle = false,
    initialFocus,
    onclose,
    header,
    footer,
    children
  }: {
    open?: boolean;
    /** Dialog title (also its accessible name). */
    title: string;
    side?: 'auto' | 'bottom' | 'right' | 'center';
    /** Panel width on desktop / centered: sm 400px, md --sheet-max, lg 800px. */
    size?: 'sm' | 'md' | 'lg';
    /** Esc and backdrop close it (default true). */
    dismissible?: boolean;
    hideTitle?: boolean;
    /** CSS selector inside the sheet to focus on open (default: first field, else the panel). */
    initialFocus?: string;
    onclose?: () => void;
    /** Extra content in the header row (after the title). */
    header?: Snippet;
    /** Sticky footer (actions). */
    footer?: Snippet;
    children: Snippet;
  } = $props();

  const uid = $props.id();
  let dialog = $state<HTMLDialogElement>();
  let panel = $state<HTMLElement>();
  let restoreTo: HTMLElement | null = null;

  function close() {
    if (!open) return;
    open = false;
    onclose?.();
  }

  $effect(() => {
    const d = dialog;
    if (!d) return;
    if (open && !d.open) {
      restoreTo = document.activeElement as HTMLElement | null;
      d.showModal();
      const target =
        (initialFocus && d.querySelector<HTMLElement>(initialFocus)) ||
        d.querySelector<HTMLElement>('.body input, .body select, .body textarea, .body [role="tree"] [tabindex="0"]') ||
        panel;
      target?.focus();
    } else if (!open && d.open) {
      d.close();
      if (restoreTo?.isConnected) restoreTo.focus();
      restoreTo = null;
    }
  });

  // Close if unmounted while open (e.g. navigation).
  $effect(() => () => {
    if (dialog?.open) dialog.close();
  });

  function onkeydown(e: KeyboardEvent) {
    if (panel) trapTab(e, panel);
  }

  // Pointer down + up on the backdrop (not a drag that ends there) closes.
  let downOnBackdrop = false;
  function onpointerdown(e: PointerEvent) {
    downOnBackdrop = e.target === dialog;
  }
  function onpointerup(e: PointerEvent) {
    if (dismissible && downOnBackdrop && e.target === dialog) close();
    downOnBackdrop = false;
  }
</script>

<dialog
  bind:this={dialog}
  class="sheet side-{side} size-{size}"
  aria-labelledby="{uid}-title"
  oncancel={(e) => {
    e.preventDefault();
    if (dismissible) close();
  }}
  onclose={() => {
    if (open) close();
  }}
  {onkeydown}
  {onpointerdown}
  {onpointerup}
>
  {#if open}
    <div class="panel" bind:this={panel} tabindex="-1">
      <header class:hidden-title={hideTitle}>
        <span class="grab" aria-hidden="true"></span>
        <h2 id="{uid}-title" class:visually-hidden={hideTitle}>{title}</h2>
        {#if header}{@render header()}{/if}
        <Button variant="ghost" icon="close" label="Close" onclick={close} class="close" />
      </header>
      <div class="body">
        {@render children()}
      </div>
      {#if footer}
        <footer>{@render footer()}</footer>
      {/if}
    </div>
  {/if}
</dialog>

<style>
  dialog {
    position: fixed;
    inset: 0;
    width: 100%;
    height: 100%;
    max-width: none;
    max-height: none;
    margin: 0;
    padding: 0;
    border: 0;
    background: transparent;
    color: var(--color-text);
    overflow: hidden;
  }
  dialog:not([open]) {
    display: none;
  }
  dialog::backdrop {
    background: var(--color-overlay);
    animation: fade var(--duration-normal) var(--ease);
  }
  dialog[open] {
    display: flex;
  }
  :global(html:has(dialog.sheet[open])) {
    overflow: hidden;
  }

  .panel {
    --sheet-width: var(--sheet-max);
    display: flex;
    flex-direction: column;
    min-height: 0;
    background: var(--color-surface);
    box-shadow: var(--shadow-lg);
    outline: none;
  }
  .size-sm .panel {
    --sheet-width: 400px;
  }
  .size-lg .panel {
    --sheet-width: 800px;
  }

  header {
    position: relative;
    display: flex;
    align-items: center;
    gap: var(--space-2);
    min-height: calc(var(--tap-min) + var(--space-3));
    padding: var(--space-2) var(--space-2) var(--space-2) var(--space-4);
    border-bottom: var(--border-width) solid var(--color-border);
  }
  header.hidden-title {
    border-bottom: 0;
  }
  h2 {
    flex: 1;
    min-width: 0;
    font-size: var(--text-lg);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  header :global(.close) {
    margin-left: auto;
  }
  .grab {
    display: none;
  }
  .body {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    overscroll-behavior: contain;
    padding: var(--space-4);
  }
  footer {
    display: flex;
    gap: var(--space-2);
    justify-content: flex-end;
    padding: var(--space-3) var(--space-4);
    border-top: var(--border-width) solid var(--color-border);
    background: var(--color-surface);
  }

  /* bottom sheet */
  .side-bottom,
  .side-auto {
    align-items: flex-end;
    justify-content: center;
  }
  .side-bottom .panel,
  .side-auto .panel {
    width: 100%;
    max-width: var(--sheet-width);
    max-height: calc(100dvh - var(--space-7));
    border-radius: var(--radius-lg) var(--radius-lg) 0 0;
    animation: rise var(--duration-normal) var(--ease);
  }
  .side-bottom footer,
  .side-auto footer,
  .side-bottom .body:last-child,
  .side-auto .body:last-child {
    padding-bottom: calc(var(--space-3) + env(safe-area-inset-bottom));
  }
  .side-bottom .grab,
  .side-auto .grab {
    display: block;
    position: absolute;
    top: var(--space-1);
    left: 50%;
    width: 36px;
    height: 4px;
    margin-left: -18px;
    border-radius: var(--radius-pill);
    background: var(--color-border-strong);
  }

  /* side panel */
  .side-right {
    justify-content: flex-end;
  }
  .side-right .panel {
    width: min(100%, var(--sheet-width));
    height: 100%;
    animation: slide var(--duration-normal) var(--ease);
  }
  .side-right footer,
  .side-right .body:last-child {
    padding-bottom: calc(var(--space-3) + env(safe-area-inset-bottom));
  }

  /* centered dialog */
  .side-center {
    align-items: center;
    justify-content: center;
    padding: var(--gutter);
  }
  .side-center .panel {
    width: min(100%, var(--sheet-width));
    max-height: calc(100dvh - 2 * var(--gutter));
    border-radius: var(--radius-lg);
    animation: pop var(--duration-normal) var(--ease);
  }

  @media (min-width: 720px) {
    .side-auto {
      align-items: stretch;
      justify-content: flex-end;
    }
    .side-auto .panel {
      width: min(100%, var(--sheet-width));
      max-height: none;
      height: 100%;
      border-radius: 0;
      animation-name: slide;
    }
    .side-auto .grab {
      display: none;
    }
  }

  @keyframes fade {
    from {
      opacity: 0;
    }
  }
  @keyframes rise {
    from {
      transform: translateY(24px);
      opacity: 0;
    }
  }
  @keyframes slide {
    from {
      transform: translateX(24px);
      opacity: 0;
    }
  }
  @keyframes pop {
    from {
      transform: scale(0.97);
      opacity: 0;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    dialog::backdrop,
    .panel {
      animation: none !important;
    }
  }
</style>
