import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  checkKeyForm,
  cleanModelId,
  draftedBy,
  isListed,
  wizardBlocker,
  formatClock,
  initialForm,
  isKeyProblem,
  inputGenReport,
  isAIDraft,
  keyLine,
  modelLabel,
  redactKey,
  saveKey,
  wizardPending,
  wizardRequest,
  wizardStatusText,
  draftChainSteps,
  withoutAIDraft,
  type InputGeneration,
  type KeyStatus,
} from './ai';
import { ApiError } from './api';

// Split so tools/check-secrets.sh does not read the fake as a credential.
const KEY = 'sk-' + 'ant-api03-TEST-ONLY-not-a-real-key-0123456789';

// The key status shape: no default model, a session's list per provider.
const PROVIDERS: KeyStatus['providers'] = [
  {
    id: 'anthropic',
    name: 'Anthropic',
    has_key: false,
    model: '',
    active_until: null,
    models: [],
  },
  {
    id: 'openai',
    name: 'OpenAI',
    has_key: true,
    model: 'gpt-x',
    active_until: 1792190392359,
    models: [
      { id: 'gpt-x', name: 'gpt-x' },
      { id: 'gpt-x-mini', name: 'gpt-x-mini' },
    ],
  },
  { id: 'gemini', name: 'Google Gemini', has_key: false, model: '', active_until: null, models: [] },
  { id: 'fake', name: 'Test provider (canned problems)', has_key: false, model: '', active_until: null, models: [{ id: 'canned', name: 'Canned problems' }] },
];

function status(over: Partial<KeyStatus> = {}): KeyStatus {
  return { active: false, provider: '', model: '', active_until: null, providers: PROVIDERS, ...over };
}

/** Every string reachable from a value, so a test can look for the key anywhere in it. */
function serialised(value: unknown): string {
  return JSON.stringify(value) ?? '';
}

describe('the key form', () => {
  it('starts on the first provider with no model: there is no default model (C11)', () => {
    expect(initialForm(status({ provider: 'anthropic' }))).toEqual({ provider: 'anthropic', model: '', key: '' });
    expect(initialForm(status({ provider: '' }))).toEqual({ provider: 'anthropic', model: '', key: '' });
  });

  it('starts on the active provider with its saved model', () => {
    const s = status({ active: true, provider: 'openai', model: 'gpt-x', active_until: 1792190392359 });
    expect(initialForm(s)).toEqual({ provider: 'openai', model: 'gpt-x', key: '' });
  });

  it('asks for a key unless the session holds one or the provider is the fake', () => {
    expect(checkKeyForm({ provider: 'anthropic', model: '', key: '  ' })).toEqual({ key: 'Paste your API key.' });
    expect(checkKeyForm({ provider: 'openai', model: 'gpt-x-mini', key: '' }, { has_key: true })).toEqual({});
    expect(checkKeyForm({ provider: 'fake', model: 'canned', key: '' })).toEqual({});
  });

  it('takes any model id the server would, typed or empty', () => {
    expect(checkKeyForm({ provider: 'gemini', model: '', key: 'k' })).toEqual({});
    expect(checkKeyForm({ provider: 'gemini', model: 'models/gemini-x-pro', key: 'k' })).toEqual({});
    expect(cleanModelId('  models/gemini-x-pro ')).toBe('gemini-x-pro');
    expect(checkKeyForm({ provider: 'openai', model: 'gpt x', key: 'k' }).model).toMatch(/letters, digits/);
    expect(checkKeyForm({ provider: 'openai', model: 'a'.repeat(129), key: 'k' }).model).toMatch(/at most 128/);
  });

  it('names a model from the list, or by its id when it is not listed', () => {
    expect(modelLabel(status(), 'openai', 'gpt-x-mini')).toBe('gpt-x-mini');
    expect(modelLabel(status(), 'anthropic', 'claude-unknown')).toBe('claude-unknown');
    expect(isListed(PROVIDERS[1], 'gpt-x')).toBe(true);
    expect(isListed(PROVIDERS[1], 'gpt-y')).toBe(false);
    expect(isListed(PROVIDERS[1], '')).toBe(false);
  });

  it('says what the wizard waits for: a key, then a model', () => {
    expect(wizardBlocker(status())).toBe('Add a key first.');
    expect(wizardBlocker(status({ active: true, provider: 'openai', model: '' }))).toBe('Choose a model for OpenAI first.');
    expect(wizardBlocker(status({ active: true, provider: 'openai', model: 'gpt-x' }))).toBe('');
    expect(wizardBlocker(status({ active: true, provider: 'fake', model: '' }))).toBe('');
  });

  it('names the provider and model that made a draft', () => {
    expect(draftedBy({ provider: 'openai', model: 'gpt-x' }, status())).toBe('Drafted by OpenAI with gpt-x');
    expect(draftedBy({ provider: 'gemini', model: '' }, status())).toBe('Drafted by Google Gemini');
    expect(draftedBy({}, status())).toBe('');
  });

  it('says until when the key is active, in local time', () => {
    const until = new Date(2026, 9, 18, 14, 32).getTime();
    expect(keyLine({ active: true, provider: 'anthropic', active_until: until })).toBe('Key active until 14:32');
    expect(keyLine({ active: false, provider: '', active_until: null })).toBe('No key yet');
    expect(keyLine({ active: true, provider: 'fake', active_until: null })).toBe('Fake provider active, no key needed');
    expect(formatClock(Math.floor(until / 1000))).toBe('14:32');
  });
});

