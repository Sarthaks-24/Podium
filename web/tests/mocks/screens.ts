// Fixtures and the API mock of screens.spec.ts, moved here so a11y.spec.ts
// and baseline.spec.ts reuse them. The spec's header describes them.
import { expect, test, type Page, type Route } from '@playwright/test';

/** cache/screenshots-m1/ at the repository root (git-ignored), with a trailing slash. */
export function shots(): string {
  return `${test.info().project.testDir}/../../cache/screenshots-m1/`;
}
export const WIDTHS = [375, 768, 1024, 1440] as const;
export const THEMES = ['light', 'dark'] as const;

export type Who = 'signed-out' | 'student' | 'owner';

export const OWNER = { id: 1, email: 'priya@college.edu', name: 'Priya Raman', avatar_url: '', role: 'owner' };
export const STUDENT = { id: 7, email: 'asha@college.edu', name: 'Asha Rao', avatar_url: '', role: 'student' };

export const PEOPLE = [
  { id: 1, email: 'priya@college.edu', name: 'Priya Raman', role: 'owner', last_login_at: 1792183192359, locked: true },
  { id: 2, email: 'arjun.mehta@college.edu', name: 'Arjun Mehta', role: 'admin', last_login_at: 1791923992359, locked: false },
  { id: 3, email: 'ta@college.edu', name: '', role: 'admin', last_login_at: null, locked: false },
];

export const DOMAINS = [
  { domain: 'college.edu', created_at: 1791000000000 },
  { domain: 'cs.college.edu', created_at: 1791500000000 },
];

export const CONFIG = { allowed_domains: ['college.edu'], contact_email: 'priya@college.edu' };

/** The site audit the People "History" shows (contract C4), newest first. */
export const SITE_AUDIT = [
  { id: 3, at: 1792183192359, action: 'role_granted', actor: { user_id: 1, email: 'priya@college.edu', role: 'owner' }, subject_email: 'ta@college.edu', details: { role: 'admin', from_role: 'student' } },
  { id: 2, at: 1792096792359, action: 'domain_added', actor: { user_id: 1, email: 'priya@college.edu', role: 'owner' }, subject_email: '', details: { domain: 'cs.college.edu' } },
  { id: 1, at: 1791923992359, action: 'mode_changed', actor: null, subject_email: '', details: { from: 'testing', to: 'production', sessions_ended: 0 } },
];

export function json(route: Route, status: number, body?: unknown) {
  return route.fulfill({
    status,
    contentType: 'application/json; charset=utf-8',
    body: body === undefined ? '' : JSON.stringify(body),
  });
}

export function apiError(route: Route, status: number, code: string, message: string) {
  return json(route, status, { error: { code, message } });
}

