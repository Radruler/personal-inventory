import { describe, expect, it } from 'vitest';
import { fixtureFacets, fixtureGalleryMedia, fixtureLocationTree, placements, photo } from '../../routes/dev/kit/fixtures';
import { chipText, formatCategory, formatConfidence, roleLabel, splitStaged } from './format';
import { groupByTab, hasVariant, swipeDirection, tabForRole, wrapIndex } from './gallery';
import {
  activeCount,
  activeFilters,
  clearField,
  filterParams,
  filtersFromParams,
  groupValues,
  sameFilters,
  toggleValue,
  toItemQuery
} from './filters';
import {
  ancestorsOf,
  buildCreateRequest,
  indexTree,
  matchesQuery,
  parentOptions,
  searchTree,
  suggestChildKind,
  treeKey,
  visibleRows
} from './locationTree';
import { rangeSelect, toggleId } from './selection';

describe('gallery grouping', () => {
  it('maps roles to tabs, unknown roles to Other', () => {
    expect(tabForRole('flat_lay')).toBe('flat_lay');
    expect(tabForRole('care_label')).toBe('labels');
    expect(tabForRole('hang_tag')).toBe('labels');
    expect(tabForRole('defect')).toBe('detail');
    expect(tabForRole('receipt')).toBe('other');
    expect(tabForRole('something_new')).toBe('other');
  });
  it('groups in tab order, drops empty tabs, keeps input order', () => {
    const groups = groupByTab(fixtureGalleryMedia);
    expect(groups.map((g) => g.id)).toEqual(['flat_lay', 'on_body', 'stock', 'labels', 'detail', 'other']);
    expect(groups.find((g) => g.id === 'labels')!.photos.map((p) => p.role)).toEqual(['label', 'care_label', 'hang_tag']);
    expect(groups.find((g) => g.id === 'on_body')!.photos).toHaveLength(2);
  });
  it('only shows tabs with photos and dedupes a media within a tab', () => {
    const m = fixtureGalleryMedia[0];
    const groups = groupByTab([m, { ...m, role: 'flat_lay', sort: 5 }, fixtureGalleryMedia[7]]);
    expect(groups.map((g) => [g.id, g.photos.length])).toEqual([
      ['flat_lay', 1],
      ['detail', 1]
    ]);
    expect(groupByTab([])).toEqual([]);
  });
  it('wraps indexes and reads swipes', () => {
    expect(wrapIndex(-1, 4)).toBe(3);
    expect(wrapIndex(4, 4)).toBe(0);
    expect(wrapIndex(2, 0)).toBe(0);
    expect(swipeDirection(-80, 5)).toBe(1);
    expect(swipeDirection(80, 5)).toBe(-1);
    expect(swipeDirection(20, 0)).toBe(0);
    expect(swipeDirection(-80, 120)).toBe(0); // mostly vertical
  });
  it('knows when variants are usable', () => {
    expect(hasVariant(photo(1, 'a'), 'thumb')).toBe(true);
    expect(hasVariant(photo(1, 'a', 10, 10, 'pending'), 'display')).toBe(false);
  });
});

describe('format', () => {
  it('formats categories, roles and confidence', () => {
    expect(formatCategory('outerwear/shell')).toBe('outerwear › shell');
    expect(formatCategory(null)).toBe('');
    expect(roleLabel('care_label')).toBe('Care label');
    expect(roleLabel('box_lid')).toBe('Box lid');
    expect(formatConfidence(0.804)).toBe('80%');
    expect(formatConfidence(null)).toBe('');
  });
  it('builds compact chip text for every status', () => {
    expect(chipText(placements.placed())).toBe('Unit 2 › B-014');
    expect(chipText(placements.approximate())).toBe('Unit 2');
    expect(chipText(placements.staged())).toBe('Bedroom → U2');
    expect(chipText(placements.stagedFromUnknown())).toBe('? → B-014');
    expect(chipText(placements.unknown())).toBe('Unknown');
    expect(chipText(placements.gone())).toBe('Sold · 2026-03');
    expect(chipText(placements.staged(), 'full')).toBe('Bedroom → going to Unit 2');
    expect(splitStaged('Bedroom → going to Unit 2')).toEqual(['Bedroom', 'going to Unit 2']);
    expect(splitStaged('Unit 2')).toBeNull();
  });
});

describe('filter state', () => {
  it('toggles multi and single value fields', () => {
    let s = toggleValue({}, 'kind', 'clothing');
    s = toggleValue(s, 'kind', 'object');
    expect(s.kind).toEqual(['clothing', 'object']);
    s = toggleValue(s, 'kind', 'clothing');
    expect(s.kind).toEqual(['object']);
    s = toggleValue(s, 'location', 'L-u2');
    s = toggleValue(s, 'location', 'L-bed');
    expect(s.location).toEqual(['L-bed']);
    s = toggleValue(s, 'location', 'L-bed');
    expect('location' in s).toBe(false);
    expect(activeCount(s)).toBe(1);
    expect(clearField(s, 'kind')).toEqual({});
  });
  it('round-trips through query params', () => {
    const s = { kind: ['clothing'], status: ['staged', 'unknown'], location: ['code:U2'], 'attr.color': ['navy'], review: ['needs_review'] };
    const p = filterParams(s, { q: 'jacket' });
    expect(p.toString()).toBe(
      'q=jacket&kind=clothing&location=code%3AU2&status=staged&status=unknown&review=needs_review&attr.color=navy'
    );
    const back = filtersFromParams(p);
    expect(sameFilters(back, s)).toBe(true);
    expect(back.q).toBeUndefined();
    expect(toItemQuery({ tag: ['a'] }).tag).toEqual(['a']);
  });
  it('keeps selected values visible in collapsed groups', () => {
    const cat = fixtureFacets.groups.find((g) => g.field === 'category')!;
    const { values, hidden } = groupValues(cat, { category: ['bags', 'missing'] }, 4);
    expect(values.map((v) => v.value)).toEqual(['outerwear', 'tops', 'bottoms', 'knitwear', 'bags', 'missing']);
    expect(values.at(-1)!.count).toBe(0);
    expect(hidden).toBe(12 - 6);
  });
  it('lists active filters with labels', () => {
    expect(activeFilters({ location: ['L-u2'], 'attr.fit': ['slim'] }, fixtureFacets)).toEqual([
      { field: 'location', value: 'L-u2', label: 'Unit 2', group: 'Location' },
      { field: 'attr.fit', value: 'slim', label: 'slim', group: 'fit' }
    ]);
  });
});

