package media

// Just enough ISOBMFF / HEIF (ISO/IEC 14496-12, ISO/IEC 23008-12) to read
// the item structure of a HEIC file and to write small grid files.
//
// Why: libvips decodes a HEIC grid (every iPhone photo is a grid of 512x512
// HEVC tiles) through libheif in one piece, so a 48 MP "HEIF Max" photo needs
// ~150 MB for the RGB buffer plus ~75 MB for the YCbCr planes. That alone
// breaks the 150 MB peak budget (PLAN.md §2). Instead, GenerateVariants cuts
// the grid into horizontal bands of whole tiles, writes each band as its own
// tiny grid HEIC (the HEVC bitstreams are copied, not re-encoded), lets vips
// decode and box-shrink one band at a time, and stitches the shrunk bands.

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

var errNotHEIF = errors.New("not a HEIF file")

type heifAssoc struct {
	index     int // 1-based index into ipco
	essential bool
}

type heifExtent struct{ off, len uint64 }

type heifItem struct {
	id      uint32
	typ     string
	hidden  bool
	props   []heifAssoc
	method  int // iloc construction method: 0 file offset, 1 idat
	base    uint64
	extents []heifExtent
}

type heifFile struct {
	r       io.ReaderAt
	size    int64
	major   string
	compat  []string
	primary uint32
	items   map[uint32]*heifItem
	props   [][]byte // raw property boxes (header included); props[i-1] is ipco index i
	refs    map[string]map[uint32][]uint32
	idat    []byte
}

// maxMetaBox caps how much of a meta box we read into memory.
const maxMetaBox = 16 << 20

// parseHEIF reads the ftyp and meta boxes of a HEIF file.
func parseHEIF(r io.ReaderAt, size int64) (*heifFile, error) {
	f := &heifFile{r: r, size: size, items: map[uint32]*heifItem{}, refs: map[string]map[uint32][]uint32{}}
	var meta []byte
	var off int64
	first := true
	for off < size {
		var hdr [16]byte
		n, _ := r.ReadAt(hdr[:], off)
		if n < 8 {
			break
		}
		bsize := int64(binary.BigEndian.Uint32(hdr[0:4]))
		typ := string(hdr[4:8])
		hlen := int64(8)
		switch bsize {
		case 1:
			if n < 16 {
				return nil, errNotHEIF
			}
			bsize = int64(binary.BigEndian.Uint64(hdr[8:16]))
			hlen = 16
		case 0:
			bsize = size - off
		}
		if bsize < hlen || off+bsize > size {
			return nil, fmt.Errorf("heif: bad %q box size", typ)
		}
		if first && typ != "ftyp" {
			return nil, errNotHEIF
		}
		first = false
		switch typ {
		case "ftyp":
			b := make([]byte, bsize-hlen)
			if _, err := r.ReadAt(b, off+hlen); err != nil {
				return nil, err
			}
			if len(b) < 8 {
				return nil, errNotHEIF
			}
			f.major = string(b[0:4])
			for i := 8; i+4 <= len(b); i += 4 {
				f.compat = append(f.compat, string(b[i:i+4]))
			}
		case "meta":
			if bsize-hlen > maxMetaBox {
				return nil, errors.New("heif: meta box too large")
			}
			meta = make([]byte, bsize-hlen)
			if _, err := r.ReadAt(meta, off+hlen); err != nil {
				return nil, err
			}
		}
		off += bsize
	}
	if meta == nil {
		return nil, errors.New("heif: no meta box")
	}
	if len(meta) < 4 {
		return nil, errors.New("heif: short meta box")
	}
	children, err := splitBoxes(meta[4:]) // meta is a FullBox
	if err != nil {
		return nil, err
	}
	for _, c := range children {
		if err := f.parseMetaChild(c); err != nil {
			return nil, fmt.Errorf("heif %s: %w", c.typ, err)
		}
	}
	if f.primary == 0 || f.items[f.primary] == nil {
		return nil, errors.New("heif: no primary item")
	}
	return f, nil
}

type rawBox struct {
	typ     string
	full    []byte // header + payload
	payload []byte
}

