# Ingest playbook (for agents)

You are turning the owner's photos into inventory records. Follow this
document exactly. It works for a Claude session with shell access and this
repo (`inv` CLI), or with plain HTTP access to the server.

The output of your work is **one bundle** (JSON) applied with
`POST /api/ingest/bundle` (or `inv ingest`). The owner then reviews
everything in the app's Inbox. Your job is to record what is on the photos
faithfully, keep what you *saw* apart from what you *concluded*, and never
make anything up.

## Ground rules

1. **Verbatim, never guess.** Transcribe labels character for character.
   If you can't read a character, write `[?]` in its place
   (`C[?]LD`, `RN 1234[?]`). Never "fix" spelling, never complete a cut-off
   code from memory.
2. **Observed vs. derived.** Text on the item goes in `observations` (and
   `identifiers`). Everything you conclude goes in `facts`, with a `basis`,
   an honest `confidence` and `sources`.
3. **Always `needs_review`.** Every item you create is `review_state:
   needs_review` (the default). Never set `ok`; that is the owner's call.
4. **When unsure, split.** If you can't tell whether two photos show the
   same thing, make two items and say so in `notes`. Merging later is easy;
   untangling a wrong merge is not.
5. **Dry run first**, then apply, then report back (§10).
6. **Reuse vocabulary.** Read `/api/schema` and reuse existing fact keys,
   identifier schemes, media roles and observation sources. Extend the
   registries deliberately (§5), never with one-off spellings.

## 1. Before you start

```sh
SERVER=${INV_SERVER:-http://localhost:8480}
curl -s $SERVER/api/health     # status ok, schema_version, auth_required
curl -s $SERVER/api/schema     # kinds, media_roles, observation_sources, fact_keys, identifier_schemes
curl -s "$SERVER/api/locations"   # location tree with codes (for code:<CODE> refs)
```

- If `auth_required` is true, every write needs `Authorization: Bearer
  $INV_TOKEN` (`inv ingest` reads `INV_TOKEN` or `--token`). Ask the owner
  for the token; don't search for it.
- Identify yourself: set `defaults.recorded_by` to `agent:<your model id>`,
  e.g. `agent:claude-opus-5-5`. (Without it the server uses the `X-Actor`
  header, else `user`, which would be wrong for agent work.)
- Ask the owner where the items are now and where they're going, if they
  haven't said. Use location codes: `"location": "code:BEDROOM"`,
  `"target_location": "code:U2"`. Don't invent locations; if the code
  doesn't exist, ask (or leave location out: "unknown" is a valid state).

## 2. Pick the workflow

### A. Inbox-driven (the usual case)

The owner uploaded photos to the Inbox (phone or desktop), maybe with a
context note per photo.

```sh
curl -s "$SERVER/api/inbox?limit=500"
```

`media[]` is oldest first by `taken_at`, so bursts stay together. Each
entry has `id`, `sha256`, `taken_at`, `original_name`, `context` (the
owner's note), `width`/`height` and `display_url` (a 1600 px WebP; view it,
the HEIC original is `orig_url`). If `display_url` is null the variants are
still being made; wait a minute and re-read the inbox. Page with
`?cursor=<next_cursor>`.

Write the bundle with **`media_id`** references:
`{"media_id": "01J…", "role": "flat_lay"}`, and in observations
`"media": "01J…"`. Apply it with curl (§9). Photos you link leave the Inbox.
Photos you don't use stay there; mention them in your report.

### B. Folder-driven

The owner exported originals into a folder, optionally with sidecar notes:
`IMG_4101.txt` (or `IMG_4101.HEIC.txt`) next to a photo, and/or a
`context.txt` for the whole folder. Read every sidecar: they are the
owner's words (basis `user`).

Write `bundle.json` **in that folder** with **`path`** references relative
to it: `{"path": "IMG_4101.HEIC", "role": "flat_lay"}`, observation
`"media": "IMG_4102.JPG"`, reference `"archive": {"path": "page.html"}`.
Then:

```sh
inv ingest bundle.json --server $SERVER --dry-run   # validates, uploads nothing
inv ingest bundle.json --server $SERVER             # uploads missing files (by sha256), applies
```

`inv ingest` checks that every path exists, hashes each file, asks the
server which it already has (`HEAD /api/media/sha256/<sha>`), validates the
bundle with a dry run, uploads only the missing files and then applies the
bundle. A media entry without `context` gets its sidecar text (else
`context.txt`) as the photo's context. Without the repo, do the same by
hand: `POST /api/media` (multipart, field `file`) for each file, then use
`"sha256": "<hex>"` or the returned `id` in the bundle.

