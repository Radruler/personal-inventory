// Item detail: value formatting, provenance resolution, identifier grouping
// and movement sentences. Pure; unit-tested in detail.test.ts.
import type { Fact, Identifier, ItemDetail, Observation, Schemas } from '$lib/api/client';

export type ValueType = NonNullable<Schemas['FactKeyInput']['value_type']>;
/** A fact_key registry entry (spelled out: the generated allOf type is awkward to use). */
export type FactKey = {
  key: string;
  label: string;
  value_type: ValueType;
  kinds: Schemas['Kind'][];
  filterable: boolean;
  sort: number;
  description?: string | null;
};
/** The parts of GET /api/schema the detail view uses. */
export type SchemaLite = {
  fact_keys: FactKey[];
  media_roles: { value: string; label: string; sort: number; cover_rank?: number | null }[];
  observation_sources: { value: string; label: string }[];
  identifier_schemes: { scheme: string; label: string; description?: string | null }[];
  gone_reasons: string[];
};
export type Reference = Schemas['Reference'];
export type Movement = Schemas['Movement'];

// ---------------------------------------------------------------- dates

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];

/** ISO timestamp → "2 Nov 2026" (local date); '' when unparseable. */
export function formatDay(iso: string | null | undefined): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  return `${d.getDate()} ${MONTHS[d.getMonth()]} ${d.getFullYear()}`;
}

/** ISO timestamp → "2 Nov 2026, 14:05" (local). */
export function formatDateTime(iso: string | null | undefined): string {
  const day = formatDay(iso);
  if (!day) return '';
  const d = new Date(iso!);
  return `${day}, ${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`;
}

/** Partial dates as stored in facts: "2024" · "Mar 2024" · "2 Mar 2024". */
export function formatPartialDate(v: string): string {
  const m = /^(\d{4})(?:-(\d{2})(?:-(\d{2}))?)?$/.exec(v.trim());
  if (!m) return v;
  const [, y, mo, d] = m;
  if (!mo) return y;
  const month = MONTHS[Number(mo) - 1];
  if (!month) return v;
  return d ? `${Number(d)} ${month} ${y}` : `${month} ${y}`;
}

// ---------------------------------------------------------------- values

export type Money = { amount: number; currency?: string };
export type CompositionPart = { part?: string; materials: { name: string; pct?: number | null }[] };

function isMoney(v: unknown): v is Money {
  return !!v && typeof v === 'object' && typeof (v as Money).amount === 'number';
}

export function formatMoney(v: Money): string {
  const cur = (v.currency ?? '').toUpperCase();
  if (/^[A-Z]{3}$/.test(cur)) {
    try {
      return new Intl.NumberFormat('en-US', { style: 'currency', currency: cur }).format(v.amount);
    } catch {
      /* unknown currency code: fall through */
    }
  }
  const n = Number.isInteger(v.amount) ? String(v.amount) : v.amount.toFixed(2);
  return cur ? `${n} ${cur}` : n;
}

function isComposition(v: unknown): v is CompositionPart[] {
  return Array.isArray(v) && v.every((p) => p && typeof p === 'object' && Array.isArray((p as CompositionPart).materials));
}

function cap(s: string): string {
  return s ? s[0].toUpperCase() + s.slice(1) : s;
}

/** "Shell: 100% nylon · Lining: 100% polyester". */
export function formatComposition(parts: CompositionPart[]): string {
  return parts
    .map((p) => {
      const mats = p.materials.map((m) => (m.pct !== undefined && m.pct !== null ? `${m.pct}% ${m.name}` : m.name)).join(', ');
      return p.part ? `${cap(p.part)}: ${mats}` : mats;
    })
    .join(' · ');
}

/** Display text for a fact/attr value, guided by the registry value_type. */
export function formatValue(value: unknown, type?: ValueType | null): string {
  if (value === null || value === undefined) return '—';
  if (type === 'money' || isMoney(value)) {
    if (isMoney(value)) return formatMoney(value);
  }
  if (type === 'composition' || isComposition(value)) {
    if (isComposition(value)) return formatComposition(value);
  }
  if (type === 'date' && typeof value === 'string') return formatPartialDate(value);
  if (typeof value === 'boolean') return value ? 'Yes' : 'No';
  if (typeof value === 'number') return String(value);
  if (typeof value === 'string') return value;
  if (Array.isArray(value) && value.every((x) => typeof x !== 'object' || x === null)) return value.map(String).join(', ');
  return JSON.stringify(value);
}

