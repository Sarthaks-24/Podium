/**
 * Byte spans and source lines for the code view. Kept apart from review.ts so
 * the student first load does not carry the integrity review code.
 */

/** A byte range in a UTF-8 source: start inclusive, end exclusive. */
export interface Span {
  start: number;
  end: number;
}

/**
 * Converts byte spans into UTF-16 index ranges of `source`, sorted and
 * merged, clamped to the source. A start inside a multibyte character moves
 * back to the character's start and an end inside one moves forward to its
 * end, so a highlight never splits a character.
 */
export function byteSpansToText(source: string, spans: readonly Span[]): Span[] {
  // charAt[b] is the UTF-16 index of the character holding byte b;
  // boundary[b] is 1 when byte b starts a character.
  const bytes: number[] = [];
  const boundary: number[] = [];
  const widths: number[] = [];
  for (let i = 0; i < source.length; ) {
    const cp = source.codePointAt(i)!;
    const units = cp > 0xffff ? 2 : 1;
    const n = cp < 0x80 ? 1 : cp < 0x800 ? 2 : cp < 0x10000 ? 3 : 4;
    for (let k = 0; k < n; k++) {
      bytes.push(i);
      boundary.push(k === 0 ? 1 : 0);
      widths.push(units);
    }
    i += units;
  }
  const total = bytes.length;
  const toStart = (b: number) => (b >= total ? source.length : bytes[Math.max(0, b)]);
  const toEnd = (b: number) => {
    if (b >= total) return source.length;
    if (b <= 0) return 0;
    return boundary[b] ? bytes[b] : bytes[b] + widths[b];
  };
  const out: Span[] = [];
  const sorted = spans
    .filter((s) => Number.isFinite(s.start) && Number.isFinite(s.end) && s.end > s.start)
    .map((s) => ({ start: toStart(Math.floor(s.start)), end: toEnd(Math.ceil(s.end)) }))
    .filter((s) => s.end > s.start)
    .sort((a, b) => a.start - b.start || a.end - b.end);
  for (const s of sorted) {
    const last = out[out.length - 1];
    if (last && s.start <= last.end) last.end = Math.max(last.end, s.end);
    else out.push({ ...s });
  }
  return out;
}

/** A run of text on one line, inside or outside a shared passage. */
export interface Segment {
  text: string;
  mark: boolean;
  /** The first segment of a shared passage. */
  opens: boolean;
  /** The last segment of a shared passage. */
  closes: boolean;
}

export interface CodeLine {
  number: number;
  segments: Segment[];
  /** True when any part of the line is in a shared passage. */
  matched: boolean;
}

/**
 * Splits a source into numbered lines of segments, marking the parts the
 * byte spans cover. Line breaks (\n or \r\n) are never part of a segment;
 * a passage that crosses lines opens on its first visible character and
 * closes on its last.
 */
export function codeLines(source: string, spans: readonly Span[]): CodeLine[] {
  const isBreak = (i: number) => source[i] === '\n' || source[i] === '\r';
  const ranges: { start: number; end: number }[] = [];
  for (const s of byteSpansToText(source, spans)) {
    let start = s.start;
    let end = s.end;
    while (start < end && isBreak(start)) start++;
    while (end > start && isBreak(end - 1)) end--;
    if (end > start) ranges.push({ start, end });
  }

  const lines: CodeLine[] = [];
  let lineStart = 0;
  let r = 0;
  for (let number = 1; ; number++) {
    const nl = source.indexOf('\n', lineStart);
    let lineEnd = nl < 0 ? source.length : nl;
    if (lineEnd > lineStart && source[lineEnd - 1] === '\r') lineEnd--;

    const segments: Segment[] = [];
    let pos = lineStart;
    while (r < ranges.length && ranges[r].end <= lineStart) r++;
    for (let k = r; k < ranges.length && ranges[k].start < lineEnd; k++) {
      const from = Math.max(ranges[k].start, lineStart);
      const to = Math.min(ranges[k].end, lineEnd);
      if (from > pos) segments.push({ text: source.slice(pos, from), mark: false, opens: false, closes: false });
      if (to > from) {
        segments.push({ text: source.slice(from, to), mark: true, opens: from === ranges[k].start, closes: to === ranges[k].end });
      }
      pos = Math.max(pos, to);
    }
    if (lineEnd > pos) segments.push({ text: source.slice(pos, lineEnd), mark: false, opens: false, closes: false });
    lines.push({ number, segments, matched: segments.some((s) => s.mark) });

    // A final line break leaves no empty last line.
    if (nl < 0 || nl + 1 >= source.length) break;
    lineStart = nl + 1;
  }
  return lines;
}
