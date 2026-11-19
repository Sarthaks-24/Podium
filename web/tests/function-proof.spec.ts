// Language checks on the problem page (v2.1.0 stage F8): for a function
// problem, the "Podium is checking" banner with Validate disabled, the
// Languages panel inside the "What students write" fold (Offered with its last
// result, "Not checked yet", Not offered with its reason and Details), Fix a
// language with AI, Check a language with a pasted solution, the "save first"
// line, and every refusal of the three routes shown next to its control. A
// whole-program problem shows none of it. The mocks are those of
// problems.spec.ts with extra routes (the way ai-edit.spec.ts does it): the
// problem as a function problem, GET .../proof walking a script, and the check,
// ai-fix and validate routes. Screenshots at 375, 768, 1024 and 1440 px in
// light and dark go to $PODIUM_SHOTS (default cache/v2.1.0/shots/F/).
import AxeBuilder from '@axe-core/playwright';
import { mkdirSync } from 'node:fs';
import { expect, test, type Locator, type Page } from '@playwright/test';
import { AXE_TAGS } from './axe';
import { activate } from './keyboard';
import { toasts } from './locators';
import { proofLanguages } from './mocks/ai';
import * as bank from './mocks/problems';

const WIDTHS = [375, 768, 1024, 1440] as const;
const THEMES = ['light', 'dark'] as const;

function shotDir(): string {
  const dir = process.env.PODIUM_SHOTS ?? `${test.info().project.testDir}/../../cache/v2.1.0/shots/F/`;
  mkdirSync(dir, { recursive: true });
  return dir;
}

const KEY = {
  active: true,
  provider: 'anthropic',
  model: 'claude-opus-5-5',
  active_until: bank.T0 + 3_600_000,
  server_now: bank.T0,
  providers: [{ id: 'anthropic', name: 'Anthropic', has_key: true, model: 'claude-opus-5-5', active_until: bank.T0 + 3_600_000, models: [{ id: 'claude-opus-5-5', name: 'Claude Opus 5.5' }] }],
};

const CODE = (language: string) => ({ top: `// top ${language}\n`, starter: `// starter ${language}\n`, bottom: `// bottom ${language}\n` });

/** Problem 1 as the page shows it before it has passed, so the bar has Validate and not Add to contest. */
const unvalidated = (): bank.Json => ({ ...bank.problemFixtures()[1], validated: false, validated_at: null, validated_hash: null });

/** Problem 1 as a function problem offering C++, Python and JavaScript; Java has no locked code. */
function functionProblem(): bank.Json {
  const offered = ['cpp17', 'python3', 'node22'];
  return {
    ...unvalidated(),
    mode: 'function',
    locked_top: Object.fromEntries(offered.map((l) => [l, CODE(l).top])),
    locked_bottom: Object.fromEntries(offered.map((l) => [l, CODE(l).bottom])),
    starters: Object.fromEntries(offered.map((l) => [l, CODE(l).starter])),
  };
}

type Lang = Record<string, bank.Json>;
type Proof = { checking: boolean; progress?: bank.Json; languages: bank.Json[]; applied?: bank.Json };

const lang = (over: Lang = {}): bank.Json[] => proofLanguages(over);
const passed = (): Lang => ({ cpp17: { result: 'passed', total: 12 }, python3: { result: 'passed', total: 12 }, node22: { result: 'passed', total: 12 } });
const JAVA_FAILED = { offered: false, result: 'failed', reason: 'wrong_answer', failed: 3, total: 12, tests: [{ idx: 4, verdict: 'WA' }, { idx: 9, verdict: 'TLE' }] };
/** The mixed state of the language list: C++ passed, Java failed, Python never checked, JavaScript passed before an edit. */
const MIXED: Proof = {
  checking: false,
  languages: lang({ cpp17: { result: 'passed', total: 12 }, java21: JAVA_FAILED, python3: { result: 'none' }, node22: { result: 'passed', total: 12, current: false } }),
  applied: { removed: ['java21'], validating: true },
};
const RUNNING: Proof = { checking: true, progress: { done: 1, total: 2, current: 'java21' }, languages: lang({ ...passed(), java21: JAVA_FAILED }) };