/** The value as an editable string for the correction form. */
export function valueToInput(value: unknown, type?: ValueType | null): string {
  if (value === null || value === undefined) return '';
  if (type === 'composition' && isComposition(value)) {
    return value
      .map((p) => {
        const mats = p.materials.map((m) => (m.pct !== undefined && m.pct !== null ? `${m.pct}% ${m.name}` : m.name)).join(', ');
        return p.part ? `${p.part}: ${mats}` : mats;
      })
      .join('; ');
  }
  if (typeof value === 'string') return value;
  if (typeof value === 'number' || typeof value === 'boolean') return String(value);
  return JSON.stringify(value);
}

export type Parsed = { ok: true; value: unknown } | { ok: false; error: string };

/**
 * Parses what the user typed into a JSON value for the key's value_type.
 *   number: "12.5" · boolean: yes/no/true/false · date: YYYY[-MM[-DD]]
 *   money: amount + currency (separate fields; currency may be empty)
 *   composition: JSON, or "shell: 100% nylon; lining: 80% cotton, 20% polyester"
 *   json: any JSON · string (default): trimmed text
 */
export function parseValueInput(type: ValueType | null | undefined, raw: string, currency = ''): Parsed {
  const s = raw.trim();
  if (!s) return { ok: false, error: 'Enter a value.' };
  switch (type) {
    case 'number': {
      const n = Number(s.replace(/,/g, ''));
      return Number.isFinite(n) ? { ok: true, value: n } : { ok: false, error: 'Enter a number.' };
    }
    case 'boolean': {
      const t = s.toLowerCase();
      if (['yes', 'true', 'y', '1'].includes(t)) return { ok: true, value: true };
      if (['no', 'false', 'n', '0'].includes(t)) return { ok: true, value: false };
      return { ok: false, error: 'Enter yes or no.' };
    }
    case 'date':
      return /^\d{4}(-(0[1-9]|1[0-2])(-(0[1-9]|[12]\d|3[01]))?)?$/.test(s)
        ? { ok: true, value: s }
        : { ok: false, error: 'Use YYYY, YYYY-MM or YYYY-MM-DD.' };
    case 'money': {
      const n = Number(s.replace(/[,$€£¥\s]/g, ''));
      if (!Number.isFinite(n)) return { ok: false, error: 'Enter an amount.' };
      const cur = currency.trim().toUpperCase();
      if (cur && !/^[A-Z]{3}$/.test(cur)) return { ok: false, error: 'Currency is a 3-letter code, e.g. USD.' };
      return { ok: true, value: cur ? { amount: n, currency: cur } : { amount: n } };
    }
    case 'composition':
      return parseComposition(s);
    case 'json':
      try {
        return { ok: true, value: JSON.parse(s) };
      } catch {
        return { ok: false, error: 'Not valid JSON.' };
      }
    default:
      return { ok: true, value: s };
  }
}

/** Composition from JSON or "part: 80% cotton, 20% polyester; lining: …". */
export function parseComposition(s: string): Parsed {
  if (s.startsWith('[')) {
    try {
      const v = JSON.parse(s);
      return isComposition(v) ? { ok: true, value: v } : { ok: false, error: 'JSON must be [{part, materials:[{name, pct}]}].' };
    } catch {
      return { ok: false, error: 'Not valid JSON.' };
    }
  }
  const parts: CompositionPart[] = [];
  for (const chunk of s.split(/[;\n]/)) {
    const c = chunk.trim();
    if (!c) continue;
    const colon = c.indexOf(':');
    const part = colon >= 0 ? c.slice(0, colon).trim().toLowerCase() : '';
    const list = colon >= 0 ? c.slice(colon + 1) : c;
    const materials = list
      .split(',')
      .map((m) => m.trim())
      .filter(Boolean)
      .map((m) => {
        const mm = /^(\d+(?:\.\d+)?)\s*%\s*(.+)$/.exec(m);
        return mm ? { name: mm[2].trim().toLowerCase(), pct: Number(mm[1]) } : { name: m.toLowerCase() };
      });
    if (!materials.length) return { ok: false, error: `No materials in “${c}”.` };
    parts.push(part ? { part, materials } : { materials });
  }
  return parts.length ? { ok: true, value: parts } : { ok: false, error: 'Enter a value.' };
}

// ---------------------------------------------------------------- keys

/** "shell.technology" → "Shell technology" (fallback when not in the registry). */
export function humanizeKey(key: string): string {
  return cap(key.replace(/[._-]+/g, ' ').trim());
}

export function keyLabel(keys: Map<string, FactKey>, key: string): string {
  return keys.get(key)?.label ?? humanizeKey(key);
}

export const PURCHASE_KEYS: readonly string[] = ['purchase_date', 'purchase_price', 'purchase_place'];

export type AttrRow = {
  key: string;
  label: string;
  value: unknown;
  text: string;
  type: ValueType | null;
  /** The winning fact, when it can be found. */
  fact: Fact | null;
  /** The value fills in from the product. */
  fromProduct: boolean;
};

