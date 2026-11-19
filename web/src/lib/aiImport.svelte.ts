/**
 * The answer a person imported on the problem list, waiting for the review
 * screen at /admin/ai/new?import=1. It lives in memory only: a reload empties
 * it, and the page then asks for the file again.
 */
import type { OpenedAnswer } from './ai';

class AiImport {
  answer = $state<OpenedAnswer | null>(null);

  set(a: OpenedAnswer) {
    this.answer = a;
  }

  /** Hands the answer over once; the store is empty afterwards. */
  take(): OpenedAnswer | null {
    const a = this.answer;
    this.answer = null;
    return a;
  }

  clear() {
    this.answer = null;
  }
}

export const aiImport = new AiImport();
