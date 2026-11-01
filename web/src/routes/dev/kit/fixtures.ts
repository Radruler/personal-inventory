// Typed fixtures for /dev/kit while the backend endpoints return 501.
// Shapes follow api/openapi.yaml exactly (via the generated Schemas types).
import type { Facets, ItemSummary, LocationNode, LocationRef, MediaRef, Placement, Schemas } from '$lib/api/client';

type Crumb = Schemas['LocationCrumb'];
type Kind = Schemas['LocationKind'];

const T0 = '2026-10-20T10:00:00.000Z';

// ------------------------------------------------------------------ photos

/** Data-URL placeholder photo (a soft gradient with a caption); stands in for /media/<sha>/thumb. */
function svgPhoto(hue: number, w: number, h: number, text: string): string {
  const svg =
    `<svg xmlns="http://www.w3.org/2000/svg" width="${w}" height="${h}" viewBox="0 0 ${w} ${h}">` +
    `<defs><linearGradient id="g" x1="0" y1="0" x2="1" y2="1">` +
    `<stop offset="0" stop-color="hsl(${hue} 32% 72%)"/><stop offset="1" stop-color="hsl(${(hue + 40) % 360} 28% 46%)"/>` +
    `</linearGradient></defs><rect width="100%" height="100%" fill="url(#g)"/>` +
    `<circle cx="${w * 0.72}" cy="${h * 0.3}" r="${Math.min(w, h) * 0.14}" fill="hsl(${hue} 40% 88% / .55)"/>` +
    `<text x="50%" y="88%" text-anchor="middle" font-family="system-ui,sans-serif" font-size="${Math.round(Math.min(w, h) / 12)}" fill="hsl(${hue} 30% 18% / .75)">${text}</text></svg>`;
  return `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`;
}

let mediaSeq = 0;
export function photo(hue: number, text: string, w = 1200, h = 1500, status: MediaRef['variant_status'] = 'ready'): MediaRef {
  mediaSeq++;
  const id = `01JCMEDIA${String(mediaSeq).padStart(17, '0')}`;
  const url = svgPhoto(hue, w, h, text);
  const ready = status === 'ready';
  return {
    id,
    sha256: `${mediaSeq.toString(16).padStart(4, '0')}`.padEnd(64, 'f'),
    mime: 'image/jpeg',
    width: w,
    height: h,
    variant_status: status,
    thumb_url: ready ? url : null,
    display_url: ready ? url : null,
    orig_url: url
  };
}

// ------------------------------------------------------------------ locations

type LocDef = { id: string; code: string | null; name: string; kind: Kind; parent: string | null; items: number; staged?: number };

const LOCS: LocDef[] = [
  { id: 'L-home', code: null, name: 'Home', kind: 'site', parent: null, items: 0 },
  { id: 'L-bed', code: null, name: 'Bedroom', kind: 'room', parent: 'L-home', items: 6 },
  { id: 'L-closet', code: 'CL-1', name: 'Closet shelf', kind: 'shelf', parent: 'L-bed', items: 2 },
  { id: 'L-b001', code: 'B-001', name: 'Shoe box', kind: 'container', parent: 'L-closet', items: 3 },
  { id: 'L-garage', code: null, name: 'Garage', kind: 'room', parent: 'L-home', items: 1 },
  { id: 'L-ga', code: 'G-A', name: 'Garage rack A', kind: 'shelf', parent: 'L-garage', items: 0 },
  { id: 'L-b020', code: 'B-020', name: 'Cables', kind: 'container', parent: 'L-ga', items: 7 },
  { id: 'L-p03', code: 'P-03', name: 'USB-C pouch', kind: 'container', parent: 'L-b020', items: 4 },
  { id: 'L-sk', code: null, name: 'Storage King', kind: 'site', parent: null, items: 0 },
  { id: 'L-u2', code: 'U2', name: 'Unit 2', kind: 'unit', parent: 'L-sk', items: 3, staged: 2 },
  { id: 'L-b014', code: 'B-014', name: 'Winter coats', kind: 'container', parent: 'L-u2', items: 5, staged: 1 },
  { id: 'L-b015', code: 'B-015', name: 'Summer clothes', kind: 'container', parent: 'L-u2', items: 9 },
  { id: 'L-s1', code: 'S-1', name: 'Metal rack', kind: 'shelf', parent: 'L-u2', items: 0 },
  { id: 'L-b016', code: 'B-016', name: 'Camping gear', kind: 'container', parent: 'L-s1', items: 4 },
  { id: 'L-p07', code: 'P-07', name: 'Small parts tin', kind: 'container', parent: 'L-b016', items: 12 },
  { id: 'L-u7', code: 'U7', name: 'Unit 7', kind: 'unit', parent: 'L-sk', items: 0 }
];

