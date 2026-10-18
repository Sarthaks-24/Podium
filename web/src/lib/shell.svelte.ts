/**
 * Layout requests from a screen to the app shell. A contest page sets
 * `shell.workspace` while it shows the workspace, so the shell trims its
 * vertical padding the way it does for /admin/problems/{id}/try, and
 * clears it when it leaves.
 */
class ShellState {
  workspace = $state(false);
}

/** Shared shell layout state. */
export const shell = new ShellState();
