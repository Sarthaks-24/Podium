/**
 * Submitting, running and the results of one workspace, and which result
 * tab shows; nothing replaces a result without the tab changing. Submit
 * sends an idempotency key per press, reused by "Submit again", so a retry
 * after a lost answer never counts twice. The gap between submits and a
 * server 429 count down in the button, and after a Run that did not
 * compile Submit asks once. While the page asks about the code (`held`),
 * Run and Submit do nothing. Results stream over GET /api/events with
 * polling as the fallback, and the announcer speaks each result once.
 * Construct it during component initialisation: its announcements are
 * effects owned by that component.
 */
import { polite } from './announce.svelte';
import { ApiError, errorText, getJSON, isSignedOut, sendJSON } from './api';
import { serverClock } from './clock';
import type { IntegrityRecorder } from './integrity';
import { LiveSubscription, type StreamMode } from './stream';
import { mergeRun, mergeSubmission, mergeSubmissions, reduceEvent, type JudgingState, type StreamEvent } from './submissionState';
import {
  announcement,
  firstFailure,
  isWaiting,
  newKey,
  runAnnouncement,
  runCompileFailed,
  workspacePaths,
  type Language,
  type Run,
  type Submission,
  type Workspace,
} from './workspace';

export type ResultTab = 'run' | 'submissions' | 'input';

/** A refusal or failure: "pace" is a normal pacing rule (a muted line), "error" a real failure. */
export interface Notice {
  action: 'submit' | 'run';
  text: string;
  retry: boolean;
  kind: 'pace' | 'error';
}

type RunBody = { language: Language; source: string; input?: string };

export interface JudgingOptions {
  base: string;
  problemId: () => number | undefined;
  /** True once the workspace has loaded. */
  ready: () => boolean;
  /** Why Run and Submit are off, or "". */
  locked: () => string;
  /** True while the page asks a question about the code (a language switch, a reset): Run and Submit wait for the answer. */
  held?: () => boolean;
  minGapS: () => number;
  code: () => { language: Language; source: string };
  integrity: () => IntegrityRecorder | null;
  /** Called after a submission is accepted (the draft is saved then). */
  submitted: () => void;
  /** Shows the result where a tab hides it (under 1024 px). */
  reveal: () => Promise<void>;
}

export class Judging {
  state = $state.raw<JudgingState>({ submissions: [], run: null, serverNow: null });
  tab = $state<ResultTab>('submissions');
  watchId = $state<number | null>(null);
  submitBusy = $state(false);
  runBusy = $state(false);
  notice = $state<Notice | null>(null);
  waitUntil = $state(0);
  nowTick = $state(Date.now());
  streamMode = $state<StreamMode>('connecting');
  confirmCompile = $state(false);
  customInput = $state('');

  /** The code and language of the last run, so a compile error is only warned about while the code is unchanged. */
  private lastRunBody = $state.raw<{ language: Language; source: string } | null>(null);
  private compileWarned = false;
  private pendingKey: string | null = null;
  private pendingRun: RunBody | null = null;
  private announced = '';
  private announcedRun = '';
  private stream: LiveSubscription | null = null;
  private alive = true;
  private readonly paths: ReturnType<typeof workspacePaths>;

  latest = $derived(this.state.submissions[0] ?? null);
  /** The submission the Submissions tab opens: the one just made, else the newest. */
  current = $derived(this.watchId !== null ? (this.state.submissions.find((s) => s.id === this.watchId) ?? this.latest) : this.latest);
  waitLeft = $derived(Math.max(0, Math.ceil((this.waitUntil - this.nowTick) / 1000)));
  /** A known wait counts down inside the Submit button; only a refusal without a known wait takes the status line. */
  paceText = $derived(this.notice?.kind === 'pace' && this.waitLeft === 0 && this.waitUntil === 0 ? this.notice.text : '');
  submitLabel = $derived(this.submitBusy ? 'Submitting' : this.waitLeft > 0 ? `Submit in ${this.waitLeft} s` : 'Submit');
  /** The latest run was on the student's own input. */
  customRun = $derived(this.state.run !== null && this.state.run.results.some((r) => r.kind === 'custom'));
  /** The open submission failed on an example, so Run shows the student's output. */
  exampleFailed = $derived(this.current?.state === 'done' ? (firstFailure(this.current)?.sample ?? false) : false);

