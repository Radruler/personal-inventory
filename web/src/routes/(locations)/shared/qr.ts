// Minimal QR code encoder for box labels (ISO/IEC 18004): byte mode (UTF-8),
// error correction level M, versions 1–10 (up to 213 bytes, far more than a
// box URL needs). Self-contained, no dependencies; follows the structure of
// Project Nayuki's reference encoder. Pure and unit-tested (qr.test.ts).

export type QrCode = {
  version: number;
  size: number;
  mask: number;
  /** modules[y][x], true = dark. */
  modules: boolean[][];
};

export const MAX_VERSION = 10;

// Level M, indexed by version (index 0 unused).
const ECC_PER_BLOCK = [-1, 10, 16, 26, 18, 24, 16, 18, 22, 22, 26];
const NUM_BLOCKS = [-1, 1, 1, 1, 2, 2, 4, 4, 4, 5, 5];
// Format bits for level M are 00 (L=01, M=00, Q=11, H=10).
const ECC_FORMAT_BITS = 0;

// ---------------------------------------------------------------- GF(256) / Reed–Solomon

function gfMul(x: number, y: number): number {
  let z = 0;
  for (let i = 7; i >= 0; i--) {
    z = (z << 1) ^ ((z >>> 7) * 0x11d);
    z ^= ((y >>> i) & 1) * x;
  }
  return z & 0xff;
}

/** Generator polynomial coefficients (highest degree first, leading 1 dropped). */
export function rsDivisor(degree: number): number[] {
  const result = new Array<number>(degree).fill(0);
  result[degree - 1] = 1;
  let root = 1;
  for (let i = 0; i < degree; i++) {
    for (let j = 0; j < result.length; j++) {
      result[j] = gfMul(result[j], root);
      if (j + 1 < result.length) result[j] ^= result[j + 1];
    }
    root = gfMul(root, 0x02);
  }
  return result;
}

/** Error-correction codewords for `data`. */
export function rsRemainder(data: readonly number[], divisor: readonly number[]): number[] {
  const result = divisor.map(() => 0);
  for (const b of data) {
    const factor = b ^ (result.shift() as number);
    result.push(0);
    divisor.forEach((coef, i) => (result[i] ^= gfMul(coef, factor)));
  }
  return result;
}

// ---------------------------------------------------------------- capacity

export function sizeOf(version: number): number {
  return version * 4 + 17;
}

function rawDataModules(ver: number): number {
  let result = (16 * ver + 128) * ver + 64;
  if (ver >= 2) {
    const numAlign = Math.floor(ver / 7) + 2;
    result -= (25 * numAlign - 10) * numAlign - 55;
    if (ver >= 7) result -= 36;
  }
  return result;
}

export function dataCodewords(ver: number): number {
  return Math.floor(rawDataModules(ver) / 8) - ECC_PER_BLOCK[ver] * NUM_BLOCKS[ver];
}

function countBits(ver: number): number {
  return ver <= 9 ? 8 : 16;
}

/** Max bytes in byte mode at level M. */
export function byteCapacity(ver: number): number {
  return Math.floor((dataCodewords(ver) * 8 - 4 - countBits(ver)) / 8);
}

export function alignmentPositions(ver: number): number[] {
  if (ver === 1) return [];
  const numAlign = Math.floor(ver / 7) + 2;
  const step = Math.floor((ver * 8 + numAlign * 3 + 5) / (numAlign * 4 - 4)) * 2;
  const result = [6];
  for (let pos = sizeOf(ver) - 7; result.length < numAlign; pos -= step) result.splice(1, 0, pos);
  return result;
}

// ---------------------------------------------------------------- BCH-coded info

/** The 15 format bits (after the 0x5412 mask) for level M and a mask number. */
export function formatBits(mask: number): number {
  const data = (ECC_FORMAT_BITS << 3) | mask;
  let rem = data;
  for (let i = 0; i < 10; i++) rem = (rem << 1) ^ ((rem >>> 9) * 0x537);
  return ((data << 10) | rem) ^ 0x5412;
}

/** The 18 version bits (versions ≥ 7). */
export function versionBits(ver: number): number {
  let rem = ver;
  for (let i = 0; i < 12; i++) rem = (rem << 1) ^ ((rem >>> 11) * 0x1f25);
  return (ver << 12) | rem;
}

const bit = (x: number, i: number) => ((x >>> i) & 1) !== 0;

// ---------------------------------------------------------------- encode

function encodeData(bytes: Uint8Array, ver: number): number[] {
  const bits: number[] = [];
  const push = (val: number, len: number) => {
    for (let i = len - 1; i >= 0; i--) bits.push((val >>> i) & 1);
  };
  push(0b0100, 4);
  push(bytes.length, countBits(ver));
  for (const b of bytes) push(b, 8);
  const capBits = dataCodewords(ver) * 8;
  push(0, Math.min(4, capBits - bits.length));
  push(0, (8 - (bits.length % 8)) % 8);
  for (let pad = 0xec; bits.length < capBits; pad ^= 0xec ^ 0x11) push(pad, 8);
  const out: number[] = [];
  for (let i = 0; i < bits.length; i += 8) {
    let b = 0;
    for (let j = 0; j < 8; j++) b = (b << 1) | bits[i + j];
    out.push(b);
  }
  return out;
}

