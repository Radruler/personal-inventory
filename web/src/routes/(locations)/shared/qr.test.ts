import { describe, expect, it } from 'vitest';
import {
  alignmentPositions,
  byteCapacity,
  encodeQr,
  formatBits,
  functionModules,
  maskHit,
  qrPath,
  rsDivisor,
  rsRemainder,
  versionBits,
  type QrCode
} from './qr';

describe('Reed–Solomon (known vectors)', () => {
  it('HELLO WORLD 1-M (10 EC codewords)', () => {
    const data = [32, 91, 11, 120, 209, 114, 220, 77, 67, 64, 236, 17, 236, 17, 236, 17];
    expect(rsRemainder(data, rsDivisor(10))).toEqual([196, 35, 39, 119, 235, 215, 231, 226, 93, 23]);
  });
  it('HELLO WORLD 1-Q (13 EC codewords)', () => {
    const data = [32, 91, 11, 120, 209, 114, 220, 77, 67, 64, 236, 17, 236];
    expect(rsRemainder(data, rsDivisor(13))).toEqual([168, 72, 22, 82, 217, 54, 156, 0, 46, 15, 180, 122, 16]);
  });
});

describe('format and version info (ISO 18004 tables)', () => {
  it('level M format strings for masks 0–7', () => {
    const table = [
      '101010000010010',
      '101000100100101',
      '101111001111100',
      '101101101001011',
      '100010111111001',
      '100000011001110',
      '100111110010111',
      '100101010100000'
    ];
    table.forEach((s, mask) => expect(formatBits(mask).toString(2).padStart(15, '0')).toBe(s));
  });
  it('version info strings', () => {
    expect(versionBits(7).toString(2).padStart(18, '0')).toBe('000111110010010100');
    expect(versionBits(8).toString(2).padStart(18, '0')).toBe('001000010110111100');
  });
});

describe('capacity and layout', () => {
  it('byte-mode capacity at level M matches the standard table', () => {
    expect([1, 2, 3, 4, 5, 6, 7, 8, 9, 10].map(byteCapacity)).toEqual([14, 26, 42, 62, 84, 106, 122, 152, 180, 213]);
  });
  it('alignment pattern centres', () => {
    expect(alignmentPositions(1)).toEqual([]);
    expect(alignmentPositions(2)).toEqual([6, 18]);
    expect(alignmentPositions(6)).toEqual([6, 34]);
    expect(alignmentPositions(7)).toEqual([6, 22, 38]);
    expect(alignmentPositions(10)).toEqual([6, 28, 50]);
  });
  it('picks the smallest version that fits', () => {
    expect(encodeQr('a'.repeat(14)).version).toBe(1);
    expect(encodeQr('a'.repeat(15)).version).toBe(2);
    expect(encodeQr('a'.repeat(62)).version).toBe(4);
    expect(encodeQr('a'.repeat(63)).version).toBe(5);
    expect(encodeQr('a'.repeat(213)).version).toBe(10);
    expect(() => encodeQr('a'.repeat(214))).toThrow(RangeError);
  });
  it('finder patterns and the dark module', () => {
    const qr = encodeQr('x');
    const s = qr.size;
    for (const [ox, oy] of [
      [0, 0],
      [s - 7, 0],
      [0, s - 7]
    ]) {
      expect(qr.modules[oy][ox]).toBe(true);
      expect(qr.modules[oy + 1][ox + 1]).toBe(false);
      expect(qr.modules[oy + 3][ox + 3]).toBe(true);
      expect(qr.modules[oy + 6][ox + 6]).toBe(true);
    }
    expect(qr.modules[s - 8][8]).toBe(true);
  });
  it('qrPath draws rects offset by the quiet zone', () => {
    const d = qrPath(encodeQr('B-014'), 4);
    expect(d.startsWith('M4 4h7v1h-7z')).toBe(true); // top row of the top-left finder
  });
});

// ---- an independent decoder: reads the symbol back the way a scanner would
// (format info → unmask → zigzag → de-interleave → ECC check → byte mode).

