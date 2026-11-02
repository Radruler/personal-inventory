// All items (home) URL state: search, filters and sort live in the query
// string so back/forward, reload and links like `/?status=unknown` or
// `/?location=<id>` restore the view. Pure; unit-tested in home.test.ts.
import type { Facets, LocationNode, Schemas } from '$lib/api/client';
import { filterParams, filtersFromParams, type FacetGroup, type FilterState } from '$lib/components/filters';

export type SortKey = 'relevance' | 'updated' | 'created' | 'title';

export const SORTS: { value: SortKey; label: string }[] = [
  { value: 'relevance', label: 'Best match' },
  { value: 'updated', label: 'Recently updated' },
  { value: 'created', label: 'Recently added' },
  { value: 'title', label: 'Title A–Z' }
];

const SORT_KEYS = new Set<string>(SORTS.map((s) => s.value));

export type HomeQuery = {
  q: string;
  /** Explicit sort from the URL; null = the default for the current q. */
  sort: SortKey | null;
  filters: FilterState;
};

export function parseHome(params: URLSearchParams): HomeQuery {
  const s = params.get('sort') ?? '';
  return {
    q: (params.get('q') ?? '').trim(),
    sort: SORT_KEYS.has(s) ? (s as SortKey) : null,
    filters: filtersFromParams(params)
  };
}

/** The default sort: relevance while searching, else recently updated. */
export function defaultSort(q: string): SortKey {
  return q ? 'relevance' : 'updated';
}

/** The sort actually sent to the API (relevance needs q). */
export function effectiveSort(h: Pick<HomeQuery, 'q' | 'sort'>): SortKey {
  if (!h.sort) return defaultSort(h.q);
  if (h.sort === 'relevance' && !h.q) return 'updated';
  return h.sort;
}

/**
 * The page's query string ('' or '?…'). `sort` is written only when it
 * differs from the default, so plain links stay short and canonical.
 */
export function homeSearch(h: HomeQuery): string {
  const sort = effectiveSort(h);
  const p = filterParams(h.filters, {
    q: h.q || undefined,
    sort: h.sort && sort !== defaultSort(h.q) ? sort : undefined
  });
  const s = p.toString();
  return s ? `?${s}` : '';
}

/** Query for GET /api/items. */
export function itemsParams(h: HomeQuery, cursor?: string | null, limit = 60): URLSearchParams {
  return filterParams(h.filters, { q: h.q || undefined, sort: effectiveSort(h), cursor: cursor || undefined, limit });
}

/** Query for GET /api/facets (same filters, no paging). */
export function facetsParams(h: HomeQuery): URLSearchParams {
  return filterParams(h.filters, { q: h.q || undefined });
}

/** Two HomeQuery values describe the same result set (sort included). */
export function sameQuery(a: HomeQuery, b: HomeQuery): boolean {
  return itemsParams(a).toString() === itemsParams(b).toString();
}

/** Filters with one field replaced (or removed when values is empty). */
export function withField(state: FilterState, field: string, values: string[]): FilterState {
  const out = { ...state };
  if (values.length) out[field] = values;
  else delete out[field];
  return out;
}

/**
 * The "Show" group the server's facets lack: lifecycle. `owned` and `gone`
 * are the counts for the current filters with lifecycle set to each.
 */
export function lifecycleGroup(owned: number, gone: number): FacetGroup {
  return {
    field: 'lifecycle',
    label: 'Show',
    values: [
      { value: 'gone', label: 'Gone only', count: gone },
      { value: 'all', label: 'Owned and gone', count: owned + gone }
    ]
  };
}

/** Finds a location node by id or `code:<CODE>` (case-insensitive). */
export function findLocation(roots: readonly LocationNode[], ref: string): LocationNode | null {
  const code = ref.toLowerCase().startsWith('code:') ? ref.slice(5).toLowerCase() : null;
  const stack = [...roots];
  while (stack.length) {
    const n = stack.pop()!;
    if (code ? n.code?.toLowerCase() === code : n.id === ref) return n;
    stack.push(...n.children);
  }
  return null;
}

/**
 * Facets as the filter UI shows them: the server's groups plus the lifecycle
 * group, and a labelled entry for a selected location the location group
 * does not list (it only lists the top two levels; deep links from box pages
 * select a box). Its count is the facets total, which already includes it.
 */
export function decorateFacets(
  facets: Facets | null,
  state: FilterState,
  extra: { lifecycle?: FacetGroup | null; locationLabel?: string | null } = {}
): Facets | null {
  if (!facets) return null;
  const groups: Schemas['FacetGroup'][] = facets.groups.map((g) => ({ ...g, values: [...g.values] }));
  const loc = state.location?.[0];
  if (loc) {
    let g = groups.find((x) => x.field === 'location');
    if (!g) {
      g = { field: 'location', label: 'Location', values: [] };
      groups.push(g);
    }
    if (!g.values.some((v) => v.value === loc)) {
      const label = loc === 'unknown' ? 'Unknown location' : (extra.locationLabel ?? loc);
      g.values.unshift({ value: loc, label, count: facets.total });
    }
  }
  const product = state.product?.[0];
  if (product && !groups.some((g) => g.field === 'product')) {
    groups.push({ field: 'product', label: 'Product', values: [{ value: product, label: 'Same product', count: facets.total }] });
  }
  if (extra.lifecycle) groups.push(extra.lifecycle);
  return { ...facets, groups };
}

/** "59 items", "1 item", "3 matches for “uniqlo”". */
export function resultSummary(total: number, q: string): string {
  if (q) return `${total} ${total === 1 ? 'match' : 'matches'} for “${q}”`;
  return `${total} ${total === 1 ? 'item' : 'items'}`;
}
