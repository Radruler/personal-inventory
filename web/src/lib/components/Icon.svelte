<script lang="ts">
  import { ICONS, type IconName } from './icons';

  let {
    name,
    size = 20,
    label,
    class: cls = ''
  }: {
    name: IconName;
    /** Pixel size, or any CSS length (e.g. '1.25em'). */
    size?: number | string;
    /** Accessible name; omit for decorative icons (aria-hidden). */
    label?: string;
    class?: string;
  } = $props();

  const dim = $derived(typeof size === 'number' ? `${size}px` : size);
</script>

<svg
  class="icon {cls}"
  viewBox="0 0 24 24"
  style:width={dim}
  style:height={dim}
  role={label ? 'img' : undefined}
  aria-label={label}
  aria-hidden={label ? undefined : 'true'}
  focusable="false"
>
  <path d={ICONS[name]} />
</svg>

<style>
  .icon {
    flex: none;
    fill: none;
    stroke: currentColor;
    stroke-width: 1.8;
    stroke-linecap: round;
    stroke-linejoin: round;
  }
</style>
