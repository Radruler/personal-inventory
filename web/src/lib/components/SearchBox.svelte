<script lang="ts">
  import { onDestroy } from 'svelte';
  import Icon from './Icon.svelte';
  import Spinner from './Spinner.svelte';

  let {
    value = $bindable(''),
    placeholder = 'Search',
    label = 'Search',
    /** Delay in ms before `onsearch` fires while typing (0 = only on Enter/clear). */
    debounce = 250,
    busy = false,
    autofocus = false,
    onsearch,
    onkeydown,
    input = $bindable()
  }: {
    value?: string;
    placeholder?: string;
    label?: string;
    debounce?: number;
    /** Shows a spinner instead of the search icon. */
    busy?: boolean;
    autofocus?: boolean;
    /** Fires after typing settles, on Enter and on clear. */
    onsearch?: (q: string) => void;
    onkeydown?: (e: KeyboardEvent) => void;
    /** The <input> element (bindable), e.g. to focus it. */
    input?: HTMLInputElement;
  } = $props();

  const id = $props.id();
  let timer: ReturnType<typeof setTimeout> | undefined;

  function schedule() {
    clearTimeout(timer);
    if (debounce > 0) timer = setTimeout(() => onsearch?.(value), debounce);
  }
  function now() {
    clearTimeout(timer);
    onsearch?.(value);
  }
  onDestroy(() => clearTimeout(timer));

  $effect(() => {
    if (autofocus) input?.focus();
  });
</script>

<form
  class="search"
  role="search"
  onsubmit={(e) => {
    e.preventDefault();
    now();
  }}
>
  <label class="visually-hidden" for="{id}-q">{label}</label>
  <span class="lead" aria-hidden="true">
    {#if busy}<Spinner size={18} />{:else}<Icon name="search" size={20} />{/if}
  </span>
  <input
    bind:this={input}
    id="{id}-q"
    type="search"
    enterkeyhint="search"
    autocomplete="off"
    autocapitalize="off"
    spellcheck="false"
    {placeholder}
    bind:value
    oninput={schedule}
    onkeydown={(e) => {
      if (e.key === 'Escape' && value) {
        e.preventDefault();
        e.stopPropagation();
        value = '';
        now();
      }
      onkeydown?.(e);
    }}
  />
  {#if value}
    <button
      type="button"
      class="clear"
      aria-label="Clear search"
      onclick={() => {
        value = '';
        now();
        input?.focus();
      }}
    >
      <Icon name="close" size={18} />
    </button>
  {/if}
</form>

<style>
  .search {
    position: relative;
    display: flex;
    align-items: center;
    width: 100%;
  }
  .lead {
    position: absolute;
    left: var(--space-3);
    display: inline-flex;
    color: var(--color-text-muted);
    pointer-events: none;
  }
  input {
    width: 100%;
    min-height: var(--tap-min);
    padding: 0 var(--tap-min) 0 calc(var(--space-3) + 20px + var(--space-2));
    border: var(--border-width) solid var(--color-border-strong);
    border-radius: var(--radius-pill);
    background: var(--color-surface);
    color: var(--color-text);
    font-size: var(--text-md);
    appearance: none;
  }
  input::placeholder {
    color: var(--color-text-faint);
  }
  input::-webkit-search-cancel-button {
    appearance: none;
  }
  input:focus-visible {
    outline: 2px solid var(--color-focus);
    outline-offset: 0;
    border-color: transparent;
  }
  .clear {
    position: absolute;
    right: 0;
    display: grid;
    place-items: center;
    width: var(--tap-min);
    height: var(--tap-min);
    border: 0;
    border-radius: var(--radius-pill);
    background: transparent;
    color: var(--color-text-muted);
    cursor: pointer;
  }
  .clear:hover {
    color: var(--color-text);
  }
</style>