func splitBoxes(b []byte) ([]rawBox, error) {
	var out []rawBox
	for len(b) > 0 {
		if len(b) < 8 {
			return nil, errors.New("truncated box")
		}
		size := uint64(binary.BigEndian.Uint32(b[0:4]))
		typ := string(b[4:8])
		hlen := uint64(8)
		switch size {
		case 1:
			if len(b) < 16 {
				return nil, errors.New("truncated box")
			}
			size = binary.BigEndian.Uint64(b[8:16])
			hlen = 16
		case 0:
			size = uint64(len(b))
		}
		if size < hlen || size > uint64(len(b)) {
			return nil, fmt.Errorf("bad %q box size", typ)
		}
		out = append(out, rawBox{typ: typ, full: b[:size], payload: b[hlen:size]})
		b = b[size:]
	}
	return out, nil
}

// rd is a big-endian cursor that records the first overrun.
type rd struct {
	b   []byte
	bad bool
}

func (r *rd) take(n int) []byte {
	if r.bad || n < 0 || n > len(r.b) {
		r.bad = true
		return make([]byte, max(n, 0))
	}
	v := r.b[:n]
	r.b = r.b[n:]
	return v
}
func (r *rd) u8() uint8   { return r.take(1)[0] }
func (r *rd) u16() uint16 { return binary.BigEndian.Uint16(r.take(2)) }
func (r *rd) u32() uint32 { return binary.BigEndian.Uint32(r.take(4)) }
func (r *rd) u64() uint64 { return binary.BigEndian.Uint64(r.take(8)) }
func (r *rd) uN(n int) uint64 {
	switch n {
	case 0:
		return 0
	case 4:
		return uint64(r.u32())
	case 8:
		return r.u64()
	}
	r.bad = true
	return 0
}
func (r *rd) id(wide bool) uint32 {
	if wide {
		return r.u32()
	}
	return uint32(r.u16())
}
func (r *rd) fullHeader() (version uint8, flags uint32) {
	v := r.u32()
	return uint8(v >> 24), v & 0xffffff
}
func (r *rd) err() error {
	if r.bad {
		return errors.New("truncated")
	}
	return nil
}

func (f *heifFile) item(id uint32) *heifItem {
	it := f.items[id]
	if it == nil {
		it = &heifItem{id: id}
		f.items[id] = it
	}
	return it
}

func (f *heifFile) parseMetaChild(c rawBox) error {
	r := &rd{b: c.payload}
	switch c.typ {
	case "pitm":
		v, _ := r.fullHeader()
		f.primary = r.id(v > 0)
	case "iinf":
		v, _ := r.fullHeader()
		if v == 0 {
			r.u16()
		} else {
			r.u32()
		}
		if err := r.err(); err != nil {
			return err
		}
		entries, err := splitBoxes(r.b)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if e.typ != "infe" {
				continue
			}
			er := &rd{b: e.payload}
			ev, flags := er.fullHeader()
			if ev < 2 {
				continue // pre-HEIF item info; not used by images
			}
			id := er.id(ev >= 3)
			er.u16() // protection index
			typ := string(er.take(4))
			if err := er.err(); err != nil {
				return err
			}
			it := f.item(id)
			it.typ = typ
			it.hidden = flags&1 != 0
		}
	case "iloc":
		v, _ := r.fullHeader()
		s := r.u16()
		offSize, lenSize := int(s>>12), int(s>>8&0xf)
		baseSize, idxSize := int(s>>4&0xf), int(s&0xf)
		var count uint32
		if v < 2 {
			count = uint32(r.u16())
		} else {
			count = r.u32()
		}
		for i := uint32(0); i < count && !r.bad; i++ {
			it := f.item(r.id(v >= 2))
			if v == 1 || v == 2 {
				it.method = int(r.u16() & 0xf)
			}
			r.u16() // data_reference_index
			it.base = r.uN(baseSize)
			n := r.u16()
			it.extents = it.extents[:0]
			for j := uint16(0); j < n && !r.bad; j++ {
				if (v == 1 || v == 2) && idxSize > 0 {
					r.uN(idxSize)
				}
				o := r.uN(offSize)
				l := r.uN(lenSize)
				it.extents = append(it.extents, heifExtent{o, l})
			}
		}
	case "iref":
		v, _ := r.fullHeader()
		if err := r.err(); err != nil {
			return err
		}
		refs, err := splitBoxes(r.b)
		if err != nil {
			return err
		}
		for _, ref := range refs {
			rr := &rd{b: ref.payload}
			from := rr.id(v > 0)
			n := rr.u16()
			to := make([]uint32, 0, n)
			for i := uint16(0); i < n && !rr.bad; i++ {
				to = append(to, rr.id(v > 0))
			}
			if err := rr.err(); err != nil {
				return err
			}
			if f.refs[ref.typ] == nil {
				f.refs[ref.typ] = map[uint32][]uint32{}
			}
			f.refs[ref.typ][from] = append(f.refs[ref.typ][from], to...)
		}
	case "iprp":
		kids, err := splitBoxes(c.payload)
		if err != nil {
			return err
		}
		for _, k := range kids {
			switch k.typ {
			case "ipco":
				props, err := splitBoxes(k.payload)
				if err != nil {
					return err
				}
				for _, p := range props {
					f.props = append(f.props, p.full)
				}
			case "ipma":
				mr := &rd{b: k.payload}
				v, flags := mr.fullHeader()
				n := mr.u32()
				for i := uint32(0); i < n && !mr.bad; i++ {
					it := f.item(mr.id(v >= 1))
					cnt := mr.u8()
					for j := uint8(0); j < cnt && !mr.bad; j++ {
						var a heifAssoc
						if flags&1 != 0 {
							x := mr.u16()
							a = heifAssoc{int(x & 0x7fff), x&0x8000 != 0}
						} else {
							x := mr.u8()
							a = heifAssoc{int(x & 0x7f), x&0x80 != 0}
						}
						if a.index > 0 {
							it.props = append(it.props, a)
						}
					}
				}
				if err := mr.err(); err != nil {
					return err
				}
			}
		}
	case "idat":
		f.idat = c.payload
	}
	return r.err()
}

