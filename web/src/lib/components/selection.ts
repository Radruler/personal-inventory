// Multi-select helpers for PhotoGrid (pure; unit-tested).

/** Toggles one id. */
export function toggleId(selected: readonly string[], id: string): string[] {
  return selected.includes(id) ? selected.filter((s) => s !== id) : [...selected, id];
}

/**
 * Shift-click range: sets every id between `anchor` and `id` (inclusive, in
 * `order`) to the state `id` is switching to. Falls back to a toggle.
 */
export function rangeSelect(order: readonly string[], selected: readonly string[], anchor: string | null, id: string): string[] {
  const a = anchor ? order.indexOf(anchor) : -1;
  const b = order.indexOf(id);
  if (a < 0 || b < 0) return toggleId(selected, id);
  const turnOn = !selected.includes(id);
  const range = order.slice(Math.min(a, b), Math.max(a, b) + 1);
  const set = new Set(selected);
  for (const r of range) {
    if (turnOn) set.add(r);
    else set.delete(r);
  }
  // keep the original order of `selected`, then new ones in grid order
  return [...selected.filter((s) => set.has(s)), ...order.filter((o) => set.has(o) && !selected.includes(o))];
}
