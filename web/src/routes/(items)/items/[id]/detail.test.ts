import { describe, expect, it } from 'vitest';
import type { Fact, ItemDetail } from '$lib/api/client';
import {
  actorLabel,
  attrRows,
  bestFact,
  factsForKey,
  formatDay,
  formatPartialDate,
  formatValue,
  groupIdentifiers,
  humanizeKey,
  movementSentence,
  normalizeTag,
  parseComposition,
  parseValueInput,
  PURCHASE_KEYS,
  regionStyle,
  resolveSources,
  roleForTab,
  splitUncertain,
  valueToInput,
  type FactKey
} from './detail';

const fact = (o: Partial<Fact>): Fact => ({
  id: 'f',
  subject_type: 'item',
  subject_id: 'i',
  key: 'color',
  value: 'navy',
  basis: 'user',
  confidence: 1,
  sources: [],
  status: 'active',
  supersedes_id: null,
  note: null,
  recorded_by: 'user',
  recorded_at: '2026-09-01T12:00:00.000Z',
  ext_key: null,
  ...o
});

const keys = new Map<string, FactKey>([
  ['brand', { key: 'brand', label: 'Brand', value_type: 'string', kinds: [], filterable: true, sort: 1 }],
  ['color', { key: 'color', label: 'Color', value_type: 'string', kinds: [], filterable: true, sort: 2 }],
  ['composition', { key: 'composition', label: 'Composition', value_type: 'composition', kinds: ['clothing'], filterable: false, sort: 5 }],
  ['purchase_price', { key: 'purchase_price', label: 'Purchase price', value_type: 'money', kinds: [], filterable: false, sort: 21 }],
  ['purchase_date', { key: 'purchase_date', label: 'Purchase date', value_type: 'date', kinds: [], filterable: false, sort: 20 }]
]);

describe('formatValue', () => {
  it('formats money with a currency code', () => {
    expect(formatValue({ amount: 79.9, currency: 'USD' }, 'money')).toBe('$79.90');
    expect(formatValue({ amount: 5000, currency: 'JPY' }, 'money')).toBe('¥5,000');
    expect(formatValue({ amount: 12 }, 'money')).toBe('12');
    expect(formatValue({ amount: 12.5, currency: 'XX' }, 'money')).toBe('12.50 XX');
  });

  it('formats composition', () => {
    const v = [
      { part: 'shell', materials: [{ name: 'nylon', pct: 100 }] },
      { part: 'lining', materials: [{ name: 'cotton', pct: 80 }, { name: 'polyester', pct: 20 }] }
    ];
    expect(formatValue(v, 'composition')).toBe('Shell: 100% nylon · Lining: 80% cotton, 20% polyester');
    expect(formatValue([{ materials: [{ name: 'wool' }] }])).toBe('wool');
  });

  it('formats partial dates, booleans, arrays, objects, null', () => {
    expect(formatValue('2024', 'date')).toBe('2024');
    expect(formatValue('2024-03', 'date')).toBe('Mar 2024');
    expect(formatValue('2024-03-02', 'date')).toBe('2 Mar 2024');
    expect(formatPartialDate('soon')).toBe('soon');
    expect(formatValue(true)).toBe('Yes');
    expect(formatValue(['a', 'b'])).toBe('a, b');
    expect(formatValue({ x: 1 })).toBe('{"x":1}');
    expect(formatValue(null)).toBe('—');
    expect(formatValue(3.5, 'number')).toBe('3.5');
  });
});