  constructor(private readonly o: JudgingOptions) {
    this.paths = workspacePaths(o.base);

    // Announces the watched submission each time its state or verdict changes.
    $effect(() => {
      const s = this.watchId !== null ? this.state.submissions.find((x) => x.id === this.watchId) : undefined;
      if (!s) return;
      // A hidden result announces only the submit itself.
      const text = s.hidden ? 'Submitted.' : announcement(s);
      // Keyed by submission, so a second "Submitted." is spoken too.
      if (`${s.id}:${text}` !== this.announced) {
        this.announced = `${s.id}:${text}`;
        polite(text);
      }
    });

    // Announces the latest run as it starts and finishes, compile errors included.
    $effect(() => {
      const r = this.state.run;
      if (!r) return;
      const text = runAnnouncement(r);
      if (text !== '' && `${r.id}:${text}` !== this.announcedRun) {
        this.announcedRun = `${r.id}:${text}`;
        polite(text);
      }
    });

    // A pacing refusal is said once, politely; the line itself only counts down.
    $effect(() => {
      if (this.notice?.kind === 'pace') polite(this.notice.text);
    });

    // A wait that ran out takes its refusal with it.
    $effect(() => {
      if (this.notice?.kind === 'pace' && this.waitUntil > 0 && this.waitLeft === 0) this.notice = null;
    });
  }

  /** The last run did not compile and the code has not changed since. */
  get runDidNotCompile(): boolean {
    const run = this.state.run;
    const body = this.lastRunBody;
    const code = this.o.code();
    return run !== null && body !== null && runCompileFailed(run) && body.source === code.source && body.language === code.language;
  }

  /** Takes the submissions the workspace loaded with. */
  loaded(data: Workspace): void {
    this.state = { ...this.state, submissions: mergeSubmissions([], data.submissions ?? []), serverNow: data.server_now };
  }

  /** Keeps the countdown in the Submit button moving; called every second. */
  tick(): void {
    if (this.waitUntil > Date.now() - 1000) this.nowTick = Date.now();
  }

  startStream(extraEvents: readonly string[], onevent?: (type: string, data: unknown) => void, onresync?: () => void): void {
    this.stream?.stop();
    this.stream = new LiveSubscription({
      events: ['submission', 'run', 'clock', ...extraEvents],
      onEvent: (type, data) => {
        if (type === 'submission' || type === 'run' || type === 'clock') this.onStreamEvent({ type, data } as StreamEvent);
        onevent?.(type, data);
      },
      // Events sent while the stream was down are lost: the page reads its
      // own state again on reconnect and on each fallback poll.
      onReconnect: () => {
        onresync?.();
        return this.refetchOwn();
      },
      poll: () => {
        onresync?.();
        return this.pollPending();
      },
      onMode: (m) => (this.streamMode = m),
    });
    this.stream.start();
  }

  resume(): void {
    this.stream?.resume();
  }

  stop(): void {
    this.alive = false;
    this.stream?.stop();
  }

  async submit(retryKey?: string, confirmed = false): Promise<void> {
    if (this.submitBusy || !this.o.ready() || this.o.locked() !== '' || this.o.held?.()) return;
    if (this.waitLeft > 0 && retryKey === undefined) {
      polite(`You can submit again in ${this.waitLeft} s.`);
      return;
    }
    // A run that did not compile asks once before a submit uses up an attempt.
    if (retryKey === undefined && !confirmed && this.runDidNotCompile && !this.compileWarned) {
      this.confirmCompile = true;
      return;
    }
    const key = retryKey ?? newKey();
    this.pendingKey = key;
    this.notice = null;
    this.submitBusy = true;
    const integrity = this.o.integrity();
    // Integrity events go first, on their own; they never hold up the submit.
    integrity?.flush();
    const counts = integrity?.counts() ?? { typed_chars: 0, pasted_chars: 0, focus_losses: 0, fullscreen_exits: 0 };
    const body = { ...this.o.code(), ...counts, client_key: key };
    try {
      const s = await this.timed(() => sendJSON<Submission>('POST', this.paths.submit, body));
      if (!this.alive) return;
      this.pendingKey = null;
      integrity?.resetCounts();
      this.state = { ...this.state, submissions: mergeSubmission(this.state.submissions, s) };
      this.watchId = s.id;
      this.tab = 'submissions';
      // The contest's gap between submits counts down in the button.
      const gap = this.o.minGapS();
      if (gap > 0) {
        this.waitUntil = Date.now() + gap * 1000;
        this.nowTick = Date.now();
      }
      this.o.submitted();
      await this.o.reveal();
    } catch (e) {
      if (isSignedOut(e)) return;
      if (e instanceof ApiError && e.code === 'network') {
        this.notice = { action: 'submit', text: offlineText(), retry: true, kind: 'error' };
      } else {
        this.pendingKey = null;
        const pace = e instanceof ApiError && e.status === 429;
        if (pace && e.retryAfter !== null) {
          this.waitUntil = Date.now() + e.retryAfter * 1000;
          this.nowTick = Date.now();
        }
        this.notice = { action: 'submit', text: errorText(e), retry: false, kind: pace ? 'pace' : 'error' };
      }
      // A pacing line belongs beside Submit, and the announcer says it; only a failure opens the result.
      if (this.notice?.kind === 'error') await this.o.reveal();
    } finally {
      this.submitBusy = false;
    }
  }

