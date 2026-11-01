-- Personal Inventory: initial schema (PLAN.md §4).
--
-- Conventions
--   * IDs are ULIDs (TEXT). Human short codes live in location.code.
--   * Timestamps are UTC ISO-8601 TEXT with milliseconds: 2026-11-01T10:00:00.000Z.
--     The app sets them explicitly; the defaults below are a safety net.
--   * JSON columns are TEXT checked with json_valid().
--   * Registries (fact_key, identifier_scheme, media_role, observation_source)
--     suggest vocabulary. Unknown values are still accepted everywhere.
--   * observation/identifier/fact/reference carry an optional ext_key so the
--     ingest bundle can be re-applied idempotently.

-- ---------------------------------------------------------------------------
-- Registries
-- ---------------------------------------------------------------------------

CREATE TABLE item_kind (
  kind        TEXT PRIMARY KEY,
  label       TEXT NOT NULL,
  sort        INTEGER NOT NULL DEFAULT 0,
  description TEXT
) STRICT;

CREATE TABLE media_role (
  role        TEXT PRIMARY KEY,
  label       TEXT NOT NULL,
  sort        INTEGER NOT NULL DEFAULT 0,
  -- cover_rank: lower wins when picking an item cover; NULL = never a cover
  cover_rank  INTEGER,
  description TEXT
) STRICT;

CREATE TABLE observation_source (
  source      TEXT PRIMARY KEY,
  label       TEXT NOT NULL,
  sort        INTEGER NOT NULL DEFAULT 0,
  description TEXT
) STRICT;

CREATE TABLE fact_key (
  key         TEXT PRIMARY KEY,
  label       TEXT NOT NULL,
  -- string | number | boolean | date | money | composition | json
  value_type  TEXT NOT NULL DEFAULT 'string',
  -- JSON array of kinds this key applies to; [] = all kinds
  kinds       TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(kinds)),
  filterable  INTEGER NOT NULL DEFAULT 0 CHECK (filterable IN (0, 1)),
  sort        INTEGER NOT NULL DEFAULT 100,
  description TEXT
) STRICT;

CREATE TABLE identifier_scheme (
  scheme      TEXT PRIMARY KEY,
  label       TEXT NOT NULL,
  brand       TEXT,
  description TEXT,
  pattern     TEXT,   -- optional regex hint for values (not enforced)
  example     TEXT,
  notes       TEXT    -- accumulated knowledge, e.g. "suffix = season"
) STRICT;

-- ---------------------------------------------------------------------------
-- Locations
-- ---------------------------------------------------------------------------

CREATE TABLE location (
  id             TEXT PRIMARY KEY,
  code           TEXT COLLATE NOCASE UNIQUE,   -- printed label: 'U2', 'B-014'
  name           TEXT NOT NULL,
  kind           TEXT NOT NULL CHECK (kind IN ('site', 'room', 'unit', 'shelf', 'container')),
  parent_id      TEXT REFERENCES location (id),
  cover_media_id TEXT REFERENCES media (id) ON DELETE SET NULL,
  notes          TEXT,
  layout         TEXT CHECK (layout IS NULL OR json_valid(layout)),  -- FUTURE map (§4.4)
  archived_at    TEXT,
  created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  CHECK (parent_id IS NULL OR parent_id <> id)
) STRICT;
CREATE INDEX location_parent ON location (parent_id);

-- ---------------------------------------------------------------------------
-- Media
-- ---------------------------------------------------------------------------

CREATE TABLE media (
  id             TEXT PRIMARY KEY,
  sha256         TEXT NOT NULL UNIQUE,        -- lowercase hex of the original bytes
  mime           TEXT NOT NULL,
  ext            TEXT NOT NULL,               -- stored file extension, e.g. 'heic'
  bytes          INTEGER NOT NULL,
  width          INTEGER,                     -- after EXIF auto-rotation
  height         INTEGER,
  taken_at       TEXT,                        -- EXIF DateTimeOriginal, as UTC if offset known
  original_name  TEXT,
  exif           TEXT CHECK (exif IS NULL OR json_valid(exif)),  -- subset, never GPS
  context        TEXT,                        -- owner's free-text note (user-asserted)
  -- variant pipeline: pending | ready | failed | none (documents: no variants)
  variant_status TEXT NOT NULL DEFAULT 'pending'
                 CHECK (variant_status IN ('pending', 'ready', 'failed', 'none')),
  variant_error  TEXT,
  created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
) STRICT;
CREATE INDEX media_taken_at ON media (taken_at);
CREATE INDEX media_variant_status ON media (variant_status) WHERE variant_status = 'pending';

