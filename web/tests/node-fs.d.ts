// The two node:fs calls a11y.spec.ts makes to save the axe report. Declared
// here so the tests need no @types/node.
declare module 'node:fs' {
  export function mkdirSync(path: string, options?: { recursive?: boolean }): string | undefined;
  export function writeFileSync(path: string, data: string): void;
}
