/**
 * A student's own standing, cases and appeals as the web app sees them
 * (contracts C9 and C12, docs/API.md "Students"; v2 P4 F9.2).
 *
 * Standing is a band, never a number: GET /api/me/standing sends the band
 * and the dated lines (confirmed outcomes and organiser adjustments, each
 * with the reason written for the student and whether it can be appealed).
 * Nothing here reads or shows a rating value; the type has no field for
 * one, and a test checks that no student page prints it.
 *
 * StudentCaseV14 is the v1.4.0 student case (GET /api/integrity/cases):
 * the reply deadline, the outcome for this student with its reason, their
 * appeal and whether they may still appeal. A manual case's evidence is
 * the student's own submission with the line ranges in question;
 * lineSpans() turns those into the byte spans CodeView highlights.
 */
import { studentStateText, type Span, type StudentCase, type StudentCaseList, type StudentEvidence } from './review';

export type Band = 'good' | 'on_record' | 'repeated';

/** An appeal as the student sees it (no reviewer names). */
export interface StudentAppeal {
  id?: number;
  case_id?: number | null;
  adjustment_id?: number | null;
  text?: string;
  created_at: number;
  decide_by?: number;
  state: 'open' | 'upheld' | 'rejected';
  ruling?: { upheld: boolean; reason: string; created_at: number } | null;
}

/** One dated line of a standing. */
export interface StandingLine {
  id: string;
  kind: 'outcome' | 'adjustment';
  at: number;
  text: string;
  reason: string;
  case_id: number | null;
  adjustment_id: number | null;
  appeal: StudentAppeal | null;
  appeal_by: number | null;
  can_appeal: boolean;
}

/** GET /api/me/standing. */
export interface Standing {
  band: Band;
  band_name: string;
  lines: StandingLine[];
  server_now?: number;
}

/** The outcome a case closed with for this student. */
export interface StudentOutcome {
  kind: 'warning' | 'zero_problem' | 'referred';
  problem_label?: string;
  decided_at: number;
  reason: string;
  appeal_by: number | null;
}

/** Evidence on a student case: a signal worded for them, or a manual case's submission and lines. */
export interface StudentEvidenceV14 extends Omit<StudentEvidence, 'kind'> {
  kind: StudentEvidence['kind'] | 'manual';
  lines?: { from: number; to: number }[];
}

/** A case shared with the student, v1.4.0. */
export interface StudentCaseV14 extends Omit<StudentCase, 'evidence'> {
  evidence: StudentEvidenceV14[];
  respond_by?: number | null;
  outcome?: StudentOutcome | null;
  appeal?: StudentAppeal | null;
  can_appeal?: boolean;
}

export interface StudentCaseListV14 extends Omit<StudentCaseList, 'cases'> {
  cases: StudentCaseV14[];
  server_now?: number;
}

/** The fewest and most characters an appeal may have. */
export const MIN_APPEAL = 20;
export const MAX_APPEAL = 4000;

const BAND_NAMES: Record<Band, string> = {
  good: 'Good standing',
  on_record: 'Outcomes on record',
  repeated: 'Repeated outcomes',
};

/** The band's name, from the server when it sends one. */
export function bandName(s: Pick<Standing, 'band'> & Partial<Pick<Standing, 'band_name'>>): string {
  return s.band_name && s.band_name.trim() !== '' ? s.band_name : (BAND_NAMES[s.band] ?? s.band);
}

/** The badge tone of a band. */
export function bandTone(band: Band): 'ok' | 'part' | 'bad' {
  if (band === 'good') return 'ok';
  return band === 'on_record' ? 'part' : 'bad';
}

/** One sentence under the band: what it means for the student. */
export function bandSentence(band: Band, lines: number): string {
  if (band === 'good') {
    return lines === 0 ? 'Nothing is on your record.' : 'The lines below are on your record. They do not change your standing band.';
  }
  if (band === 'on_record') return 'An organiser recorded an outcome or an adjustment. Each one is listed below with its reason.';
  return 'More than one outcome is on your record. Each one is listed below with its reason.';
}

/** How the band works, in two sentences, the same on every view. */
export const BAND_HELP =
  'Your standing changes only when an organiser confirms an outcome or adjusts it, and each change is listed here with its reason. You can appeal any line within 14 days.';