describe('the key never stays in state', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('clears the key from the form after a successful save', async () => {
    const sent: unknown[] = [];
    const answer = status({ active: true, provider: 'anthropic', model: 'claude-opus-5-5', active_until: 1792190392359 });
    const result = await saveKey(async (body) => {
      sent.push(body);
      return answer;
    }, { provider: 'anthropic', model: 'claude-opus-5-5', key: `  ${KEY}\n` });
    expect(sent).toEqual([{ provider: 'anthropic', model: 'claude-opus-5-5', key: KEY }]);
    expect(result.ok).toBe(true);
    expect(result.form.key).toBe('');
    expect(serialised(result)).not.toContain(KEY);
  });

  it('clears the key and redacts it from a refusal that echoes it', async () => {
    const result = await saveKey(async () => {
      throw new ApiError(400, 'bad_key', `The key ${KEY} was refused by Anthropic.`);
    }, { provider: 'anthropic', model: 'claude-opus-5-5', key: KEY });
    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.code).toBe('bad_key');
    expect(result.message).toBe('The key [redacted] was refused by Anthropic.');
    expect(result.form.key).toBe('');
    expect(serialised(result)).not.toContain(KEY);
  });

  it('clears the key after a network failure too', async () => {
    const result = await saveKey(async () => {
      throw new TypeError(`fetch failed for ${KEY}`);
    }, { provider: 'anthropic', model: 'claude-opus-5-5', key: KEY });
    expect(result.ok).toBe(false);
    expect(serialised(result)).not.toContain(KEY);
  });

  it('never sends a key with the fake provider', async () => {
    let body: unknown;
    await saveKey(async (b) => {
      body = b;
      return status({ active: true, provider: 'fake', model: 'canned' });
    }, { provider: 'fake', model: 'canned', key: KEY });
    expect(body).toEqual({ provider: 'fake', model: 'canned', key: '' });
  });

  it('writes nothing to browser storage or the console while saving', async () => {
    const writes: string[] = [];
    const storage = {
      getItem: () => null,
      setItem: (k: string, v: string) => writes.push(`${k}=${v}`),
      removeItem: () => undefined,
      clear: () => undefined,
      key: () => null,
      length: 0,
    };
    vi.stubGlobal('localStorage', storage);
    vi.stubGlobal('sessionStorage', storage);
    const logged: unknown[] = [];
    const spies = (['log', 'info', 'warn', 'error', 'debug'] as const).map((m) =>
      vi.spyOn(console, m).mockImplementation((...args: unknown[]) => void logged.push(args)),
    );
    await saveKey(async () => status({ active: true }), { provider: 'anthropic', model: 'claude-opus-5-5', key: KEY });
    await saveKey(async () => {
      throw new ApiError(400, 'bad_key', 'The key was refused by Anthropic.');
    }, { provider: 'anthropic', model: 'claude-opus-5-5', key: KEY });
    for (const s of spies) s.mockRestore();
    expect(writes).toEqual([]);
    expect(serialised(logged)).not.toContain(KEY);
    expect(logged).toEqual([]);
  });

  it('leaves short strings alone when redacting, so an empty key redacts nothing', () => {
    expect(redactKey('Refused.', '')).toBe('Refused.');
    expect(redactKey(`a ${KEY} b ${KEY}`, KEY)).toBe('a [redacted] b [redacted]');
  });
});