CREATE TABLE media_link (
  media_id     TEXT NOT NULL REFERENCES media (id) ON DELETE CASCADE,
  subject_type TEXT NOT NULL CHECK (subject_type IN ('item', 'product', 'location')),
  subject_id   TEXT NOT NULL,
  role         TEXT NOT NULL DEFAULT 'other',
  sort         INTEGER NOT NULL DEFAULT 0,
  caption      TEXT,
  created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  PRIMARY KEY (media_id, subject_type, subject_id)
) STRICT;
CREATE INDEX media_link_subject ON media_link (subject_type, subject_id, sort);

-- ---------------------------------------------------------------------------
-- Products and items
-- ---------------------------------------------------------------------------

CREATE TABLE product (
  id         TEXT PRIMARY KEY,
  brand      TEXT,
  name       TEXT NOT NULL,
  attrs      TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(attrs)),  -- projection of product facts
  notes      TEXT,
  ext_key    TEXT UNIQUE,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
) STRICT;
CREATE INDEX product_brand ON product (brand COLLATE NOCASE);

CREATE TABLE item (
  id                 TEXT PRIMARY KEY,
  kind               TEXT NOT NULL CHECK (kind IN ('clothing', 'object', 'utility')),
  title              TEXT NOT NULL,
  category           TEXT,          -- freeform path within kind: 'outerwear/shell'
  product_id         TEXT REFERENCES product (id),
  parent_item_id     TEXT REFERENCES item (id),
  quantity           INTEGER NOT NULL DEFAULT 1 CHECK (quantity >= 1),
  location_id        TEXT REFERENCES location (id),   -- deepest KNOWN location; NULL = unknown
  target_location_id TEXT REFERENCES location (id),   -- non-null = staged to go there
  lifecycle          TEXT NOT NULL DEFAULT 'owned' CHECK (lifecycle IN ('owned', 'gone')),
  gone_reason        TEXT,          -- sold | donated | discarded | lost | other
  gone_at            TEXT,
  review_state       TEXT NOT NULL DEFAULT 'ok' CHECK (review_state IN ('ok', 'needs_review')),
  cover_media_id     TEXT REFERENCES media (id) ON DELETE SET NULL,
  notes              TEXT,
  attrs              TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(attrs)),  -- projection cache
  ext_key            TEXT UNIQUE,   -- idempotency key supplied by agents
  created_at         TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at         TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  CHECK (parent_item_id IS NULL OR parent_item_id <> id)
) STRICT;
CREATE INDEX item_location ON item (location_id);
CREATE INDEX item_target ON item (target_location_id) WHERE target_location_id IS NOT NULL;
CREATE INDEX item_kind_category ON item (kind, category);
CREATE INDEX item_review ON item (review_state) WHERE review_state = 'needs_review';
CREATE INDEX item_updated ON item (updated_at DESC, id DESC);
CREATE INDEX item_created ON item (created_at DESC, id DESC);
CREATE INDEX item_product ON item (product_id);
CREATE INDEX item_parent ON item (parent_item_id);

CREATE TABLE tag (
  item_id TEXT NOT NULL REFERENCES item (id) ON DELETE CASCADE,
  tag     TEXT NOT NULL COLLATE NOCASE,
  PRIMARY KEY (item_id, tag)
) STRICT, WITHOUT ROWID;
CREATE INDEX tag_tag ON tag (tag);

-- ---------------------------------------------------------------------------
-- Observed layer (append-only; corrections are new rows)
-- ---------------------------------------------------------------------------

