// Pure helpers shared by thread F's views (places, box page, packing, inbox,
// quick add). Unit-tested in places.test.ts.
import type { ItemSummary, LocationNode, Schemas } from '$lib/api/client';
import { indexTree, type LocationKind } from '$lib/components/locationTree';

/** MoveRequest with `clear_target` optional (it has a server default; the generated type marks it required). */
export type MoveRequest = Omit<Schemas['MoveRequest'], 'clear_target'> & { clear_target?: boolean };
type Movement = Schemas['Movement'];

/** Finds a node by id anywhere in the tree. */
export function findNode(roots: readonly LocationNode[], id: string | null | undefined): LocationNode | null {
  if (!id) return null;
  return indexTree(roots).get(id)?.node ?? null;
}

/** Ids of a node and everything below it. */
export function subtreeIds(node: LocationNode): Set<string> {
  const out = new Set<string>();
  const walk = (n: LocationNode) => {
    out.add(n.id);
    n.children.forEach(walk);
  };
  walk(node);
  return out;
}

/** Boxes (containers) inside `target` (any depth), tree order; the target itself if it is one. */
export function boxesUnder(roots: readonly LocationNode[], targetId: string): LocationNode[] {
  const t = findNode(roots, targetId);
  if (!t) return [];
  const out: LocationNode[] = [];
  const walk = (n: LocationNode) => {
    if (n.kind === 'container' && !n.archived_at) out.push(n);
    n.children.forEach(walk);
  };
  walk(t);
  return out;
}

/** Places that can hold a new box under the target: the target and its non-box descendants. */
export function boxParents(roots: readonly LocationNode[], targetId: string): LocationNode[] {
  const t = findNode(roots, targetId);
  if (!t) return [];
  const out: LocationNode[] = [];
  const walk = (n: LocationNode) => {
    if (n.kind === 'container' || n.archived_at) return;
    out.push(n);
    n.children.forEach(walk);
  };
  walk(t);
  return out;
}

const BIG: readonly LocationKind[] = ['site', 'room', 'unit'];
const SMALL: readonly LocationKind[] = ['shelf', 'container'];

/**
 * Whether `moving` may be re-parented into `dest` (server rules: no cycles,
 * and a site/room/unit can't sit inside a shelf or box).
 */
export function canMoveInto(moving: Pick<LocationNode, 'id' | 'kind'> & { children?: LocationNode[] }, dest: LocationNode, roots: readonly LocationNode[]): boolean {
  const self = findNode(roots, moving.id);
  if (self && subtreeIds(self).has(dest.id)) return false;
  if (dest.id === moving.id) return false;
  if (dest.archived_at) return false;
  if (BIG.includes(moving.kind) && SMALL.includes(dest.kind)) return false;
  if (self?.parent_id === dest.id) return false; // already there
  return true;
}

/**
 * Suggests the next free code in the most common `PREFIX-NNN` series
 * (B-001 … B-020 → B-021). Keeps the zero padding. Default "B-001".
 */
export function nextCode(roots: readonly LocationNode[], prefix?: string): string {
  const series = new Map<string, { max: number; width: number; n: number }>();
  for (const { node } of indexTree(roots).values()) {
    const m = node.code?.match(/^([A-Za-z]+-?)(\d+)$/);
    if (!m) continue;
    const p = m[1].toUpperCase();
    const s = series.get(p) ?? { max: 0, width: m[2].length, n: 0 };
    s.max = Math.max(s.max, Number(m[2]));
    s.width = Math.max(s.width, m[2].length);
    s.n++;
    series.set(p, s);
  }
  let pick = prefix?.toUpperCase();
  if (!pick || !series.has(pick)) {
    if (pick && !series.has(pick)) return `${pick}${'1'.padStart(3, '0')}`;
    let best: [string, number] | null = null;
    for (const [p, s] of series) if (p.startsWith('B') && (!best || s.n > best[1])) best = [p, s.n];
    if (!best) for (const [p, s] of series) if (!best || s.n > best[1]) best = [p, s.n];
    pick = best?.[0] ?? 'B-';
  }
  const s = series.get(pick);
  if (!s) return `${pick}001`;
  return `${pick}${String(s.max + 1).padStart(s.width, '0')}`;
}

/** "3 items, 2 locations and 6 staged items" from an archive 409's details. */
export function archiveBlockers(details: unknown): string {
  const d = (details && typeof details === 'object' ? details : {}) as Record<string, unknown>;
  const n = (k: string) => (typeof d[k] === 'number' ? (d[k] as number) : 0);
  const parts: string[] = [];
  const plural = (c: number, one: string, many: string) => `${c} ${c === 1 ? one : many}`;
  if (n('items')) parts.push(plural(n('items'), 'item', 'items'));
  if (n('children')) parts.push(plural(n('children'), 'location', 'locations'));
  if (n('staged')) parts.push(plural(n('staged'), 'item staged to go here', 'items staged to go here'));
  if (!parts.length) return '';
  if (parts.length === 1) return parts[0];
  return `${parts.slice(0, -1).join(', ')} and ${parts[parts.length - 1]}`;
}

