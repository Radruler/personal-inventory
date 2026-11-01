// Stroke icon paths on a 24×24 grid (rendered by Icon.svelte with
// stroke=currentColor, round caps/joins). `v.01` segments draw dots.
import type { Schemas } from '$lib/api/client';

export const ICONS = {
  // kinds
  clothing: 'M8.5 3.5 3 6.5l2 4 2.5-1.2V20.5h9V9.3l2.5 1.2 2-4-5.5-3c-.6 1.6-1.9 2.5-3.5 2.5s-2.9-.9-3.5-2.5z',
  object: 'M12 3l8 4.5v9L12 21l-8-4.5v-9z M4 7.5l8 4.5 8-4.5 M12 12v9',
  utility: 'M9 3v4 M15 3v4 M7 7h10v3.5a5 5 0 0 1-10 0z M12 15.5v1.5a3.5 3.5 0 0 0 3.5 3.5H19',

  // location kinds
  site: 'M3 20.5h18 M5 20.5V10l7-5 7 5v10.5 M10 20.5v-5.5h4v5.5',
  room: 'M5 20.5V3.5h11v17 M16 6.5h3v14 M13 12.5v.01',
  unit: 'M3 20.5V8l9-5 9 5v12.5 M7 20.5v-9h10v9 M7 14.5h10 M7 17.5h10',
  shelf: 'M4.5 3.5v17 M19.5 3.5v17 M4.5 9h15 M4.5 15h15',
  container: 'M12 3l8 4.5v9L12 21l-8-4.5v-9z M4 7.5l8 4.5 8-4.5 M12 12v9',

  // placement status
  placed: 'M12 21s-7-6.2-7-11.5A7 7 0 0 1 19 9.5C19 14.8 12 21 12 21z M12 12a2.5 2.5 0 1 0 0-5 2.5 2.5 0 0 0 0 5z',
  approximate: 'M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18z M12 8.5a3.5 3.5 0 1 0 0 7 3.5 3.5 0 0 0 0-7z',
  staged: 'M4.5 12h15 M13.5 6l6 6-6 6',
  unknown: 'M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18z M9.6 9.3a2.5 2.5 0 1 1 3.4 2.3c-.6.3-1 .8-1 1.5v.6 M12 16.8v.01',
  gone: 'M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18z M8 12h8',

  // ui
  search: 'M11 4a7 7 0 1 0 0 14 7 7 0 0 0 0-14z M20 20l-4-4',
  close: 'M6 6l12 12 M18 6L6 18',
  check: 'M5 12.5l4.5 4.5L19 7',
  'chevron-left': 'M15 6l-6 6 6 6',
  'chevron-right': 'M9 6l6 6-6 6',
  'chevron-down': 'M6 9l6 6 6-6',
  filter: 'M4 6.5h16 M7 12h10 M10 17.5h4',
  plus: 'M12 5v14 M5 12h14',
  download: 'M12 4v11 M7 10l5 5 5-5 M5 20h14',
  external: 'M14 4h6v6 M20 4l-9 9 M18 14v5a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V7a1 1 0 0 1 1-1h5',
  image: 'M4 5h16v14H4z M4 16l5-5 4 4 3-3 4 4 M15.5 9.5v.01',
  alert: 'M12 4l9 16H3z M12 10v4 M12 17v.01',
  refresh: 'M20 12a8 8 0 1 1-2.3-5.7 M20 4v5h-5',
  inbox: 'M4 13l2.5-8h11L20 13v6H4z M4 13h5l1 2h4l1-2h5',
  review: 'M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18z M12 7.5v5.5 M12 16.5v.01',
  info: 'M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18z M12 11v5.5 M12 7.5v.01',
  sun: 'M12 8a4 4 0 1 0 0 8 4 4 0 0 0 0-8z M12 2.5v2 M12 19.5v2 M2.5 12h2 M19.5 12h2 M5.3 5.3l1.4 1.4 M17.3 17.3l1.4 1.4 M5.3 18.7l1.4-1.4 M17.3 6.7l1.4-1.4',
  moon: 'M20 14.5A8 8 0 0 1 9.5 4a8 8 0 1 0 10.5 10.5z',
  phone: 'M8 2.5h8a1.5 1.5 0 0 1 1.5 1.5v16a1.5 1.5 0 0 1-1.5 1.5H8A1.5 1.5 0 0 1 6.5 20V4A1.5 1.5 0 0 1 8 2.5z M11 18.5h2',
  desktop: 'M3 4.5h18v12H3z M8.5 20.5h7 M12 16.5v4'
} as const;

export type IconName = keyof typeof ICONS;

export const KIND_ICON: Record<Schemas['Kind'], IconName> = {
  clothing: 'clothing',
  object: 'object',
  utility: 'utility'
};

export const LOCATION_KIND_ICON: Record<Schemas['LocationKind'], IconName> = {
  site: 'site',
  room: 'room',
  unit: 'unit',
  shelf: 'shelf',
  container: 'container'
};

export const STATUS_ICON: Record<Schemas['PlacementStatus'], IconName> = {
  placed: 'placed',
  approximate: 'approximate',
  staged: 'staged',
  unknown: 'unknown',
  gone: 'gone'
};

/** Icon for a kind string that may be unknown to this build. */
export function kindIcon(kind: string): IconName {
  return (KIND_ICON as Record<string, IconName>)[kind] ?? 'object';
}