CREATE TABLE observation (
  id          TEXT PRIMARY KEY,
  item_id     TEXT NOT NULL REFERENCES item (id) ON DELETE CASCADE,
  source      TEXT NOT NULL DEFAULT 'other',
  media_id    TEXT REFERENCES media (id) ON DELETE SET NULL,
  region      TEXT CHECK (region IS NULL OR json_valid(region)),  -- {x,y,w,h} as 0..1 fractions
  text        TEXT,                                               -- verbatim, line breaks kept
  data        TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(data)), -- as-read structure, no inference
  recorded_by TEXT NOT NULL DEFAULT 'user',
  recorded_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  ext_key     TEXT UNIQUE
) STRICT;
CREATE INDEX observation_item ON observation (item_id, recorded_at);

CREATE TABLE identifier (
  id             TEXT PRIMARY KEY,
  subject_type   TEXT NOT NULL CHECK (subject_type IN ('item', 'product')),
  subject_id     TEXT NOT NULL,
  scheme         TEXT NOT NULL,
  value          TEXT NOT NULL,
  value_norm     TEXT NOT NULL,   -- uppercase, no spaces/dashes/dots/slashes
  observation_id TEXT REFERENCES observation (id) ON DELETE SET NULL,
  note           TEXT,
  recorded_by    TEXT NOT NULL DEFAULT 'user',
  recorded_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  ext_key        TEXT UNIQUE
) STRICT;
CREATE INDEX identifier_value ON identifier (value_norm);
CREATE INDEX identifier_scheme_value ON identifier (scheme, value_norm);
CREATE INDEX identifier_subject ON identifier (subject_type, subject_id);

-- ---------------------------------------------------------------------------
-- Derived layer (append-only with supersede chains)
-- ---------------------------------------------------------------------------

CREATE TABLE reference (
  id               TEXT PRIMARY KEY,
  subject_type     TEXT NOT NULL CHECK (subject_type IN ('item', 'product')),
  subject_id       TEXT NOT NULL,
  url              TEXT,
  title            TEXT,
  kind             TEXT NOT NULL DEFAULT 'other',  -- product_page | listing | review | receipt | other
  archive_media_id TEXT REFERENCES media (id) ON DELETE SET NULL,
  fetched_at       TEXT,
  notes            TEXT,
  recorded_by      TEXT NOT NULL DEFAULT 'user',
  recorded_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  ext_key          TEXT UNIQUE
) STRICT;
CREATE INDEX reference_subject ON reference (subject_type, subject_id);
CREATE INDEX reference_archive ON reference (archive_media_id) WHERE archive_media_id IS NOT NULL;

CREATE TABLE fact (
  id            TEXT PRIMARY KEY,
  subject_type  TEXT NOT NULL CHECK (subject_type IN ('item', 'product')),
  subject_id    TEXT NOT NULL,
  key           TEXT NOT NULL,     -- dotted, freeform: 'brand', 'shell.technology'
  value         TEXT NOT NULL CHECK (json_valid(value)),
  basis         TEXT NOT NULL CHECK (basis IN ('user', 'observed', 'looked_up', 'inferred')),
  confidence    REAL NOT NULL DEFAULT 1.0 CHECK (confidence >= 0 AND confidence <= 1),
  -- [{"observation_id":…}|{"reference_id":…}|{"media_id":…}|{"url":…}|{"note":…}]
  sources       TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(sources)),
  status        TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'superseded', 'rejected')),
  supersedes_id TEXT REFERENCES fact (id),
  note          TEXT,              -- why it was superseded/rejected, or free commentary
  recorded_by   TEXT NOT NULL DEFAULT 'user',
  recorded_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  ext_key       TEXT UNIQUE
) STRICT;
CREATE INDEX fact_subject ON fact (subject_type, subject_id, key, status);
CREATE INDEX fact_key_status ON fact (key, status);
CREATE INDEX fact_supersedes ON fact (supersedes_id) WHERE supersedes_id IS NOT NULL;

-- ---------------------------------------------------------------------------
-- Movement history (append-only)
-- ---------------------------------------------------------------------------

CREATE TABLE movement (
  id                TEXT PRIMARY KEY,
  -- exactly one of item_id / location_moved_id is set
  item_id           TEXT REFERENCES item (id) ON DELETE CASCADE,
  location_moved_id TEXT REFERENCES location (id),
  -- move: location changed | stage: target set/changed | unstage: target cleared
  kind              TEXT NOT NULL DEFAULT 'move' CHECK (kind IN ('move', 'stage', 'unstage')),
  from_location_id  TEXT REFERENCES location (id),
  to_location_id    TEXT REFERENCES location (id),
  at                TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  note              TEXT,
  actor             TEXT NOT NULL DEFAULT 'user',
  CHECK ((item_id IS NULL) <> (location_moved_id IS NULL))
) STRICT;
CREATE INDEX movement_item ON movement (item_id, at) WHERE item_id IS NOT NULL;
CREATE INDEX movement_location ON movement (location_moved_id, at) WHERE location_moved_id IS NOT NULL;
CREATE INDEX movement_to ON movement (to_location_id, at);

