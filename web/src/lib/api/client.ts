// Typed API client generated from api/openapi.yaml (`make gen` rewrites
// schema.d.ts). Use `api.GET('/api/items', { params: { query } })` etc. and
// `unwrap()` to get data or throw an ApiError.
import createClient, { type Middleware } from 'openapi-fetch';
import type { components, paths } from './schema';
import { auth } from './auth.svelte';

export type Schemas = components['schemas'];
export type ItemSummary = Schemas['ItemSummary'];
export type ItemDetail = Schemas['ItemDetail'];
export type ItemList = Schemas['ItemList'];
export type Placement = Schemas['Placement'];
export type PlacementStatus = Schemas['PlacementStatus'];
export type LocationRef = Schemas['LocationRef'];
export type LocationNode = Schemas['LocationNode'];
export type LocationDetail = Schemas['LocationDetail'];
export type MediaRef = Schemas['MediaRef'];
export type Media = Schemas['Media'];
export type Fact = Schemas['Fact'];
export type Observation = Schemas['Observation'];
export type Identifier = Schemas['Identifier'];
export type Facets = Schemas['Facets'];
export type SchemaInfo = Schemas['SchemaInfo'];
export type Kind = Schemas['Kind'];

const authMiddleware: Middleware = {
  onRequest({ request }) {
    if (auth.token) request.headers.set('Authorization', `Bearer ${auth.token}`);
    return request;
  },
  onResponse({ response }) {
    if (response.status === 401) auth.needed = true;
    return response;
  }
};

export const api = createClient<paths>({ baseUrl: '' });
api.use(authMiddleware);

export type ApiErrorBody = Schemas['ErrorResponse']['error'];

export class ApiError extends Error {
  status: number;
  code: string;
  details: unknown;
  constructor(status: number, body?: Partial<ApiErrorBody>) {
    super(body?.message ?? `HTTP ${status}`);
    this.status = status;
    this.code = body?.code ?? 'internal';
    this.details = body?.details;
  }
}

/** Returns data or throws ApiError. */
export function unwrap<T>(res: { data?: T; error?: unknown; response: Response }): T {
  if (res.error !== undefined || !res.response.ok) {
    const body = (res.error as { error?: Partial<ApiErrorBody> } | undefined)?.error;
    throw new ApiError(res.response.status, body);
  }
  return res.data as T;
}

/**
 * Builds a query string for list/facet endpoints. Arrays repeat the key;
 * `attrs` become literal `attr.<key>` parameters (see openapi Attr param).
 */
export function itemQuery(f: {
  q?: string;
  kind?: string[];
  category?: string[];
  location?: string;
  status?: string[];
  tag?: string[];
  review?: string;
  lifecycle?: string;
  product?: string;
  sort?: string;
  cursor?: string;
  limit?: number;
  attrs?: Record<string, string[]>;
}): URLSearchParams {
  const p = new URLSearchParams();
  const add = (k: string, v: string | number | undefined | null) => {
    if (v !== undefined && v !== null && v !== '') p.append(k, String(v));
  };
  add('q', f.q);
  f.kind?.forEach((v) => add('kind', v));
  f.category?.forEach((v) => add('category', v));
  add('location', f.location);
  f.status?.forEach((v) => add('status', v));
  f.tag?.forEach((v) => add('tag', v));
  add('review', f.review);
  add('lifecycle', f.lifecycle);
  add('product', f.product);
  add('sort', f.sort);
  add('cursor', f.cursor);
  add('limit', f.limit);
  for (const [k, vs] of Object.entries(f.attrs ?? {})) vs.forEach((v) => add(`attr.${k}`, v));
  return p;
}

/** GET with a raw query string (for attr.<key> params openapi-fetch can't type). */
export async function getJSON<T>(path: string, query?: URLSearchParams): Promise<T> {
  const url = query && [...query].length ? `${path}?${query}` : path;
  const res = await fetch(url, { headers: auth.token ? { Authorization: `Bearer ${auth.token}` } : {} });
  const body = res.headers.get('content-type')?.includes('json') ? await res.json() : undefined;
  if (!res.ok) {
    if (res.status === 401) auth.needed = true;
    throw new ApiError(res.status, body?.error);
  }
  return body as T;
}

export type UploadOptions = {
  context?: string;
  link?: { subject_type: 'item' | 'product' | 'location'; subject_id: string; role?: string };
  onProgress?: (fraction: number) => void;
  signal?: AbortSignal;
};

/**
 * Uploads one file to POST /api/media with progress (XHR, since fetch has no
 * upload progress). Resolves with the MediaUploadResult.
 */
export function uploadMedia(file: File, opts: UploadOptions = {}): Promise<Schemas['MediaUploadResult']> {
  return new Promise((resolve, reject) => {
    const form = new FormData();
    form.append('file', file, file.name);
    if (opts.context) form.append('context', opts.context);
    if (opts.link) {
      form.append('link_subject_type', opts.link.subject_type);
      form.append('link_subject_id', opts.link.subject_id);
      if (opts.link.role) form.append('link_role', opts.link.role);
    }
    const xhr = new XMLHttpRequest();
    xhr.open('POST', '/api/media');
    if (auth.token) xhr.setRequestHeader('Authorization', `Bearer ${auth.token}`);
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable) opts.onProgress?.(e.loaded / e.total);
    };
    xhr.onload = () => {
      let body: unknown;
      try {
        body = JSON.parse(xhr.responseText);
      } catch {
        body = undefined;
      }
      if (xhr.status >= 200 && xhr.status < 300) resolve(body as Schemas['MediaUploadResult']);
      else {
        if (xhr.status === 401) auth.needed = true;
        reject(new ApiError(xhr.status, (body as { error?: ApiErrorBody } | undefined)?.error));
      }
    };
    xhr.onerror = () => reject(new ApiError(0, { code: 'internal', message: 'network error' }));
    opts.signal?.addEventListener('abort', () => xhr.abort());
    xhr.send(form);
  });
}
