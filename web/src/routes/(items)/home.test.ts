import { describe, expect, it } from 'vitest';
import type { Facets, LocationNode } from '$lib/api/client';
import {
  decorateFacets,
  defaultSort,
  effectiveSort,
  facetsParams,
  findLocation,
  homeSearch,
  itemsParams,
  lifecycleGroup,
  parseHome,
  resultSummary,
  sameQuery,
  withField
} from './home';

const p = (s: string) => new URLSearchParams(s);

describe('home URL state', () => {
  it('parses q, sort and filters (including lifecycle and attrs)', () => {
    const h = parseHome(p('q=+uniqlo+&sort=title&status=unknown&status=staged&attr.color=navy&lifecycle=all&cursor=x'));
    expect(h.q).toBe('uniqlo');
    expect(h.sort).toBe('title');
    expect(h.filters).toEqual({ status: ['unknown', 'staged'], 'attr.color': ['navy'], lifecycle: ['all'] });
  });

  it('ignores unknown sorts', () => {
    expect(parseHome(p('sort=bogus')).sort).toBeNull();
  });

  it('defaults to relevance while searching, else updated', () => {
    expect(defaultSort('')).toBe('updated');
    expect(defaultSort('x')).toBe('relevance');
    expect(effectiveSort({ q: '', sort: null })).toBe('updated');
    expect(effectiveSort({ q: 'x', sort: null })).toBe('relevance');
    expect(effectiveSort({ q: '', sort: 'relevance' })).toBe('updated');
    expect(effectiveSort({ q: 'x', sort: 'created' })).toBe('created');
  });

  it('writes sort to the URL only when it is not the default', () => {
    expect(homeSearch({ q: '', sort: null, filters: {} })).toBe('');
    expect(homeSearch({ q: '', sort: 'updated', filters: {} })).toBe('');
    expect(homeSearch({ q: 'a b', sort: 'relevance', filters: {} })).toBe('?q=a+b');
    expect(homeSearch({ q: '', sort: 'title', filters: { kind: ['clothing'] } })).toBe('?kind=clothing&sort=title');
  });

  it('round-trips through the URL', () => {
    const h = parseHome(p('q=jacket&location=01ABC&attr.size=M&attr.size=L&sort=created'));
    expect(parseHome(p(homeSearch(h).slice(1)))).toEqual(h);
  });

  it('builds API params with the effective sort and paging; facets without them', () => {
    const h = parseHome(p('q=navy&review=needs_review'));
    expect(itemsParams(h, 'CUR', 30).toString()).toBe('q=navy&review=needs_review&sort=relevance&cursor=CUR&limit=30');
    expect(facetsParams(h).toString()).toBe('q=navy&review=needs_review');
  });

  it('sends lifecycle and product filters to the API', () => {
    expect(itemsParams(parseHome(p('lifecycle=gone&product=P1')), null, 60).toString()).toBe('lifecycle=gone&product=P1&sort=updated&limit=60');
  });

  it('treats an explicit default sort as the same query', () => {
    expect(sameQuery(parseHome(p('q=a')), parseHome(p('q=a&sort=relevance')))).toBe(true);
    expect(sameQuery(parseHome(p('q=a')), parseHome(p('q=a&sort=title')))).toBe(false);
  });

  it('withField replaces or removes a field', () => {
    expect(withField({ kind: ['object'] }, 'lifecycle', ['gone'])).toEqual({ kind: ['object'], lifecycle: ['gone'] });
    expect(withField({ lifecycle: ['gone'] }, 'lifecycle', [])).toEqual({});
  });
});

describe('facet decoration', () => {
  const facets: Facets = {
    total: 3,
    groups: [{ field: 'location', label: 'Location', values: [{ value: 'U2', label: 'Unit 2', count: 19 }] }]
  };

  it('adds the lifecycle group with owned+gone for "all"', () => {
    const g = lifecycleGroup(57, 2);
    expect(g.values).toEqual([
      { value: 'gone', label: 'Gone only', count: 2 },
      { value: 'all', label: 'Owned and gone', count: 59 }
    ]);
    expect(decorateFacets(facets, {}, { lifecycle: g })!.groups.map((x) => x.field)).toEqual(['location', 'lifecycle']);
  });

  it('labels a deep-linked location the facet does not list, counting the total', () => {
    const out = decorateFacets(facets, { location: ['BOX'] }, { locationLabel: 'Unit 2 › B-014' })!;
    expect(out.groups[0].values[0]).toEqual({ value: 'BOX', label: 'Unit 2 › B-014', count: 3 });
    expect(facets.groups[0].values).toHaveLength(1); // input not mutated
  });

  it('labels the unknown location and leaves listed ones alone', () => {
    expect(decorateFacets(facets, { location: ['unknown'] })!.groups[0].values[0].label).toBe('Unknown location');
    expect(decorateFacets(facets, { location: ['U2'] })!.groups[0].values).toHaveLength(1);
  });

  it('adds a product group for ?product=', () => {
    const out = decorateFacets(facets, { product: ['P1'] })!;
    expect(out.groups.find((g) => g.field === 'product')?.values[0]).toEqual({ value: 'P1', label: 'Same product', count: 3 });
  });

  it('passes null through', () => {
    expect(decorateFacets(null, {})).toBeNull();
  });
});

describe('findLocation', () => {
  const node = (id: string, code: string | null, children: LocationNode[] = []) =>
    ({ id, code, name: id, label: id, children }) as unknown as LocationNode;
  const roots = [node('S', 'SS', [node('U2', 'U2', [node('B', 'B-014')])])];
  it('finds by id or code (case-insensitive)', () => {
    expect(findLocation(roots, 'B')?.id).toBe('B');
    expect(findLocation(roots, 'code:b-014')?.id).toBe('B');
    expect(findLocation(roots, 'code:nope')).toBeNull();
  });
});

describe('resultSummary', () => {
  it('pluralises and quotes the search', () => {
    expect(resultSummary(1, '')).toBe('1 item');
    expect(resultSummary(59, '')).toBe('59 items');
    expect(resultSummary(6, 'uniqlo')).toBe('6 matches for “uniqlo”');
  });
});
