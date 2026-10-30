/**
 * The web app's API types against what the server really sends.
 *
 * tests/fixtures holds the JSON of the core-path routes as the Go test
 * TestMiniContest records it (server/internal/httpapi/golden_test.go;
 * PODIUM_GOLDEN=1 rewrites the files). Each file is assigned to its type
 * here, so svelte-check fails when a type wants a field the server does not
 * send, or gives a field another kind. Then each object is checked at run
 * time for every key its type requires, through key lists the compiler
 * holds to the types, so a list cannot fall behind its type.
 */
import { describe, expect, it } from 'vitest';
import adminAnnouncementsJson from '../../tests/fixtures/admin-announcements.json';
import adminClarificationsJson from '../../tests/fixtures/admin-clarifications.json';
import adminContestJson from '../../tests/fixtures/admin-contest.json';
import adminContestsJson from '../../tests/fixtures/admin-contests.json';
import adminFeedJson from '../../tests/fixtures/admin-feed.json';
import adminStandingsJson from '../../tests/fixtures/admin-standings.json';
import adminStatsJson from '../../tests/fixtures/admin-stats.json';
import contestJson from '../../tests/fixtures/contest.json';
import contestsJson from '../../tests/fixtures/contests.json';
import meJson from '../../tests/fixtures/me.json';
import standingsJson from '../../tests/fixtures/standings.json';
import submissionJson from '../../tests/fixtures/submission.json';
import submitJson from '../../tests/fixtures/submit.json';
import workspaceJson from '../../tests/fixtures/workspace.json';
import type {
  AdminContest,
  AdminContestProblem,
  AdminContestSummary,
  Announcement,
  Clarification,
  Contest,
  ContestList,
  ContestProblem,
  ContestRules,
  ContestStats,
  ContestSummary,
  FeedPage,
  FeedRow,
  ProblemStats,
  StudentWindow,
} from './contests';
import type { User } from './session.svelte';
import type { Standings, StandingsCell, StandingsRow } from './standings';
import type { Sample, Submission, SubtaskResult, TestResult, Workspace } from './workspace';

/** T as an imported JSON file is typed: string, number and boolean literals widened. */
type Widened<T> = T extends string
  ? string
  : T extends number
    ? number
    : T extends boolean
      ? boolean
      : T extends null | undefined
        ? T
        : T extends readonly (infer U)[]
          ? Widened<U>[]
          : { [K in keyof T]: Widened<T[K]> };

/** Every key of T, marked as T has it; the compiler refuses a list that misses a key or marks one wrongly. */
type Keys<T> = { [K in keyof T]-?: undefined extends T[K] ? 'optional' : 'required' };

// The responses, each assigned to the type the web app reads it as.
const me: Widened<User> = meJson;
const contests: Widened<ContestList> = contestsJson;
const contest: Widened<Contest> = contestJson;
const workspace: Widened<Workspace> = workspaceJson;
const submitted: Widened<Submission> = submitJson;
const submission: Widened<Submission> = submissionJson;
const standings: Widened<Standings> = standingsJson;
const adminContests: Widened<{ contests: AdminContestSummary[]; server_now: number }> = adminContestsJson;
const adminContest: Widened<AdminContest> = adminContestJson;
const adminFeed: Widened<FeedPage> = adminFeedJson;
const adminStandings: Widened<Standings> = adminStandingsJson;
const adminClarifications: Widened<{ clarifications: Clarification[] }> = adminClarificationsJson;
const adminAnnouncements: Widened<{ announcements: Announcement[] }> = adminAnnouncementsJson;
const adminStats: Widened<ContestStats> = adminStatsJson;

