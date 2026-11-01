// Location tree helpers for LocationPicker (pure; unit-tested): indexing,
// search filtering, the flattened visible-row list and keyboard moves.
import type { LocationNode, Schemas } from '$lib/api/client';

export type LocationKind = Schemas['LocationKind'];

export type TreeIndexEntry = { node: LocationNode; parentId: string | null; depth: number };
export type TreeIndex = Map<string, TreeIndexEntry>;

export function indexTree(roots: readonly LocationNode[]): TreeIndex {
  const idx: TreeIndex = new Map();
  const walk = (nodes: readonly LocationNode[], parentId: string | null, depth: number) => {
    for (const n of nodes) {
      idx.set(n.id, { node: n, parentId, depth });
      walk(n.children, n.id, depth + 1);
    }
  };
  walk(roots, null, 1);
  return idx;
}

/** Ancestor ids, nearest first (excluding the node itself). */
export function ancestorsOf(idx: TreeIndex, id: string): string[] {
  const out: string[] = [];
  let cur = idx.get(id)?.parentId ?? null;
  while (cur) {
    out.push(cur);
    cur = idx.get(cur)?.parentId ?? null;
  }
  return out;
}

function norm(s: string): string {
  return s.toLowerCase().normalize('NFKD').replace(/[̀-ͯ]/g, '');
}

/** Every whitespace-separated word must prefix-match the name, code or label words. */
export function matchesQuery(node: Pick<LocationNode, 'name' | 'code' | 'label'>, query: string): boolean {
  const words = norm(query).split(/\s+/).filter(Boolean);
  if (!words.length) return true;
  const hay = norm(`${node.name} ${node.code ?? ''} ${node.label}`);
  const tokens = hay.split(/[\s›/,()·]+/).filter(Boolean);
  const compact = (s: string) => s.replace(/[-_.\s]/g, '');
  return words.every((w) => tokens.some((t) => t.startsWith(w) || compact(t).startsWith(compact(w))));
}

export type TreeSearch = {
  /** Nodes that match the query. */
  matches: Set<string>;
  /** Matches plus all their ancestors (what stays visible). */
  keep: Set<string>;
  /** Ancestors of a match: opened automatically while searching. */
  autoOpen: Set<string>;
};

export function searchTree(roots: readonly LocationNode[], query: string): TreeSearch | null {
  if (!query.trim()) return null;
  const idx = indexTree(roots);
  const matches = new Set<string>();
  const keep = new Set<string>();
  const autoOpen = new Set<string>();
  for (const [id, { node }] of idx) {
    if (!matchesQuery(node, query)) continue;
    matches.add(id);
    keep.add(id);
    for (const a of ancestorsOf(idx, id)) {
      keep.add(a);
      autoOpen.add(a);
    }
  }
  return { matches, keep, autoOpen };
}

export type TreeRow = {
  node: LocationNode;
  depth: number;
  parentId: string | null;
  posinset: number;
  setsize: number;
  hasChildren: boolean;
  expanded: boolean;
  match: boolean;
};

/**
 * Flattens the tree into the rows currently visible. Without a search,
 * children show when their parent is in `expanded`. With a search, only
 * matches and their ancestors show (ancestors open automatically); a match
 * the user expands shows all of its children.
 */
export function visibleRows(
  roots: readonly LocationNode[],
  expanded: ReadonlySet<string>,
  search: TreeSearch | null = null
): TreeRow[] {
  const rows: TreeRow[] = [];
  const walk = (nodes: readonly LocationNode[], depth: number, parentId: string | null, filtered: boolean) => {
    const list = filtered && search ? nodes.filter((n) => search.keep.has(n.id)) : nodes;
    list.forEach((n, i) => {
      const auto = filtered && !!search?.autoOpen.has(n.id);
      const hasChildren = n.children.length > 0;
      const open = hasChildren && (auto || expanded.has(n.id));
      rows.push({
        node: n,
        depth,
        parentId,
        posinset: i + 1,
        setsize: list.length,
        hasChildren,
        expanded: open,
        match: !!search?.matches.has(n.id)
      });
      if (open) walk(n.children, depth + 1, n.id, auto);
    });
  };
  walk(roots, 1, null, !!search);
  return rows;
}

export type TreeKeyResult =
  | { type: 'focus'; id: string }
  | { type: 'expand'; id: string }
  | { type: 'collapse'; id: string }
  | { type: 'select'; id: string }
  | null;

/** WAI-ARIA tree keyboard behaviour over the visible rows. */
export function treeKey(rows: readonly TreeRow[], focusedId: string | null, key: string): TreeKeyResult {
  if (!rows.length) return null;
  const i = Math.max(
    0,
    rows.findIndex((r) => r.node.id === focusedId)
  );
  const row = rows[i];
  switch (key) {
    case 'ArrowDown':
      return { type: 'focus', id: rows[Math.min(rows.length - 1, i + 1)].node.id };
    case 'ArrowUp':
      return { type: 'focus', id: rows[Math.max(0, i - 1)].node.id };
    case 'Home':
      return { type: 'focus', id: rows[0].node.id };
    case 'End':
      return { type: 'focus', id: rows[rows.length - 1].node.id };
    case 'ArrowRight':
      if (row.hasChildren && !row.expanded) return { type: 'expand', id: row.node.id };
      if (row.expanded && rows[i + 1]) return { type: 'focus', id: rows[i + 1].node.id };
      return null;
    case 'ArrowLeft':
      if (row.expanded) return { type: 'collapse', id: row.node.id };
      if (row.parentId) return { type: 'focus', id: row.parentId };
      return null;
    case 'Enter':
    case ' ':
      return { type: 'select', id: row.node.id };
    default:
      return null;
  }
}

/** A sensible default kind for a new child of `parent`. */
export function suggestChildKind(parentKind: LocationKind | null | undefined): LocationKind {
  switch (parentKind) {
    case undefined:
    case null:
      return 'site';
    case 'site':
      return 'unit';
    default:
      return 'container';
  }
}

/** Flat parent options for the create form ("Unit 2 › B-014" style labels, tree order). */
export function parentOptions(roots: readonly LocationNode[]): { id: string; label: string; depth: number }[] {
  const out: { id: string; label: string; depth: number }[] = [];
  const walk = (nodes: readonly LocationNode[], depth: number) => {
    for (const n of nodes) {
      out.push({ id: n.id, label: n.code ? `${n.name} (${n.code})` : n.name, depth });
      walk(n.children, depth + 1);
    }
  };
  walk(roots, 0);
  return out;
}

/** Request emitted by LocationPicker; the caller POSTs it to /api/locations. */
export type LocationCreateRequest = Pick<Schemas['LocationCreate'], 'name' | 'kind'> & {
  code?: string | null;
  parent?: string | null;
};

/** Normalizes the create form; returns an error message or the request. */
export function buildCreateRequest(f: {
  name: string;
  code: string;
  kind: LocationKind;
  parent: string | null;
}): LocationCreateRequest | string {
  const name = f.name.trim();
  if (!name) return 'Name is required';
  const code = f.code.trim();
  const req: LocationCreateRequest = { name, kind: f.kind };
  if (code) req.code = code;
  if (f.parent) req.parent = f.parent;
  return req;
}