  /** Answers the "did not compile" question; either way it is not asked again for this code. */
  answerCompile(submit: boolean): void {
    this.confirmCompile = false;
    this.compileWarned = true;
    if (submit) void this.submit(undefined, true);
  }

  /** Run on the examples. */
  runExamples(): void {
    void this.startRun({ ...this.o.code() });
  }

  /** Run on the input in Your input. */
  runCustom(): void {
    void this.startRun({ ...this.o.code(), input: this.customInput });
  }

  retry(): void {
    const n = this.notice;
    if (!n) return;
    if (n.action === 'submit' && this.pendingKey) void this.submit(this.pendingKey);
    else if (n.action === 'run' && this.pendingRun) void this.startRun(this.pendingRun);
  }

  private async startRun(body: RunBody): Promise<void> {
    if (this.runBusy || !this.o.ready() || this.o.locked() !== '' || this.o.held?.()) return;
    this.pendingRun = body;
    this.lastRunBody = { language: body.language, source: body.source };
    this.compileWarned = false;
    this.notice = null;
    this.runBusy = true;
    try {
      const r = await this.timed(() => sendJSON<Run>('POST', this.paths.run, body));
      if (!this.alive) return;
      this.pendingRun = null;
      this.state = { ...this.state, run: r };
      this.tab = body.input === undefined ? 'run' : 'input';
      await this.o.reveal();
    } catch (e) {
      if (isSignedOut(e)) return;
      if (e instanceof ApiError && e.code === 'network') {
        this.notice = { action: 'run', text: offlineText(), retry: true, kind: 'error' };
      } else {
        this.pendingRun = null;
        this.notice = { action: 'run', text: errorText(e), retry: false, kind: e instanceof ApiError && e.status === 429 ? 'pace' : 'error' };
      }
      if (this.notice?.kind === 'error') await this.o.reveal();
    } finally {
      this.runBusy = false;
    }
  }

  private onStreamEvent(ev: StreamEvent): void {
    const id = this.o.problemId();
    if (id === undefined) return;
    if (ev.type === 'clock' && typeof ev.data?.server_now === 'number') {
      const now = Date.now();
      serverClock.observe(ev.data.server_now, now, now);
    }
    this.state = reduceEvent(this.state, ev, id);
  }

  private async refetchOwn(): Promise<void> {
    try {
      const data = await this.timed(() => getJSON<Workspace>(this.paths.load));
      if (!this.alive) return;
      this.state = { ...this.state, submissions: mergeSubmissions(this.state.submissions, data.submissions ?? []) };
    } catch {
      // A 401 has signed the page out; anything else waits for the next poll.
    }
    await this.pollPending();
  }

  private async pollPending(): Promise<void> {
    for (const s of this.state.submissions.filter(isWaiting)) {
      try {
        const fresh = await this.timed(() => getJSON<Submission>(`/api/submissions/${s.id}`));
        if (this.alive) this.state = { ...this.state, submissions: mergeSubmission(this.state.submissions, fresh) };
      } catch (e) {
        if (isSignedOut(e)) return;
      }
    }
    const run = this.state.run;
    if (run && isWaiting(run)) {
      try {
        const fresh = await this.timed(() => getJSON<Run>(`/api/runs/${run.id}`));
        if (this.alive) this.state = { ...this.state, run: mergeRun(this.state.run, fresh) };
      } catch {
        // As above.
      }
    }
  }

  /** Runs a request and feeds its server_now to the clock. */
  private async timed<T extends { server_now?: number }>(fn: () => Promise<T>): Promise<T> {
    const sent = Date.now();
    const value = await fn();
    if (value && typeof value.server_now === 'number') serverClock.observe(value.server_now, sent, Date.now());
    return value;
  }
}

function offlineText(): string {
  return typeof navigator !== 'undefined' && navigator.onLine === false
    ? 'Not sent: you are offline. Your code is kept on this device. Retry when you are back online.'
    : 'Not sent: the server could not be reached. Your code is kept on this device. Retry in a moment.';
}