describe('screens that touch the key keep it out of storage, addresses and logs', () => {
  const sources = import.meta.glob<string>(['/src/components/AccountMenu.svelte', '/src/routes/AiWizard.svelte', '/src/components/ai/KeyForm.svelte', '/src/lib/ai.ts'], {
    query: '?raw',
    import: 'default',
    eager: true,
  });

  it('finds the files', () => {
    expect(Object.keys(sources)).toHaveLength(4);
  });

  it('uses no browser storage and no console', () => {
    for (const [path, text] of Object.entries(sources)) {
      expect(text, path).not.toMatch(/localStorage|sessionStorage|indexedDB|console\./);
    }
  });

  it('keeps the key field a password field with autocomplete off', () => {
    // The key form is components/ai/KeyForm, shown inline in the wizard.
    const page = sources['/src/components/ai/KeyForm.svelte'];
    const input = /<input\s+id=\{fid\('api-key'\)\}[\s\S]*?\/>/.exec(page)?.[0] ?? '';
    expect(input).toContain('type="password"');
    expect(input).toContain('autocomplete="off"');
    // The key is never put in an address.
    expect(page).not.toMatch(/navigate\([^)]*key/);
  });
});

describe('the wizard request', () => {
  const brief = { topic: '', difficulty: 'medium' as const, constraints: '', subtasks: '' };

  it('needs a topic for a brief and trims every field', () => {
    expect(wizardRequest('brief', brief, '').ok).toBe(false);
    expect(wizardRequest('brief', { ...brief, topic: ' Grid paths ', constraints: ' n <= 1000 ' }, '')).toEqual({
      ok: true,
      body: { mode: 'brief', brief: { topic: 'Grid paths', difficulty: 'medium', constraints: 'n <= 1000', subtasks: '' } },
    });
  });

  it('needs text to rewrite, at most 20,000 characters', () => {
    const empty = wizardRequest('paste', brief, '   ');
    expect(empty.ok).toBe(false);
    const long = wizardRequest('paste', brief, 'x'.repeat(20_001));
    expect(long).toEqual({ ok: false, fields: { text: 'Paste at most 20,000 characters. This text has 20,001.' } });
    expect(wizardRequest('paste', brief, ' Two sum ')).toEqual({ ok: true, body: { mode: 'paste', text: 'Two sum' } });
  });
});

describe('wizard progress', () => {
  it('reads "Writing the problem", then "Draft created, generating tests"', () => {
    expect(wizardStatusText({ state: 'thinking' })).toBe('Writing the problem');
    expect(wizardStatusText({ state: 'draft_created' })).toBe('Draft created, generating tests');
  });

  it('links to the settings for key problems only', () => {
    expect(isKeyProblem('no_key')).toBe(true);
    expect(isKeyProblem('key_refused')).toBe(true);
    expect(isKeyProblem('invalid_answer')).toBe(false);
    expect(isKeyProblem(undefined)).toBe(false);
  });

  it('is pending only while thinking without a submission', () => {
    expect(wizardPending(null)).toBe(false);
    expect(wizardPending({ state: 'thinking' })).toBe(true);
    expect(wizardPending({ state: 'error' })).toBe(false);
  });
});

