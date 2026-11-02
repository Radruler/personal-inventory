// Shared, in-memory location tree for pickers across thread F's views, plus
// the move helper (one POST /api/moves, toast with undo). Nothing persisted
// except the per-viewer "last location" in localStorage.
import { api, createLocation, unwrap, type ItemSummary, type LocationNode, type Schemas } from '$lib/api/client';
import { toast } from '$lib/components/toast.svelte';
import { errorMessage } from '$lib/components/format';
import { undoRequests, type MoveRequest } from './places';

export const tree = $state({
  roots: [] as LocationNode[],
  unknown: 0,
  loaded: false,
  loading: false,
  error: null as unknown
});

let inflight: Promise<void> | null = null;
let stale = true;

/** Loads GET /api/locations once (or again after invalidateTree / force). */
export function loadTree(force = false): Promise<void> {
  if (inflight) return inflight;
  if (!force && !stale && tree.loaded) return Promise.resolve();
  tree.loading = true;
  inflight = (async () => {
    try {
      const t = unwrap(await api.GET('/api/locations'));
      tree.roots = t.roots;
      tree.unknown = t.unknown_count;
      tree.loaded = true;
      tree.error = null;
      stale = false;
    } catch (e) {
      tree.error = e;
    } finally {
      tree.loading = false;
      inflight = null;
    }
  })();
  return inflight;
}

export function invalidateTree() {
  stale = true;
}

/** Creates a location, reloads the tree and returns the new id. */
export async function createAndReload(req: Schemas['LocationCreate']): Promise<string> {
  const loc = await createLocation(req);
  invalidateTree();
  await loadTree(true);
  return loc.id;
}

export async function postMove(req: MoveRequest) {
  const res = unwrap(await api.POST('/api/moves', { body: req as Schemas['MoveRequest'] }));
  invalidateTree();
  return res;
}

/**
 * Runs an item move/stage/unstage and shows a toast with Undo. `prev` are the
 * cards before the change (for undo). `after` runs after the change and after
 * an undo (reload the view). Throws on failure (the toast shows the error).
 */
export async function moveItems(
  req: MoveRequest & { item_ids: string[] },
  prev: readonly ItemSummary[],
  message: string,
  after?: () => void | Promise<void>
) {
  try {
    const res = await postMove(req);
    const undo = undoRequests(prev, req.action);
    toast(res.moved === 0 ? 'Nothing changed' : message, {
      tone: 'success',
      timeout: 8000,
      action: res.moved
        ? {
            label: 'Undo',
            run: async () => {
              try {
                for (const r of undo) await postMove(r);
                toast('Undone');
              } catch (e) {
                toast(`Undo failed: ${errorMessage(e)}`, { tone: 'error' });
              }
              await after?.();
            }
          }
        : undefined
    });
    await after?.();
    return res;
  } catch (e) {
    toast(errorMessage(e), { tone: 'error' });
    throw e;
  }
}

// ---- per-viewer preference (localStorage only)

const LAST_KEY = 'inv.add.location';

export function lastLocation(): string | null {
  try {
    return localStorage.getItem(LAST_KEY);
  } catch {
    return null;
  }
}

export function rememberLocation(id: string | null) {
  try {
    if (id) localStorage.setItem(LAST_KEY, id);
    else localStorage.removeItem(LAST_KEY);
  } catch {
    /* private mode */
  }
}

export function readPref(key: string): string | null {
  try {
    return localStorage.getItem(key);
  } catch {
    return null;
  }
}

export function writePref(key: string, value: string | null) {
  try {
    if (value === null) localStorage.removeItem(key);
    else localStorage.setItem(key, value);
  } catch {
    /* private mode */
  }
}