interface Api {
  /** What each GET .../proof answers, one after another; the last repeats. */
  script: Proof[];
  proofGets: number;
  problemGets: number;
  checks: { language: string; body: bank.Json }[];
  fixes: { language: string; body: bank.Json }[];
  validates: number;
  /** The answer of the next check, ai-fix or validate: a refusal or the default 202. */
  refuse?: { status: number; code: string; message: string; extra?: bank.Json };
  /** Wait this long before the check or fix answers. */
  delay: number;
  /** How many of the next GET .../proof answers are a 500. */
  failProofGets: number;
  /** When set, a check or fix moves the script to this and is answered 202. */
  after?: Proof[];
}

async function setup(page: Page, opts: { mode?: 'function' | 'whole'; script?: Proof[] } = {}): Promise<Api> {
  await bank.mockApi(page);
  const api: Api = { script: opts.script ?? [MIXED], proofGets: 0, problemGets: 0, checks: [], fixes: [], validates: 0, delay: 0, failProofGets: 0 };
  await page.route(
    (url) => url.pathname.startsWith('/api/'),
    async (route) => {
      const req = route.request();
      const path = new URL(req.url()).pathname;
      const method = req.method();
      if (path === '/api/admin/ai/key' && method === 'GET') return bank.json(route, 200, KEY);
      if (path === '/api/admin/problems/1' && method === 'GET') {
        api.problemGets++;
        return bank.json(route, 200, opts.mode === 'whole' ? unvalidated() : functionProblem());
      }
      if (path === '/api/admin/problems/1/proof' && method === 'GET') {
        if (api.failProofGets > 0) {
          api.failProofGets--;
          return bank.apiError(route, 500, 'internal', 'The server failed.');
        }
        api.proofGets++;
        const at = Math.min(api.proofGets, api.script.length) - 1;
        return bank.json(route, 200, { ...api.script[at], server_now: bank.T0 });
      }
      const m = path.match(/^\/api\/admin\/problems\/1\/languages\/([^/]+)\/(check|ai-fix)$/);
      if (m && method === 'POST') {
        const body = (req.postDataJSON() ?? {}) as bank.Json;
        (m[2] === 'check' ? api.checks : api.fixes).push({ language: m[1], body });
        if (api.delay > 0) await new Promise((r) => setTimeout(r, api.delay));
        if (api.refuse) return bank.apiError(route, api.refuse.status, api.refuse.code, api.refuse.message, api.refuse.extra ?? {});
        if (api.after) {
          api.script = api.after;
          api.proofGets = 0;
        }
        return bank.json(route, 202, { state: 'checking', language: m[1], provider: 'anthropic', model: 'claude-opus-5-5', server_now: bank.T0 });
      }
      if (path === '/api/admin/problems/1/validate' && method === 'POST') {
        api.validates++;
        if (api.refuse) return bank.apiError(route, api.refuse.status, api.refuse.code, api.refuse.message);
      }
      return route.fallback();
    },
  );
  return api;
}

async function openFold(page: Page) {
  await page.goto('/admin/problems/1');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('A plus B');
  await page.locator('summary', { hasText: 'What students write' }).click();
}

const panel = (page: Page) => page.getByRole('region', { name: 'Languages' });
const row = (page: Page, name: string) => panel(page).getByRole('group', { name, exact: true });
const validateButton = (page: Page) => page.getByRole('button', { name: /^Validate|^Save and validate/ });
const checkBox = async (page: Page, name: string) => {
  const r = row(page, name);
  await r.locator('summary', { hasText: `Check ${name}` }).click();
  return r;
};

test('a whole-program problem shows none of it and never asks for the checks', async ({ page }) => {
  const api = await setup(page, { mode: 'whole' });
  await page.goto('/admin/problems/1');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('A plus B');
  await page.locator('summary', { hasText: 'What students write' }).click();
  await expect(page.getByRole('region', { name: 'Languages' })).toHaveCount(0);
  await expect(page.getByRole('button', { name: /^Fix .* with AI$/ })).toHaveCount(0);
  await expect(page.getByRole('button', { name: /^Check / })).toHaveCount(0);
  await expect(page.getByText('Podium is checking the languages of this problem.')).toHaveCount(0);
  await expect(validateButton(page)).toBeEnabled();
  expect(api.proofGets).toBe(0);
});