/**
 * Projected attrs as display rows, ordered by the registry sort, then key.
 * `only` keeps just those keys; `except` drops them (e.g. purchase keys).
 */
export function attrRows(
  item: Pick<ItemDetail, 'attrs' | 'attr_sources' | 'facts' | 'product'>,
  keys: Map<string, FactKey>,
  opt: { only?: readonly string[]; except?: readonly string[] } = {}
): AttrRow[] {
  const facts = allFacts(item);
  const byId = new Map(facts.map((f) => [f.id, f]));
  const rows: AttrRow[] = [];
  for (const [key, value] of Object.entries(item.attrs ?? {})) {
    if (opt.only && !opt.only.includes(key)) continue;
    if (opt.except?.includes(key)) continue;
    const src = item.attr_sources?.[key];
    const fact = src ? (byId.get(src.fact_id) ?? null) : null;
    const type = keys.get(key)?.value_type ?? null;
    rows.push({
      key,
      label: keyLabel(keys, key),
      value,
      text: formatValue(value, type),
      type,
      fact,
      fromProduct: src?.subject_type === 'product'
    });
  }
  const sortOf = (k: string) => keys.get(k)?.sort ?? 1000;
  return rows.sort((a, b) => sortOf(a.key) - sortOf(b.key) || a.key.localeCompare(b.key));
}

/** Item facts plus the product's facts. */
export function allFacts(item: Pick<ItemDetail, 'facts' | 'product'>): Fact[] {
  return [...(item.facts ?? []), ...(item.product?.facts ?? [])];
}

const STATUS_ORDER: Record<string, number> = { active: 0, superseded: 1, rejected: 2 };

/**
 * Every fact for a key (item and product), the winner first, then the other
 * active ones, then superseded and rejected, newest first within each.
 */
export function factsForKey(item: Pick<ItemDetail, 'facts' | 'product'>, key: string, winnerId?: string | null): Fact[] {
  return allFacts(item)
    .filter((f) => f.key === key)
    .sort((a, b) => {
      if (a.id === winnerId) return -1;
      if (b.id === winnerId) return 1;
      return (STATUS_ORDER[a.status] ?? 3) - (STATUS_ORDER[b.status] ?? 3) || b.recorded_at.localeCompare(a.recorded_at);
    });
}

// ---------------------------------------------------------------- provenance sources

export type ResolvedSource =
  | { type: 'observation'; observation: Observation | null; id: string }
  | { type: 'reference'; reference: Reference | null; id: string }
  | { type: 'media'; media: Schemas['MediaRef'] | null; id: string }
  | { type: 'url'; url: string }
  | { type: 'note'; note: string };

/** Resolves a fact's sources against the item (and its product). */
export function resolveSources(
  fact: Pick<Fact, 'sources'>,
  item: Pick<ItemDetail, 'observations' | 'references' | 'media' | 'product'>
): ResolvedSource[] {
  const obs = new Map((item.observations ?? []).map((o) => [o.id, o]));
  const refs = new Map([...(item.references ?? []), ...(item.product?.references ?? [])].map((r) => [r.id, r]));
  const media = new Map<string, Schemas['MediaRef']>();
  for (const l of [...(item.media ?? []), ...(item.product?.media ?? [])]) media.set(l.media.id, l.media);
  for (const o of item.observations ?? []) if (o.media) media.set(o.media.id, o.media);
  const out: ResolvedSource[] = [];
  for (const s of fact.sources ?? []) {
    if (s.observation_id) out.push({ type: 'observation', id: s.observation_id, observation: obs.get(s.observation_id) ?? null });
    if (s.reference_id) out.push({ type: 'reference', id: s.reference_id, reference: refs.get(s.reference_id) ?? null });
    if (s.media_id) out.push({ type: 'media', id: s.media_id, media: media.get(s.media_id) ?? null });
    if (s.url) out.push({ type: 'url', url: s.url });
    if (s.note) out.push({ type: 'note', note: s.note });
  }
  return out;
}

/** "user" → "You"; "agent:claude-opus-5-5" → "Agent (claude-opus-5-5)". */
export function actorLabel(actor: string | null | undefined): string {
  if (!actor || actor === 'user') return 'You';
  if (actor.startsWith('agent:')) return `Agent (${actor.slice(6)})`;
  return actor;
}

// ---------------------------------------------------------------- observations

/** Splits verbatim text so `[?]` uncertainty marks can be highlighted. */
export function splitUncertain(text: string): { text: string; uncertain: boolean }[] {
  const out: { text: string; uncertain: boolean }[] = [];
  for (const part of text.split(/(\[\?\])/)) {
    if (!part) continue;
    out.push({ text: part, uncertain: part === '[?]' });
  }
  return out;
}

