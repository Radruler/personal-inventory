<script lang="ts">
  // App shell: top bar on desktop, bottom tab bar on phone (PLAN.md §8).
  import '$styles/tokens.css';
  import '$styles/base.css';
  import { page } from '$app/state';
  import { auth, setToken } from '$lib/api/auth.svelte';

  let { children } = $props();

  const nav = [
    { href: '/', label: 'Items', icon: 'M4 5h16v14H4z M4 10h16 M9 5v14' },
    { href: '/locations', label: 'Places', icon: 'M12 21s-7-6.2-7-11.5A7 7 0 0 1 19 9.5C19 14.8 12 21 12 21z M12 12.5a2.5 2.5 0 1 0 0-5 2.5 2.5 0 0 0 0 5z' },
    { href: '/add', label: 'Add', icon: 'M12 5v14 M5 12h14', primary: true },
    { href: '/staging', label: 'Packing', icon: 'M3 8l9-5 9 5v8l-9 5-9-5z M3 8l9 5 9-5 M12 13v8' },
    { href: '/inbox', label: 'Inbox', icon: 'M4 13l2.5-8h11L20 13v6H4z M4 13h5l1 2h4l1-2h5' }
  ];

  function active(href: string): boolean {
    const p = page.url.pathname;
    if (href === '/') return p === '/' || p.startsWith('/items');
    return p === href || p.startsWith(href + '/');
  }

  let tokenInput = $state('');
</script>

<div class="shell">
  <header class="topbar no-print">
    <a class="brand" href="/">Personal Inventory</a>
    <nav class="topnav" aria-label="Main">
      {#each nav as n (n.href)}
        <a href={n.href} class:active={active(n.href)} class:primary={n.primary} aria-current={active(n.href) ? 'page' : undefined}>
          {n.label}
        </a>
      {/each}
    </nav>
  </header>

  <main class="main">
    {@render children()}
  </main>

  <nav class="tabbar no-print" aria-label="Main">
    {#each nav as n (n.href)}
      <a href={n.href} class:active={active(n.href)} class:primary={n.primary} aria-current={active(n.href) ? 'page' : undefined}>
        <svg viewBox="0 0 24 24" aria-hidden="true"><path d={n.icon} /></svg>
        <span>{n.label}</span>
      </a>
    {/each}
  </nav>
</div>

{#if auth.needed}
  <div class="token-backdrop" role="presentation">
    <form
      class="token-dialog"
      onsubmit={(e) => {
        e.preventDefault();
        setToken(tokenInput);
        tokenInput = '';
      }}
    >
      <h2>Access token</h2>
      <p class="muted">This server requires a token for changes. It is stored only in this browser.</p>
      <!-- svelte-ignore a11y_autofocus -->
      <input type="password" autocomplete="current-password" bind:value={tokenInput} placeholder="INV_TOKEN" autofocus />
      <div class="row">
        <button type="button" onclick={() => (auth.needed = false)}>Cancel</button>
        <button type="submit" class="primary">Save</button>
      </div>
    </form>
  </div>
{/if}

<style>
  .shell {
    min-height: 100dvh;
    display: flex;
    flex-direction: column;
  }

  .topbar {
    position: sticky;
    top: 0;
    z-index: var(--z-nav);
    height: var(--topbar-height);
    display: none;
    align-items: center;
    gap: var(--space-5);
    padding: 0 var(--gutter);
    background: color-mix(in srgb, var(--color-bg) 88%, transparent);
    backdrop-filter: blur(12px);
    border-bottom: var(--border-width) solid var(--color-border);
  }
  .brand {
    font-weight: var(--weight-bold);
    color: var(--color-text);
  }
  .topnav {
    display: flex;
    gap: var(--space-1);
    margin-left: auto;
  }
  .topnav a {
    padding: var(--space-2) var(--space-3);
    border-radius: var(--radius-pill);
    color: var(--color-text-muted);
    font-weight: var(--weight-medium);
  }
  .topnav a:hover {
    text-decoration: none;
    background: var(--color-surface-2);
  }
  .topnav a.active {
    color: var(--color-text);
    background: var(--color-surface-2);
  }
  .topnav a.primary {
    color: var(--color-accent-contrast);
    background: var(--color-accent);
  }

  .main {
    flex: 1;
    padding-bottom: calc(var(--tabbar-height) + env(safe-area-inset-bottom));
  }

  .tabbar {
    position: fixed;
    inset: auto 0 0 0;
    z-index: var(--z-nav);
    display: grid;
    grid-template-columns: repeat(5, 1fr);
    height: calc(var(--tabbar-height) + env(safe-area-inset-bottom));
    padding-bottom: env(safe-area-inset-bottom);
    background: color-mix(in srgb, var(--color-surface) 92%, transparent);
    backdrop-filter: blur(12px);
    border-top: var(--border-width) solid var(--color-border);
  }
  .tabbar a {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: var(--space-0);
    min-height: var(--tap-min);
    color: var(--color-text-muted);
    font-size: var(--text-xs);
  }
  .tabbar a:hover {
    text-decoration: none;
  }
  .tabbar a.active {
    color: var(--color-accent);
  }
  .tabbar svg {
    width: 24px;
    height: 24px;
    fill: none;
    stroke: currentColor;
    stroke-width: 1.8;
    stroke-linecap: round;
    stroke-linejoin: round;
  }
  .tabbar a.primary svg {
    width: 36px;
    height: 36px;
    padding: 6px;
    border-radius: var(--radius-pill);
    background: var(--color-accent);
    stroke: var(--color-accent-contrast);
  }

  @media (min-width: 720px) {
    .topbar {
      display: flex;
    }
    .tabbar {
      display: none;
    }
    .main {
      padding-bottom: 0;
    }
  }

  .token-backdrop {
    position: fixed;
    inset: 0;
    z-index: var(--z-sheet);
    display: grid;
    place-items: center;
    padding: var(--gutter);
    background: var(--color-overlay);
  }
  .token-dialog {
    width: min(100%, 400px);
    display: grid;
    gap: var(--space-3);
    padding: var(--space-5);
    border-radius: var(--radius-lg);
    background: var(--color-surface);
    box-shadow: var(--shadow-lg);
  }
  .token-dialog input {
    min-height: var(--tap-min);
    padding: 0 var(--space-3);
    border: var(--border-width) solid var(--color-border-strong);
    border-radius: var(--radius-md);
    background: var(--color-bg);
  }
  .row {
    display: flex;
    justify-content: flex-end;
    gap: var(--space-2);
  }
  .row button {
    min-height: var(--tap-min);
    padding: 0 var(--space-4);
    border: var(--border-width) solid var(--color-border);
    border-radius: var(--radius-md);
    background: var(--color-surface);
    cursor: pointer;
  }
  .row button.primary {
    border-color: var(--color-accent);
    background: var(--color-accent);
    color: var(--color-accent-contrast);
  }
</style>