// prop returns the raw box and type of ipco index i (1-based).
func (f *heifFile) prop(i int) (string, []byte) {
	if i < 1 || i > len(f.props) {
		return "", nil
	}
	b := f.props[i-1]
	return string(b[4:8]), b
}

// propOf returns the first property of type typ associated with item it.
func (f *heifFile) propOf(it *heifItem, typ string) []byte {
	for _, a := range it.props {
		if t, b := f.prop(a.index); t == typ {
			return b
		}
	}
	return nil
}

// itemData returns the bytes of an item (all extents concatenated).
func (f *heifFile) itemData(it *heifItem) ([]byte, error) {
	var buf bytes.Buffer
	for _, e := range it.extents {
		if e.len == 0 {
			return nil, errors.New("heif: open-ended extent")
		}
		if e.len > 256<<20 {
			return nil, errors.New("heif: extent too large")
		}
		start := it.base + e.off
		switch it.method {
		case 0:
			if int64(start+e.len) > f.size {
				return nil, errors.New("heif: extent past end of file")
			}
			b := make([]byte, e.len)
			if _, err := f.r.ReadAt(b, int64(start)); err != nil {
				return nil, err
			}
			buf.Write(b)
		case 1:
			if start+e.len > uint64(len(f.idat)) {
				return nil, errors.New("heif: extent past end of idat")
			}
			buf.Write(f.idat[start : start+e.len])
		default:
			return nil, fmt.Errorf("heif: construction method %d not supported", it.method)
		}
	}
	return buf.Bytes(), nil
}

// ispe returns the image spatial extents property of a raw ispe box.
func ispeSize(b []byte) (w, h uint32, ok bool) {
	if len(b) < 20 {
		return 0, 0, false
	}
	return binary.BigEndian.Uint32(b[12:16]), binary.BigEndian.Uint32(b[16:20]), true
}

// gridInfo describes the primary image when it is a grid of HEVC tiles.
type gridInfo struct {
	rows, cols   int
	width        int // output size before transformations
	height       int
	tileW, tileH int
	tiles        []*heifItem // row-major
	gridProps    [][]byte    // colr and pixi of the grid item, copied into bands
	icc          []byte      // ICC profile payload from colr prof/rICC, if any
	transforms   []string    // vips ops after decoding, in order: rot d90|d180|d270, flip horizontal|vertical
}