const byId = new Map(LOCS.map((l) => [l.id, l]));

function crumbs(id: string): Crumb[] {
  const out: Crumb[] = [];
  let cur = byId.get(id);
  while (cur) {
    out.unshift({ id: cur.id, code: cur.code, name: cur.name, kind: cur.kind });
    cur = cur.parent ? byId.get(cur.parent) : undefined;
  }
  return out;
}

/** Label rule from the contract: the site is omitted when something deeper follows; containers/shelves by code. */
function labelFor(path: Crumb[]): string {
  const parts = path.length > 1 && path[0].kind === 'site' ? path.slice(1) : path;
  return parts.map((c) => ((c.kind === 'container' || c.kind === 'shelf') && c.code ? c.code : c.name)).join(' › ');
}

export function locRef(id: string): LocationRef {
  const path = crumbs(id);
  const last = path[path.length - 1];
  return { ...last, path, label: labelFor(path), archived: false };
}

function node(def: LocDef): LocationNode {
  const children = LOCS.filter((l) => l.parent === def.id).map(node);
  const total = def.items + children.reduce((s, c) => s + c.total_count, 0);
  return {
    id: def.id,
    code: def.code,
    name: def.name,
    kind: def.kind,
    parent_id: def.parent,
    cover_media_id: null,
    notes: null,
    layout: null,
    archived_at: null,
    created_at: T0,
    updated_at: T0,
    label: labelFor(crumbs(def.id)),
    cover: null,
    item_count: def.items,
    total_count: total,
    staged_count: def.staged ?? 0,
    children
  };
}

export const fixtureLocationTree: Schemas['LocationTree'] = {
  roots: LOCS.filter((l) => l.parent === null).map(node),
  unknown_count: 3
};

// ------------------------------------------------------------------ placements

export const placements = {
  placed: (id = 'L-b014'): Placement => ({
    status: 'placed',
    label: locRef(id).label,
    location: locRef(id),
    target: null,
    gone_reason: null,
    gone_at: null
  }),
  room: (): Placement => ({
    status: 'placed',
    label: 'Bedroom',
    location: locRef('L-bed'),
    target: null,
    gone_reason: null,
    gone_at: null
  }),
  approximate: (): Placement => ({
    status: 'approximate',
    label: 'Somewhere in Unit 2 (box unknown)',
    location: locRef('L-u2'),
    target: null,
    gone_reason: null,
    gone_at: null
  }),
  staged: (): Placement => ({
    status: 'staged',
    label: 'Bedroom → going to Unit 2',
    location: locRef('L-bed'),
    target: locRef('L-u2'),
    gone_reason: null,
    gone_at: null
  }),
  stagedFromUnknown: (): Placement => ({
    status: 'staged',
    label: 'Location unknown → going to B-014',
    location: null,
    target: locRef('L-b014'),
    gone_reason: null,
    gone_at: null
  }),
  unknown: (): Placement => ({
    status: 'unknown',
    label: 'Location unknown',
    location: null,
    target: null,
    gone_reason: null,
    gone_at: null
  }),
  gone: (): Placement => ({
    status: 'gone',
    label: 'Sold · 2026-03',
    location: null,
    target: null,
    gone_reason: 'sold',
    gone_at: '2026-03-14T12:00:00.000Z'
  })
};

// ------------------------------------------------------------------ items

