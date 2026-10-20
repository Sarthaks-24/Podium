// The one jsdom call lib/privacyBody.ts makes at build time: a window for
// DOMPurify in Node. Declared here so the project needs no @types/jsdom.
declare module 'jsdom' {
  export class JSDOM {
    constructor(html?: string);
    readonly window: Window & typeof globalThis;
  }
}
