import { describe, expect, it } from 'vitest';
import { ApiError } from './api';
import {
  checkingLine,
  checkResultLine,
  failureText,
  languageName,
  lastCheckLine,
  notOffered,
  notOfferedLine,
  offeredNames,
  proofPaths,
  proofRefusal,
  referenceBanner,
  roundFinished,
  type ProofLanguage,
  type ProofState,
} from './proof';

const lang = (over: Partial<ProofLanguage> = {}): ProofLanguage => ({
  language: 'java21',
  offered: false,
  result: 'failed',
  reason: 'wrong_answer',
  failed: 3,
  total: 12,
  compiler: '',
  tests: [],
  current: true,
  source: 'wizard',
  ...over,
});

const four = (over: Record<string, Partial<ProofLanguage>> = {}): ProofLanguage[] =>
  ['cpp17', 'java21', 'python3', 'node22'].map((language) => lang({ language, offered: true, result: 'passed', reason: '', failed: 0, total: 12, ...(over[language] ?? {}) }));

describe('names and lists', () => {
  it('says each language in a short name', () => {
    expect(['cpp17', 'java21', 'python3', 'node22', 'other'].map(languageName)).toEqual(['C++', 'Java', 'Python', 'JavaScript', 'other']);
  });

  it('splits the languages into offered and not offered, in the server order', () => {
    const state = { languages: four({ java21: { offered: false } }) };
    expect(offeredNames(state)).toEqual(['C++', 'Python', 'JavaScript']);
    expect(notOffered(state).map((l) => l.language)).toEqual(['java21']);
    expect(offeredNames({ languages: [] })).toEqual([]);
  });

  it('builds the paths', () => {
    expect(proofPaths.state(7)).toBe('/api/admin/problems/7/proof');
    expect(proofPaths.check(7, 'java21')).toBe('/api/admin/problems/7/languages/java21/check');
    expect(proofPaths.fix(7, 'node22')).toBe('/api/admin/problems/7/languages/node22/ai-fix');
  });
});

describe('why a language failed', () => {
  it('counts the failed tests', () => {
    expect(failureText(lang())).toBe("The AI's Java code failed 3 of 12 tests.");
    expect(failureText(lang({ failed: 1, total: 1 }))).toBe("The AI's Java code failed 1 of 1 test.");
    expect(failureText(lang({ reason: 'time_limit', failed: 2, total: 12 }))).toBe("The AI's Java code failed 2 of 12 tests.");
  });

  it('names a compile error, a locked code that ignores the function, and a check that could not run', () => {
    expect(failureText(lang({ reason: 'compile_error', failed: 0 }))).toBe("The AI's Java code did not compile.");
    expect(failureText(lang({ reason: 'ignores_function', failed: 0 }))).toBe("The locked code passes without the student's function.");
    expect(failureText(lang({ result: 'unchecked', reason: 'judge_error', failed: 0, total: 0 }))).toBe('It could not be checked.');
    expect(failureText(lang({ result: 'none', reason: '' }))).toBe('');
  });

  it('writes the not offered line of the wizard with the way forward', () => {
    expect(notOfferedLine(lang())).toBe("Not offered: Java. The AI's Java code failed 3 of 12 tests. Open the draft to fix it.");
    expect(notOfferedLine(lang({ reason: 'compile_error', failed: 0 }))).toBe("Not offered: Java. The AI's Java code did not compile. Open the draft to fix it.");
    expect(notOfferedLine(lang({ reason: 'ignores_function', failed: 0 }))).toBe(
      "Not offered: Java. The locked code passes without the student's function. Open the draft to fix it.",
    );
    expect(notOfferedLine(lang({ result: 'unchecked', reason: 'judge_error' }))).toBe('Not offered: Java. It could not be checked. Open the draft and press Fix Java with AI.');
    expect(notOfferedLine(lang({ language: 'node22', result: 'none', reason: '' }))).toBe('Not offered: JavaScript. The AI did not write it.');
  });
});