describe('parseValueInput', () => {
  it('parses by value type', () => {
    expect(parseValueInput('string', '  navy ')).toEqual({ ok: true, value: 'navy' });
    expect(parseValueInput('number', '1,200.5')).toEqual({ ok: true, value: 1200.5 });
    expect(parseValueInput('number', 'abc').ok).toBe(false);
    expect(parseValueInput('boolean', 'Yes')).toEqual({ ok: true, value: true });
    expect(parseValueInput('boolean', 'maybe').ok).toBe(false);
    expect(parseValueInput('date', '2024-13').ok).toBe(false);
    expect(parseValueInput('date', '2024-12')).toEqual({ ok: true, value: '2024-12' });
    expect(parseValueInput('money', '$1,299.00', 'usd')).toEqual({ ok: true, value: { amount: 1299, currency: 'USD' } });
    expect(parseValueInput('money', '10', '')).toEqual({ ok: true, value: { amount: 10 } });
    expect(parseValueInput('money', '10', 'dollars').ok).toBe(false);
    expect(parseValueInput('json', '{"a":1}')).toEqual({ ok: true, value: { a: 1 } });
    expect(parseValueInput('json', '{').ok).toBe(false);
    expect(parseValueInput(null, '').ok).toBe(false);
  });

  it('parses composition text and JSON, and round-trips with valueToInput', () => {
    const r = parseComposition('Shell: 100% Nylon; lining: 80% cotton, 20% polyester');
    expect(r).toEqual({
      ok: true,
      value: [
        { part: 'shell', materials: [{ name: 'nylon', pct: 100 }] },
        { part: 'lining', materials: [{ name: 'cotton', pct: 80 }, { name: 'polyester', pct: 20 }] }
      ]
    });
    if (!r.ok) throw new Error();
    expect(parseComposition(valueToInput(r.value, 'composition'))).toEqual(r);
    expect(parseComposition('[{"materials":[{"name":"wool","pct":100}]}]').ok).toBe(true);
    expect(parseComposition('[{"x":1}]').ok).toBe(false);
    expect(parseComposition('wool')).toEqual({ ok: true, value: [{ materials: [{ name: 'wool' }] }] });
  });

  it('valueToInput gives editable text', () => {
    expect(valueToInput('navy')).toBe('navy');
    expect(valueToInput(3)).toBe('3');
    expect(valueToInput({ amount: 1 })).toBe('{"amount":1}');
    expect(valueToInput(null)).toBe('');
  });
});

describe('attr rows and provenance', () => {
  const productFact = fact({ id: 'pf', subject_type: 'product', subject_id: 'p', key: 'retail_price', value: { amount: 80, currency: 'USD' }, basis: 'looked_up' });
  const item = {
    attrs: { color: 'navy', brand: 'Uniqlo', retail_price: { amount: 80, currency: 'USD' }, purchase_price: { amount: 120, currency: 'USD' }, 'shell.technology': 'GORE-TEX' },
    attr_sources: {
      color: { fact_id: 'c2', subject_type: 'item' as const },
      brand: { fact_id: 'b1', subject_type: 'item' as const },
      retail_price: { fact_id: 'pf', subject_type: 'product' as const },
      purchase_price: { fact_id: 'pp', subject_type: 'item' as const }
    },
    facts: [
      fact({ id: 'c2', key: 'color', value: 'navy', recorded_at: '2026-10-01T00:00:00.000Z' }),
      fact({ id: 'c1', key: 'color', value: 'black', status: 'superseded', recorded_at: '2026-09-01T00:00:00.000Z' }),
      fact({ id: 'c0', key: 'color', value: 'blue', basis: 'inferred', confidence: 0.4, recorded_at: '2026-08-01T00:00:00.000Z' }),
      fact({ id: 'b1', key: 'brand', value: 'Uniqlo', basis: 'observed', sources: [{ observation_id: 'o1' }, { note: 'neck label' }] }),
      fact({ id: 'pp', key: 'purchase_price', value: { amount: 120, currency: 'USD' } })
    ],
    product: { facts: [productFact], references: [{ id: 'r1', url: 'https://x', title: 'Page' }], media: [] },
    observations: [{ id: 'o1', source: 'garment_label', text: 'UNIQLO', media: null }],
    references: [],
    media: []
  } as unknown as ItemDetail;

  it('orders rows by registry sort, splits out purchase keys, marks product values', () => {
    const rows = attrRows(item, keys, { except: PURCHASE_KEYS });
    expect(rows.map((r) => r.key)).toEqual(['brand', 'color', 'retail_price', 'shell.technology']);
    expect(rows.find((r) => r.key === 'retail_price')).toMatchObject({ fromProduct: true, text: '$80.00', label: 'Retail price' });
    expect(rows.find((r) => r.key === 'shell.technology')).toMatchObject({ fact: null, label: 'Shell technology' });
    expect(rows[0].fact?.id).toBe('b1');
    expect(attrRows(item, keys, { only: PURCHASE_KEYS }).map((r) => r.text)).toEqual(['$120.00']);
  });

  it('lists the winner first, then active, then superseded/rejected', () => {
    expect(factsForKey(item, 'color', 'c2').map((f) => f.id)).toEqual(['c2', 'c0', 'c1']);
    expect(factsForKey(item, 'retail_price', 'pf').map((f) => f.id)).toEqual(['pf']);
  });

  it('resolves sources to observations, references, urls and notes', () => {
    const s = resolveSources(fact({ sources: [{ observation_id: 'o1' }, { reference_id: 'r1' }, { url: 'https://y' }, { note: 'n' }, { observation_id: 'gone' }] }), item);
    expect(s.map((x) => x.type)).toEqual(['observation', 'reference', 'url', 'note', 'observation']);
    expect(s[0]).toMatchObject({ observation: { text: 'UNIQLO' } });
    expect(s[1]).toMatchObject({ reference: { title: 'Page' } });
    expect(s[4]).toMatchObject({ observation: null, id: 'gone' });
  });

  it('bestFact follows the projection rule', () => {
    const fs = [
      fact({ id: 'a', basis: 'inferred', confidence: 0.9 }),
      fact({ id: 'b', basis: 'looked_up', confidence: 0.5 }),
      fact({ id: 'c', basis: 'looked_up', confidence: 0.7 }),
      fact({ id: 'd', basis: 'looked_up', confidence: 0.7, recorded_at: '2026-12-01T00:00:00.000Z' }),
      fact({ id: 'e', basis: 'user', status: 'rejected' })
    ];
    expect(bestFact(fs, 'color')?.id).toBe('d');
    expect(bestFact(fs, 'size')).toBeNull();
  });
});