/** Installs the API mock for one page and returns its mutable state. */
export async function mockApi(page: Page, who: Who) {
  const state = {
    who,
    people: structuredClone(PEOPLE),
    domains: structuredClone(DOMAINS),
    nextId: 10,
    resets: [] as string[],
    disabled: [] as { id: number; email: string; name: string; role: string; disabled: boolean; last_login_at: number | null }[],
    accountCalls: [] as string[],
    auditQueries: [] as string[],
    audit: structuredClone(SITE_AUDIT) as typeof SITE_AUDIT,
    cases: [] as { id: number; decided: boolean }[],
    lookups: [] as string[],
    organisation: { name: '', contact_email: '', phone: '', privacy_contact: '' },
    settingsPuts: [] as Record<string, string>[],
    rollovers: [] as Record<string, unknown>[],
    /** The server_now of each rollover answer, in order. */
    rolloverNows: [] as number[],
  };

  await page.route(
    (url) => url.pathname.startsWith('/api/'),
    async (route) => {
      const req = route.request();
      const path = new URL(req.url()).pathname;
      const method = req.method();
      const me = state.who === 'owner' ? OWNER : state.who === 'student' ? STUDENT : null;

      if (path === '/api/auth/config' && method === 'GET') return json(route, 200, CONFIG);
      if (path === '/api/auth/logout' && method === 'POST') {
        state.who = 'signed-out';
        return json(route, 204);
      }
      if (path === '/api/me' && method === 'GET') {
        return me ? json(route, 200, me) : apiError(route, 401, 'signed_out', 'Sign in to continue.');
      }
      if (path === '/api/contests' && method === 'GET') {
        return me ? json(route, 200, { live: [], upcoming: [], past: [] }) : apiError(route, 401, 'signed_out', 'Sign in to continue.');
      }
      if (path.startsWith('/api/admin/')) {
        if (!me) return apiError(route, 401, 'signed_out', 'Sign in to continue.');
        if (me.role !== 'owner') return apiError(route, 403, 'forbidden', 'Only owners can do that.');
      }
      if (path === '/api/integrity/cases' && method === 'GET') {
        return me ? json(route, 200, { cases: state.cases, contests: {}, problems: {} }) : apiError(route, 401, 'signed_out', 'Sign in to continue.');
      }
      if (path.startsWith('/api/admin/') && me?.role === 'owner' && path === '/api/admin/contests' && method === 'GET') {
        return json(route, 200, { contests: [], server_now: Date.now() });
      }
      if (path === '/api/admin/site-audit' && method === 'GET') {
        const q = new URL(req.url()).searchParams;
        state.auditQueries.push(q.toString());
        const action = q.get('action') ?? '';
        const text = (q.get('q') ?? '').toLowerCase();
        const entries = state.audit.filter(
          (e) => (action === '' || e.action === action) && (text === '' || e.subject_email.includes(text) || (e.actor?.email ?? '').includes(text)),
        );
        return json(route, 200, { entries, page: Number(q.get('page') ?? 1), has_more: false, server_now: Date.now() });
      }
      // The organiser home's first-run list asks whether a validated problem exists (C2-024).
      if (path === '/api/admin/problems' && method === 'GET') return json(route, 200, { problems: [] });
      if (path === '/api/admin/people' && method === 'GET') {
        return json(route, 200, { people: state.people, domains: state.domains, disabled: state.disabled, organisation: state.organisation });
      }
      if (path === '/api/admin/accounts' && method === 'GET') {
        const email = (new URL(req.url()).searchParams.get('email') ?? '').trim().toLowerCase();
        state.lookups.push(email);
        const person = state.people.find((x) => x.email === email);
        if (person) return json(route, 200, { account: { id: person.id, email: person.email, name: person.name, role: person.role, disabled: false, last_login_at: person.last_login_at }, server_now: Date.now() });
        if (email === STUDENT.email) {
          const off = state.disabled.some((a) => a.email === email);
          return json(route, 200, { account: { id: STUDENT.id, email, name: STUDENT.name, role: 'student', disabled: off, last_login_at: 1792100000000 }, server_now: Date.now() });
        }
        return apiError(route, 404, 'not_found', 'Nobody with that email has an account.');
      }
      if (path === '/api/admin/integrity/oversight' && method === 'GET') {
        return json(route, 200, {
          reviewers: [
            { user_id: 1, name: 'Priya Raman', email: 'priya@college.edu', manual_opened: 2, cleared: 5, actioned: 1, overdue: 0, proposals: 2, confirmations: 1, rulings: 0 },
            { user_id: 2, name: 'Arjun Mehta', email: 'arjun.mehta@college.edu', manual_opened: 0, cleared: 1, actioned: 0, overdue: 1, proposals: 0, confirmations: 2, rulings: 1 },
          ],
          server_now: Date.now(),
        });
      }
      if (path === '/api/admin/record-views' && method === 'GET') {
        const before = new URL(req.url()).searchParams.get('before');
        if (before) return json(route, 200, { views: [{ id: 1, at: 1791900000000, kind: 'notes', ref_id: 80, viewer: { id: 2, name: 'Arjun Mehta', email: 'arjun.mehta@college.edu' }, subject: { id: 7, name: 'Asha Rao' } }], has_more: false, server_now: Date.now() });
        return json(route, 200, {
          views: [
            { id: 3, at: 1792183000000, kind: 'record', ref_id: 0, viewer: { id: 2, name: 'Arjun Mehta', email: 'arjun.mehta@college.edu' }, subject: { id: 7, name: 'Asha Rao' } },
            { id: 2, at: 1792100000000, kind: 'case', ref_id: 12, viewer: { id: 1, name: 'Priya Raman', email: 'priya@college.edu' }, subject: { id: 7, name: 'Asha Rao' } },
          ],
          has_more: true,
          server_now: Date.now(),
        });
      }
      if (path === '/api/admin/site-settings' && method === 'PUT') {
        const body = req.postDataJSON() as Record<string, string>;
        state.settingsPuts.push(body);
        if (body.contact_email !== '' && !/^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(body.contact_email)) {
          return json(route, 400, { error: { code: 'bad_request', message: 'Check the highlighted fields.', fields: { contact_email: 'Enter an email address, for example podium@college.edu.' } } });
        }
        state.organisation = { name: body.name, contact_email: body.contact_email, phone: body.phone, privacy_contact: body.privacy_contact };
        return json(route, 200, state.organisation);
      }
      if (path === '/api/admin/rollover' && method === 'POST') {
        const body = req.postDataJSON() as { label: string; apply?: boolean; ended_before?: number };
        state.rollovers.push(body);
        const serverNow = Date.now();
        state.rolloverNows.push(serverNow);
        const accounts = [{ id: 41, email: 'old@college.edu', name: 'Old Student', role: 'student', disabled: false, last_login_at: 1770000000000 }];
        if (body.apply) state.disabled.push({ ...accounts[0], disabled: true });
        return json(route, 200, {
          applied: !!body.apply,
          label: body.label,
          ended_before: body.ended_before ?? Date.now(),
          idle_days: 180,
          contests: [{ id: 12, slug: 'week-12', title: 'Week 12', ends_at: 1791000000000 }, { id: 13, slug: 'week-13', title: 'Week 13', ends_at: 1791600000000 }],
          accounts,
          server_now: serverNow,
        });
      }
      const accountMatch = path.match(/^\/api\/admin\/accounts\/(end-sessions|disable|enable)$/);
      if (accountMatch && method === 'POST') {
        const kind = accountMatch[1];
        const target = (req.postDataJSON() as { email: string }).email;
        const known = [...state.people.map((p) => p.email), STUDENT.email];
        if (!known.includes(target)) return apiError(route, 404, 'not_found', 'Nobody with that email has an account.');
        if (kind === 'disable' && target === OWNER.email) {
          return apiError(route, 409, 'conflict', 'This owner is set in the server configuration and cannot be disabled.');
        }
        state.accountCalls.push(`${kind} ${target}`);
        const account = { id: target === STUDENT.email ? STUDENT.id : 50, email: target, name: '', role: 'student', disabled: kind === 'disable', last_login_at: null };
        state.disabled = state.disabled.filter((a) => a.email !== target);
        if (kind === 'disable') state.disabled.push(account);
        return json(route, 200, { account, ended: kind === 'enable' ? 0 : 2 });
      }
      if (path === '/api/admin/people' && method === 'POST') {
        const body = req.postDataJSON() as { email: string; role: string };
        if (!/^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(body.email)) {
          return apiError(route, 400, 'bad_request', 'Enter a full email address, for example ta@college.edu.');
        }
        const existing = state.people.find((p) => p.email === body.email);
        if (existing) {
          existing.role = body.role;
          return json(route, 200, existing);
        }
        const person = { id: state.nextId++, email: body.email, name: '', role: body.role, last_login_at: null, locked: false };
        state.people.push(person);
        return json(route, 200, person);
      }
      const personMatch = path.match(/^\/api\/admin\/people\/(\d+)$/);
      if (personMatch && method === 'DELETE') {
        const id = Number(personMatch[1]);
        const person = state.people.find((p) => p.id === id);
        if (!person) return apiError(route, 404, 'not_found', 'That person is not an organiser.');
        if (person.locked) return apiError(route, 409, 'conflict', 'The configured owner cannot be changed here.');
        state.people = state.people.filter((p) => p.id !== id);
        return json(route, 204);
      }
      if (path === '/api/admin/domains' && method === 'POST') {
        const raw = (req.postDataJSON() as { domain: string }).domain.toLowerCase().replace(/^@/, '');
        if (!/^[a-z0-9-]+(\.[a-z0-9-]+)+$/.test(raw)) {
          return apiError(route, 400, 'bad_request', 'Enter a domain like college.edu.');
        }
        const existing = state.domains.find((d) => d.domain === raw);
        if (existing) return json(route, 200, existing);
        const added = { domain: raw, created_at: Date.now() };
        state.domains.push(added);
        return json(route, 201, added);
      }
      if (path === '/api/admin/accounts/reset-google' && method === 'POST') {
        const type = (await req.headerValue('content-type')) ?? '';
        if (!type.startsWith('application/json')) {
          return apiError(route, 415, 'unsupported_media_type', 'Send the request as JSON.');
        }
        const target = (req.postDataJSON() as { email: string }).email;
        if (!/^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(target)) {
          return apiError(route, 400, 'bad_request', 'Enter a full email address, for example ta@college.edu.');
        }
        const known = [...state.people.map((p) => p.email), STUDENT.email];
        if (!known.includes(target)) return apiError(route, 404, 'not_found', 'Nobody with that email has an account.');
        state.resets.push(target);
        return json(route, 204);
      }
      const domainMatch = path.match(/^\/api\/admin\/domains\/(.+)$/);
      if (domainMatch && method === 'DELETE') {
        const domain = decodeURIComponent(domainMatch[1]);
        if (!state.domains.some((d) => d.domain === domain)) {
          return apiError(route, 404, 'not_found', 'That domain is not on the list.');
        }
        state.domains = state.domains.filter((d) => d.domain !== domain);
        return json(route, 204);
      }
      return apiError(route, 404, 'not_found', 'This API route does not exist.');
    },
  );
  return state;
}