describe('an offered language', () => {
  it('says its last check, or that it was never checked', () => {
    const offered = (over: Partial<ProofLanguage>) => lang({ offered: true, ...over });
    expect(lastCheckLine(offered({ result: 'passed', reason: '', failed: 0 }))).toBe('Passes every test.');
    expect(lastCheckLine(offered({ failed: 2 }))).toBe('Failed 2 of 12 tests in the last check.');
    expect(lastCheckLine(offered({ result: 'none', reason: '', failed: 0, total: 0 }))).toBe('Not checked yet. Press Check Java before a contest.');
    expect(lastCheckLine(offered({ result: 'unchecked', reason: 'judge_error' }))).toBe('The last check could not be run. Press Check Java to try again.');
    expect(lastCheckLine(offered({ result: 'passed', reason: '', current: false }))).toBe('Passed before the last change. Press Check Java again.');
  });

  it('says the result of one finished check', () => {
    expect(checkResultLine(lang({ result: 'passed', reason: '', failed: 0 }))).toBe('Java passes every test.');
    expect(checkResultLine(lang({ failed: 2 }))).toBe('Java fails 2 of 12 tests.');
    expect(checkResultLine(lang({ reason: 'compile_error', failed: 0 }))).toBe('Java does not compile.');
    expect(checkResultLine(lang({ result: 'unchecked', reason: 'judge_error' }))).toBe('Java could not be checked.');
  });
});

describe('progress and rounds', () => {
  it('names the language being checked and its place among the offered ones', () => {
    const languages = four();
    expect(checkingLine({ progress: { done: 2, total: 7, current: 'java21' }, languages })).toBe('Checking Java, 2 of 4.');
    expect(checkingLine({ progress: { done: 0, total: 7, current: '' }, languages })).toBe('Checking the languages.');
    expect(checkingLine({ progress: { done: 0, total: 2, current: 'java21' }, languages: four({ java21: { offered: false } }) })).toBe('Checking Java.');
    expect(checkingLine({ languages })).toBe('Checking the languages.');
  });

  it('knows when a round has finished', () => {
    const state = (over: Partial<ProofState>): ProofState => ({ checking: false, languages: four(), ...over });
    expect(roundFinished(state({}))).toBe(true);
    expect(roundFinished(state({ checking: true }))).toBe(false);
    expect(roundFinished(state({ languages: four({ cpp17: { result: 'none' }, java21: { result: 'none' }, python3: { result: 'none' }, node22: { result: 'none' } }) }))).toBe(false);
    expect(roundFinished(state({ languages: [], applied: { validating: false } }))).toBe(true);
  });

  it('says when the reference language failed, and says nothing otherwise', () => {
    expect(referenceBanner({ applied: { kept: 'reference_failed', validating: false } })).toBe(
      "The AI's own solution failed its tests, so this draft cannot be validated. Open it to look, or ask again with a simpler description.",
    );
    expect(referenceBanner({ applied: { kept: 'reference_unchecked', validating: false } })).toBe(
      "The AI's own solution could not be checked, so this draft cannot be validated. Open it and press Validate.",
    );
    expect(referenceBanner({ applied: { removed: ['java21'], validating: true } })).toBe('');
    expect(referenceBanner(null)).toBe('');
  });
});

describe('refusals', () => {
  const refused = (status: number, code: string, message: string, problems: string[] = []) => new ApiError(status, code, message, { problems });
  const say = (code: string, language = 'java21', status = 409) => proofRefusal(refused(status, code, 'server text'), language).message;

  it('maps every refusal code of the three routes to its copy', () => {
    expect(say('not_found', 'java21', 404)).toBe('There is no such problem.');
    expect(say('deleted')).toBe('This problem was deleted and cannot be changed.');
    expect(say('not_function')).toBe('Only a function problem has languages to check.');
    expect(say('not_offered')).toBe('Java 21 is not offered. Write its locked code and starting code and save, or use Fix Java 21 with AI.');
    expect(say('already_offered', 'python3')).toBe('Python 3 is already offered. Use Check Python 3 to test it.');
    expect(say('checking')).toBe('Podium is still checking the languages of this problem.');
    expect(say('not_ready')).toBe('The problem is not ready. Fix these first.');
    expect(say('no_key')).toBe('Add a key first.');
    expect(say('busy', 'java21', 429)).toBe('One AI request at a time. Wait for the current one to finish.');
    expect(say('invalid_answer', 'node22', 422)).toBe("The AI's JavaScript (Node 22) code could not be used, so nothing was changed. Press Fix again, or write the code yourself.");
    expect(say('changed')).toBe('The problem changed while the AI was working. Press Fix again.');
  });

  it('keeps the server sentence where it names something the page does not know', () => {
    expect(say('no_model')).toBe('server text');
    expect(say('bad_request', 'java21', 400)).toBe('server text');
    expect(say('provider_error', 'java21', 502)).toBe('server text');
    expect(proofRefusal(new Error('boom'), 'java21').message).not.toBe('');
  });

  it('carries the problems list and the code', () => {
    const r = proofRefusal(refused(409, 'not_ready', 'x', ['Tests need outputs.']), 'java21');
    expect(r.problems).toEqual(['Tests need outputs.']);
    expect(r.code).toBe('not_ready');
  });
});