describe('input generation', () => {
  const plan = [
    { idx: 3, subtask: 1, seed: 1, args: ['n=10'] },
    { idx: 7, subtask: 2, seed: 3, args: [] },
  ];
  const base: InputGeneration = { id: 1, state: 'done', planned: 2, results: [], summary: null, error: null };

  it('counts progress while running', () => {
    expect(inputGenReport({ ...base, state: 'running', results: [{ idx: 3, verdict: 'OK' }, { idx: 7, verdict: '' }] }, plan).text).toBe(
      'Generating inputs, 1 of 2',
    );
    expect(inputGenReport({ ...base, state: 'queued' }, plan).text).toBe('Generating inputs');
  });

  it('names a failure by test, subtask and seed', () => {
    const r = inputGenReport(
      { ...base, results: [{ idx: 3, verdict: 'OK' }, { idx: 7, verdict: 'RE' }], summary: { applied: 0, problems: [] } },
      plan,
    );
    expect(r.tone).toBe('bad');
    expect(r.lines).toEqual(['The generator failed on test 7 (subtask 2, seed 3): Runtime error']);
  });

  it('says what comes next after good inputs', () => {
    const r = inputGenReport({ ...base, results: [{ idx: 3, verdict: 'OK' }, { idx: 7, verdict: 'OK' }], summary: { applied: 2, generating_outputs: true, problems: [] } }, plan);
    expect(r).toEqual({
      tone: 'ok',
      text: 'Generated 2 inputs. Outputs are being generated from the reference solution, then the problem is validated.',
      lines: [],
    });
  });

  it('says when the generator did not compile', () => {
    const r = inputGenReport({ ...base, compile_output: 'File "generator.py", line 3\nSyntaxError: invalid syntax\n' }, plan);
    expect(r.text).toBe('The generator did not compile, so no inputs were generated.');
    expect(r.lines).toEqual(['File "generator.py", line 3', 'SyntaxError: invalid syntax']);
  });

  it('shows the server problems and errors', () => {
    expect(inputGenReport({ ...base, summary: { applied: 0, problems: ['The problem changed. Generate inputs again.'] } }, plan).lines).toEqual([
      'The problem changed. Generate inputs again.',
    ]);
    expect(inputGenReport({ ...base, state: 'error', error: 'The generator did not compile.' }, plan).text).toBe('The generator did not compile.');
  });
});

describe('AI drafts', () => {
  it('finds the AI draft tag in any case', () => {
    expect(isAIDraft(['greedy', 'AI draft'])).toBe(true);
    expect(isAIDraft(['greedy'])).toBe(false);
    expect(withoutAIDraft(['ai draft', 'dp'])).toEqual(['dp']);
  });
});

describe('draftChainSteps', () => {
  const states = (r: ReturnType<typeof draftChainSteps>) => r.steps.map((s) => s.state);
  const draft = { state: 'draft_created', problem_id: 20 };

  it('follows the draft past its creation to a validated problem (audit O7)', () => {
    expect(states(draftChainSteps({ state: 'thinking' }, null, null, null))).toEqual(['current', 'waiting', 'waiting', 'waiting']);
    expect(states(draftChainSteps(draft, { state: 'running' }, null, null))).toEqual(['done', 'done', 'current', 'waiting']);
    expect(states(draftChainSteps(draft, { state: 'done' }, { state: 'done' }, { state: 'queued' }))).toEqual(['done', 'done', 'done', 'current']);
    const passed = draftChainSteps(draft, { state: 'done' }, { state: 'done' }, { state: 'passed' });
    expect(states(passed)).toEqual(['done', 'done', 'done', 'done']);
    expect(passed).toMatchObject({ done: true, outcome: 'passed' });
  });

  it('stops on a failure and says where', () => {
    expect(draftChainSteps(draft, { state: 'error' }, null, null)).toMatchObject({ done: true, outcome: 'tests_failed' });
    const failed = draftChainSteps(draft, { state: 'done' }, { state: 'done' }, { state: 'failed' });
    expect(states(failed)).toEqual(['done', 'done', 'done', 'failed']);
    expect(failed.outcome).toBe('failed');
    expect(states(draftChainSteps({ state: 'error' }, null, null, null))).toEqual(['failed', 'waiting', 'waiting', 'waiting']);
  });
});