/**
 * Requests that put items back where they were before a move/stage/unstage,
 * from their cards taken *before* the change. Moves are grouped by previous
 * location, then items that had a target are re-staged (packing clears it),
 * and items that had none are unstaged (after a stage).
 */
export function undoRequests(prev: readonly ItemSummary[], action: 'move' | 'stage' | 'unstage'): MoveRequest[] {
  const out: MoveRequest[] = [];
  const group = (key: (i: ItemSummary) => string | null) => {
    const m = new Map<string | null, string[]>();
    for (const i of prev) {
      const k = key(i);
      m.set(k, [...(m.get(k) ?? []), i.id]);
    }
    return m;
  };
  if (action === 'move') {
    for (const [loc, ids] of group((i) => i.placement.location?.id ?? null)) out.push({ action: 'move', item_ids: ids, to: loc, note: 'undo' });
  }
  for (const [target, ids] of group((i) => i.placement.target?.id ?? null)) {
    if (target) out.push({ action: 'stage', item_ids: ids, to: target, note: 'undo' });
    else if (action === 'stage') out.push({ action: 'unstage', item_ids: ids, note: 'undo' });
  }
  return out;
}

/** "just now", "5 min ago", "3 h ago", "2 d ago", else a short date. */
export function relTime(iso: string | null | undefined, now = Date.now()): string {
  if (!iso) return '';
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return iso;
  const s = Math.round((now - t) / 1000);
  if (s < 45) return 'just now';
  if (s < 3600) return `${Math.round(s / 60)} min ago`;
  if (s < 86400) return `${Math.round(s / 3600)} h ago`;
  if (s < 86400 * 14) return `${Math.round(s / 86400)} d ago`;
  return shortDate(iso);
}

export function shortDate(iso: string | null | undefined): string {
  if (!iso) return '';
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return iso;
  // EXIF times without an offset are local wall-clock: show them as written.
  return new Date(t).toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' });
}

export function shortDateTime(iso: string | null | undefined): string {
  if (!iso) return '';
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return iso;
  return new Date(t).toLocaleString(undefined, { month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' });
}

const refName = (r: Movement['from']) => (r ? r.label : 'unknown');

/** One line for a movement row on a location page. */
export function describeMovement(mv: Movement, hereId: string, titles: ReadonlyMap<string, string>): string {
  if (mv.location_moved_id) {
    const what = mv.location_moved_id === hereId ? 'This location' : 'A location inside';
    return `${what} moved ${mv.from ? `from ${mv.from.label} ` : ''}to ${mv.to ? mv.to.label : 'top level'}`;
  }
  const who = (mv.item_id && titles.get(mv.item_id)) || 'An item';
  if (mv.kind === 'stage') return `${who} staged to go to ${refName(mv.to)}`;
  if (mv.kind === 'unstage') return `${who} unstaged`;
  const inHere = (r: Movement['from']) => !!r && r.path.some((c) => c.id === hereId);
  if (inHere(mv.to) && !inHere(mv.from)) return `${who} moved in from ${refName(mv.from)}`;
  if (inHere(mv.from) && !inHere(mv.to)) return `${who} moved out to ${refName(mv.to)}`;
  return `${who} moved ${mv.from ? `from ${refName(mv.from)} ` : ''}to ${refName(mv.to)}`;
}

/** Parses a /locations/[id] route param: ULIDs and `code:` refs pass through; anything else is treated as a code. */
export function locationParam(param: string): string {
  const p = decodeURIComponent(param).trim();
  if (/^code:/i.test(p)) return p;
  if (/^[0-9A-HJKMNP-TV-Z]{26}$/i.test(p)) return p.toUpperCase();
  return `code:${p}`;
}

/** Plural helper: plural(3, 'item') → "3 items". */
export function plural(n: number, one: string, many = `${one}s`): string {
  return `${n} ${n === 1 ? one : many}`;
}

/** Photo roles offered when linking photos to an item (registry values; see /api/schema). */
export const ITEM_ROLES = ['flat_lay', 'on_body', 'detail', 'label', 'care_label', 'hang_tag', 'defect', 'receipt', 'stock', 'other'] as const;

/** Default role for the i-th photo of an item: the first is the flat lay, the rest details. */
export function defaultRole(i: number): string {
  return i === 0 ? 'flat_lay' : 'detail';
}