test('while the languages are being checked a banner says so and Validate is disabled with the same sentence', async ({ page }) => {
  const api = await setup(page, { script: [RUNNING, RUNNING, MIXED] });
  await page.goto('/admin/problems/1');
  await expect(page.getByRole('status')).toHaveCount(1);
  const banner = page.locator('.banner-info', { hasText: 'Podium is checking the languages of this problem. This takes a few minutes.' });
  await expect(banner).toBeVisible();
  await expect(banner).toHaveClass(/banner-info/);
  const validate = validateButton(page);
  await expect(validate).toBeDisabled();
  await expect(validate).toHaveAccessibleDescription(/Podium is checking the languages of this problem\. This takes a few minutes\./);
  // The round ends: the banner goes, Validate works again and the problem is read again.
  const gets = api.problemGets;
  await expect(banner).toHaveCount(0, { timeout: 10_000 });
  await expect(validate).toBeEnabled();
  expect(api.problemGets).toBeGreaterThan(gets);
});

test('pressing Validate when the server says it is checking shows the server sentence and starts watching', async ({ page }) => {
  const api = await setup(page, { script: [{ checking: false, languages: lang(passed()) }, RUNNING] });
  api.refuse = { status: 409, code: 'checking', message: 'Podium is still checking the languages of this problem.' };
  await page.goto('/admin/problems/1');
  await expect(validateButton(page)).toBeEnabled();
  await validateButton(page).click();
  await expect(page.getByRole('alert').filter({ hasText: 'Podium is still checking the languages of this problem.' })).toBeVisible();
  await expect(page.locator('.banner-info', { hasText: 'Podium is checking the languages of this problem.' })).toBeVisible();
  expect(api.validates).toBe(1);
});

test('the languages are listed: offered with the last result, not checked yet, and not offered with its reason', async ({ page }) => {
  await setup(page);
  await openFold(page);
  await expect(panel(page)).toBeVisible();
  const cpp = row(page, 'C++');
  await expect(cpp).toContainText('Offered');
  await expect(cpp).toContainText('Passes every test.');
  const python = row(page, 'Python');
  await expect(python).toContainText('Offered');
  await expect(python).toContainText('Not checked yet. Press Check Python before a contest.');
  await expect(row(page, 'JavaScript')).toContainText('Passed before the last change. Press Check JavaScript again.');
  const java = row(page, 'Java');
  await expect(java).toContainText('Not offered');
  await expect(java.locator('.banner-error')).toContainText("The AI's Java code failed 3 of 12 tests.");
  await expect(java.locator('.banner-error svg')).toHaveCount(1);
  const details = java.locator('details', { hasText: 'Details' });
  await expect(details).not.toHaveAttribute('open', '');
  await details.locator('summary', { hasText: 'Details' }).click();
  await expect(details).toContainText('Test 4: Wrong answer');
  await expect(details).toContainText('Test 9: Time limit exceeded');
});

test('"Not checked yet" shows in the fold summary so a hand-edited problem is not missed', async ({ page }) => {
  await setup(page);
  await page.goto('/admin/problems/1');
  await expect(page.locator('summary', { hasText: 'What students write' })).toContainText('Not checked yet: Python');
});

test('Fix is for a language that is not offered and Check for one that is', async ({ page }) => {
  await setup(page);
  await openFold(page);
  await expect(page.getByRole('button', { name: /^Fix .* with AI$/ })).toHaveText(['Fix Java with AI']);
  for (const name of ['C++', 'Python', 'JavaScript']) await expect(row(page, name).locator('summary', { hasText: `Check ${name}` })).toBeVisible();
  await expect(row(page, 'Java').locator('summary', { hasText: 'Check Java' })).toHaveCount(0);
  await expect(row(page, 'C++').getByRole('button', { name: /^Fix / })).toHaveCount(0);
  const r = await checkBox(page, 'C++');
  await expect(r.getByLabel('Your solution in C++')).toBeVisible();
  await expect(r.getByText('The solution is used for this check and is not saved with the problem.')).toBeVisible();
  await expect(r.getByRole('button', { name: 'Check C++' })).toBeVisible();
});