let itemSeq = 0;
function item(p: Partial<ItemSummary> & Pick<ItemSummary, 'kind' | 'title'>): ItemSummary {
  itemSeq++;
  return {
    id: `01JCITEM${String(itemSeq).padStart(18, '0')}`,
    category: null,
    quantity: 1,
    lifecycle: 'owned',
    review_state: 'ok',
    cover: null,
    placement: placements.placed(),
    attrs: {},
    tags: [],
    product_id: null,
    ext_key: null,
    created_at: T0,
    updated_at: T0,
    ...p
  };
}

export const fixtureItems: ItemSummary[] = [
  item({ kind: 'clothing', title: 'Navy Gore-Tex shell jacket', category: 'outerwear/shell', cover: photo(215, 'shell jacket'), attrs: { brand: 'Arc’teryx', color: 'navy', size: 'M' } }),
  item({ kind: 'clothing', title: 'Grey merino crew sweater', category: 'knitwear/sweater', cover: photo(30, 'merino crew', 1200, 1200), placement: placements.staged(), attrs: { brand: 'Uniqlo', color: 'grey', size: 'L' } }),
  item({ kind: 'clothing', title: 'Black wool overcoat', category: 'outerwear/coat', cover: photo(260, 'overcoat'), placement: placements.approximate() }),
  item({ kind: 'utility', title: 'USB-C cable 1 m', category: 'cable/usb-c', quantity: 4, cover: photo(160, 'USB-C ×4', 1500, 1000), placement: placements.placed('L-p03') }),
  item({ kind: 'clothing', title: 'White AIRism crew neck T', category: 'tops/t-shirt', review_state: 'needs_review', cover: photo(50, 'AIRism tee'), placement: placements.room(), attrs: { brand: 'Uniqlo', color: 'white', size: 'M' } }),
  item({ kind: 'object', title: 'Camping stove', category: 'outdoor/cooking', placement: placements.placed('L-b016') }),
  item({ kind: 'clothing', title: 'Selvedge denim jeans', category: 'bottoms/jeans', cover: photo(225, 'denim', 1000, 1500), placement: placements.unknown(), tags: ['favourite'] }),
  item({ kind: 'utility', title: 'HDMI 2.1 cable 2 m', category: 'cable/hdmi', quantity: 2, placement: placements.placed('L-b020') }),
  item({ kind: 'object', title: 'Film camera (Olympus XA)', category: 'electronics/camera', lifecycle: 'gone', cover: photo(10, 'camera', 1500, 1000), placement: placements.gone() }),
  item({ kind: 'clothing', title: 'Running shoes', category: 'footwear/running', cover: photo(340, 'shoes', 1500, 1000, 'pending'), placement: placements.placed('L-b001') }),
  item({ kind: 'clothing', title: 'Linen shirt', category: 'tops/shirt', review_state: 'needs_review', quantity: 2, placement: placements.stagedFromUnknown() }),
  item({ kind: 'object', title: 'Headlamp', category: 'outdoor/lighting', cover: photo(90, 'headlamp', 1200, 1200, 'failed'), placement: placements.placed('L-p07') }),
  item({ kind: 'utility', title: 'Assorted screws and wall plugs', category: 'hardware/fasteners', quantity: 120, placement: placements.placed('L-p07') }),
  item({ kind: 'clothing', title: 'Down vest with a rather long title that should wrap onto two lines', category: 'outerwear/vest', cover: photo(190, 'vest'), placement: placements.placed('L-b014') })
];

export const fixtureItemList: Schemas['ItemList'] = { items: fixtureItems, next_cursor: null, total: fixtureItems.length };

// ------------------------------------------------------------------ gallery

function link(media: MediaRef, role: string, sort: number, caption: string | null = null): Schemas['MediaLinkView'] {
  return {
    media: {
      ...media,
      ext: 'jpg',
      bytes: 2_400_000,
      taken_at: '2026-10-19T15:02:11',
      original_name: `IMG_${4100 + sort}.JPG`,
      exif: { Make: 'Apple', Model: 'iPhone 16' },
      context: null,
      variant_error: media.variant_status === 'failed' ? 'vips: unsupported HEIC profile' : null,
      created_at: T0,
      links: []
    },
    role,
    sort,
    caption
  };
}