const BLOCKS = [-1, 1, 1, 1, 2, 2, 4, 4, 4, 5, 5];
const ECC = [-1, 10, 16, 26, 18, 24, 16, 18, 22, 22, 26];

function decode(qr: QrCode): { text: string; mask: number } {
  const s = qr.size;
  const m = (x: number, y: number) => (qr.modules[y][x] ? 1 : 0);
  let fmtA = 0;
  const a: [number, number][] = [[8, 0], [8, 1], [8, 2], [8, 3], [8, 4], [8, 5], [8, 7], [8, 8], [7, 8], [5, 8], [4, 8], [3, 8], [2, 8], [1, 8], [0, 8]];
  a.forEach(([x, y], i) => (fmtA |= m(x, y) << i));
  let fmtB = 0;
  for (let i = 0; i < 8; i++) fmtB |= m(s - 1 - i, 8) << i;
  for (let i = 8; i < 15; i++) fmtB |= m(8, s - 15 + i) << i;
  expect(fmtB).toBe(fmtA);
  const mask = [0, 1, 2, 3, 4, 5, 6, 7].find((k) => formatBits(k) === fmtA);
  if (mask === undefined) throw new Error('no format match');

  const fn = functionModules(qr.version);
  const bits: number[] = [];
  let up = true;
  for (let right = s - 1; right >= 1; right -= 2) {
    if (right === 6) right = 5;
    for (let v = 0; v < s; v++) {
      const y = up ? s - 1 - v : v;
      for (const x of [right, right - 1]) if (!fn[y][x]) bits.push(m(x, y) ^ (maskHit(mask, x, y) ? 1 : 0));
    }
    up = !up;
  }
  const cw: number[] = [];
  for (let i = 0; i + 8 <= bits.length; i += 8) cw.push(bits.slice(i, i + 8).reduce((p, b) => (p << 1) | b, 0));

  const v = qr.version;
  const nb = BLOCKS[v];
  const ecc = ECC[v];
  const total = cw.length;
  const numShort = nb - (total % nb);
  const shortData = Math.floor(total / nb) - ecc;
  const data: number[][] = Array.from({ length: nb }, () => []);
  const eccs: number[][] = Array.from({ length: nb }, () => []);
  let k = 0;
  for (let i = 0; i <= shortData; i++)
    for (let j = 0; j < nb; j++) if (i < shortData || j >= numShort) data[j].push(cw[k++]);
  for (let i = 0; i < ecc; i++) for (let j = 0; j < nb; j++) eccs[j].push(cw[k++]);
  expect(k).toBe(total);
  data.forEach((d, j) => expect(rsRemainder(d, rsDivisor(ecc))).toEqual(eccs[j]));

  const stream = data.flat().flatMap((b) => [7, 6, 5, 4, 3, 2, 1, 0].map((i) => (b >> i) & 1));
  let p = 0;
  const read = (n: number) => {
    let x = 0;
    for (let i = 0; i < n; i++) x = (x << 1) | stream[p++];
    return x;
  };
  expect(read(4)).toBe(0b0100);
  const len = read(v <= 9 ? 8 : 16);
  const bytes = new Uint8Array(len).map(() => read(8));
  expect(read(4)).toBe(0); // terminator
  return { text: new TextDecoder().decode(bytes), mask };
}

describe('encode → decode round trip', () => {
  const samples = [
    'B-014',
    'http://localhost:8480/locations/01M6CE32R5FFC734X22QQA1PB8',
    'http://nas.tail1234.ts.net:8480/locations/01M6CE32R5FFC734X22QQA1PB8',
    'Ünït 2 › Bøx ✓',
    'x'.repeat(150) // version 8: multiple blocks of two lengths + version info
  ];
  for (const text of samples) {
    it(`reads back ${JSON.stringify(text.slice(0, 40))}`, () => {
      const qr = encodeQr(text);
      const r = decode(qr);
      expect(r.text).toBe(text);
      expect(r.mask).toBe(qr.mask);
    });
  }
  it('every mask produces a readable symbol', () => {
    for (let mask = 0; mask < 8; mask++) expect(decode(encodeQr('B-014 Winter coats', { mask })).text).toBe('B-014 Winter coats');
  });
});