test('Fix Java with AI works, then checks, then offers Java with a toast and focus on the language', async ({ page }) => {
  const api = await setup(page);
  api.delay = 500;
  api.after = [RUNNING, { checking: false, languages: lang({ ...passed(), java21: { offered: true, result: 'passed', total: 12, source: 'fix' } }), applied: { added: 'java21', validating: true } }];
  await openFold(page);
  await expect(row(page, 'Java').getByText("The AI writes new locked code and starting code for Java. Your current Java code is replaced only if the AI's version passes every test.")).toBeVisible();
  const fix = page.getByRole('button', { name: 'Fix Java with AI' });
  await fix.click();
  const working = row(page, 'Java').getByRole('button', { name: 'Asking the AI' });
  await expect(working).toBeDisabled();
  await expect(working).toHaveAttribute('aria-busy', 'true');
  await expect(row(page, 'Java').getByRole('button', { name: 'Checking Java' })).toHaveAttribute('aria-busy', 'true');
  await expect(toasts(page)).toContainText('Java passes every test and is now offered.', { timeout: 10_000 });
  expect(api.fixes).toEqual([{ language: 'java21', body: {} }]);
  await expect(row(page, 'Java')).toContainText('Offered');
  await expect(page.getByRole('button', { name: 'Fix Java with AI' })).toHaveCount(0);
  await expect(row(page, 'Java')).toBeFocused();
});

test('a Fix that fails keeps the block, says what failed and that nothing changed, and returns focus to the button', async ({ page }) => {
  const api = await setup(page);
  api.after = [{ checking: false, languages: lang({ ...passed(), java21: { ...JAVA_FAILED, failed: 2, total: 12, source: 'fix' } }), applied: { kept: 'not_added', validating: false } }];
  await openFold(page);
  await page.getByRole('button', { name: 'Fix Java with AI' }).click();
  const line = row(page, 'Java').getByRole('status');
  await expect(line).toHaveText("The AI's Java code failed 2 of 12 tests. Nothing was changed.", { timeout: 10_000 });
  await expect(page.getByRole('button', { name: 'Fix Java with AI' })).toBeFocused();
  await expect(toasts(page)).not.toContainText('now offered');
});

test('Check runs on a pasted solution and says the result in one status line', async ({ page }) => {
  const api = await setup(page);
  api.delay = 400;
  api.after = [RUNNING, { checking: false, languages: lang({ ...passed(), java21: JAVA_FAILED, node22: { result: 'failed', reason: 'wrong_answer', failed: 2, total: 12, source: 'check' } }) }];
  await openFold(page);
  const r = await checkBox(page, 'JavaScript');
  await r.getByLabel('Your solution in JavaScript').fill('function solve(a) { return 0; }');
  await r.getByRole('button', { name: 'Check JavaScript' }).click();
  await expect(r.getByRole('button', { name: 'Checking JavaScript' })).toHaveAttribute('aria-busy', 'true');
  await expect(r.getByRole('status')).toHaveText('JavaScript fails 2 of 12 tests.', { timeout: 10_000 });
  expect(api.checks).toEqual([{ language: 'node22', body: { solution: 'function solve(a) { return 0; }' } }]);
  await expect(r.getByRole('button', { name: 'Check JavaScript' })).toBeFocused();
  await expect(toasts(page)).not.toContainText('passes');
});

test('Check says when a language passes', async ({ page }) => {
  const api = await setup(page);
  api.after = [{ checking: false, languages: lang({ ...passed(), java21: JAVA_FAILED, python3: { result: 'passed', total: 12, source: 'check' } }) }];
  await openFold(page);
  const r = await checkBox(page, 'Python');
  await r.getByLabel('Your solution in Python').fill('def solve(a): return sum(a)');
  await r.getByRole('button', { name: 'Check Python' }).click();
  await expect(r.getByRole('status')).toHaveText('Python passes every test.', { timeout: 10_000 });
});

test('an empty paste box is refused before anything is sent, tied to the box', async ({ page }) => {
  const api = await setup(page);
  await openFold(page);
  const r = await checkBox(page, 'C++');
  await r.getByRole('button', { name: 'Check C++' }).click();
  const box = r.getByLabel('Your solution in C++');
  await expect(box).toBeFocused();
  await expect(box).toHaveAttribute('aria-invalid', 'true');
  await expect(box).toHaveAccessibleDescription(/Paste a solution of at most 64 KB\./);
  expect(api.checks).toEqual([]);
});