/** The outcome as words: "Zero for problem B", "A warning", "Referred to the college". */
export function outcomeWords(o: Pick<StudentOutcome, 'kind' | 'problem_label'>): string {
  switch (o.kind) {
    case 'warning':
      return 'A warning';
    case 'zero_problem':
      return o.problem_label ? `Zero for problem ${o.problem_label}` : 'Zero for one problem';
    case 'referred':
      return 'Referred to the college';
    default:
      return 'An outcome';
  }
}

/** Where an appeal stands, as a badge word and its tone. */
export function appealState(a: Pick<StudentAppeal, 'state'>): { word: string; tone: 'wait' | 'ok' | 'muted' } {
  if (a.state === 'open') return { word: 'Under appeal', tone: 'wait' };
  if (a.state === 'upheld') return { word: 'Appeal upheld', tone: 'ok' };
  return { word: 'Appeal not upheld', tone: 'muted' };
}

/** The sentence under an appeal's state. */
export function appealSentence(a: StudentAppeal): string {
  if (a.state === 'open') return 'An organiser who took no part in the decision reads your appeal and rules on it.';
  if (a.state === 'upheld') return 'The decision was reversed. It no longer counts on your record.';
  return 'The decision stands.';
}

/** Characters as the server counts them: code points of the trimmed text. */
export function appealLength(text: string): number {
  return [...text.trim()].length;
}

/** What is wrong with an appeal's text, or "" when it can be sent. */
export function appealProblem(text: string): string {
  const n = appealLength(text);
  if (n === 0) return 'Write why you think the decision is wrong first.';
  if (n < MIN_APPEAL) return `Write at least ${MIN_APPEAL} characters, so the organiser can understand your appeal.`;
  if (n > MAX_APPEAL) return `Keep the appeal to ${MAX_APPEAL} characters.`;
  return '';
}

/**
 * Byte spans of 1-based line ranges in a UTF-8 source, for CodeView: each
 * range covers its lines without their final newline. Ranges past the end
 * are clamped, empty or reversed ones dropped.
 */
export function lineSpans(source: string, lines: readonly { from: number; to: number }[]): Span[] {
  const enc = new TextEncoder();
  const starts: number[] = [0];
  const parts = source.split('\n');
  let at = 0;
  for (let i = 0; i < parts.length; i++) {
    at += enc.encode(parts[i]).length;
    if (i < parts.length - 1) {
      at += 1;
      starts.push(at);
    }
  }
  const count = parts.length;
  const lineEnd = (n: number) => starts[n - 1] + enc.encode(parts[n - 1]).length;
  const out: Span[] = [];
  for (const r of lines) {
    const from = Math.max(1, Math.floor(r.from));
    const to = Math.min(count, Math.floor(r.to));
    if (!(to >= from) || from > count) continue;
    const span = { start: starts[from - 1], end: lineEnd(to) };
    if (span.end > span.start) out.push(span);
  }
  return out;
}

/** The highlight spans of one piece of evidence: its own spans, or a manual case's lines. */
export function evidenceSpans(ev: StudentEvidenceV14): Span[] {
  if (ev.kind === 'manual' && ev.source) return lineSpans(ev.source, ev.lines ?? []);
  return ev.spans ?? [];
}

/** The case's id from a "#case-12" address, as the notices link to it. */
export function caseFromHash(hash: string): number | null {
  const m = /^#case-(\d+)$/.exec(hash);
  return m ? Number(m[1]) : null;
}

/** Where a case stands for the student, as a badge word and tone. */
export function caseBadge(c: Pick<StudentCaseV14, 'state' | 'decided' | 'explanation' | 'outcome' | 'appeal'>): { word: string; tone: 'act' | 'wait' | 'ok' | 'part' | 'muted' } {
  if (c.appeal && c.appeal.state === 'open') return { word: 'Under appeal', tone: 'wait' };
  if (c.decided || c.state === 'cleared' || c.state === 'actioned') {
    if (c.outcome) return { word: 'Outcome decided', tone: 'part' };
    if (c.state === 'actioned') return { word: 'Decided', tone: 'muted' };
    return { word: 'Cleared', tone: 'ok' };
  }
  if (c.explanation.trim() !== '') return { word: 'Explanation saved', tone: 'muted' };
  return { word: 'Waiting for you', tone: 'act' };
}

/** The sentence for a case in the student's list: with an outcome, where to read it. */
export function caseSentence(c: Pick<StudentCaseV14, 'state' | 'decided' | 'explanation' | 'outcome'>): string {
  if (c.outcome) return 'Decided. Open the question to read the decision and the reason for it.';
  return studentStateText(c);
}
