import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  checkKeyForm,
  defaultModel,
  formatClock,
  initialForm,
  isKeyProblem,
  inputGenReport,
  isAIDraft,
  keyLine,
  modelLabel,
  redactKey,
  saveKey,
  solveLine,
  solveMeaning,
  wizardPending,
  wizardRequest,
  wizardStatusText,
  wizardSteps,
  withoutAIDraft,
  type InputGeneration,
  type KeyStatus,
} from './ai';
import { ApiError } from './api';

// Split so tools/check-secrets.sh does not read the fake as a credential.
const KEY = 'sk-' + 'ant-api03-TEST-ONLY-not-a-real-key-0123456789';

const PROVIDERS: KeyStatus['providers'] = [
  {
    id: 'anthropic',
    name: 'Anthropic',
    models: [
      { id: 'claude-opus-5-5', name: 'Claude Opus 5.5 (default)' },
      { id: 'claude-sonnet-5-5', name: 'Claude Sonnet 5.5' },
      { id: 'claude-haiku-5-5', name: 'Claude Haiku 5.5' },
    ],
  },
  { id: 'fake', name: 'Test provider (canned problems)', models: [{ id: 'canned', name: 'Canned problems' }] },
];

function status(over: Partial<KeyStatus> = {}): KeyStatus {
  return { active: false, provider: '', model: '', active_until: null, providers: PROVIDERS, ...over };
}

/** Every string reachable from a value, so a test can look for the key anywhere in it. */
function serialised(value: unknown): string {
  return JSON.stringify(value) ?? '';
}

describe('the key form', () => {
  it('starts on Anthropic with Claude Opus 5.5 and an empty key', () => {
    expect(initialForm(status())).toEqual({ provider: 'anthropic', model: 'claude-opus-5-5', key: '' });
    expect(defaultModel(PROVIDERS[0])).toBe('claude-opus-5-5');
    expect(defaultModel({ id: 'x', name: 'X', models: [{ id: 'only', name: 'Only' }] })).toBe('only');
  });

  it('starts on the active provider and model when a key is active', () => {
    const s = status({ active: true, provider: 'anthropic', model: 'claude-haiku-5-5', active_until: 1792183192359 });
    expect(initialForm(s)).toEqual({ provider: 'anthropic', model: 'claude-haiku-5-5', key: '' });
  });

  it('asks for a key except for the fake provider', () => {
    expect(checkKeyForm({ provider: 'anthropic', model: 'claude-opus-5-5', key: '  ' })).toEqual({ key: 'Paste your API key.' });
    expect(checkKeyForm({ provider: 'fake', model: 'canned', key: '' })).toEqual({});
  });

  it('names the model without the default note', () => {
    expect(modelLabel(status(), 'anthropic', 'claude-opus-5-5')).toBe('Claude Opus 5.5');
    expect(modelLabel(status(), 'anthropic', 'claude-unknown')).toBe('claude-unknown');
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
  const sources = import.meta.glob<string>(['/src/routes/AiSettings.svelte', '/src/routes/AiWizard.svelte', '/src/lib/ai.ts'], {
    query: '?raw',
    import: 'default',
    eager: true,
  });

  it('finds the files', () => {
    expect(Object.keys(sources)).toHaveLength(3);
  });

  it('uses no browser storage and no console', () => {
    for (const [path, text] of Object.entries(sources)) {
      expect(text, path).not.toMatch(/localStorage|sessionStorage|indexedDB|console\./);
    }
  });

  it('keeps the key field a password field with autocomplete off', () => {
    const page = sources['/src/routes/AiSettings.svelte'];
    const input = /<input\s+id="api-key"[\s\S]*?\/>/.exec(page)?.[0] ?? '';
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
    expect(wizardSteps({ state: 'thinking' }).map((s) => s.state)).toEqual(['current', 'waiting']);
    expect(wizardStatusText({ state: 'draft_created' })).toBe('Draft created, generating tests');
    expect(wizardSteps({ state: 'draft_created' }).map((s) => s.state)).toEqual(['done', 'current']);
    expect(wizardSteps({ state: 'error' }).map((s) => s.state)).toEqual(['failed', 'waiting']);
  });

  it('links to the settings for key problems only', () => {
    expect(isKeyProblem('no_key')).toBe(true);
    expect(isKeyProblem('key_refused')).toBe(true);
    expect(isKeyProblem('invalid_answer')).toBe(false);
    expect(isKeyProblem(undefined)).toBe(false);
  });

  it('is pending only while thinking without a submission', () => {
    expect(wizardPending({ state: 'submitted', submission_id: 81 })).toBe(false);
    expect(wizardPending(null)).toBe(false);
    expect(wizardPending({ state: 'thinking' })).toBe(true);
    expect(wizardPending({ state: 'thinking', submission_id: 9 })).toBe(false);
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

describe('AI drafts and the solve check', () => {
  it('finds the AI draft tag in any case', () => {
    expect(isAIDraft(['greedy', 'AI draft'])).toBe(true);
    expect(isAIDraft(['greedy'])).toBe(false);
    expect(withoutAIDraft(['ai draft', 'dp'])).toEqual(['dp']);
  });

  it('says how much a model scored and what it means', () => {
    expect(solveLine({ score: 100, max_score: 100 })).toBe('A model solved this zero-shot: 100 of 100');
    expect(solveMeaning({ score: 100, max_score: 100 })).toMatch(/full marks/);
    expect(solveMeaning({ score: 40, max_score: 100 })).toMatch(/part of the marks/);
    expect(solveMeaning({ score: 0, max_score: 100 })).toMatch(/scored nothing/);
  });
});
