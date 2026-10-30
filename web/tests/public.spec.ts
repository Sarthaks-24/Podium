// The public pages: sign-in with the organisation's details and the privacy
// page with its retention table from the config (C5). Every /api/ call is
// answered by the mock below. The sweep saves screenshots to
// cache/screenshots-v2-p3/<screen>-<width>-<theme>.png at 375, 768, 1024
// and 1440 px in light and dark and checks one h1 and no horizontal
// overflow.
import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Page, type Route } from '@playwright/test';

const WIDTHS = [375, 768, 1024, 1440] as const;
const THEMES = ['light', 'dark'] as const;

function shots(): string {
  return `${test.info().project.testDir}/../../cache/screenshots-v2-p3/`;
}

type Who = 'signed-out' | 'student' | 'owner';

const OWNER = { id: 1, email: 'priya@college.edu', name: 'Priya Raman', avatar_url: '', role: 'owner', theme: '', verdicts: 'standard' };
const STUDENT = { id: 7, email: 'asha@college.edu', name: 'Asha Rao', avatar_url: '', role: 'student', theme: '', verdicts: 'standard' };

export const RETENTION = [
  { id: 'sessions', data: 'Sign-in sessions', amount: 12, unit: 'hours', after: 'sign_in', kept: '12 hours after sign-in' },
  { id: 'integrity_events', data: 'Paste and focus records during contests', amount: 180, unit: 'days', after: 'contest_end', kept: '180 days after the contest' },
  { id: 'case_evidence', data: 'Evidence and explanations in a closed integrity case', amount: 2, unit: 'years', after: 'decision', kept: '2 years after the decision' },
  { id: 'run_payloads', data: 'The input and output of a run', amount: 30, unit: 'days', after: 'created', kept: '30 days after the run' },
  { id: 'contact_messages', data: 'Messages sent to the organisers', amount: 180, unit: 'days', after: 'created', kept: '180 days after they are sent' },
  { id: 'draft_revisions', data: 'Problem versions no contest used', amount: 90, unit: 'days', after: 'created', kept: '90 days after they are made' },
  { id: 'submissions', data: 'Submissions, their verdicts and what each contest showed', amount: 4, unit: 'years', after: 'contest_end', kept: '4 years after the contest' },
  { id: 'audit', data: 'Records of what organisers did', amount: 4, unit: 'years', after: 'created', kept: '4 years' },
  { id: 'backups', data: 'Nightly backups', amount: 14, unit: 'days', after: 'created', kept: '14 days' },
];

export const CONFIG = {
    contact_email: 'podium@college.edu',
  mode: 'production',
  organisation: { name: 'Example College of Engineering', contact_email: 'podium@college.edu', phone: '+91 80 1234 5678', privacy_contact: 'privacy@college.edu' },
  retention: RETENTION,
};

const NOW = Date.now();

function json(route: Route, status: number, body?: unknown) {
  return route.fulfill({ status, contentType: 'application/json; charset=utf-8', body: body === undefined ? '' : JSON.stringify(body) });
}

function apiError(route: Route, status: number, code: string, message: string, extra: Record<string, unknown> = {}) {
  return json(route, status, { error: { code, message, ...extra } });
}

async function mockApi(page: Page, who: Who, options: { config?: unknown; siteUpdatedAt?: number; live?: boolean } = {}) {
  const state = { who };
  await page.route(
    (url) => url.pathname.startsWith('/api/'),
    async (route) => {
      const req = route.request();
      const url = new URL(req.url());
      const path = url.pathname;
      const me = state.who === 'owner' ? OWNER : state.who === 'student' ? STUDENT : null;
      if (path === '/api/auth/config') return json(route, 200, options.config ?? CONFIG);
      if (path === '/api/me') return me ? json(route, 200, { ...me, site_updated_at: options.siteUpdatedAt ?? null }) : apiError(route, 401, 'signed_out', 'Sign in to continue.');
      if (path === '/api/events') return route.abort();
      if (!me) return apiError(route, 401, 'signed_out', 'Sign in to continue.');
      if (path === '/api/contests') {
        const live = options.live
          ? [{ id: 4, slug: 'week-6', title: 'Week 6 contest', state: 'live', starts_at: NOW - 3_600_000, ends_at: NOW + 3_600_000, close_at: NOW + 3_600_000, problem_count: 3, my_score: null, my_window: null }]
          : [];
        return json(route, 200, { live, upcoming: [], past: [], server_now: NOW });
      }
      if (path === '/api/integrity/cases') return json(route, 200, { cases: [], contests: {}, problems: {} });
      if (path === '/api/admin/contests') return json(route, 200, { contests: [], server_now: NOW });
      return apiError(route, 404, 'not_found', 'This API route does not exist.');
    },
  );
  return state;
}

