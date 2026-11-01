<script lang="ts">
  let { size = 20, label }: { size?: number; /** When set, the spinner announces itself. */ label?: string } = $props();
</script>

<span class="spinner" style:width="{size}px" style:height="{size}px" role={label ? 'status' : undefined} aria-label={label} aria-hidden={label ? undefined : 'true'}></span>

<style>
  .spinner {
    display: inline-block;
    flex: none;
    border-radius: var(--radius-pill);
    border: 2px solid color-mix(in srgb, currentColor 22%, transparent);
    border-top-color: currentColor;
    animation: spin 0.8s linear infinite;
  }
  @keyframes spin {
    to {
      transform: rotate(1turn);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .spinner {
      animation-duration: 2.4s;
    }
  }
</style>