// grid inspects the primary item. ok is false when the primary image is not
// a plain HEVC grid we know how to split (single images, AVIF, clap crops).
func (f *heifFile) grid() (*gridInfo, bool, error) {
	p := f.items[f.primary]
	if p.typ != "grid" {
		return nil, false, nil
	}
	desc, err := f.itemData(p)
	if err != nil {
		return nil, false, err
	}
	r := &rd{b: desc}
	r.u8() // version
	flags := r.u8()
	g := &gridInfo{rows: int(r.u8()) + 1, cols: int(r.u8()) + 1}
	if flags&1 != 0 {
		g.width, g.height = int(r.u32()), int(r.u32())
	} else {
		g.width, g.height = int(r.u16()), int(r.u16())
	}
	if err := r.err(); err != nil {
		return nil, false, fmt.Errorf("heif grid descriptor: %w", err)
	}
	ids := f.refs["dimg"][p.id]
	if len(ids) != g.rows*g.cols {
		return nil, false, fmt.Errorf("heif grid: %d tiles for %dx%d", len(ids), g.cols, g.rows)
	}
	for _, id := range ids {
		t := f.items[id]
		if t == nil || t.typ != "hvc1" {
			return nil, false, nil
		}
		w, h, ok := ispeSize(f.propOf(t, "ispe"))
		if !ok {
			return nil, false, errors.New("heif grid: tile without ispe")
		}
		if g.tileW == 0 {
			g.tileW, g.tileH = int(w), int(h)
		} else if int(w) != g.tileW || int(h) != g.tileH {
			return nil, false, nil
		}
		g.tiles = append(g.tiles, t)
	}
	if g.tileW*g.cols < g.width || g.tileH*g.rows < g.height {
		return nil, false, errors.New("heif grid: tiles do not cover the image")
	}
	for _, a := range p.props {
		typ, b := f.prop(a.index)
		switch typ {
		case "colr":
			g.gridProps = append(g.gridProps, b)
			if len(b) > 12 {
				if ct := string(b[8:12]); ct == "prof" || ct == "rICC" {
					g.icc = b[12:]
				}
			}
		case "pixi":
			g.gridProps = append(g.gridProps, b)
		case "irot":
			if len(b) < 9 {
				return nil, false, errors.New("heif: short irot")
			}
			// irot angle is anti-clockwise in 90° steps; vips rot is clockwise.
			switch b[8] & 3 {
			case 1:
				g.transforms = append(g.transforms, "rot d270")
			case 2:
				g.transforms = append(g.transforms, "rot d180")
			case 3:
				g.transforms = append(g.transforms, "rot d90")
			}
		case "imir":
			if len(b) < 9 {
				return nil, false, errors.New("heif: short imir")
			}
			if b[8]&1 == 0 {
				g.transforms = append(g.transforms, "flip vertical") // mirror about the horizontal axis
			} else {
				g.transforms = append(g.transforms, "flip horizontal")
			}
		case "clap":
			return nil, false, nil // a crop window: let libheif handle it whole
		}
	}
	if g.icc == nil {
		// libheif falls back to the tiles' colour profile (heif-enc puts it
		// there); the band files carry tile properties, but the final
		// colour conversion needs the profile too.
		if b := f.propOf(g.tiles[0], "colr"); len(b) > 12 {
			if ct := string(b[8:12]); ct == "prof" || ct == "rICC" {
				g.icc = b[12:]
			}
		}
	}
	return g, true, nil
}

// ---------------------------------------------------------------- writing

func be16(v uint16) []byte { return binary.BigEndian.AppendUint16(nil, v) }
func be32(v uint32) []byte { return binary.BigEndian.AppendUint32(nil, v) }

func mkbox(typ string, parts ...[]byte) []byte {
	n := 8
	for _, p := range parts {
		n += len(p)
	}
	b := make([]byte, 0, n)
	b = binary.BigEndian.AppendUint32(b, uint32(n))
	b = append(b, typ...)
	for _, p := range parts {
		b = append(b, p...)
	}
	return b
}

func mkfull(typ string, version uint8, flags uint32, parts ...[]byte) []byte {
	return mkbox(typ, append([][]byte{be32(uint32(version)<<24 | flags&0xffffff)}, parts...)...)
}

// gridTile is one HEVC tile: its property boxes (hvcC, ispe, …) and bitstream.
type gridTile struct {
	props [][]byte
	data  []byte
}

// gridSpec is a grid image to write: rows x cols tiles (row-major), the
// output size, and extra properties of the grid item (colr, pixi, irot…).
type gridSpec struct {
	rows, cols    int
	width, height int
	props         [][]byte
	tiles         []gridTile
}

// essentialProp reports whether a property must be marked essential.
func essentialProp(b []byte) bool {
	switch string(b[4:8]) {
	case "hvcC", "irot", "imir", "clap":
		return true
	}
	return false
}