async function settle(page: Page) {
  await page.evaluate(() => document.fonts.ready);
  await page.waitForTimeout(150);
}

const SCREENS = [
  { name: 'signin-organisation', path: '/signin', who: 'signed-out' as Who, ready: (p: Page) => expect(p.getByRole('link', { name: 'Continue with Google' })).toBeVisible() },
];

for (const theme of THEMES) {
  test.describe(`${theme} theme`, () => {
    test.use({ colorScheme: theme });
    for (const screen of SCREENS) {
      test(screen.name, async ({ page }) => {
        await mockApi(page, screen.who);
        await page.setViewportSize({ width: 1440, height: 900 });
        await page.goto(screen.path);
        await screen.ready(page);
        for (const width of WIDTHS) {
          await page.setViewportSize({ width, height: width < 768 ? 812 : 900 });
          await settle(page);
          const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
          expect(overflow, `horizontal overflow at ${width}px`).toBeLessThanOrEqual(0);
          await expect(page.locator('h1')).toHaveCount(1);
          await page.screenshot({ path: `${shots()}${screen.name}-${width}-${theme}.png`, fullPage: true });
        }
      });
    }
  });
}

test('sign-in names the organisation and how to reach it, and explains a rate limit (FP.3)', async ({ page }) => {
  await mockApi(page, 'signed-out');
  await page.goto('/signin?error=rate_limited');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Weekly programming contests for Example College of Engineering');
  await expect(page.getByRole('alert')).toHaveText('Too many people are signing in from this network at once. Wait a few seconds, then start again.');
  await expect(page.getByRole('link', { name: 'podium@college.edu' })).toHaveAttribute('href', 'mailto:podium@college.edu');
  await expect(page.getByRole('link', { name: '+91 80 1234 5678' })).toHaveAttribute('href', 'tel:+918012345678');
  // Privacy waits for the owner (D85): no link anywhere yet.
  await expect(page.getByRole('link', { name: /podium records about you|Privacy/ })).toHaveCount(0);
});

test('an organisation with nothing entered shows nothing made up', async ({ page }) => {
  await mockApi(page, 'signed-out', { config: { contact_email: '', mode: 'production', organisation: { name: '', contact_email: '', phone: '', privacy_contact: '' }, retention: [] } });
  await page.goto('/signin');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Weekly programming contests');
  await expect(page.getByText('Contact the organisers')).toHaveCount(0);
});

test('the privacy notice waits for the owner, and its draft lists every retention period from the config (FP.1)', async ({ page }) => {
  await mockApi(page, 'signed-out');
  await page.goto('/privacy');
  await expect(page.getByText('The privacy notice for podium is being written.', { exact: false })).toBeVisible();
  await expect(page.getByRole('table')).toHaveCount(0);

  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/privacy?draft');
  await expect(page.getByRole('note')).toHaveText("Draft for the owner's approval. Nothing links to this page yet.");
  await expect(page.getByRole('heading', { level: 2, name: 'What podium records' })).toBeVisible();
  const rows = page.getByRole('table', { name: 'How long podium keeps each kind of data' }).locator('tbody tr');
  await expect(rows).toHaveCount(RETENTION.length);
  for (const [i, r] of RETENTION.entries()) {
    await expect(rows.nth(i).locator('th')).toHaveText(r.data);
    await expect(rows.nth(i).locator('td')).toHaveText(r.kept);
  }
  await expect(page.getByRole('link', { name: 'privacy@college.edu' })).toHaveAttribute('href', 'mailto:privacy@college.edu');
  for (const width of WIDTHS) {
    await page.setViewportSize({ width, height: width < 768 ? 812 : 900 });
    await settle(page);
    expect(await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)).toBeLessThanOrEqual(0);
    await page.screenshot({ path: `${shots()}privacy-draft-${width}-light.png`, fullPage: true });
  }
});

for (const theme of THEMES) {
  test(`axe finds nothing serious on the P3 pages, ${theme}`, async ({ page }) => {
    test.setTimeout(120_000);
    await page.emulateMedia({ colorScheme: theme });
    for (const [path, who, ready] of [
      ['/signin?error=rate_limited', 'signed-out', 'Continue with Google'],
      ['/privacy?draft', 'signed-out', 'What podium records'],
    ] as const) {
      await page.unrouteAll({ behavior: 'ignoreErrors' });
      await mockApi(page, who);
      for (const width of [1440, 320]) {
        await page.setViewportSize({ width, height: 900 });
        await page.goto(path);
        await expect(page.getByText(ready).first()).toBeVisible();
        await settle(page);
        const results = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa']).analyze();
        const serious = results.violations.filter((v) => v.impact === 'serious' || v.impact === 'critical').map((v) => `${v.id}: ${v.nodes[0]?.target}`);
        expect(serious, `${path} at ${width}`).toEqual([]);
      }
    }
  });
}
