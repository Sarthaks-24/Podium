// The P3 public and contact pages (cache/v2-plan/phases/P3-public-pages.md
// FP.1 to FP.3): sign-in with the organisation's details, the signed-in
// contact form and the organisers' inbox (contract C6), and the privacy
// page with its retention table from the config (C5). Every /api/ call is
// answered by the mock below. The sweep saves screenshots to
// cache/screenshots-v2-p3/<screen>-<width>-<theme>.png at 375, 768, 1024
// and 1440 px in light and dark and checks one h1 and no horizontal
// overflow.
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
  allowed_domains: ['college.edu'],
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

async function mockApi(page: Page, who: Who, options: { contactMode?: 'ok' | 'rate'; config?: unknown } = {}) {
  const state = {
    who,
    sent: [] as { topic: string; message: string }[],
    deleted: [] as number[],
    inboxQueries: [] as string[],
    contactMode: options.contactMode ?? 'ok',
    messages: [
      { id: 12, topic: 'contest', message: 'The statement of problem B says 1 <= n but the sample has n = 0.\nWhich one is right?', created_at: NOW - 3_600_000, sender: { user_id: 7, email: 'asha@college.edu', name: 'Asha Rao', role: 'student' } },
      { id: 11, topic: 'privacy', message: 'Please send me a copy of what podium keeps about me.', created_at: NOW - 86_400_000, sender: { user_id: 9, email: 'ravi@college.edu', name: '', role: 'student' } },
    ],
  };
  await page.route(
    (url) => url.pathname.startsWith('/api/'),
    async (route) => {
      const req = route.request();
      const url = new URL(req.url());
      const path = url.pathname;
      const method = req.method();
      const me = state.who === 'owner' ? OWNER : state.who === 'student' ? STUDENT : null;
      if (path === '/api/auth/config') return json(route, 200, options.config ?? CONFIG);
      if (path === '/api/me') return me ? json(route, 200, { ...me, site_updated_at: null }) : apiError(route, 401, 'signed_out', 'Sign in to continue.');
      if (path === '/api/events') return route.abort();
      if (!me) return apiError(route, 401, 'signed_out', 'Sign in to continue.');
      if (path === '/api/contests') return json(route, 200, { live: [], upcoming: [], past: [] });
      if (path === '/api/integrity/cases') return json(route, 200, { cases: [], contests: {}, problems: {} });
      if (path === '/api/admin/contests') return json(route, 200, { contests: [], server_now: NOW });
      if (path === '/api/contact' && method === 'POST') {
        const body = req.postDataJSON() as { topic: string; message: string };
        if (state.contactMode === 'rate') {
          return apiError(route, 429, 'rate_limited', 'You have sent 5 messages in the last 24 hours. Try again tomorrow, or email the organisers.', { retry_after_s: 3600 });
        }
        state.sent.push(body);
        return json(route, 201, { id: 13, created_at: NOW });
      }
      if (path === '/api/admin/contact' && method === 'GET') {
        if (me.role === 'student') return apiError(route, 403, 'forbidden', 'Only organisers can read these messages.');
        state.inboxQueries.push(url.search);
        if (url.searchParams.get('before') === '11') {
          return json(route, 200, { messages: [{ id: 3, topic: 'other', message: 'An old note.', created_at: NOW - 30 * 86_400_000, sender: { user_id: 4, email: 'neha@college.edu', name: 'Neha Gupta', role: 'student' } }], more: false, server_now: NOW });
        }
        return json(route, 200, { messages: state.messages.filter((m) => !state.deleted.includes(m.id)), more: true, server_now: NOW });
      }
      const del = path.match(/^\/api\/admin\/contact\/(\d+)$/);
      if (del && method === 'DELETE') {
        state.deleted.push(Number(del[1]));
        return json(route, 204);
      }
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
  { name: 'contact-form', path: '/contact', who: 'student' as Who, ready: (p: Page) => expect(p.getByLabel('Message')).toBeVisible() },
  { name: 'contact-inbox', path: '/admin/contact', who: 'owner' as Who, ready: (p: Page) => expect(p.getByText('Asha Rao')).toBeVisible() },
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
  await mockApi(page, 'signed-out', { config: { allowed_domains: ['college.edu'], contact_email: '', mode: 'production', organisation: { name: '', contact_email: '', phone: '', privacy_contact: '' }, retention: [] } });
  await page.goto('/signin');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Weekly programming contests');
  await expect(page.getByText('Contact the organisers')).toHaveCount(0);
});

test('the contact form is for signed-in people only (C6)', async ({ page }) => {
  await mockApi(page, 'signed-out');
  await page.goto('/contact');
  await expect(page).toHaveURL('/signin?next=%2Fcontact');
});

test('a student writes to the organisers, from the footer (FP.2)', async ({ page }) => {
  const api = await mockApi(page, 'student');
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/');
  const foot = page.locator('footer');
  await expect(foot).toContainText('Example College of Engineering. Contact the organisers: podium@college.edu');
  await foot.getByRole('link', { name: 'Write to the organisers' }).click();
  await expect(page).toHaveURL('/contact');
  await expect(page.getByRole('heading', { level: 1 })).toBeFocused();
  await expect(page).toHaveTitle('Write to the organisers · podium');

  await page.getByRole('button', { name: 'Send message' }).click();
  await expect(page.getByText('Write your message first.')).toBeVisible();
  await expect(page.getByLabel('Message')).toBeFocused();
  await expect(page.getByLabel('Message')).toHaveAttribute('aria-invalid', 'true');
  expect(api.sent).toEqual([]);

  await page.getByLabel('About').selectOption('privacy');
  await page.getByLabel('Message').fill('  Please send me a copy of my data.  ');
  await expect(page.locator('#contact-count')).toContainText('33 of 2000 characters');
  await page.getByRole('button', { name: 'Send message' }).click();
  await expect(page.getByText('Sent. The organisers reply by email, usually within two days.')).toBeFocused();
  expect(api.sent).toEqual([{ topic: 'privacy', message: 'Please send me a copy of my data.' }]);
  await page.getByRole('button', { name: 'Write another message' }).click();
  await expect(page.getByLabel('Message')).toBeFocused();
  await expect(page.getByLabel('Message')).toHaveValue('');
});

test('a refused message keeps its text and says what to do', async ({ page }) => {
  await mockApi(page, 'student', { contactMode: 'rate' });
  await page.goto('/contact');
  await page.getByLabel('Message').fill('Problem C has no samples.');
  await page.getByRole('button', { name: 'Send message' }).click();
  await expect(page.getByRole('alert')).toHaveText('You have sent 5 messages in the last 24 hours. Try again tomorrow, or email the organisers.');
  await expect(page.getByLabel('Message')).toHaveValue('Problem C has no samples.');
});

test('organisers read, reply to and delete messages (FP.2)', async ({ page }) => {
  const api = await mockApi(page, 'owner');
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/contests');
  await page.getByRole('navigation', { name: 'Main' }).getByRole('link', { name: 'Inbox' }).click();
  await expect(page).toHaveURL('/admin/contact');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Messages to the organisers');
  const first = page.locator('.message').first();
  await expect(first).toContainText('Asha Rao');
  await expect(first).toContainText('A contest or a problem');
  // Plain text with its line break, never HTML.
  await expect(first.locator('.text')).toHaveText('The statement of problem B says 1 <= n but the sample has n = 0.\nWhich one is right?');
  await expect(first.getByRole('link', { name: 'Reply by email' })).toHaveAttribute('href', /^mailto:asha@college\.edu\?subject=/);

  await first.getByRole('button', { name: 'Delete' }).click();
  const ask = page.getByRole('dialog', { name: 'Delete this message?' });
  await expect(ask).toContainText('The message from Asha Rao is removed for every organiser.');
  await ask.getByRole('button', { name: 'Delete' }).click();
  await expect.poll(() => api.deleted).toEqual([12]);
  await expect(page.locator('.message')).toHaveCount(1);

  await page.getByRole('button', { name: 'Show older messages' }).click();
  await expect(page.locator('.message')).toHaveCount(2);
  expect(api.inboxQueries.at(-1)).toBe('?before=11');
  await expect(page.getByRole('button', { name: 'Show older messages' })).toHaveCount(0);
});

test('a student who opens the inbox is told why it is closed', async ({ page }) => {
  await mockApi(page, 'student');
  await page.goto('/admin/contact');
  await expect(page.getByText('Only organisers can read these messages.')).toBeVisible();
});