const USER: Keys<User> = { id: 'required', email: 'required', name: 'required', avatar_url: 'required', role: 'required', theme: 'required', verdicts: 'required' };
const WINDOW: Keys<StudentWindow> = { started_at: 'required', ends_at: 'required' };
const SUMMARY: Keys<ContestSummary> = {
  slug: 'required',
  title: 'required',
  starts_at: 'required',
  ends_at: 'required',
  problem_count: 'required',
  state: 'required',
  my_score: 'required',
  max_score: 'optional',
  my_window: 'required',
};
const LIST: Keys<ContestList> = { live: 'required', upcoming: 'required', past: 'required', server_now: 'optional' };
const RULES: Keys<ContestRules> = {
  min_gap_s: 'required',
  max_per_problem: 'required',
  languages: 'required',
  freeze_minutes: 'required',
  full_screen: 'optional',
  count_focus: 'optional',
  block_paste: 'optional',
  block_copy: 'optional',
};
const PROBLEM: Keys<ContestProblem> = { label: 'required', title: 'required', my_score: 'required', max_score: 'required', my_submission_count: 'optional' };
const ANNOUNCEMENT: Keys<Announcement> = { id: 'required', body_md: 'required', created_at: 'required' };
const CLARIFICATION: Keys<Clarification> = {
  id: 'required',
  problem_label: 'required',
  question: 'required',
  answer: 'required',
  public: 'required',
  created_at: 'required',
  answered_at: 'required',
  asked_by: 'optional',
};
const CONTEST: Keys<Contest> = {
  ...SUMMARY,
  id: 'optional',
  description_md: 'required',
  rules: 'required',
  honour_text: 'required',
  problems: 'required',
  announcements: 'required',
  my_clarifications: 'required',
  public_clarifications: 'required',
  editorial_md: 'required',
  server_now: 'required',
  accepted: 'required',
  second_session: 'optional',
  organiser: 'required',
};
const SAMPLE: Keys<Sample> = { idx: 'required', input: 'required', output: 'required', truncated: 'required' };
const WORKSPACE: Keys<Workspace> = {
  problem_id: 'required',
  title: 'required',
  statement_md: 'required',
  time_limit_ms: 'required',
  memory_limit_mb: 'required',
  languages: 'required',
  samples: 'required',
  starters: 'required',
  draft: 'required',
  submissions: 'required',
  server_now: 'required',
  refusal: 'optional',
  can_submit: 'optional',
  practice: 'optional',
};
const TEST: Keys<TestResult> = { idx: 'required', verdict: 'required', time_ms: 'required', memory_kb: 'required', message: 'optional' };
const SUBTASK: Keys<SubtaskResult> = { idx: 'required', points: 'required', earned: 'required', verdict: 'required', tests: 'required' };
const SUBMISSION: Keys<Submission> = {
  id: 'required',
  problem_id: 'required',
  language: 'required',
  state: 'required',
  verdict: 'required',
  score: 'required',
  max_score: 'required',
  max_time_ms: 'required',
  max_memory_kb: 'required',
  created_at: 'required',
  judged_at: 'required',
  queue_position: 'required',
  compile_output: 'required',
  subtasks: 'required',
  samples: 'required',
  server_now: 'optional',
  hidden: 'optional',
  revealed: 'optional',
  time_step_ms: 'optional',
};
const STANDINGS: Keys<Standings> = {
  variant: 'required',
  version: 'required',
  frozen_at: 'required',
  final: 'required',
  judging_remaining: 'required',
  rows: 'required',
  me: 'required',
  labels: 'optional',
  server_now: 'optional',
};
const ROW: Keys<StandingsRow> = { rank: 'required', user_id: 'required', name: 'required', email: 'optional', total: 'required', last_improvement_s: 'required', cells: 'required' };
const CELL: Keys<StandingsCell> = { label: 'required', score: 'required', max: 'required', pending: 'required', tried: 'optional' };
const ADMIN_PROBLEM: Keys<AdminContestProblem> = {
  label: 'required',
  problem_id: 'required',
  title: 'required',
  max_score: 'optional',
  ready: 'optional',
  revision_id: 'optional',
  rev: 'optional',
  latest_revision_id: 'optional',
  latest_rev: 'optional',
  update_available: 'optional',
  update_statement_only: 'optional',
  reconstructed: 'optional',
};
const ADMIN_SUMMARY: Keys<AdminContestSummary> = {
  id: 'required',
  slug: 'required',
  title: 'required',
  starts_at: 'required',
  ends_at: 'required',
  state: 'required',
  published: 'required',
  archived: 'required',
  problems: 'required',
  paused: 'required',
  close_at: 'required',
};
const ADMIN_CONTEST: Keys<AdminContest> = {
  ...ADMIN_SUMMARY,
  description_md: 'required',
  freeze_minutes: 'required',
  practice_after_end: 'required',
  min_gap_s: 'required',
  max_per_problem: 'required',
  languages: 'required',
  honour_text: 'required',
  hold_reveal: 'optional',
  full_screen: 'optional',
  count_focus: 'optional',
  block_paste: 'optional',
  block_copy: 'optional',
  extended_minutes: 'required',
  editorial_md: 'required',
  editorial_published: 'required',
  revealed_at: 'required',
  extensions: 'optional',
  default_honour_text: 'required',
  queue: 'optional',
  judge_last_claim_at: 'required',
  unanswered: 'required',
  server_now: 'required',
};
const FEED_ROW: Keys<FeedRow> = {
  id: 'required',
  user_id: 'required',
  name: 'required',
  email: 'required',
  problem_label: 'required',
  language: 'required',
  state: 'required',
  verdict: 'required',
  score: 'required',
  max_score: 'required',
  created_at: 'required',
  judged_at: 'required',
  practice: 'required',
  rejudged: 'required',
  hidden: 'optional',
  typed_chars: 'optional',
  pasted_chars: 'optional',
  focus_losses: 'optional',
  fullscreen_exits: 'optional',
};
const FEED: Keys<FeedPage> = { submissions: 'required', page: 'required', has_more: 'required' };
const STATS: Keys<ContestStats> = { problems: 'required', started: 'required' };
const PROBLEM_STATS: Keys<ProblemStats> = {
  label: 'required',
  title: 'required',
  solved: 'required',
  attempted: 'required',
  median_attempts: 'required',
  histogram: 'required',
  max_score: 'required',
};

