// The server's own responses on the core path, as the Go test
// TestMiniContest records them in tests/fixtures (PODIUM_GOLDEN=1 rewrites
// them). A mock body typed Like<fixture> must carry every top-level key the
// server sends, so svelte-check fails when the server adds a field the mocks
// never send. Extra keys stay allowed: a mock may describe a case the
// recorded contest never reaches.
import type adminAnnouncements from '../fixtures/admin-announcements.json';
import type adminClarifications from '../fixtures/admin-clarifications.json';
import type adminContest from '../fixtures/admin-contest.json';
import type adminFeed from '../fixtures/admin-feed.json';
import type contest from '../fixtures/contest.json';
import type contests from '../fixtures/contests.json';
import type me from '../fixtures/me.json';
import type standings from '../fixtures/standings.json';
import type submission from '../fixtures/submission.json';
import type workspace from '../fixtures/workspace.json';

/** An object with every top-level key of T, of any value, and any other keys. */
export type Like<T> = { [K in keyof T]: unknown } & Record<string, unknown>;

export type MeLike = Like<typeof me>;
export type ContestListLike = Like<typeof contests>;
export type ContestSummaryLike = Like<(typeof contests)['live'][number]>;
export type ContestLike = Like<typeof contest>;
export type RulesLike = Like<(typeof contest)['rules']>;
export type WorkspaceLike = Like<typeof workspace>;
/** A submission as the mocks keep it; server_now is added when it is served. */
export type SubmissionLike = Like<Omit<typeof submission, 'server_now'>>;
export type StandingsLike = Like<typeof standings>;
export type AdminContestLike = Like<typeof adminContest>;
export type FeedLike = Like<typeof adminFeed>;
/** A feed row; the editor's counts are left out by the server when none were recorded. */
export type FeedRowLike = Like<Omit<(typeof adminFeed)['submissions'][number], 'typed_chars' | 'pasted_chars' | 'focus_losses' | 'fullscreen_exits'>>;
export type ClarificationsLike = Like<typeof adminClarifications>;
export type AnnouncementsLike = Like<typeof adminAnnouncements>;