-- ---------------------------------------------------------------------------
-- Full-text search. Maintained by app code (internal/items) on every item write:
-- delete the item's row, insert a fresh one. Not an external-content table.
-- ---------------------------------------------------------------------------

CREATE VIRTUAL TABLE item_fts USING fts5 (
  item_id UNINDEXED,
  title,
  category,
  notes,
  tags,
  observations,   -- observation text, newline-joined
  identifiers,    -- identifier value and value_norm, space-joined
  attrs,          -- selected projected attr values (brand, color, …)
  tokenize = 'unicode61 remove_diacritics 2',
  prefix = '2 3'
);

-- ---------------------------------------------------------------------------
-- Seeded registries
-- ---------------------------------------------------------------------------

INSERT INTO item_kind (kind, label, sort, description) VALUES
  ('clothing', 'Clothing', 1, 'Garments, shoes, accessories worn on the body'),
  ('object',   'Object',   2, 'Things: books, decor, tools, electronics, keepsakes'),
  ('utility',  'Utility',  3, 'Consumables and bulk supplies: cables, batteries, linens');

INSERT INTO media_role (role, label, sort, cover_rank, description) VALUES
  ('flat_lay',      'Flat lay',      1, 1,    'Whole item laid flat or on a hanger, plain background'),
  ('on_body',       'On body',       2, 3,    'Worn'),
  ('stock',         'Stock',         3, 2,    'Retailer or manufacturer photo'),
  ('label',         'Label',         4, NULL, 'Brand / size label (neck tag, woven label)'),
  ('care_label',    'Care label',    5, NULL, 'Care / composition label'),
  ('hang_tag',      'Hang tag',      6, NULL, 'Retail hang tag or sticker'),
  ('detail',        'Detail',        7, 5,    'Close-up of a feature'),
  ('defect',        'Defect',        8, NULL, 'Damage, stains, wear'),
  ('box_exterior',  'Box exterior',  9, 4,    'Outside of a box or container'),
  ('box_contents',  'Box contents', 10, 6,    'Open box showing what is inside'),
  ('receipt',       'Receipt',      11, NULL, 'Receipt or invoice'),
  ('location',      'Location',     12, 7,    'Photo of a place (room, unit, shelf)'),
  ('other',         'Other',        99, 8,    'Anything else');

INSERT INTO observation_source (source, label, sort, description) VALUES
  ('garment_label', 'Garment label', 1, 'Woven or printed brand/size label sewn in'),
  ('care_label',    'Care label',    2, 'Care and composition label'),
  ('hang_tag',      'Hang tag',      3, 'Retail hang tag, price sticker'),
  ('print',         'Print',         4, 'Text printed or embossed on the item itself'),
  ('packaging',     'Packaging',     5, 'Box, bag or wrapper'),
  ('receipt',       'Receipt',       6, 'Receipt, invoice, order confirmation'),
  ('user_note',     'Owner note',    7, 'Something the owner said about the item'),
  ('other',         'Other',        99, 'Anything else');

