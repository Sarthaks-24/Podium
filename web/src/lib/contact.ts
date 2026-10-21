/**
 * The contact form and the organisers' inbox (contract C6, P3 FP.2).
 * Messages are plain text everywhere: the page renders them as text, never
 * as HTML or Markdown. The server keeps them 180 days.
 */
import { ApiError } from './api';

export type ContactTopic = 'contest' | 'account' | 'privacy' | 'other';

/** The topics in the order the form offers them, with their words. */
export const TOPICS: readonly { id: ContactTopic; label: string }[] = [
  { id: 'contest', label: 'A contest or a problem' },
  { id: 'account', label: 'Signing in or my account' },
  { id: 'privacy', label: 'A copy or removal of my data' },
  { id: 'other', label: 'Something else' },
];

/** The longest message the server takes, in characters after trimming. */
export const MAX_MESSAGE = 2000;

/** The topic's words, or the raw id for one this build does not know. */
export function topicLabel(id: string): string {
  return TOPICS.find((t) => t.id === id)?.label ?? id;
}

/** The success line (docs/API.md C6). */
export const SENT = 'Sent. The organisers reply by email, usually within two days.';

/**
 * Checks a message before it is sent: the same rules the server applies,
 * so the student hears them without a round trip. Returns a sentence, or
 * "" when the message may go.
 */
export function messageProblem(message: string): string {
  const t = message.trim();
  if (t === '') return 'Write your message first.';
  if ([...t].length > MAX_MESSAGE) return `Keep the message to ${MAX_MESSAGE} characters or fewer. It has ${[...t].length}.`;
  return '';
}

/** What to say when a send fails: the field it is about, or a line for the form. */
export function sendError(e: unknown): { field: 'topic' | 'message' | null; text: string } {
  if (e instanceof ApiError) {
    if (e.fields.message) return { field: 'message', text: e.fields.message };
    if (e.fields.topic) return { field: 'topic', text: e.fields.topic };
    if (e.code === 'network') return { field: null, text: 'Not sent: the server could not be reached. Your message is still here. Try again in a moment.' };
    if (e.code === 'cross_site') return { field: null, text: 'Not sent: this page could not prove it is podium. Reload the page and send again.' };
    if (e.status === 401) return { field: null, text: 'Not sent: you are signed out. Sign in again; your message is still here.' };
    return { field: null, text: e.message };
  }
  return { field: null, text: 'Not sent: something went wrong. Try again in a moment.' };
}

/** One message in the inbox. */
export interface InboxMessage {
  id: number;
  topic: string;
  message: string;
  created_at: number;
  sender: { user_id: number; email: string; name: string; role: string };
}

export interface InboxPage {
  messages: InboxMessage[];
  has_more: boolean;
  server_now?: number;
}

/** The "Reply by email" link: the sender's address and a subject naming the topic. */
export function replyHref(m: Pick<InboxMessage, 'topic' | 'sender'>): string {
  const subject = `Your message to the organisers: ${topicLabel(m.topic).toLowerCase()}`;
  return `mailto:${m.sender.email}?subject=${encodeURIComponent(subject)}`;
}
