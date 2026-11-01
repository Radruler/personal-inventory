import { describe, expect, it } from 'vitest';
import { ApiError, itemQuery, unwrap } from './client';

describe('itemQuery', () => {
  it('repeats arrays and prefixes attrs', () => {
    const q = itemQuery({ q: 'navy', kind: ['clothing', 'object'], attrs: { color: ['navy'] }, limit: 60 });
    expect(q.toString()).toBe('q=navy&kind=clothing&kind=object&limit=60&attr.color=navy');
  });
  it('skips empty values', () => {
    expect(itemQuery({ q: '', location: undefined }).toString()).toBe('');
  });
});

describe('unwrap', () => {
  it('returns data on success', () => {
    expect(unwrap({ data: 1, response: new Response(null, { status: 200 }) })).toBe(1);
  });
  it('throws ApiError with the server code', () => {
    const res = { error: { error: { code: 'not_found', message: 'no such item' } }, response: new Response(null, { status: 404 }) };
    expect(() => unwrap(res)).toThrowError(ApiError);
    try {
      unwrap(res);
    } catch (e) {
      expect((e as ApiError).code).toBe('not_found');
      expect((e as ApiError).status).toBe(404);
    }
  });
});
