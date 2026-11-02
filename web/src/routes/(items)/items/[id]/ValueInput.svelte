<script lang="ts">
  // A fact value field shaped by the key's value_type (money gets a currency
  // field, booleans a select, composition/json a textarea). The caller
  // parses with parseValueInput(type, text, currency).
  import type { ValueType } from './detail';

  let {
    type = null,
    text = $bindable(''),
    currency = $bindable(''),
    id,
    error = ''
  }: {
    type?: ValueType | null;
    text?: string;
    currency?: string;
    /** id of the main control (for the caller's <label for>). */
    id: string;
    error?: string;
  } = $props();

  const hint = $derived(
    type === 'date'
      ? 'YYYY, YYYY-MM or YYYY-MM-DD'
      : type === 'composition'
        ? 'e.g. shell: 100% nylon; lining: 80% cotton, 20% polyester'
        : type === 'json'
          ? 'Any JSON value'
          : ''
  );
</script>

<div class="vi">
  {#if type === 'boolean'}
    <select {id} bind:value={text} aria-invalid={error ? 'true' : undefined}>
      <option value="">Choose…</option>
      <option value="yes">Yes</option>
      <option value="no">No</option>
    </select>
  {:else if type === 'money'}
    <div class="money">
      <input {id} type="text" inputmode="decimal" bind:value={text} placeholder="Amount" aria-invalid={error ? 'true' : undefined} />
      <input type="text" bind:value={currency} placeholder="USD" maxlength="3" aria-label="Currency" autocapitalize="characters" />
    </div>
  {:else if type === 'composition' || type === 'json'}
    <textarea {id} rows="3" bind:value={text} aria-invalid={error ? 'true' : undefined} spellcheck="false"></textarea>
  {:else}
    <input
      {id}
      type="text"
      inputmode={type === 'number' ? 'decimal' : undefined}
      bind:value={text}
      placeholder={type === 'date' ? '2024-03' : ''}
      aria-invalid={error ? 'true' : undefined}
    />
  {/if}
  {#if hint}<p class="hint muted">{hint}</p>{/if}
  {#if error}<p class="err" role="alert">{error}</p>{/if}
</div>

<style>
  .vi {
    display: grid;
    gap: var(--space-1);
  }
  input,
  select,
  textarea {
    width: 100%;
    min-height: var(--tap-min);
    padding: var(--space-2) var(--space-3);
    border: var(--border-width) solid var(--color-border-strong);
    border-radius: var(--radius-md);
    background: var(--color-bg);
  }
  textarea {
    font-family: var(--font-mono);
    font-size: var(--text-sm);
    resize: vertical;
  }
  [aria-invalid='true'] {
    border-color: var(--color-danger);
  }
  .money {
    display: grid;
    grid-template-columns: 1fr 6rem;
    gap: var(--space-2);
  }
  .hint,
  .err {
    font-size: var(--text-xs);
  }
  .err {
    color: var(--color-danger);
  }
</style>