const FIX_REFUSALS: { status: number; code: string; message: string; copy: string; extra?: bank.Json }[] = [
  { status: 404, code: 'not_found', message: 'x', copy: 'There is no such problem.' },
  { status: 409, code: 'deleted', message: 'x', copy: 'This problem was deleted and cannot be changed.' },
  { status: 409, code: 'not_function', message: 'x', copy: 'Only a function problem has languages to check.' },
  { status: 409, code: 'already_offered', message: 'x', copy: 'Java 21 is already offered. Use Check Java 21 to test it.' },
  { status: 409, code: 'checking', message: 'x', copy: 'Podium is still checking the languages of this problem.' },
  { status: 409, code: 'not_ready', message: 'x', copy: 'The problem is not ready. Fix these first.', extra: { problems: ['The tests have no expected outputs.'] } },
  { status: 409, code: 'no_model', message: 'Choose a model for Anthropic first.', copy: 'Choose a model for Anthropic first.' },
  { status: 429, code: 'busy', message: 'x', copy: 'One AI request at a time. Wait for the current one to finish.' },
  { status: 502, code: 'provider_error', message: 'Anthropic could not answer. Try again.', copy: 'Anthropic could not answer. Try again.' },
  { status: 422, code: 'invalid_answer', message: 'x', copy: "The AI's Java 21 code could not be used, so nothing was changed. Press Fix again, or write the code yourself.", extra: { problems: ['The starting code is empty.'] } },
  { status: 409, code: 'changed', message: 'x', copy: 'The problem changed while the AI was working. Press Fix again.' },
];

for (const r of FIX_REFUSALS) {
  test(`Fix refused with ${r.code} shows its copy next to the button`, async ({ page }) => {
    const api = await setup(page);
    api.refuse = r;
    await openFold(page);
    const fix = page.getByRole('button', { name: 'Fix Java with AI' });
    await fix.click();
    const note = row(page, 'Java').getByRole('alert');
    await expect(note).toContainText(r.copy);
    if (r.extra?.problems) await expect(note).toContainText((r.extra.problems as string[])[0]);
    await expect(fix).toHaveAccessibleDescription(new RegExp(r.copy.slice(0, 20).replace(/[.*+?^${}()|[\]\\]/g, '\\$&')));
    await expect(fix).toBeEnabled();
    await expect(fix).toBeFocused();
  });
}

const CHECK_REFUSALS: { status: number; code: string; copy: string; message?: string; extra?: bank.Json }[] = [
  { status: 400, code: 'bad_request', message: 'Paste a solution of at most 64 KB.', copy: 'Paste a solution of at most 64 KB.' },
  { status: 409, code: 'not_offered', copy: 'C++17 is not offered. Write its locked code and starting code and save, or use Fix C++17 with AI.' },
  { status: 409, code: 'checking', copy: 'Podium is still checking the languages of this problem.' },
  { status: 409, code: 'not_function', copy: 'Only a function problem has languages to check.' },
  { status: 409, code: 'not_ready', copy: 'The problem is not ready. Fix these first.', extra: { problems: ['The tests have no expected outputs.'] } },
  { status: 409, code: 'deleted', copy: 'This problem was deleted and cannot be changed.' },
];

for (const r of CHECK_REFUSALS) {
  test(`Check refused with ${r.code} shows its copy next to the button`, async ({ page }) => {
    const api = await setup(page);
    api.refuse = { ...r, message: r.message ?? 'x' };
    await openFold(page);
    const box = await checkBox(page, 'C++');
    await box.getByLabel('Your solution in C++').fill('int solve() { return 0; }');
    const button = box.getByRole('button', { name: 'Check C++' });
    await button.click();
    const note = box.getByRole('alert');
    await expect(note).toContainText(r.copy);
    if (r.extra?.problems) await expect(note).toContainText((r.extra.problems as string[])[0]);
    await expect(button).toBeEnabled();
    await expect(button).toBeFocused();
  });
}

