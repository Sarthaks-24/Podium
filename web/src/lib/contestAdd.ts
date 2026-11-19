/**
 * Adding bank problems to a contest. Organiser screens only, so it stays out of
 * contests.ts, which the student first load pulls in.
 */
import { sendJSON } from './api';

/** What `POST /api/admin/contests/{id}/problems/add` answers (V211-66). */
export interface AddProblemsResult {
  added: { problem_id: number; label: string }[];
  refused: { problem_id: number; reason: string }[];
}

/** The page's sentence for each refusal code of an add; `needs_ai_reason` also links to the contest's editor. */
export const ADD_REFUSAL_TEXT: Record<string, string> = {
  already_added: 'Already in this contest',
  not_found: 'No longer available',
  deleted: 'Deleted',
  not_validated: 'Validate it first',
  being_checked: 'Its languages are being checked. Try again when that finishes',
  ai_draft: 'Read it and save it in the editor first',
  needs_ai_reason: 'Add it from the contest editor, where you can give the required reason',
  limit: 'The contest already has 26 problems',
};

/** Adds bank problems to a contest under the next free letters. */
export function addProblemsToContest(contestId: number, problemIds: number[]): Promise<AddProblemsResult> {
  return sendJSON<AddProblemsResult>('POST', `/api/admin/contests/${contestId}/problems/add`, { problem_ids: problemIds });
}
