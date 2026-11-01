// Filter-sheet selection state (pure; unit-tested).
//
// A FilterState maps a facet `field` (the query parameter name: kind,
// category, status, location, tag, review, attr.<key>) to the selected
// values. It converts to and from URL query strings with the same encoding
// as `itemQuery` in $lib/api/client, so views can keep filters in the URL.
import { itemQuery, type Facets, type Schemas } from '$lib/api/client';

export type FilterState = Record<string, string[]>;
export type FacetGroup = Schemas['FacetGroup'];
export type FacetValue = Schemas['FacetValue'];

/** Fields the API accepts once (a plain string, not an array). */
export const SINGLE_VALUE_FIELDS: ReadonlySet<string> = new Set(['location', 'review']);

/** Fields filterState understands besides attr.<key>. */
export const LIST_FIELDS: ReadonlySet<string> = new Set(['kind', 'category', 'status', 'tag']);

export function isSelected(state: FilterState, field: string, value: string): boolean {
  return state[field]?.includes(value) ?? false;
}

/** Returns a new state with `value` toggled; single-value fields replace. */
export function toggleValue(state: FilterState, field: string, value: string): FilterState {
  const cur = state[field] ?? [];
  let next: string[];
  if (cur.includes(value)) next = cur.filter((v) => v !== value);
  else next = SINGLE_VALUE_FIELDS.has(field) ? [value] : [...cur, value];
  const out = { ...state };
  if (next.length) out[field] = next;
  else delete out[field];
  return out;
}

/** Removes one field (or everything when field is omitted). */
export function clearField(state: FilterState, field?: string): FilterState {
  if (field === undefined) return {};
  const out = { ...state };
  delete out[field];
  return out;
}

/** Number of selected values across all fields. */
export function activeCount(state: FilterState): number {
  let n = 0;
  for (const vs of Object.values(state)) n += vs.length;
  return n;
}

export type ItemQueryInput = Parameters<typeof itemQuery>[0];

/** Converts selection state (plus search/sort extras) into itemQuery input. */
export function toItemQuery(state: FilterState, extra: Omit<ItemQueryInput, 'attrs'> = {}): ItemQueryInput {
  const q: ItemQueryInput = { ...extra };
  const attrs: Record<string, string[]> = {};
  for (const [field, values] of Object.entries(state)) {
    if (!values.length) continue;
    if (field.startsWith('attr.')) attrs[field.slice(5)] = [...values];
    else if (field === 'kind') q.kind = [...values];
    else if (field === 'category') q.category = [...values];
    else if (field === 'status') q.status = [...values];
    else if (field === 'tag') q.tag = [...values];
    else if (field === 'location') q.location = values[0];
    else if (field === 'review') q.review = values[0];
  }
  if (Object.keys(attrs).length) q.attrs = attrs;
  return q;
}

/** Query string for GET /api/items and /api/facets. */
export function filterParams(state: FilterState, extra: Omit<ItemQueryInput, 'attrs'> = {}): URLSearchParams {
  return itemQuery(toItemQuery(state, extra));
}

/** Reads filter fields back from a query string (ignores q, sort, cursor…). */
export function filtersFromParams(params: URLSearchParams): FilterState {
  const out: FilterState = {};
  for (const [k, v] of params) {
    if (!v) continue;
    if (!(LIST_FIELDS.has(k) || SINGLE_VALUE_FIELDS.has(k) || (k.startsWith('attr.') && k.length > 5))) continue;
    if (SINGLE_VALUE_FIELDS.has(k)) out[k] = [v];
    else if (!out[k]?.includes(v)) out[k] = [...(out[k] ?? []), v];
  }
  return out;
}

/**
 * Values to render for a group: the facet values, plus any selected values
 * the server did not return (count 0), so a selection never disappears.
 * `limit` collapses long groups; selected values always stay visible.
 */
export function groupValues(
  group: FacetGroup,
  state: FilterState,
  limit = Infinity
): { values: FacetValue[]; hidden: number } {
  const selected = state[group.field] ?? [];
  const known = new Set(group.values.map((v) => v.value));
  const all: FacetValue[] = [
    ...group.values,
    ...selected.filter((s) => !known.has(s)).map((s) => ({ value: s, label: s, count: 0 }))
  ];
  if (all.length <= limit) return { values: all, hidden: 0 };
  const shown = all.filter((v, i) => i < limit || selected.includes(v.value));
  return { values: shown, hidden: all.length - shown.length };
}

/** Flat list of active filters with display labels, for a removable-chip row. */
export function activeFilters(
  state: FilterState,
  facets: Facets | null | undefined
): { field: string; value: string; label: string; group: string }[] {
  const groups = new Map((facets?.groups ?? []).map((g) => [g.field, g]));
  const out: { field: string; value: string; label: string; group: string }[] = [];
  for (const [field, values] of Object.entries(state)) {
    const g = groups.get(field);
    for (const value of values) {
      const label = g?.values.find((v) => v.value === value)?.label ?? value;
      out.push({ field, value, label, group: g?.label ?? field.replace(/^attr\./, '') });
    }
  }
  return out;
}

export function sameFilters(a: FilterState, b: FilterState): boolean {
  return filterParams(a).toString() === filterParams(b).toString();
}
