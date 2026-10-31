/**
 * The screen for each row of the route table (lib/routing.ts), by route
 * name; the type makes a row without a screen a compile error. A student's
 * screens are imported here, so they are in the first load; every
 * organiser screen is loaded with import() on first visit, so a student's
 * first load carries none of them. The shell passes the path's parameters
 * as props and remounts a screen when its key changes: by default every
 * parameter, but a contest stays mounted while the student moves between
 * its problems.
 */
import type { Component } from 'svelte';
import type { RouteName } from './lib/routing';
import ContestPage from './routes/ContestPage.svelte';
import Home from './routes/Home.svelte';
import Landing from './routes/Landing.svelte';
import NotFound from './routes/NotFound.svelte';
import Privacy from './routes/Privacy.svelte';
import SignIn from './routes/SignIn.svelte';
import StandingsPage from './routes/StandingsPage.svelte';

/** Any screen: its props are the path's parameters. */
// eslint-disable-next-line @typescript-eslint/no-explicit-any
export type Screen = Component<any>;

/** How the shell gets one row's screen. */
export interface ScreenDef {
  /** The component, for a screen in the first load. */
  component?: Screen;
  /** Loads the component on first visit. */
  load?: () => Promise<Screen>;
  /** What remounts the screen when it changes; every parameter when left out. */
  key?: (params: Record<string, string>) => string;
}

const lazy = (load: () => Promise<{ default: unknown }>): ScreenDef => ({ load: async () => (await load()).default as Screen });

const contest: ScreenDef = { component: ContestPage, key: (p) => p.slug };

export const SCREENS: Record<RouteName, ScreenDef> = {
  home: { component: Home },
  signin: { component: SignIn },
  dashboard: lazy(() => import('./routes/Dashboard.svelte')),
  me: lazy(() => import('./routes/Me.svelte')),
  problems: lazy(() => import('./routes/ProblemBank.svelte')),
  problem: lazy(() => import('./routes/ProblemEditor.svelte')),
  'problem-try': lazy(() => import('./routes/ProblemTry.svelte')),
  'ai-new': lazy(() => import('./routes/AiWizard.svelte')),
  contest,
  'contest-problem': contest,
  standings: { component: StandingsPage },
  'admin-contests': lazy(() => import('./routes/AdminContests.svelte')),
  'admin-contest': lazy(() => import('./routes/ContestEditor.svelte')),
  'contest-console': lazy(() => import('./routes/ContestConsole.svelte')),
  'contest-results': lazy(() => import('./routes/ContestResults.svelte')),
  'contest-integrity': lazy(() => import('./routes/ContestIntegrity.svelte')),
  'integrity-pair': lazy(() => import('./routes/IntegrityPair.svelte')),
  privacy: { component: Privacy },
  'not-found': { component: NotFound },
};

/** The key that remounts a screen: its own rule, or every parameter. */
export function screenKey(def: ScreenDef, params: Record<string, string>): string {
  return def.key ? def.key(params) : Object.values(params).join('/');
}

/** The signed-out home (v4 P1): not a route of its own, App shows it for / when nobody is signed in. */
export { Landing };