export interface Screen {
  name: string;
  path: string;
  who: Who;
  title: string;
  /** Something that is only on the page once it has finished loading. */
  ready: (page: Page) => Promise<void>;
}

export const SCREENS: Screen[] = [
  {
    name: 'signin-error-account',
    path: '/signin?error=account',
    who: 'signed-out',
    title: 'Sign in · podium',
    ready: (page) => expect(page.getByRole('alert')).toContainText('This email is linked to a different Google account.'),
  },
  {
    name: 'signin',
    path: '/signin',
    who: 'signed-out',
    title: 'Sign in · podium',
    ready: (page) => expect(page.getByText('Sign in with your @college.edu Google account.')).toBeVisible(),
  },
  {
    name: 'signin-error-domain',
    path: '/signin?error=domain',
    who: 'signed-out',
    title: 'Sign in · podium',
    ready: (page) => expect(page.getByRole('alert')).toContainText('This account is not on the allowed list.'),
  },
  {
    name: 'home-owner',
    path: '/',
    who: 'owner',
    title: 'Contests · podium',
    ready: (page) => expect(page.getByText('No contests yet. Contests use validated problems')).toBeVisible(),
  },
  {
    name: 'people-owner',
    path: '/admin/people',
    who: 'owner',
    title: 'People · podium',
    ready: async (page) => {
      await expect(page.getByRole('table').first()).toBeVisible();
      await expect(page.getByText('Made ta@college.edu an admin')).toBeVisible();
    },
  },
  {
    name: 'people-student',
    path: '/admin/people',
    who: 'student',
    title: 'People · podium',
    ready: (page) => expect(page.getByText('Only owners can manage people.')).toBeVisible(),
  },
  {
    name: 'privacy',
    path: '/privacy',
    who: 'signed-out',
    title: 'Privacy · podium',
    ready: (page) => expect(page.getByText('The privacy notice for podium is being written.')).toBeVisible(),
  },
  {
    name: 'not-found',
    path: '/no-such-page',
    who: 'owner',
    title: 'Page not found · podium',
    ready: (page) => expect(page.getByText('Nothing lives at /no-such-page.')).toBeVisible(),
  },
];
