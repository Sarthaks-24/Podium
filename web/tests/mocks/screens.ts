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
  { id: 2, email: 'arjun.mehta@college.edu', name: 'Arjun Mehta', role: 'organiser', last_login_at: 1791923992359, locked: false },
  { id: 3, email: 'ta@college.edu', name: '', role: 'organiser', last_login_at: null, locked: false },
];

export const CONFIG = { contact_email: 'priya@college.edu' };

/** The site audit that "History of changes" on Settings shows (contract C4), newest first. */
export const SITE_AUDIT = [
  { id: 3, at: 1792183192359, action: 'role_granted', actor: { user_id: 1, email: 'priya@college.edu', role: 'owner' }, subject_email: 'ta@college.edu', details: { role: 'organiser', from_role: 'student' } },
  { id: 2, at: 1792096792359, action: 'list_imported', actor: { user_id: 1, email: 'priya@college.edu', role: 'owner' }, subject_email: '', details: { added: 12, duplicates: 1, invalid: 0 } },
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
    list: [{ role: 'student' }] as { role: string }[],
    nextId: 10,
    resets: [] as string[],
    disabled: [] as { id: number; email: string; name: string; role: string; disabled: boolean; last_login_at: number | null }[],
    accountCalls: [] as string[],
    auditQueries: [] as string[],
    audit: structuredClone(SITE_AUDIT) as typeof SITE_AUDIT,
    lookups: [] as string[],
    organisation: { name: '', contact_email: '', phone: '', privacy_contact: '' },
    settingsPuts: [] as Record<string, string>[],
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
        return json(route, 200, { people: state.people, list: state.list, counts: { on_list: state.list.length, signed_in: 0, never_came: state.list.length }, refused: [], disabled: state.disabled, organisation: state.organisation });
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
      if (path === '/api/admin/site-settings' && method === 'PUT') {
        const body = req.postDataJSON() as Record<string, string>;
        state.settingsPuts.push(body);
        if (body.contact_email !== '' && !/^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(body.contact_email)) {
          return json(route, 400, { error: { code: 'bad_request', message: 'Check the highlighted fields.', fields: { contact_email: 'Enter an email address, for example podium@college.edu.' } } });
        }
        state.organisation = { name: body.name, contact_email: body.contact_email, phone: body.phone, privacy_contact: body.privacy_contact };
        return json(route, 200, state.organisation);
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
    ready: (page) => expect(page.getByText('Sign in with the Google account of the email the organisers added.')).toBeVisible(),
  },
  {
    name: 'signin-error-not-listed',
    path: '/signin?error=not_listed',
    who: 'signed-out',
    title: 'Sign in · podium',
    ready: (page) => expect(page.getByRole('alert')).toContainText('Your email is not on the list for this site.'),
  },
  {
    name: 'home-owner',
    path: '/',
    who: 'owner',
    title: 'Contests · podium',
    ready: (page) => expect(page.getByText('No contests yet. Contests use validated problems')).toBeVisible(),
  },
  {
    name: 'settings-people',
    path: '/admin/people',
    who: 'owner',
    title: 'Settings · podium',
    ready: (page) => expect(page.getByRole('table').first()).toBeVisible(),
  },
  {
    name: 'settings-sign-in',
    path: '/admin/people?tab=sign-in',
    who: 'owner',
    title: 'Settings · podium',
    ready: (page) => expect(page.getByRole('button', { name: 'Reset Google link' })).toBeVisible(),
  },
  {
    name: 'settings-organisation',
    path: '/admin/people?tab=organisation',
    who: 'owner',
    title: 'Settings · podium',
    ready: (page) => expect(page.getByRole('button', { name: 'Save organisation details' })).toBeVisible(),
  },
  {
    name: 'settings-student',
    path: '/admin/people',
    who: 'student',
    title: 'Settings · podium',
    ready: (page) => expect(page.getByText('Only owners can change the settings.')).toBeVisible(),
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