## 3. Group photos into items

Typical input: iPhone photos, **1–2 per garment**, mixed with unrelated
shots. Many items will have no label photo at all; that's normal.

Use, in this order:

1. **The owner's context notes.** "the grey one not the navy" overrides
   your eyes.
2. **EXIF `taken_at` bursts.** Photos seconds apart usually show one item
   (flat lay, then its label). A gap of minutes plus a different background
   or subject usually means a new item.
3. **Visual similarity**: same fabric, color, print, hardware, background.
   A label photo belongs to the garment whose color and texture are visible
   around the label.

Rules:

- One physical thing = one item. Several identical things you don't need to
  track one by one (cables, socks) = one item with `quantity`.
- Sets (suit = jacket + trousers): one item per piece, plus `parent` (the
  set item's ext_key) when the owner treats them as a set.
- Unsure? Separate items, and say it in `notes`: "May be the same jacket as
  batch-2026-11-01/navy-shell-jacket (similar color; no shared label)."
- Photos you can't place (blurry, unrelated): leave them out and list them
  in your report.
- An item can have no photos (the owner describes something).

## 4. Transcribe labels (observations)

One observation per physical label or print surface you read: `source` is
one of `/api/schema` `observation_sources` (`garment_label`, `care_label`,
`hang_tag`, `print`, `packaging`, `receipt`, `user_note`, `other`).
Set `media` to the photo it was read from (and also list that photo in the
item's `media` with a matching role such as `care_label`).

- `text`: **exactly** what is printed, top to bottom, one line per printed
  line, original capitalization and punctuation, `[?]` for each unreadable
  character. Translate nothing. Don't transcribe care symbols as words
  unless words are printed; describe symbols in `data.care_symbols` only if
  you are sure of them.
- `data`: the same content split into structure, **no inference**. Only
  restructure what is printed. Useful shapes:

```jsonc
{
  "composition": [                      // as printed, one entry per part
    { "part": "shell",  "materials": [{ "name": "nylon", "pct": 100 }] },
    { "part": "lining", "materials": [{ "name": "polyester", "pct": 65 }, { "name": "cotton", "pct": 35 }] }
  ],
  "codes": ["RN 12345", "341-456789(23SS)"],   // every code-like string, verbatim
  "size": { "label": "M", "systems": { "US": "M", "EU": "48" } },  // only systems that are printed
  "country_of_origin": "VIETNAM",             // as printed (MADE IN …)
  "care": ["MACHINE WASH COLD"],               // printed care text lines
  "brand_text": "UNIQLO"                       // a brand name only if printed
}
```

`part` names: use the printed word in lower case (`shell`, `lining`,
`body`, `trim`, `fill`…); material names lower case, as printed (`nylon`,
`recycled polyester`). If a percentage is unreadable, put the text in
`text` with `[?]` and leave that material out of `data`.

If the owner says something about the item ("bought in Tokyo 2019"), that
is not an observation of the item; it becomes a `user` fact (§7).

## 5. Identifiers

Every code you transcribe that identifies the product, the company or the
unit also goes in `identifiers`, pointing at its observation:
`{"scheme": "rn", "value": "12345", "observation": 0}` (`observation` is an
index into the same item's `observations`).

- `value` is the code **as printed** (`341-456789`, not `341456789`); the
  server normalizes it for matching (upper case, no spaces/dashes/dots).
- **Scheme choice**: reuse a scheme from `/api/schema identifier_schemes`
  whenever it fits (`rn`, `ca`, `upc`, `ean`, `jan`, `style`, `sku`,
  `model_number`, `serial`, `order_number`, `uniqlo.product_code`,
  `uniqlo.season_code`…). Read each scheme's `notes`: they hold what we've
  learned (e.g. "Uniqlo 3xx-xxxxxx; suffix = season").
- **New brand-specific pattern?** Use `style` the first time. Once you've
  seen the pattern on several items and are confident, register a scheme
  and say what you know:

```sh
curl -s -X PUT $SERVER/api/schema/identifier-schemes/patagonia.style \
  -H 'Content-Type: application/json' -H "Authorization: Bearer $INV_TOKEN" \
  -d '{"label": "Patagonia style number", "brand": "Patagonia",
       "pattern": "^\\d{5}$", "example": "84212",
       "notes": "5 digits on the care label after STY; the color code follows separately."}'
```

  Scheme names are lower case, `brand.kind` for brand-specific schemes.
  The same goes for fact keys (`PUT /api/schema/fact-keys/<key>`): reuse
  first, add only keys you'll use repeatedly.
- **Look up before creating.** For each product or unit identifier
  (anything except company ids like `rn`/`ca`):

```sh
curl -s "$SERVER/api/identifiers/lookup?value=341-456789"
```

  A match on another item may mean the owner already has this item
  recorded (a duplicate) or owns two of the same product (fine). Don't
  merge or skip on your own: create the item, mention the match in `notes`
  and in your report. A match on a **product** means you should link that
  product (`"product": {"ext_key": "<its ext_key>"}`) instead of declaring a
  new one. The bundle result also warns about such matches.

## 6. Products

A `product` is the catalog thing ("Uniqlo Blocktech Parka, 23SS"); the
item is the owner's physical copy. Declare a product only when the label
gives you something product-level (a product/style code, a model number).
Product-level identifiers (product code, UPC) go on the product; unit-level
ones (serial, order number) and the RN go on the item. Products are declared
once in `products[]` and linked from items with `{"ext_key": "…"}`.

Product ext_key: `<brand-slug>/<product code>[/<season or version>]`,
e.g. `uniqlo/341-456789/23SS`, `apple/A2337`. Without a code, don't create
a product; the enrich pass may.

If the product **already exists** (an earlier bundle declared it), link it
with `{"ext_key": "…"}` only. To add identifiers or facts to an existing
product, give each one an explicit `ext_key` (e.g.
`uniqlo/341-456789/23SS#batch-2026-11-08/id/upc`): derived keys are by
position and would collide with the rows the first bundle created.

## 7. Facts

Facts are claims with provenance. Pick keys from `fact_keys` (`brand`,
`color`, `size`, `season`, `composition`, `material`, `country_of_origin`,
`purchase_date`, `purchase_place`, `purchase_price`, …).

| basis | Use when | confidence | sources |
|---|---|---|---|
| `observed` | The value is **read off the item**: a printed size, composition, "MADE IN" | 1.0 (lower if partly `[?]`) | `[{"observation": i}]` |
| `inferred` | You concluded it: color from a photo, brand from a code format, season from a suffix convention | 0.5–0.9, honestly | the observation and/or a `{"note": "why"}` |
| `user` | The owner said it (photo context, sidecar, chat) | 1.0 | `[{"note": "owner's context note on IMG_4101"}]` |
| `looked_up` | From an external page (enrich pass only) | see ENRICH_PLAYBOOK | `[{"reference": i}]` |

- A brand is `observed` only if the brand **name** is printed on a label
  you transcribed. A brand recognized from a code format or a logo shape is
  `inferred`.
- Colors are `inferred` from photos (lighting lies): use plain words
  (`navy`, `olive`, `off-white`), confidence ≤ 0.8.
- Values: strings for most keys; `composition` mirrors the observation's
  `data.composition`; money is `{"amount": 129.0, "currency": "USD"}`;
  dates are `YYYY`, `YYYY-MM` or `YYYY-MM-DD`.
- Never state a fact you can't source. "Probably a 2019 model" without a
  reason is not a fact; leave it out or put the hunch in `notes`.
- Never write `user` facts that the owner didn't actually state.

## 8. Item fields

- `kind`: `clothing` | `object` | `utility` (consumables, cables, linens).
- `title`: short and plain, what you'd say out loud: "Navy shell jacket",
  "USB-C to USB-C cable, black". No brand unless it's printed.
- `category`: a path inside the kind, reuse existing ones (see
  `GET /api/facets`): `outerwear/shell`, `tops/t-shirt`, `cable/usb-c`.
- `notes`: how you grouped the photos, doubts, possible duplicates.
- `tags`: only if the owner uses tags or asked for them.
- `media[].role`: from `media_roles` (`flat_lay`, `on_body`, `label`,
  `care_label`, `hang_tag`, `detail`, `defect`, …). `cover` (an index into
  `media`) only if the best overall photo isn't obvious.
- `location` / `target_location` in `defaults` apply when an item is
  **created** (later re-applies never undo a move). If the item already sits
  in (or inside) the target, the target is dropped.

## 9. Bundle, ext_keys and applying

**ext_key** makes the bundle idempotent. Use `batch-YYYY-MM-DD/<slug>` with
the date of the session and a slug from the title:
`batch-2026-11-01/navy-shell-jacket`, `-2`, `-3` for look-alikes. Keep the
bundle file; to fix something, edit it and apply it again.

What a re-apply does (matching by ext_key):

- Items: updates `title`, `kind`, `category`, `quantity`, `notes`,
  `product`, `parent`, `cover` when given and different; `review_state`
  only if the item itself sets it; tags are added, never removed; location
  is never touched again. Note the owner may have edited a title in the
  app since: re-read the item (`GET /api/items/<id>`) and keep their edits
  before re-applying an old bundle.
- Observations, identifiers, facts and references get derived ext_keys
  (`<item ext_key>#obs/0`, `#id/0`, `#fact/0`, `#ref/0`), so **keep their
  order** when editing a bundle; append new ones at the end. Unchanged rows
  are left alone. A changed observation is added as a new row (the old one
  stays as history); a changed identifier/reference is corrected in place; a
  changed fact supersedes the old one (history kept). Facts the owner
  rejected stay rejected.
- An unchanged bundle writes nothing at all.

Apply:

```sh
# folder workflow
inv ingest bundle.json --server $SERVER --dry-run
inv ingest bundle.json --server $SERVER
# inbox workflow (media_id refs)
curl -s -X POST "$SERVER/api/ingest/bundle?dry_run=true" -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $INV_TOKEN" --data @bundle.json
curl -s -X POST "$SERVER/api/ingest/bundle" -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $INV_TOKEN" --data @bundle.json
```

A `422` means nothing was written; `error.details` lists **every** problem
as `{path, message}` (e.g. `items[3].kind`, `items[0].media[1]: unknown
sha256 …`). Fix them all and retry. The `200` result lists each item and
product with `action` (`created`/`updated`/`unchanged`), `changes` and
`warnings` (possible duplicates, label photos missing from `media`).

## 10. Report back to the owner

Finish with a short message:

- How many items you created/updated, grouped by kind, each with its title
  and app link (`$SERVER/items/<id>`); they are in the Inbox for review.
- Every doubt: uncertain groupings, `[?]` readings, possible duplicates
  (with the matching item), photos you didn't use.
- Registry changes you made (new schemes/keys) and why.
- What the enrich pass could add (items with product codes).

## Worked example (folder workflow)

The folder holds `IMG_4101.HEIC` (flat lay), `IMG_4102.JPG` (its care
label, 40 s later), `IMG_4103.HEIC` (a T-shirt, no label shot),
`IMG_4104.JPG` (two cables), the sidecar `IMG_4101.txt` ("Bought in Tokyo
in 2019. The navy one, not the black one.") and `context.txt`. Locations
`BEDROOM` and `U2` exist. This exact bundle is applied twice by the test
suite (`internal/ingest/testdata/folder/`); the second run changes nothing.

<!-- worked-example: internal/ingest/testdata/folder/bundle.json -->
```json
{
  "defaults": {
    "location": "code:BEDROOM",
    "target_location": "code:U2",
    "review_state": "needs_review",
    "recorded_by": "agent:claude-opus-5-5"
  },
  "products": [
    {
      "ext_key": "uniqlo/341-456789/23SS",
      "brand": "Uniqlo",
      "name": "Blocktech Parka (23SS)",
      "identifiers": [
        { "scheme": "uniqlo.product_code", "value": "341-456789" },
        { "scheme": "uniqlo.season_code", "value": "23SS" }
      ],
      "facts": [
        {
          "key": "season", "value": "2023 Spring/Summer", "basis": "inferred", "confidence": 0.8,
          "sources": [{ "note": "Uniqlo season suffix: 23SS = 2023 Spring/Summer (identifier scheme uniqlo.season_code)" }]
        }
      ]
    }
  ],
  "items": [
    {
      "ext_key": "batch-2026-11-01/navy-shell-jacket",
      "kind": "clothing",
      "title": "Navy shell jacket",
      "category": "outerwear/shell",
      "notes": "Grouped from IMG_4101 (flat lay) and IMG_4102 (care label), taken 40 s apart. Product name not confirmed; see the enrich pass.",
      "tags": ["rain"],
      "product": { "ext_key": "uniqlo/341-456789/23SS" },
      "media": [
        { "path": "IMG_4101.HEIC", "role": "flat_lay" },
        { "path": "IMG_4102.JPG", "role": "care_label" }
      ],
      "cover": 0,
      "observations": [
        {
          "source": "care_label",
          "media": "IMG_4102.JPG",
          "text": "SHELL: 100% NYLON\nLINING: 100% POLYESTER\nRN 12345\nMADE IN VIETNAM\n341-456789(23SS)\nMACHINE WASH C[?]LD",
          "data": {
            "composition": [
              { "part": "shell", "materials": [{ "name": "nylon", "pct": 100 }] },
              { "part": "lining", "materials": [{ "name": "polyester", "pct": 100 }] }
            ],
            "country_of_origin": "VIETNAM",
            "codes": ["RN 12345", "341-456789(23SS)"],
            "care": ["MACHINE WASH C[?]LD"]
          }
        }
      ],
      "identifiers": [
        { "scheme": "rn", "value": "12345", "observation": 0 },
        { "scheme": "uniqlo.product_code", "value": "341-456789", "observation": 0 },
        { "scheme": "uniqlo.season_code", "value": "23SS", "observation": 0 }
      ],
      "facts": [
        {
          "key": "composition", "basis": "observed", "confidence": 1,
          "value": [
            { "part": "shell", "materials": [{ "name": "nylon", "pct": 100 }] },
            { "part": "lining", "materials": [{ "name": "polyester", "pct": 100 }] }
          ],
          "sources": [{ "observation": 0 }]
        },
        { "key": "country_of_origin", "value": "Vietnam", "basis": "observed", "confidence": 1, "sources": [{ "observation": 0 }] },
        {
          "key": "brand", "value": "Uniqlo", "basis": "inferred", "confidence": 0.9,
          "sources": [{ "observation": 0 }, { "note": "341-xxxxxx(23SS) is the Uniqlo product code format; no brand name is printed on this label" }]
        },
        { "key": "color", "value": "navy", "basis": "inferred", "confidence": 0.7, "sources": [{ "note": "judged from the flat-lay photo IMG_4101" }] },
        { "key": "purchase_place", "value": "Tokyo", "basis": "user", "confidence": 1, "sources": [{ "note": "owner's context note on IMG_4101" }] },
        { "key": "purchase_date", "value": "2019", "basis": "user", "confidence": 1, "sources": [{ "note": "owner's context note on IMG_4101" }] }
      ]
    },
    {
      "ext_key": "batch-2026-11-01/grey-crew-tee",
      "kind": "clothing",
      "title": "Grey crew-neck T-shirt",
      "category": "tops/t-shirt",
      "notes": "Only a flat lay; no label photographed, so nothing observed.",
      "media": [{ "path": "IMG_4103.HEIC", "role": "flat_lay" }],
      "facts": [
        { "key": "color", "value": "grey", "basis": "inferred", "confidence": 0.6, "sources": [{ "note": "judged from the flat-lay photo IMG_4103" }] }
      ]
    },
    {
      "ext_key": "batch-2026-11-01/usb-c-cables",
      "kind": "utility",
      "title": "USB-C to USB-C cable, black",
      "category": "cable/usb-c",
      "quantity": 2,
      "media": [{ "path": "IMG_4104.JPG", "role": "detail" }],
      "observations": [
        { "source": "print", "media": "IMG_4104.JPG", "text": "1M 60W", "data": { "codes": ["1M 60W"] } }
      ],
      "facts": [
        { "key": "length", "value": "1 m", "basis": "observed", "confidence": 1, "sources": [{ "observation": 0 }] },
        { "key": "connector", "value": "USB-C to USB-C", "basis": "inferred", "confidence": 0.8, "sources": [{ "note": "both plug ends visible in IMG_4104" }] }
      ]
    }
  ]
}
```

Things to notice: the RN is on the item (it names the company), the
product code and season on the product; `brand` is `inferred` because no
brand name is printed; the `[?]` stays in both `text` and `data`; the T-shirt
has no observations at all; the owner's sidecar note became two `user`
facts; `IMG_4102.txt` doesn't exist, so that photo gets `context.txt` as its
context.

**The same item in the inbox workflow** differs only in its media
references (ids from `GET /api/inbox`):

```json
"media": [
  { "media_id": "01JC4M3F6R6Q4Q0J8N7V2Y5A1B", "role": "flat_lay" },
  { "media_id": "01JC4M3G0K1T8W3E5S2D9F7H4C", "role": "care_label" }
],
"observations": [
  { "source": "care_label", "media": "01JC4M3G0K1T8W3E5S2D9F7H4C", "text": "…" }
]
```
