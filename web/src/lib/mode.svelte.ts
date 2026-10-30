/**
 * The public site configuration from GET /api/auth/config as
 * reactive state: the server's run mode (PODIUM_MODE), the
 * organisers' contact email, the organisation details an owner enters on
 * People, and the retention periods the privacy page lists. "testing" mode
 * means sign-in is a form with no Google account behind it; the shell shows
 * a banner so nobody mistakes a testing server for the real one. Unknown
 * until the config has loaded. The config is fetched once per page load
 * (loadMode); the sign-in page, the footer, PageState and the privacy page
 * all read this one copy.
 */
import { getJSON } from './api';

export type RunMode = 'testing' | 'dev' | 'production';

/** The organisation details. Every field is "" until an owner enters it. */
export interface Organisation {
  name: string;
  contact_email: string;
  phone: string;
  privacy_contact: string;
}

/** One retention period, as the server applies it (server/internal/retention/periods.go). */
export interface RetentionPeriod {
  id: string;
  data: string;
  amount: number;
  unit: string;
  after: string;
  kept: string;
}

/** The parsed config. */
export interface SiteConfig {
  allowed_domains: string[];
  contact_email: string;
  mode: RunMode;
  organisation: Organisation;
  retention: RetentionPeriod[];
}

const EMPTY_ORG: Organisation = { name: '', contact_email: '', phone: '', privacy_contact: '' };

class ModeState {
  mode = $state<RunMode | 'unknown'>('unknown');
  /** The organisers' contact email, or "" until the config loads or when none is set. */
  contactEmail = $state('');
  organisation = $state<Organisation>({ ...EMPTY_ORG });
  retention = $state<RetentionPeriod[]>([]);
  allowedDomains = $state<string[]>([]);
  /** "loading" until the first answer; "failed" when it could not be read. */
  status = $state<'loading' | 'ready' | 'failed'>('loading');
}

/** Shared site configuration. */
export const runMode = new ModeState();

/** Turns any value into a run mode; anything unexpected counts as production. */
export function parseMode(value: unknown): RunMode {
  return value === 'testing' || value === 'dev' ? value : 'production';
}

function text(value: unknown): string {
  return typeof value === 'string' ? value.trim() : '';
}

/** Reads a config response defensively: missing or odd fields become empty, never invented. */
export function parseConfig(raw: unknown): SiteConfig {
  const c = (typeof raw === 'object' && raw !== null ? raw : {}) as Record<string, unknown>;
  const org = (typeof c.organisation === 'object' && c.organisation !== null ? c.organisation : {}) as Record<string, unknown>;
  const rows = Array.isArray(c.retention) ? c.retention : [];
  return {
    allowed_domains: Array.isArray(c.allowed_domains) ? c.allowed_domains.filter((d): d is string => typeof d === 'string') : [],
    contact_email: text(c.contact_email),
    mode: parseMode(c.mode),
    organisation: { name: text(org.name), contact_email: text(org.contact_email), phone: text(org.phone), privacy_contact: text(org.privacy_contact) },
    retention: rows
      .filter((r): r is Record<string, unknown> => typeof r === 'object' && r !== null)
      .map((r) => ({
        id: text(r.id),
        data: text(r.data),
        amount: typeof r.amount === 'number' ? r.amount : 0,
        unit: text(r.unit),
        after: text(r.after),
        kept: text(r.kept),
      }))
      .filter((r) => r.data !== '' && r.kept !== ''),
  };
}

/** Puts a parsed config into the shared state. */
export function applyConfig(c: SiteConfig): void {
  runMode.mode = c.mode;
  runMode.contactEmail = c.contact_email;
  runMode.organisation = c.organisation;
  runMode.retention = c.retention;
  runMode.allowedDomains = c.allowed_domains;
  runMode.status = 'ready';
}

let pending: Promise<void> | null = null;

/** Loads the config once per page; failures leave the mode unknown, which shows no banner. */
export function loadMode(): Promise<void> {
  pending ??= getJSON<unknown>('/api/auth/config')
    .then((c) => applyConfig(parseConfig(c)))
    .catch(() => {
      // The sign-in page reports the failure itself.
      runMode.status = 'failed';
    });
  return pending;
}

/** The text for a phone number in a tel: link: digits and a leading plus only. */
export function telHref(phone: string): string {
  const digits = phone.replace(/[^\d+]/g, '');
  return `tel:${digits.startsWith('+') ? `+${digits.slice(1).replace(/\+/g, '')}` : digits.replace(/\+/g, '')}`;
}