/** Splits data into blocks, appends ECC to each, interleaves (ISO 18004 §7.6). */
export function addEccAndInterleave(data: readonly number[], ver: number): number[] {
  const numBlocks = NUM_BLOCKS[ver];
  const eccLen = ECC_PER_BLOCK[ver];
  const raw = Math.floor(rawDataModules(ver) / 8);
  const numShort = numBlocks - (raw % numBlocks);
  const shortLen = Math.floor(raw / numBlocks);
  const div = rsDivisor(eccLen);
  const blocks: number[][] = [];
  for (let i = 0, k = 0; i < numBlocks; i++) {
    const dat = data.slice(k, k + shortLen - eccLen + (i < numShort ? 0 : 1));
    k += dat.length;
    const ecc = rsRemainder(dat, div);
    if (i < numShort) dat.push(0);
    blocks.push([...dat, ...ecc]);
  }
  const result: number[] = [];
  for (let i = 0; i < blocks[0].length; i++) {
    blocks.forEach((b, j) => {
      if (i !== shortLen - eccLen || j >= numShort) result.push(b[i]);
    });
  }
  return result;
}

class Matrix {
  size: number;
  modules: boolean[][];
  isFunction: boolean[][];
  constructor(readonly version: number) {
    this.size = sizeOf(version);
    this.modules = Array.from({ length: this.size }, () => new Array<boolean>(this.size).fill(false));
    this.isFunction = Array.from({ length: this.size }, () => new Array<boolean>(this.size).fill(false));
  }
  setFn(x: number, y: number, dark: boolean) {
    this.modules[y][x] = dark;
    this.isFunction[y][x] = true;
  }
  drawFunctionPatterns() {
    const s = this.size;
    for (let i = 0; i < s; i++) {
      this.setFn(6, i, i % 2 === 0);
      this.setFn(i, 6, i % 2 === 0);
    }
    this.finder(3, 3);
    this.finder(s - 4, 3);
    this.finder(3, s - 4);
    const pos = alignmentPositions(this.version);
    const n = pos.length;
    for (let i = 0; i < n; i++)
      for (let j = 0; j < n; j++) {
        if ((i === 0 && j === 0) || (i === 0 && j === n - 1) || (i === n - 1 && j === 0)) continue;
        for (let dy = -2; dy <= 2; dy++)
          for (let dx = -2; dx <= 2; dx++) this.setFn(pos[i] + dx, pos[j] + dy, Math.max(Math.abs(dx), Math.abs(dy)) !== 1);
      }
    this.drawFormat(0); // reserve; overwritten after masking
    this.drawVersion();
  }
  finder(x: number, y: number) {
    for (let dy = -4; dy <= 4; dy++)
      for (let dx = -4; dx <= 4; dx++) {
        const d = Math.max(Math.abs(dx), Math.abs(dy));
        const xx = x + dx;
        const yy = y + dy;
        if (xx >= 0 && xx < this.size && yy >= 0 && yy < this.size) this.setFn(xx, yy, d !== 2 && d !== 4);
      }
  }
  drawFormat(mask: number) {
    const bits = formatBits(mask);
    const s = this.size;
    for (let i = 0; i <= 5; i++) this.setFn(8, i, bit(bits, i));
    this.setFn(8, 7, bit(bits, 6));
    this.setFn(8, 8, bit(bits, 7));
    this.setFn(7, 8, bit(bits, 8));
    for (let i = 9; i < 15; i++) this.setFn(14 - i, 8, bit(bits, i));
    for (let i = 0; i < 8; i++) this.setFn(s - 1 - i, 8, bit(bits, i));
    for (let i = 8; i < 15; i++) this.setFn(8, s - 15 + i, bit(bits, i));
    this.setFn(8, s - 8, true); // dark module
  }
  drawVersion() {
    if (this.version < 7) return;
    const bits = versionBits(this.version);
    for (let i = 0; i < 18; i++) {
      const a = this.size - 11 + (i % 3);
      const b = Math.floor(i / 3);
      this.setFn(a, b, bit(bits, i));
      this.setFn(b, a, bit(bits, i));
    }
  }
  drawCodewords(data: readonly number[]) {
    const s = this.size;
    let i = 0;
    for (let right = s - 1; right >= 1; right -= 2) {
      if (right === 6) right = 5;
      for (let vert = 0; vert < s; vert++)
        for (let j = 0; j < 2; j++) {
          const x = right - j;
          const upward = ((right + 1) & 2) === 0;
          const y = upward ? s - 1 - vert : vert;
          if (!this.isFunction[y][x] && i < data.length * 8) {
            this.modules[y][x] = bit(data[i >>> 3], 7 - (i & 7));
            i++;
          }
        }
    }
  }
  applyMask(mask: number) {
    for (let y = 0; y < this.size; y++)
      for (let x = 0; x < this.size; x++)
        if (!this.isFunction[y][x] && maskHit(mask, x, y)) this.modules[y][x] = !this.modules[y][x];
  }
}

