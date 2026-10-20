/**
 * Layout requests from a screen to the app shell, and the little the shell
 * itself loads for the header.
 *
 * A contest page sets `shell.workspace` while it shows the workspace, so the
 * shell trims its vertical padding the way it does for
 * /admin/problems/{id}/try, and clears it when it leaves. `shell.shortcuts`
 * opens the "Keyboard shortcuts" dialog the shell owns, so any screen can
 * offer a button for it; `shell.theme` opens the "Theme and colours"
 * dialog the same way. `shell.questions` says where a student's integrity
 * cases stand (GET /api/integrity/cases, loaded once after sign-in by
 * loadQuestions): the header shows "Questions" for any case and Your
 * contests words its line by it (v2 S7). A failed load reads as "none".
 */
import { getJSON } from './api';
import { casesStanding, type CasesStanding, type StudentCaseList } from './review';

class ShellState {
  workspace = $state(false);
  shortcuts = $state(false);
  theme = $state(false);
  questions = $state<CasesStanding>('none');
}

/** Shared shell layout state. */
export const shell = new ShellState();

/** Loads where the signed-in student's cases stand; any failure leaves "none". */
export async function loadQuestions(): Promise<void> {
  try {
    const data = await getJSON<StudentCaseList>('/api/integrity/cases');
    shell.questions = casesStanding(data.cases);
  } catch {
    shell.questions = 'none';
  }
}
