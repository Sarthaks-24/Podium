import { describe, expect, it } from 'vitest';
import { ApiError } from './api';
import { MAX_MESSAGE, messageProblem, replyHref, sendError, topicLabel } from './contact';

describe('contact form (C6)', () => {
  it('checks the message before sending', () => {
    expect(messageProblem('   ')).toBe('Write your message first.');
    expect(messageProblem('Sample 2 looks wrong.')).toBe('');
    expect(messageProblem('x'.repeat(MAX_MESSAGE))).toBe('');
    expect(messageProblem('x'.repeat(MAX_MESSAGE + 1))).toBe('Keep the message to 2000 characters or fewer. It has 2001.');
  });

  it('says what to do when a send fails', () => {
    expect(sendError(new ApiError(400, 'bad_request', 'Check the form.', { fields: { message: 'Write a message.' } }))).toEqual({ field: 'message', text: 'Write a message.' });
    expect(sendError(new ApiError(0, 'network', 'x')).text).toMatch(/^Not sent: the server could not be reached/);
    expect(sendError(new ApiError(429, 'rate_limited', 'You have sent 5 messages in the last 24 hours. Try again tomorrow, or email the organisers.')).text).toBe(
      'You have sent 5 messages in the last 24 hours. Try again tomorrow, or email the organisers.',
    );
    expect(sendError(new ApiError(403, 'cross_site', 'x')).text).toMatch(/Reload the page/);
  });

  it('builds the reply link from the sender', () => {
    expect(topicLabel('privacy')).toBe('A copy or removal of my data');
    expect(replyHref({ topic: 'contest', sender: { user_id: 7, email: 'asha@college.edu', name: 'Asha Rao', role: 'student' } })).toBe(
      'mailto:asha@college.edu?subject=Your%20message%20to%20the%20organisers%3A%20a%20contest%20or%20a%20problem',
    );
  });
});