// writeGridHEIF writes g as a minimal HEIC file: ftyp, meta (grid item 1 plus
// hidden hvc1 tile items 2…n+1) and one mdat holding the grid descriptor and
// the tile bitstreams.
func writeGridHEIF(w io.Writer, g gridSpec) error {
	if len(g.tiles) != g.rows*g.cols || g.rows < 1 || g.cols < 1 || g.rows > 256 || g.cols > 256 {
		return errors.New("heif: bad grid shape")
	}
	if len(g.tiles)+1 > 0xffff {
		return errors.New("heif: too many tiles")
	}
	// Property table, deduplicated by content.
	var ipco [][]byte
	index := map[string]int{}
	add := func(b []byte) heifAssoc {
		k := string(b)
		i, ok := index[k]
		if !ok {
			ipco = append(ipco, b)
			i = len(ipco)
			index[k] = i
		}
		return heifAssoc{i, essentialProp(b)}
	}
	ispe := mkfull("ispe", 0, 0, be32(uint32(g.width)), be32(uint32(g.height)))
	assoc := make([][]heifAssoc, len(g.tiles)+1)
	assoc[0] = append(assoc[0], add(ispe))
	for _, p := range g.props {
		assoc[0] = append(assoc[0], add(p))
	}
	for i, t := range g.tiles {
		for _, p := range t.props {
			assoc[i+1] = append(assoc[i+1], add(p))
		}
	}
	if len(ipco) > 0x7fff {
		return errors.New("heif: too many properties")
	}
	var ipma bytes.Buffer
	ipma.Write(be32(uint32(len(assoc))))
	for i, as := range assoc {
		ipma.Write(be16(uint16(i + 1)))
		ipma.WriteByte(byte(len(as)))
		for _, a := range as {
			v := uint16(a.index)
			if a.essential {
				v |= 0x8000
			}
			ipma.Write(be16(v))
		}
	}

	var infe [][]byte
	infe = append(infe, be16(uint16(len(g.tiles)+1)))
	infe = append(infe, mkfull("infe", 2, 0, be16(1), be16(0), []byte("grid"), []byte{0}))
	to := []byte{}
	for i := range g.tiles {
		id := uint16(i + 2)
		infe = append(infe, mkfull("infe", 2, 1, be16(id), be16(0), []byte("hvc1"), []byte{0}))
		to = append(to, be16(id)...)
	}

	desc := []byte{0, 1, byte(g.rows - 1), byte(g.cols - 1)}
	desc = append(desc, be32(uint32(g.width))...)
	desc = append(desc, be32(uint32(g.height))...)
	lengths := []int{len(desc)}
	for _, t := range g.tiles {
		lengths = append(lengths, len(t.data))
	}

	ftyp := mkbox("ftyp", []byte("heic"), be32(0), []byte("mif1"), []byte("heic"))
	meta := func(dataStart int) []byte {
		var iloc bytes.Buffer
		iloc.Write(be16(0x4400)) // offset_size 4, length_size 4, base_offset_size 0
		iloc.Write(be16(uint16(len(lengths))))
		off := dataStart
		for i, l := range lengths {
			iloc.Write(be16(uint16(i + 1)))
			iloc.Write(be16(0)) // data_reference_index
			iloc.Write(be16(1)) // extent_count
			iloc.Write(be32(uint32(off)))
			iloc.Write(be32(uint32(l)))
			off += l
		}
		return mkfull("meta", 0, 0,
			mkfull("hdlr", 0, 0, be32(0), []byte("pict"), make([]byte, 12), []byte{0}),
			mkfull("pitm", 0, 0, be16(1)),
			mkfull("iinf", 0, 0, infe...),
			mkfull("iref", 0, 0, mkbox("dimg", be16(1), be16(uint16(len(g.tiles))), to)),
			mkbox("iprp", mkbox("ipco", ipco...), mkfull("ipma", 0, 1, ipma.Bytes())),
			mkfull("iloc", 0, 0, iloc.Bytes()),
		)
	}
	total := 0
	for _, l := range lengths {
		total += l
	}
	m := meta(0)
	dataStart := len(ftyp) + len(m) + 8
	if dataStart+total > 0xffffffff {
		return errors.New("heif: band too large")
	}
	m = meta(dataStart)
	for _, part := range [][]byte{ftyp, m, be32(uint32(8 + total)), []byte("mdat"), desc} {
		if _, err := w.Write(part); err != nil {
			return err
		}
	}
	for _, t := range g.tiles {
		if _, err := w.Write(t.data); err != nil {
			return err
		}
	}
	return nil
}

// tileProps returns the property boxes of a tile item that a band file
// needs: everything except transformations, which apply to the whole image.
func (f *heifFile) tileProps(t *heifItem) [][]byte {
	var out [][]byte
	for _, a := range t.props {
		typ, b := f.prop(a.index)
		switch typ {
		case "irot", "imir", "clap", "":
			continue
		}
		out = append(out, b)
	}
	return out
}
