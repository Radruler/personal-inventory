# Enrich playbook (for agents)

The follow-up pass after ingest. For items whose labels gave a product-level
clue (a product or style code, a model number, a UPC), find the product's
page, **archive it** (pages disappear), attach the stock photos to the
**product**, and add `looked_up` facts that cite the archived page. Read
`INGEST_PLAYBOOK.md` first: the same server checks, vocabulary rules, bundle
semantics and reporting apply here.

## Ground rules

1. **Exact match or nothing.** Only use a page that shows the same product
   code (and season/version where the code has one). A "similar" product
   is not this product. If you can't confirm, record nothing and tell the
   owner.
2. **Everything looked up cites its source**: a `reference` with an
   archived snapshot, and facts with `basis: looked_up` whose `sources`
   point at that reference.
3. **Never overwrite the owner.** Don't supersede or reject `user` facts,
   and don't fight `observed` ones; projection already ranks
   `user` > `observed` > `looked_up` > `inferred`. If a page contradicts
   what the owner said or what the label reads, add nothing for that key
   and report the conflict.
4. **Product-level data goes on the product**: stock photos, style name,
   retail price, release season, technologies. Item-level data stays on the
   item: size, condition, purchase price/date, defects.
5. Dry run, apply, report back, as in the ingest playbook.

## 1. Pick candidates

```sh
SERVER=${INV_SERVER:-http://localhost:8480}
curl -s "$SERVER/api/products?limit=200"              # products, with item_count
curl -s "$SERVER/api/products/<id>"                   # identifiers, facts, references, media
curl -s "$SERVER/api/items/<id>"                      # an item's identifiers and observations
curl -s "$SERVER/api/identifiers/lookup?value=341-456789"
```

Good candidates: products with identifiers but no `references`; items with
a product code or UPC but no product (declare one, see §5).

## 2. Find the product page

- Search for the exact code with the brand: `"341-456789" uniqlo`. Prefer
  the manufacturer's own page, then a large retailer, then an archived copy
  (web.archive.org) of either. Listings by resellers are kind `listing`,
  not `product_page`.
- Check the page shows the code (or the code is in its URL/SKU field),
  the colorway if the code includes one, and the season/version.
- Note the page's own product name, list price with currency, materials
  and features. Copy names as written on the page.

## 3. Archive the page and the stock photos

Save what you used, before writing facts:

- **Snapshot**: the page's HTML (`curl -sL -o page.html <url>`) or a PDF
  print of it. HTML and PDF are stored as-is and served sandboxed. One
  snapshot per page.
- **Stock photos**: the product's own images from that page (not
  unrelated banners), full size, original format (JPEG/PNG/WebP). Usually
  1–4 is enough: the main shot first.

Folder workflow: put the files next to the bundle and reference them by
`path` (`inv ingest` uploads them). HTTP-only: `POST /api/media` each file
(multipart field `file`) and use the returned `sha256` or `id`.

## 4. Write the bundle

A product-only bundle (`"items": []`) is fine. Declare the product with the
same `ext_key` it already has (see `GET /api/products/<id>`; ingest uses
`<brand-slug>/<product code>[/<season>]`) so it is updated, not duplicated.

**Give every reference and fact an explicit `ext_key`.** Without one, a
nested row's key is derived from its position (`<product ext_key>#fact/0`),
and the ingest bundle that declared the product already used those
positions: your first fact would supersede its first fact. Use
`<product ext_key>#enrich/<what>`, e.g. `uniqlo/341-456789/23SS#enrich/ref/uniqlo-us`,
`…#enrich/fact/retail_price`. Keep them stable: re-applying the same key
with a new value supersedes the old value.

- `references[]`: `kind` (`product_page`, `listing`, `review`, `receipt`,
  `other`), `url`, `title` (the page title), `fetched_at` (UTC, when you
  downloaded it), `archive` (the snapshot), `notes` (what you checked:
  "code 341-456789 and 23SS shown under the size chart").
- `media[]`: stock photos with `"role": "stock"` (they become the cover of
  items that have no flat lay).
