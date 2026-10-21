import { describe, expect, it } from 'vitest';
import source from '../content/privacy.md?raw';
import { RETENTION_FALLBACK, privacyBodyHtml } from './privacyBody';

// The first call loads the Markdown renderer and DOMPurify's DOM, which took
// over the 5 s default once under a full parallel run (C2-049); alone it is
// well under a second.
describe('the no-JavaScript privacy body (FP.1)', { timeout: 15_000 }, () => {
  it('renders the notice with a block for the retention table', async () => {
    const html = await privacyBodyHtml(source);
    expect(html.startsWith('<h1>Privacy</h1>')).toBe(true);
    expect(html).toContain('<h2>What podium records</h2>');
    expect(html).toContain('<h2>How long it is kept</h2>');
    expect(html).toContain(`<!--podium:retention-->${RETENTION_FALLBACK}<!--/podium:retention-->`);
    expect(html).not.toContain('<!-- retention -->');
  });

  it('refuses raw HTML and scripts in the Markdown', async () => {
    const html = await privacyBodyHtml('Hello <script>alert(1)</script> <img src=x onerror=alert(1)>\n\n[x](javascript:alert(1))');
    expect(html).not.toMatch(/<script|onerror|javascript:/i);
  });
});
