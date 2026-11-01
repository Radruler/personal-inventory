// Role → gallery tab grouping (PLAN.md §8 item detail: Flat lay · On body ·
// Stock · Labels · Detail · Other). Pure; unit-tested.
import type { MediaRef, Schemas } from '$lib/api/client';

export type GalleryTabId = 'flat_lay' | 'on_body' | 'stock' | 'labels' | 'detail' | 'other';

export const GALLERY_TABS: { id: GalleryTabId; label: string; roles: readonly string[] }[] = [
  { id: 'flat_lay', label: 'Flat lay', roles: ['flat_lay'] },
  { id: 'on_body', label: 'On body', roles: ['on_body'] },
  { id: 'stock', label: 'Stock', roles: ['stock'] },
  { id: 'labels', label: 'Labels', roles: ['label', 'care_label', 'hang_tag'] },
  { id: 'detail', label: 'Detail', roles: ['detail', 'defect'] },
  { id: 'other', label: 'Other', roles: [] } // everything else
];

/** Which tab a role belongs to; unknown roles land in Other. */
export function tabForRole(role: string): GalleryTabId {
  for (const t of GALLERY_TABS) if (t.roles.includes(role)) return t.id;
  return 'other';
}

/** One photo as the gallery and viewer see it. */
export type GalleryPhoto = {
  media: MediaRef;
  role: string;
  caption: string | null;
};

export type GalleryGroup = { id: GalleryTabId; label: string; photos: GalleryPhoto[] };

/**
 * Groups links into tabs in tab order, keeping the input order inside each
 * tab (the server sorts by role sort, then link sort). Empty tabs are dropped.
 * The same media linked twice under one tab appears once.
 */
export function groupByTab(links: readonly Schemas['MediaLinkView'][]): GalleryGroup[] {
  const by = new Map<GalleryTabId, GalleryPhoto[]>();
  const seen = new Map<GalleryTabId, Set<string>>();
  for (const l of links) {
    const id = tabForRole(l.role);
    const ids = seen.get(id) ?? new Set<string>();
    if (ids.has(l.media.id)) continue;
    ids.add(l.media.id);
    seen.set(id, ids);
    const list = by.get(id) ?? [];
    list.push({ media: l.media, role: l.role, caption: l.caption });
    by.set(id, list);
  }
  return GALLERY_TABS.filter((t) => by.has(t.id)).map((t) => ({ id: t.id, label: t.label, photos: by.get(t.id)! }));
}

/** Wraps an index into [0, n). */
export function wrapIndex(i: number, n: number): number {
  if (n <= 0) return 0;
  return ((i % n) + n) % n;
}

/** Interprets a horizontal swipe: -1 previous, +1 next, 0 none. */
export function swipeDirection(dx: number, dy: number, threshold = 48): -1 | 0 | 1 {
  if (Math.abs(dx) < threshold || Math.abs(dx) < Math.abs(dy) * 1.2) return 0;
  return dx < 0 ? 1 : -1;
}

/** True when the media has a usable variant of this size. */
export function hasVariant(m: MediaRef, size: 'thumb' | 'display'): boolean {
  const url = size === 'thumb' ? m.thumb_url : m.display_url;
  return m.variant_status === 'ready' && !!url;
}