export const fixtureGalleryMedia: Schemas['MediaLinkView'][] = [
  link(photo(215, 'flat lay — front'), 'flat_lay', 0),
  link(photo(215, 'flat lay — back'), 'flat_lay', 1),
  link(photo(200, 'on body', 1000, 1500), 'on_body', 0, 'Worn in Tokyo, 2024'),
  link(photo(230, 'stock photo', 1500, 1500), 'stock', 0, 'Retailer photo'),
  link(photo(40, 'neck label', 1200, 900), 'label', 0),
  link(photo(45, 'care label', 900, 1200), 'care_label', 0),
  link(photo(55, 'hang tag', 900, 1200), 'hang_tag', 0),
  link(photo(0, 'zip detail', 1200, 1200), 'detail', 0),
  link(photo(5, 'small tear', 1200, 1200), 'defect', 0, 'Small tear on the left cuff'),
  link(photo(120, 'receipt', 900, 1400), 'receipt', 0),
  link(photo(300, 'processing', 1200, 1500, 'pending'), 'on_body', 1)
];

// ------------------------------------------------------------------ facets

export const fixtureFacets: Facets = {
  total: 58,
  groups: [
    { field: 'kind', label: 'Kind', values: [{ value: 'clothing', label: 'Clothing', count: 40 }, { value: 'object', label: 'Object', count: 11 }, { value: 'utility', label: 'Utility', count: 7 }] },
    {
      field: 'status',
      label: 'Where',
      values: [
        { value: 'placed', label: 'Placed', count: 30 },
        { value: 'staged', label: 'Staged', count: 8 },
        { value: 'approximate', label: 'Approximate', count: 9 },
        { value: 'unknown', label: 'Unknown', count: 3 },
        { value: 'gone', label: 'Gone', count: 2 }
      ]
    },
    {
      field: 'category',
      label: 'Category',
      values: [
        { value: 'outerwear', label: 'outerwear', count: 12 },
        { value: 'tops', label: 'tops', count: 11 },
        { value: 'bottoms', label: 'bottoms', count: 7 },
        { value: 'knitwear', label: 'knitwear', count: 5 },
        { value: 'footwear', label: 'footwear', count: 5 },
        { value: 'cable', label: 'cable', count: 5 },
        { value: 'outdoor', label: 'outdoor', count: 4 },
        { value: 'electronics', label: 'electronics', count: 3 },
        { value: 'hardware', label: 'hardware', count: 2 },
        { value: 'accessories', label: 'accessories', count: 2 },
        { value: 'bags', label: 'bags', count: 1 }
      ]
    },
    {
      field: 'location',
      label: 'Location',
      values: [
        { value: 'L-u2', label: 'Unit 2', count: 22 },
        { value: 'L-bed', label: 'Bedroom', count: 11 },
        { value: 'L-garage', label: 'Garage', count: 12 },
        { value: 'unknown', label: 'Location unknown', count: 3 }
      ]
    },
    { field: 'attr.brand', label: 'Brand', values: [{ value: 'Uniqlo', label: 'Uniqlo', count: 14 }, { value: 'Arc’teryx', label: 'Arc’teryx', count: 3 }, { value: 'Muji', label: 'Muji', count: 2 }] },
    { field: 'attr.color', label: 'Color', values: [{ value: 'navy', label: 'navy', count: 6 }, { value: 'black', label: 'black', count: 9 }, { value: 'grey', label: 'grey', count: 5 }, { value: 'white', label: 'white', count: 4 }] },
    { field: 'attr.size', label: 'Size', values: [{ value: 'M', label: 'M', count: 18 }, { value: 'L', label: 'L', count: 12 }, { value: 'S', label: 'S', count: 2 }] },
    { field: 'tag', label: 'Tags', values: [{ value: 'favourite', label: 'favourite', count: 4 }, { value: 'travel', label: 'travel', count: 3 }] },
    { field: 'review', label: 'Review', values: [{ value: 'needs_review', label: 'Needs review', count: 5 }] }
  ]
};