INSERT INTO fact_key (key, label, value_type, kinds, filterable, sort, description) VALUES
  ('brand',             'Brand',              'string',      '[]',            1, 1,  'Maker or label name as commonly written'),
  ('color',             'Color',              'string',      '[]',            1, 2,  'Main color in plain words: navy, olive, off-white'),
  ('size',              'Size',               'string',      '["clothing"]',  1, 3,  'Size as labelled: M, 32x30, EU 42, US 9'),
  ('season',            'Season',             'string',      '["clothing"]',  1, 4,  'Release season, e.g. 2023 Spring/Summer'),
  ('composition',       'Composition',        'composition', '["clothing"]',  0, 5,  '[{part, materials:[{name, pct}]}]'),
  ('material',          'Main material',      'string',      '[]',            1, 6,  'Dominant material in plain words: wool, cotton, nylon'),
  ('pattern',           'Pattern',            'string',      '["clothing"]',  0, 7,  'solid, striped, check, print …'),
  ('fit',               'Fit',                'string',      '["clothing"]',  0, 8,  'slim, regular, relaxed, oversized'),
  ('gender',            'Cut',                'string',      '["clothing"]',  0, 9,  'mens, womens, unisex, kids'),
  ('condition',         'Condition',          'string',      '[]',            1, 10, 'new, like_new, good, worn, damaged'),
  ('country_of_origin', 'Made in',            'string',      '[]',            0, 11, 'Country of manufacture as labelled'),
  ('model',             'Model',              'string',      '["object","utility"]', 0, 12, 'Model name or number'),
  ('style_name',        'Style name',         'string',      '["clothing"]',  0, 13, 'Retail product/style name'),
  ('purchase_date',     'Purchase date',      'date',        '[]',            0, 20, 'YYYY or YYYY-MM or YYYY-MM-DD'),
  ('purchase_price',    'Purchase price',     'money',       '[]',            0, 21, '{"amount": 129.0, "currency": "USD"}'),
  ('purchase_place',    'Bought at',          'string',      '[]',            0, 22, 'Store, site or city'),
  ('retail_price',      'Retail price',       'money',       '[]',            0, 23, 'Original list price: {"amount", "currency"}'),
  ('shell.technology',  'Shell technology',   'string',      '["clothing"]',  0, 30, 'e.g. GORE-TEX 3L'),
  ('length',            'Length',             'string',      '["utility","object"]', 0, 31, 'e.g. 1 m, 6 ft'),
  ('connector',         'Connector',          'string',      '["utility"]',   1, 32, 'e.g. USB-C to USB-C'),
  ('dimensions',        'Dimensions',         'string',      '["object"]',    0, 33, 'W×D×H with units');

INSERT INTO identifier_scheme (scheme, label, brand, description, pattern, example, notes) VALUES
  ('rn',       'RN number',     NULL, 'US FTC Registered Identification Number (identifies the company)', '^\d{4,6}$', '12345', 'Printed as "RN 12345" or "RN#12345". Store just the digits.'),
  ('ca',       'CA number',     NULL, 'Canadian CA identification number', '^\d{5,6}$', '12345', 'Printed as "CA 12345".'),
  ('wpl',      'WPL number',    NULL, 'Old US Wool Products Labeling number', '^\d+$', '1234', NULL),
  ('upc',      'UPC',           NULL, '12-digit UPC-A barcode', '^\d{12}$', '012345678905', NULL),
  ('ean',      'EAN / GTIN-13', NULL, '13-digit EAN barcode', '^\d{13}$', '4901234567894', NULL),
  ('jan',      'JAN',           NULL, 'Japanese Article Number (EAN-13 starting 45/49)', '^4[59]\d{11}$', '4901234567894', NULL),
  ('style',    'Style number',  NULL, 'Brand style / article number when the brand-specific scheme is unknown', NULL, 'AB1234-001', 'Prefer a brand-specific scheme (brand.style) once the pattern is known.'),
  ('sku',      'SKU',           NULL, 'Retailer stock keeping unit', NULL, NULL, NULL),
  ('model_number', 'Model number', NULL, 'Manufacturer model number (electronics, appliances)', NULL, 'A2337', NULL),
  ('serial',   'Serial number', NULL, 'Unique per-unit serial', NULL, NULL, 'Belongs on the item, never the product.'),
  ('order_number', 'Order number', NULL, 'Retailer order / receipt number', NULL, NULL, 'Belongs on the item.'),
  ('uniqlo.product_code', 'Uniqlo product code', 'Uniqlo', 'Uniqlo item code on the care label', '^\d{3}-\d{6}$', '341-456789', 'Format 3xx-xxxxxx; often followed by a season suffix in parentheses, e.g. (23SS).'),
  ('uniqlo.season_code',  'Uniqlo season code',  'Uniqlo', 'Season suffix after the product code', '^\d{2}(SS|FW|AW)$', '23SS', 'YY + SS (Spring/Summer) or FW/AW (Fall/Autumn-Winter).');