describe('observations and identifiers', () => {
  it('splits [?] marks', () => {
    expect(splitUncertain('RN 1234[?]\nMADE IN [?]')).toEqual([
      { text: 'RN 1234', uncertain: false },
      { text: '[?]', uncertain: true },
      { text: '\nMADE IN ', uncertain: false },
      { text: '[?]', uncertain: true }
    ]);
    expect(splitUncertain('plain')).toEqual([{ text: 'plain', uncertain: false }]);
  });

  it('crops a region: scale by 1/w, shift by -x/w, aspect from pixels', () => {
    const r = regionStyle({ x: 0.25, y: 0.5, w: 0.5, h: 0.25 }, 900, 1200);
    expect(r.img).toBe('width:200.000%;height:400.000%;left:-50.000%;top:-200.000%');
    expect(r.aspect).toBeCloseTo((0.5 * 900) / (0.25 * 1200));
    expect(regionStyle({ x: 0, y: 0, w: 1, h: 1 }).aspect).toBeNull();
  });

  it('groups identifiers by scheme in first-seen order', () => {
    const id = (scheme: string, value: string) => ({ id: value, scheme, value }) as never;
    const g = groupIdentifiers([id('rn', '1'), id('upc', '2'), id('rn', '3')]);
    expect(g.map((x) => [x.scheme, x.identifiers.length])).toEqual([
      ['rn', 2],
      ['upc', 1]
    ]);
  });
});

describe('movement sentences and labels', () => {
  const loc = (label: string) => ({ label }) as never;
  it('describes moves, stages and unstages', () => {
    expect(movementSentence({ kind: 'move', from: null, to: loc('Unit 2 › B-014'), at: '2026-11-02T12:00:00.000Z' })).toEqual({
      text: 'Moved to Unit 2 › B-014',
      date: '2 Nov 2026'
    });
    expect(movementSentence({ kind: 'move', from: loc('Garage'), to: null, at: '2026-11-02T12:00:00.000Z' }).text).toBe('Marked location unknown (was Garage)');
    expect(movementSentence({ kind: 'stage', from: null, to: loc('Unit 1'), at: '' }).text).toBe('Staged to go to Unit 1');
    expect(movementSentence({ kind: 'stage', from: loc('Unit 1'), to: loc('Unit 2'), at: '' }).text).toBe('Restaged from Unit 1 to Unit 2');
    expect(movementSentence({ kind: 'unstage', from: loc('Unit 1'), to: null, at: '' }).text).toBe('No longer going to Unit 1');
  });

  it('formats days, actors, keys, tags and roles', () => {
    expect(formatDay('2026-03-05T12:00:00.000Z')).toBe('5 Mar 2026');
    expect(formatDay('nope')).toBe('');
    expect(actorLabel('user')).toBe('You');
    expect(actorLabel('agent:claude-opus-5-5')).toBe('Agent (claude-opus-5-5)');
    expect(humanizeKey('shell.technology')).toBe('Shell technology');
    expect(normalizeTag('  Winter   Coats ')).toBe('winter coats');
    expect(roleForTab('labels')).toBe('label');
    expect(roleForTab(undefined)).toBe('flat_lay');
  });
});