export type Region = { x: number; y: number; w: number; h: number };

/**
 * CSS for showing a region of a photo inside a box: the image is scaled up
 * by 1/w and shifted so the region fills the box. `aspect` is the region's
 * width/height in pixels (null when the photo size is unknown).
 */
export function regionStyle(r: Region, width?: number | null, height?: number | null): { img: string; aspect: number | null } {
  const w = Math.max(r.w, 0.01);
  const h = Math.max(r.h, 0.01);
  const img = `width:${(100 / w).toFixed(3)}%;height:${(100 / h).toFixed(3)}%;left:${((-r.x / w) * 100).toFixed(3)}%;top:${((-r.y / h) * 100).toFixed(3)}%`;
  const aspect = width && height ? (w * width) / (h * height) : null;
  return { img, aspect };
}

/** Identifiers grouped by scheme, in first-seen order. */
export function groupIdentifiers(ids: readonly Identifier[]): { scheme: string; identifiers: Identifier[] }[] {
  const by = new Map<string, Identifier[]>();
  for (const i of ids) {
    const list = by.get(i.scheme) ?? [];
    list.push(i);
    by.set(i.scheme, list);
  }
  return [...by].map(([scheme, identifiers]) => ({ scheme, identifiers }));
}

// ---------------------------------------------------------------- movements

/** "Moved to Unit 2 › B-014 · 2 Nov 2026" and friends. */
export function movementSentence(m: Pick<Movement, 'kind' | 'from' | 'to' | 'at'>): { text: string; date: string } {
  const date = formatDay(m.at);
  let text: string;
  switch (m.kind) {
    case 'stage':
      text = m.to ? `Staged to go to ${m.to.label}` : 'Staged';
      if (m.from && m.to) text = `Restaged from ${m.from.label} to ${m.to.label}`;
      break;
    case 'unstage':
      text = m.from ? `No longer going to ${m.from.label}` : 'Unstaged';
      break;
    default:
      if (!m.to) text = m.from ? `Marked location unknown (was ${m.from.label})` : 'Marked location unknown';
      else text = `Moved to ${m.to.label}`;
  }
  return { text, date };
}

// ---------------------------------------------------------------- misc

/** Item-relevant media roles for the upload picker (box/location roles left out). */
export function itemRoles(roles: SchemaLite['media_roles']): { value: string; label: string }[] {
  const skip = new Set(['box_exterior', 'box_contents', 'location']);
  return roles.filter((r) => !skip.has(r.value)).map((r) => ({ value: r.value, label: r.label }));
}

/** Sensible upload role for the gallery tab being looked at. */
export function roleForTab(tab: string | undefined): string {
  switch (tab) {
    case 'on_body':
      return 'on_body';
    case 'stock':
      return 'stock';
    case 'labels':
      return 'label';
    case 'detail':
      return 'detail';
    case 'other':
      return 'other';
    default:
      return 'flat_lay';
  }
}

/** Normalises tag input: trimmed, lower case, inner whitespace collapsed; '' when empty. */
export function normalizeTag(raw: string): string {
  return raw.trim().replace(/\s+/g, ' ').toLowerCase();
}

const BASIS_RANK: Record<string, number> = { user: 0, observed: 1, looked_up: 2, inferred: 3 };

/** The projection rule for one key: best basis, then confidence, then newest active fact. */
export function bestFact(facts: readonly Fact[], key: string): Fact | null {
  let best: Fact | null = null;
  for (const f of facts) {
    if (f.key !== key || f.status !== 'active') continue;
    if (
      !best ||
      (BASIS_RANK[f.basis] ?? 9) < (BASIS_RANK[best.basis] ?? 9) ||
      ((BASIS_RANK[f.basis] ?? 9) === (BASIS_RANK[best.basis] ?? 9) &&
        (f.confidence > best.confidence || (f.confidence === best.confidence && f.recorded_at > best.recorded_at)))
    ) {
      best = f;
    }
  }
  return best;
}

/** Product attrs as rows (winning fact found by the projection rule). */
export function productAttrRows(product: NonNullable<ItemDetail['product']>, keys: Map<string, FactKey>): AttrRow[] {
  return Object.entries(product.attrs ?? {})
    .map(([key, value]) => {
      const type = keys.get(key)?.value_type ?? null;
      return { key, label: keyLabel(keys, key), value, text: formatValue(value, type), type, fact: bestFact(product.facts, key), fromProduct: true };
    })
    .sort((a, b) => (keys.get(a.key)?.sort ?? 1000) - (keys.get(b.key)?.sort ?? 1000) || a.key.localeCompare(b.key));
}
