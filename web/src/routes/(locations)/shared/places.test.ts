import { describe, expect, it } from 'vitest';
import type { ItemSummary, LocationNode, LocationRef, Schemas } from '$lib/api/client';
import {
  archiveBlockers,
  boxesUnder,
  boxParents,
  canMoveInto,
  describeMovement,
  locationParam,
  nextCode,
  relTime,
  undoRequests
} from './places';

function node(id: string, kind: LocationNode['kind'], code: string | null, children: LocationNode[] = [], parent: string | null = null): LocationNode {
  const n: LocationNode = {
    id,
    code,
    name: id,
    kind,
    parent_id: parent,
    cover_media_id: null,
    notes: null,
    layout: null,
    archived_at: null,
    created_at: '',
    updated_at: '',
    label: id,
    cover: null,
    item_count: 0,
    total_count: 0,
    staged_count: 0,
    children
  };
  children.forEach((c) => (c.parent_id = id));
  return n;
}

const roots = [
  node('home', 'site', 'HOME', [node('bed', 'room', 'BEDROOM'), node('gar', 'room', 'GARAGE', [node('b20', 'container', 'B-020')])]),
  node('ss', 'site', 'SS', [
    node('u1', 'unit', 'U1', [node('b2', 'container', 'B-002')]),
    node('u2', 'unit', 'U2', [node('b1', 'container', 'B-001'), node('b3', 'container', 'B-003'), node('sh', 'shelf', 'SH-1', [node('b9', 'container', 'B-009')])])
  ])
];

describe('tree helpers', () => {
  it('boxesUnder lists containers at any depth', () => {
    expect(boxesUnder(roots, 'u2').map((n) => n.id)).toEqual(['b1', 'b3', 'b9']);
    expect(boxesUnder(roots, 'nope')).toEqual([]);
  });
  it('boxParents lists the target and its non-box descendants', () => {
    expect(boxParents(roots, 'u2').map((n) => n.id)).toEqual(['u2', 'sh']);
  });
  it('canMoveInto rejects cycles, big-into-small, and the current parent', () => {
    const u2 = roots[1].children[1];
    const b1 = u2.children[0];
    const u1 = roots[1].children[0];
    expect(canMoveInto(b1, u1, roots)).toBe(true);
    expect(canMoveInto(b1, u2, roots)).toBe(false); // already there
    expect(canMoveInto(u2, u2.children[2], roots)).toBe(false); // own descendant
    expect(canMoveInto(u2, roots[0].children[0], roots)).toBe(true); // unit into a room is allowed
    expect(canMoveInto(u1, b1, roots)).toBe(false); // unit into a box
  });
  it('nextCode continues the main box series', () => {
    expect(nextCode(roots)).toBe('B-021');
    expect(nextCode(roots, 'SH-')).toBe('SH-2');
    expect(nextCode([])).toBe('B-001');
  });
});

describe('archiveBlockers', () => {
  it('reads the 409 details', () => {
    expect(archiveBlockers({ items: 3, children: 4, staged: 6 })).toBe('3 items, 4 locations and 6 items staged to go here');
    expect(archiveBlockers({ items: 1 })).toBe('1 item');
    expect(archiveBlockers(null)).toBe('');
  });
});

function ref(id: string): LocationRef {
  return { id, code: id.toUpperCase(), name: id, kind: 'room', path: [{ id, code: null, name: id, kind: 'room' }], label: id, archived: false };
}
function item(id: string, loc: string | null, target: string | null): ItemSummary {
  return {
    id,
    kind: 'clothing',
    title: id,
    category: null,
    quantity: 1,
    lifecycle: 'owned',
    review_state: 'ok',
    cover: null,
    placement: { status: target ? 'staged' : loc ? 'placed' : 'unknown', label: '', location: loc ? ref(loc) : null, target: target ? ref(target) : null, gone_reason: null, gone_at: null },
    attrs: {},
    tags: [],
    product_id: null,
    ext_key: null,
    created_at: '',
    updated_at: ''
  };
}

describe('undoRequests', () => {
  it('after packing: move back per location, then re-stage', () => {
    const prev = [item('a', 'bed', 'u2'), item('b', 'bed', 'u2'), item('c', null, 'u2')];
    expect(undoRequests(prev, 'move')).toEqual([
      { action: 'move', item_ids: ['a', 'b'], to: 'bed', note: 'undo' },
      { action: 'move', item_ids: ['c'], to: null, note: 'undo' },
      { action: 'stage', item_ids: ['a', 'b', 'c'], to: 'u2', note: 'undo' }
    ]);
  });
  it('after staging: restore old targets, unstage the rest', () => {
    expect(undoRequests([item('a', 'bed', null), item('b', 'bed', 'u1')], 'stage')).toEqual([
      { action: 'unstage', item_ids: ['a'], note: 'undo' },
      { action: 'stage', item_ids: ['b'], to: 'u1', note: 'undo' }
    ]);
  });
  it('after unstaging: re-stage', () => {
    expect(undoRequests([item('a', 'bed', 'u2')], 'unstage')).toEqual([{ action: 'stage', item_ids: ['a'], to: 'u2', note: 'undo' }]);
  });
});

describe('describeMovement', () => {
  const mv = (o: Partial<Schemas['Movement']>): Schemas['Movement'] => ({
    id: 'm',
    item_id: null,
    location_moved_id: null,
    kind: 'move',
    from: null,
    to: null,
    at: '',
    note: null,
    actor: 'user',
    ...o
  });
  it('box moves and item moves', () => {
    expect(describeMovement(mv({ location_moved_id: 'here', from: ref('u1'), to: ref('u2') }), 'here', new Map())).toBe('This location moved from u1 to u2');
    const toHere = { ...ref('here') };
    expect(describeMovement(mv({ item_id: 'i', from: ref('bed'), to: toHere }), 'here', new Map([['i', 'Scarf']]))).toBe('Scarf moved in from bed');
    expect(describeMovement(mv({ item_id: 'i', from: toHere, to: ref('bed') }), 'here', new Map())).toBe('An item moved out to bed');
    expect(describeMovement(mv({ item_id: 'i', kind: 'stage', to: ref('u2') }), 'here', new Map())).toBe('An item staged to go to u2');
  });
});

describe('misc', () => {
  it('locationParam', () => {
    expect(locationParam('code:B-014')).toBe('code:B-014');
    expect(locationParam('01m6ce32r5ffc734x22qqa1pb8')).toBe('01M6CE32R5FFC734X22QQA1PB8');
    expect(locationParam('B-014')).toBe('code:B-014');
  });
  it('relTime', () => {
    const now = Date.parse('2026-11-01T12:00:00Z');
    expect(relTime('2026-11-01T11:59:50Z', now)).toBe('just now');
    expect(relTime('2026-11-01T11:00:00Z', now)).toBe('1 h ago');
    expect(relTime('2026-10-30T12:00:00Z', now)).toBe('2 d ago');
  });
});