/** The required keys of `keys` that `value` lacks, as "where.key". */
function missing(keys: Record<string, 'required' | 'optional'>, value: object, where: string): string[] {
  return Object.entries(keys)
    .filter(([k, mark]) => mark === 'required' && !(k in value))
    .map(([k]) => `${where}.${k}`);
}

/** missing() over every item of a list, which must not be empty. */
function missingEach(keys: Record<string, 'required' | 'optional'>, values: readonly object[], where: string): string[] {
  expect(values.length, `${where} is empty, so its items go unchecked`).toBeGreaterThan(0);
  return values.flatMap((v, i) => missing(keys, v, `${where}[${i}]`));
}

function submissionGaps(s: Widened<Submission>, where: string): string[] {
  return [
    ...missing(SUBMISSION, s, where),
    ...missingEach(TEST, s.samples, `${where}.samples`),
    ...missingEach(SUBTASK, s.subtasks, `${where}.subtasks`),
    ...s.subtasks.flatMap((t, i) => missingEach(TEST, t.tests, `${where}.subtasks[${i}].tests`)),
  ];
}

function standingsGaps(s: Widened<Standings>, where: string): string[] {
  return [
    ...missing(STANDINGS, s, where),
    ...missingEach(ROW, s.rows, `${where}.rows`),
    ...s.rows.flatMap((r, i) => missingEach(CELL, r.cells, `${where}.rows[${i}].cells`)),
  ];
}

describe('the student core path matches the server', () => {
  it('GET /api/me', () => {
    expect(missing(USER, me, 'me')).toEqual([]);
  });

  it('GET /api/contests', () => {
    const rows = [...contests.live, ...contests.upcoming, ...contests.past];
    expect([
      ...missing(LIST, contests, 'list'),
      ...missingEach(SUMMARY, rows, 'list.rows'),
      ...rows.flatMap((r, i) => (r.my_window ? missing(WINDOW, r.my_window, `list.rows[${i}].my_window`) : [])),
    ]).toEqual([]);
  });

  it('GET /api/contests/{slug}', () => {
    expect([
      ...missing(CONTEST, contest, 'contest'),
      ...missing(RULES, contest.rules, 'contest.rules'),
      ...missingEach(PROBLEM, contest.problems ?? [], 'contest.problems'),
      ...missingEach(ANNOUNCEMENT, contest.announcements, 'contest.announcements'),
      ...missingEach(CLARIFICATION, contest.my_clarifications, 'contest.my_clarifications'),
    ]).toEqual([]);
  });

  it('GET /api/contests/{slug}/problems/{label}', () => {
    expect([
      ...missing(WORKSPACE, workspace, 'workspace'),
      ...missingEach(SAMPLE, workspace.samples, 'workspace.samples'),
      ...workspace.submissions.flatMap((s, i) => submissionGaps(s, `workspace.submissions[${i}]`)),
    ]).toEqual([]);
    expect(workspace.submissions.length).toBeGreaterThan(0);
  });

  it('POST submit and GET /api/submissions/{id}', () => {
    expect([...submissionGaps(submitted, 'submit'), ...submissionGaps(submission, 'submission')]).toEqual([]);
  });

  it('GET /api/contests/{slug}/standings', () => {
    expect(standingsGaps(standings, 'standings')).toEqual([]);
    expect(standings.variant).toBe('frozen');
  });
});

describe('the organiser core path matches the server', () => {
  it('GET /api/admin/contests', () => {
    expect([
      ...missingEach(ADMIN_SUMMARY, adminContests.contests, 'contests'),
      ...adminContests.contests.flatMap((c, i) => missingEach(ADMIN_PROBLEM, c.problems, `contests[${i}].problems`)),
    ]).toEqual([]);
  });

  it('GET /api/admin/contests/{id}', () => {
    expect([...missing(ADMIN_CONTEST, adminContest, 'contest'), ...missingEach(ADMIN_PROBLEM, adminContest.problems, 'contest.problems')]).toEqual([]);
  });

  it('GET /api/admin/contests/{id}/submissions', () => {
    expect([...missing(FEED, adminFeed, 'feed'), ...missingEach(FEED_ROW, adminFeed.submissions, 'feed.submissions')]).toEqual([]);
  });

  it('GET /api/admin/contests/{id}/standings', () => {
    expect(standingsGaps(adminStandings, 'standings')).toEqual([]);
    expect(adminStandings.rows.every((r) => typeof r.email === 'string')).toBe(true);
  });

  it('GET /api/admin/contests/{id}/clarifications and announcements', () => {
    expect([
      ...missingEach(CLARIFICATION, adminClarifications.clarifications, 'clarifications'),
      ...missingEach(ANNOUNCEMENT, adminAnnouncements.announcements, 'announcements'),
    ]).toEqual([]);
    expect(adminClarifications.clarifications.every((c) => typeof c.asked_by?.name === 'string')).toBe(true);
  });

  it('GET /api/admin/contests/{id}/stats', () => {
    expect([...missing(STATS, adminStats, 'stats'), ...missingEach(PROBLEM_STATS, adminStats.problems, 'stats.problems')]).toEqual([]);
  });
});
