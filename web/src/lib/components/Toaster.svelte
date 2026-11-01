<script lang="ts">
  import Icon from './Icon.svelte';
  import { dismissToast, toasts } from './toast.svelte';
</script>

<div class="toaster" aria-live="polite" aria-relevant="additions">
  {#each toasts as t (t.id)}
    <div class="toast {t.tone}" role={t.tone === 'error' ? 'alert' : 'status'}>
      <Icon name={t.tone === 'error' ? 'alert' : t.tone === 'success' ? 'check' : 'info'} size={18} />
      <span class="msg">{t.message}</span>
      {#if t.action}
        <button
          type="button"
          class="action"
          onclick={() => {
            t.action?.run();
            dismissToast(t.id);
          }}>{t.action.label}</button
        >
      {/if}
      <button type="button" class="x" aria-label="Dismiss" onclick={() => dismissToast(t.id)}>
        <Icon name="close" size={16} />
      </button>
    </div>
  {/each}
</div>

<style>
  .toaster {
    position: fixed;
    left: 50%;
    bottom: calc(var(--tabbar-height) + env(safe-area-inset-bottom) + var(--space-3));
    z-index: var(--z-toast);
    display: grid;
    gap: var(--space-2);
    width: min(calc(100% - 2 * var(--gutter)), 440px);
    transform: translateX(-50%);
    pointer-events: none;
  }
  .toast {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    min-height: var(--tap-min);
    padding: var(--space-1) var(--space-1) var(--space-1) var(--space-3);
    border-radius: var(--radius-md);
    background: var(--color-text);
    color: var(--color-bg);
    box-shadow: var(--shadow-lg);
    font-size: var(--text-sm);
    pointer-events: auto;
    animation: in var(--duration-normal) var(--ease);
  }
  .toast.error {
    background: var(--color-danger);
    color: var(--color-surface);
  }
  .msg {
    flex: 1;
    min-width: 0;
  }
  button {
    min-height: var(--tap-min);
    border: 0;
    border-radius: var(--radius-sm);
    background: transparent;
    color: inherit;
    cursor: pointer;
  }
  .action {
    padding: 0 var(--space-3);
    font-weight: var(--weight-bold);
  }
  .x {
    display: grid;
    place-items: center;
    width: var(--tap-min);
    opacity: 0.75;
  }
  button:hover {
    background: color-mix(in srgb, currentColor 14%, transparent);
  }
  @media (min-width: 720px) {
    .toaster {
      bottom: calc(var(--space-5) + env(safe-area-inset-bottom));
    }
  }
  @keyframes in {
    from {
      transform: translateY(8px);
      opacity: 0;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .toast {
      animation: none;
    }
  }
</style>