- `facts[]` with `basis: looked_up` and `sources: [{"reference": i}]`
  (an index into this product's `references`). Confidence:

| Source | confidence |
|---|---|
| Manufacturer page, exact code and season | 0.9 |
| Major retailer, exact code | 0.75 |
| Archived copy / reseller listing, exact code | 0.6 |
| Anything less certain | don't record it |

Values follow the registry types: `retail_price` is
`{"amount": 79.9, "currency": "USD"}` (the list price, not a sale price;
say in `notes` which market), `style_name` is the page's product name,
`season` is like `2023 Spring/Summer`. Reuse keys from `/api/schema`;
register a new key (`PUT /api/schema/fact-keys/<key>`) only when it'll be
used across products (e.g. `shell.technology`).

<!-- enrich-example: applied by internal/ingest tests after the ingest example -->
```json
{
  "defaults": { "recorded_by": "agent:claude-opus-5-5" },
  "products": [
    {
      "ext_key": "uniqlo/341-456789/23SS",
      "name": "Blocktech Parka (23SS)",
      "media": [
        { "path": "stock-341-456789-1.jpg", "role": "stock", "caption": "Navy, front (uniqlo.com)" }
      ],
      "references": [
        {
          "ext_key": "uniqlo/341-456789/23SS#enrich/ref/uniqlo-us",
          "kind": "product_page",
          "url": "https://www.uniqlo.com/us/en/products/E341456-000",
          "title": "BLOCKTECH Parka | UNIQLO US",
          "fetched_at": "2026-11-02T09:30:00.000Z",
          "archive": { "path": "uniqlo-341-456789.html" },
          "notes": "Product code 341-456789 shown in the item details; season 23SS in the page's product data."
        }
      ],
      "facts": [
        { "ext_key": "uniqlo/341-456789/23SS#enrich/fact/style_name", "key": "style_name", "value": "BLOCKTECH Parka", "basis": "looked_up", "confidence": 0.9, "sources": [{ "reference": 0 }] },
        {
          "ext_key": "uniqlo/341-456789/23SS#enrich/fact/retail_price",
          "key": "retail_price", "value": { "amount": 79.9, "currency": "USD" }, "basis": "looked_up", "confidence": 0.9,
          "sources": [{ "reference": 0 }], "note": "US list price on the archived page"
        },
        { "ext_key": "uniqlo/341-456789/23SS#enrich/fact/shell.technology", "key": "shell.technology", "value": "BLOCKTECH (windproof, water-repellent)", "basis": "looked_up", "confidence": 0.9, "sources": [{ "reference": 0 }] }
      ]
    }
  ],
  "items": []
}
```

Apply it like an ingest bundle (`inv ingest enrich.json --dry-run`, then
without `--dry-run`). Re-applying is safe: unchanged rows are left alone.

## 5. Products that don't exist yet

If an item has a product code but no product: declare the product in
`products[]` (brand, name from the page, the product-level identifiers
moved from the label, the facts above) and link the item. Two ways:

- `PATCH /api/items/<id>` with `{"product_id": "<new product id>"}` after
  the bundle created the product (the result lists its id); or
- include the item in the bundle by its ext_key with
  `"product": {"ext_key": "…"}`. A bundle item must restate `kind` and
  `title`; copy them from `GET /api/items/<id>` so you don't undo the
  owner's edits.

## 6. Corrections

- To correct a fact you (or an earlier agent) wrote: change its value in
  the bundle that created it and re-apply. Same ext_key (explicit, or the
  same position) means the old fact is **superseded** (history kept), not
  duplicated.
- Outside a bundle: `POST /api/facts/<id>/supersede` with
  `{"value": …, "basis": "looked_up", "confidence": …, "sources": [{"reference_id": "…"}], "note": "why"}`.
- A fact that is simply wrong: `POST /api/facts/<id>/reject` with
  `{"note": "why"}`. Never reject or supersede `user` facts; ask the owner.

## 7. Report back

List per product: the page you used (URL), what you archived, the facts you
added with their confidence, and anything you couldn't confirm (no exact
match, conflicting data, owner-stated values you left alone).