test('with no key the refusal links to where the key is added', async ({ page }) => {
  const api = await setup(page);
  api.refuse = { status: 409, code: 'no_key', message: 'x' };
  await openFold(page);
  await page.getByRole('button', { name: 'Fix Java with AI' }).click();
  const note = row(page, 'Java').getByRole('alert');
  await expect(note).toContainText('Add a key first.');
  await expect(note.getByRole('link', { name: 'Change key or model' })).toHaveAttribute('href', '/admin/ai/new');
});

test('with unsaved changes Fix and Check ask to save first, with the line tied to each button', async ({ page }) => {
  const api = await setup(page);
  await openFold(page);
  await page.getByLabel('Title', { exact: true }).fill('A plus B, edited');
  const line = 'Save the problem first. The check runs on the saved version.';
  await expect(panel(page).getByText(line)).toBeVisible();
  const fix = page.getByRole('button', { name: 'Fix Java with AI' });
  await expect(fix).toBeDisabled();
  await expect(fix).toHaveAccessibleDescription(new RegExp(line.slice(0, 30)));
  const r = await checkBox(page, 'C++');
  await expect(r.getByRole('button', { name: 'Check C++' })).toBeDisabled();
  expect(api.fixes).toEqual([]);
});

test('Fix and Check are reachable and work from the keyboard', async ({ page }) => {
  const api = await setup(page);
  api.after = [{ checking: false, languages: lang({ ...passed(), java21: JAVA_FAILED, cpp17: { result: 'passed', total: 12, source: 'check' } }) }];
  await openFold(page);
  await activate(page, page.getByRole('button', { name: 'Fix Java with AI' }));
  await expect.poll(() => api.fixes.length).toBe(1);
  await expect(page.getByRole('button', { name: 'Fix Java with AI' })).toBeEnabled({ timeout: 15_000 });
  api.after = [{ checking: false, languages: lang({ ...passed(), java21: JAVA_FAILED, cpp17: { result: 'passed', total: 12, source: 'check' } }) }];
  const r = row(page, 'C++');
  await r.locator('summary', { hasText: 'Check C++' }).focus();
  await page.keyboard.press('Enter');
  await page.keyboard.press('Tab');
  await expect(r.getByLabel('Your solution in C++')).toBeFocused();
  await page.keyboard.type('int solve() { return 1; }');
  await page.keyboard.press('Tab');
  await expect(r.getByRole('button', { name: 'Check C++' })).toBeFocused();
  await page.keyboard.press('Enter');
  await expect.poll(() => api.checks.length).toBe(1);
});

async function fits(page: Page, name: string, theme: string) {
  await page.evaluate(() => document.fonts.ready);
  for (const width of WIDTHS) {
    await page.setViewportSize({ width, height: width < 768 ? 812 : 900 });
    await page.evaluate(() => new Promise((resolve) => requestAnimationFrame(() => resolve(null))));
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    expect(overflow, `overflow at ${width}px`).toBeLessThanOrEqual(0);
    await page.screenshot({ path: `${shotDir()}${name}-${width}-${theme}.png`, fullPage: true });
  }
}

const noScroll = (l: Locator) => l.evaluate((el) => el.scrollWidth <= el.clientWidth + 1);

test('a 400 character compiler line scrolls inside its box at 375 px', async ({ page }) => {
  const long = `Main.java:1: error: ${'x'.repeat(380)}`;
  const javaCompile = { offered: false, result: 'failed', reason: 'compile_error', failed: 0, total: 12, compiler: long };
  await setup(page, { script: [{ checking: false, languages: lang({ ...passed(), java21: javaCompile }) }] });
  await page.setViewportSize({ width: 375, height: 812 });
  await openFold(page);
  await row(page, 'Java').locator('details', { hasText: 'Details' }).locator('summary').click();
  await expect(row(page, 'Java')).toContainText('x'.repeat(40));
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  expect(overflow).toBeLessThanOrEqual(0);
});

