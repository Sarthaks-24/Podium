// The node:fs calls the web app's tooling makes: a11y.spec.ts saves the axe
// report, the screen audit (tests/audit/) writes and reads its results, and
// vite.config.ts reads the privacy notice at build time.
// Declared here so the project needs no @types/node.
declare module 'node:fs' {
  export function readFileSync(path: string | URL, encoding: 'utf8'): string;
  export function mkdirSync(path: string, options?: { recursive?: boolean }): string | undefined;
  export function writeFileSync(path: string, data: string): void;
  export function readdirSync(path: string): string[];
  export function existsSync(path: string): boolean;
  export function rmSync(path: string, options?: { recursive?: boolean; force?: boolean }): void;
}

// The screen audit finds the repository root from its own file.
declare module 'node:url' {
  export function fileURLToPath(url: string | URL): string;
}

// The screen audit's config passes the run's date to its workers through the environment.
declare const process: { env: Record<string, string | undefined> };