export function maskHit(mask: number, x: number, y: number): boolean {
  switch (mask) {
    case 0:
      return (x + y) % 2 === 0;
    case 1:
      return y % 2 === 0;
    case 2:
      return x % 3 === 0;
    case 3:
      return (x + y) % 3 === 0;
    case 4:
      return (Math.floor(x / 3) + Math.floor(y / 2)) % 2 === 0;
    case 5:
      return ((x * y) % 2) + ((x * y) % 3) === 0;
    case 6:
      return (((x * y) % 2) + ((x * y) % 3)) % 2 === 0;
    case 7:
      return (((x + y) % 2) + ((x * y) % 3)) % 2 === 0;
    default:
      throw new Error('bad mask');
  }
}

/** Mask penalty (ISO 18004 §7.8.3: N1=3, N2=3, N3=40, N4=10). */
export function penalty(m: readonly boolean[][]): number {
  const s = m.length;
  let score = 0;
  const line = (get: (i: number) => boolean) => {
    let run = 1;
    for (let i = 1; i <= s; i++) {
      if (i < s && get(i) === get(i - 1)) run++;
      else {
        if (run >= 5) score += 3 + (run - 5);
        run = 1;
      }
    }
    // finder-like 1:1:3:1:1 with 4 light modules on one side
    const pat = [true, false, true, true, true, false, true];
    for (let i = 0; i + 7 <= s; i++) {
      if (!pat.every((p, k) => get(i + k) === p)) continue;
      const lightBefore = [1, 2, 3, 4].every((k) => i - k < 0 || !get(i - k));
      const lightAfter = [0, 1, 2, 3].every((k) => i + 7 + k >= s || !get(i + 7 + k));
      if (lightBefore || lightAfter) score += 40;
    }
  };
  for (let y = 0; y < s; y++) line((x) => m[y][x]);
  for (let x = 0; x < s; x++) line((y) => m[y][x]);
  for (let y = 0; y + 1 < s; y++)
    for (let x = 0; x + 1 < s; x++) {
      const c = m[y][x];
      if (c === m[y][x + 1] && c === m[y + 1][x] && c === m[y + 1][x + 1]) score += 3;
    }
  let dark = 0;
  for (const row of m) for (const c of row) if (c) dark++;
  const total = s * s;
  const k = Math.ceil(Math.abs(dark * 20 - total * 10) / total) - 1;
  score += k * 10;
  return score;
}

/** The function-module map for a version (for tests/decoders). */
export function functionModules(version: number): boolean[][] {
  const m = new Matrix(version);
  m.drawFunctionPatterns();
  return m.isFunction;
}

/**
 * Encodes `text` (UTF-8, byte mode, level M) in the smallest version that
 * fits. `mask` forces a mask pattern (tests); default picks the lowest penalty.
 */
export function encodeQr(text: string, opts: { minVersion?: number; mask?: number } = {}): QrCode {
  const bytes = new TextEncoder().encode(text);
  let ver = Math.max(1, opts.minVersion ?? 1);
  while (ver <= MAX_VERSION && byteCapacity(ver) < bytes.length) ver++;
  if (ver > MAX_VERSION) throw new RangeError(`text too long for a QR code here (${bytes.length} bytes, max ${byteCapacity(MAX_VERSION)})`);
  const m = new Matrix(ver);
  m.drawFunctionPatterns();
  m.drawCodewords(addEccAndInterleave(encodeData(bytes, ver), ver));
  let best = opts.mask ?? -1;
  if (best < 0) {
    let bestScore = Infinity;
    for (let mask = 0; mask < 8; mask++) {
      m.applyMask(mask);
      m.drawFormat(mask);
      const p = penalty(m.modules);
      if (p < bestScore) {
        bestScore = p;
        best = mask;
      }
      m.applyMask(mask); // XOR undoes it
    }
  }
  m.applyMask(best);
  m.drawFormat(best);
  return { version: ver, size: m.size, mask: best, modules: m.modules };
}

/** SVG path (one rect per horizontal run of dark modules), offset by `border` quiet-zone modules. */
export function qrPath(qr: QrCode, border = 4): string {
  const parts: string[] = [];
  for (let y = 0; y < qr.size; y++) {
    let x = 0;
    while (x < qr.size) {
      if (!qr.modules[y][x]) {
        x++;
        continue;
      }
      const start = x;
      while (x < qr.size && qr.modules[y][x]) x++;
      parts.push(`M${start + border} ${y + border}h${x - start}v1h${start - x}z`);
    }
  }
  return parts.join('');
}