for (const theme of THEMES) {
  test.describe(`${theme} theme`, () => {
    test.use({ colorScheme: theme });

    test('the Languages panel fits, passes axe and is shot at four widths', async ({ page }) => {
      await setup(page);
      await page.setViewportSize({ width: 1440, height: 900 });
      await openFold(page);
      await row(page, 'Java').locator('details', { hasText: 'Details' }).locator('summary').click();
      await checkBox(page, 'C++');
      await expect(panel(page)).toBeVisible();
      await fits(page, 'problem-languages', theme);
      for (const width of WIDTHS) {
        await page.setViewportSize({ width, height: 900 });
        expect(await noScroll(panel(page)), `panel overflows at ${width}px`).toBe(true);
      }
      const results = await new AxeBuilder({ page }).include('.fold').withTags(AXE_TAGS).analyze();
      expect(results.violations.map((v) => v.id)).toEqual([]);
    });

    test('the being-checked banner fits and passes axe', async ({ page }) => {
      await setup(page, { script: [RUNNING] });
      await page.setViewportSize({ width: 1440, height: 900 });
      await page.goto('/admin/problems/1');
      await expect(page.locator('.banner-info', { hasText: 'Podium is checking the languages' })).toBeVisible();
      await fits(page, 'problem-checking', theme);
      const results = await new AxeBuilder({ page }).include('.editor').withTags(AXE_TAGS).analyze();
      expect(results.violations.map((v) => v.id)).toEqual([]);
    });
  });
}

test('the live region is in the page before a round starts and holds its result after', async ({ page }) => {
  const api = await setup(page);
  api.after = [{ checking: false, languages: lang({ ...passed(), java21: JAVA_FAILED, python3: { result: 'passed', total: 12, source: 'check' } }) }];
  await openFold(page);
  const r = await checkBox(page, 'Python');
  await expect(r.getByRole('status')).toHaveCount(1);
  await r.getByLabel('Your solution in Python').fill('def solve(a): return 1');
  await r.getByRole('button', { name: 'Check Python' }).click();
  await expect(r.getByRole('status')).toHaveText('Python passes every test.', { timeout: 10_000 });
});

test('a failed read right after Fix is accepted does not end the working state early', async ({ page }) => {
  const api = await setup(page);
  api.after = [RUNNING, { checking: false, languages: lang({ ...passed(), java21: { ...JAVA_FAILED, failed: 2, source: 'fix' } }), applied: { kept: 'not_added', validating: false } }];
  await openFold(page);
  api.failProofGets = 1;
  await page.getByRole('button', { name: 'Fix Java with AI' }).click();
  await page.waitForTimeout(700);
  await expect(row(page, 'Java').getByRole('status')).not.toContainText('Nothing was changed');
  await expect(row(page, 'Java').getByRole('button', { name: 'Checking Java' })).toHaveAttribute('aria-busy', 'true');
  await expect(row(page, 'Java').getByRole('status')).toContainText('Nothing was changed.', { timeout: 15_000 });
});

test('an older proof answer that arrives late does not replace a newer one', async ({ page }) => {
  const api = await setup(page, { script: [RUNNING] });
  let release: () => void = () => undefined;
  const gate = new Promise<void>((r) => (release = r));
  let held = false;
  await page.route('**/api/admin/problems/1/proof', async (route) => {
    if (held) return route.fallback();
    held = true;
    await gate;
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ ...MIXED, server_now: bank.T0 }) });
  });
  await page.goto('/admin/problems/1');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('A plus B');
  await page.getByLabel('Title', { exact: true }).fill('A plus B, edited');
  await page.getByRole('button', { name: 'Save changes' }).click();
  const banner = page.locator('.banner-info', { hasText: 'Podium is checking the languages' });
  await expect(banner).toBeVisible();
  release();
  await page.waitForTimeout(700);
  await expect(banner).toBeVisible();
  expect(api.proofGets).toBeGreaterThan(0);
});

test('already_offered and not_offered reload the state so the right control shows', async ({ page }) => {
  const api = await setup(page, { script: [MIXED, { checking: false, languages: lang({ ...passed(), java21: { offered: true, result: 'none' } }) }] });
  api.refuse = { status: 409, code: 'already_offered', message: 'x' };
  await openFold(page);
  await page.getByRole('button', { name: 'Fix Java with AI' }).click();
  await expect(row(page, 'Java').locator('.lang-state')).toHaveText('Offered');
  await expect(page.getByRole('button', { name: 'Fix Java with AI' })).toHaveCount(0);
});