describe('location tree', () => {
  const roots = fixtureLocationTree.roots;
  const idx = indexTree(roots);

  it('indexes depth and ancestors', () => {
    expect(idx.get('L-p03')!.depth).toBe(5);
    expect(ancestorsOf(idx, 'L-p03')).toEqual(['L-b020', 'L-ga', 'L-garage', 'L-home']);
    expect(ancestorsOf(idx, 'L-home')).toEqual([]);
  });
  it('matches names, codes and labels by prefix', () => {
    const n = idx.get('L-b014')!.node;
    expect(matchesQuery(n, 'b-014')).toBe(true);
    expect(matchesQuery(n, 'b014')).toBe(true);
    expect(matchesQuery(n, 'winter')).toBe(true);
    expect(matchesQuery(n, 'unit winter')).toBe(true);
    expect(matchesQuery(n, 'summer')).toBe(false);
  });
  it('filters to matches plus ancestors', () => {
    const s = searchTree(roots, 'pouch')!;
    expect([...s.matches]).toEqual(['L-p03']);
    const rows = visibleRows(roots, new Set(), s);
    expect(rows.map((r) => r.node.id)).toEqual(['L-home', 'L-garage', 'L-ga', 'L-b020', 'L-p03']);
    expect(rows.at(-1)!.match).toBe(true);
    expect(searchTree(roots, '  ')).toBeNull();
  });
  it('shows children of a match the user expands', () => {
    const s = searchTree(roots, 'rack a')!;
    expect([...s.matches]).toEqual(['L-ga']);
    const rows = visibleRows(roots, new Set(['L-ga', 'L-b020']), s).map((r) => r.node.id);
    expect(rows).toEqual(['L-home', 'L-garage', 'L-ga', 'L-b020', 'L-p03']);
  });
  it('flattens by expansion with aria positions', () => {
    const rows = visibleRows(roots, new Set(['L-sk']));
    expect(rows.map((r) => r.node.id)).toEqual(['L-home', 'L-sk', 'L-u2', 'L-u7']);
    const u7 = rows[3];
    expect([u7.depth, u7.posinset, u7.setsize, u7.hasChildren]).toEqual([2, 2, 2, false]);
  });
  it('implements tree keyboard moves', () => {
    const rows = visibleRows(roots, new Set(['L-sk']));
    expect(treeKey(rows, 'L-home', 'ArrowDown')).toEqual({ type: 'focus', id: 'L-sk' });
    expect(treeKey(rows, 'L-home', 'ArrowRight')).toEqual({ type: 'expand', id: 'L-home' });
    expect(treeKey(rows, 'L-sk', 'ArrowRight')).toEqual({ type: 'focus', id: 'L-u2' });
    expect(treeKey(rows, 'L-sk', 'ArrowLeft')).toEqual({ type: 'collapse', id: 'L-sk' });
    expect(treeKey(rows, 'L-u7', 'ArrowLeft')).toEqual({ type: 'focus', id: 'L-sk' });
    expect(treeKey(rows, 'L-u7', 'End')).toEqual({ type: 'focus', id: 'L-u7' });
    expect(treeKey(rows, 'L-u2', 'Enter')).toEqual({ type: 'select', id: 'L-u2' });
    expect(treeKey([], null, 'ArrowDown')).toBeNull();
  });
  it('builds create requests and parent options', () => {
    expect(buildCreateRequest({ name: ' ', code: '', kind: 'container', parent: null })).toBe('Name is required');
    expect(buildCreateRequest({ name: ' Tools ', code: ' B-030 ', kind: 'container', parent: 'L-u2' })).toEqual({
      name: 'Tools',
      code: 'B-030',
      kind: 'container',
      parent: 'L-u2'
    });
    expect(suggestChildKind('site')).toBe('unit');
    expect(suggestChildKind(null)).toBe('site');
    expect(suggestChildKind('unit')).toBe('container');
    expect(parentOptions(roots)).toHaveLength(16);
  });
});

describe('selection', () => {
  const order = ['a', 'b', 'c', 'd', 'e'];
  it('toggles', () => {
    expect(toggleId(['a'], 'b')).toEqual(['a', 'b']);
    expect(toggleId(['a', 'b'], 'a')).toEqual(['b']);
  });
  it('selects and clears ranges', () => {
    expect(rangeSelect(order, ['b'], 'b', 'd')).toEqual(['b', 'c', 'd']);
    expect(rangeSelect(order, ['e'], 'd', 'b')).toEqual(['e', 'b', 'c', 'd']);
    expect(rangeSelect(order, ['a', 'b', 'c', 'd'], 'a', 'c')).toEqual(['d']);
    expect(rangeSelect(order, [], null, 'c')).toEqual(['c']);
  });
});
