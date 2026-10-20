// The node:fs calls the web app's tooling makes: a11y.spec.ts saves the axe
// report, and vite.config.ts reads the privacy notice at build time.
// Declared here so the project needs no @types/node.
declare module 'node:fs' {
  export function readFileSync(path: string | URL, encoding: 'utf8'): string;
  export function mkdirSync(path: string, options?: { recursive?: boolean }): string | undefined;
  export function writeFileSync(path: string, data: string): void;
}
