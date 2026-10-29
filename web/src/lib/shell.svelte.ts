/**
 * Layout requests from a screen to the app shell.
 *
 * A contest page sets `shell.workspace` while it shows the workspace, so the
 * shell trims its vertical padding the way it does for
 * /admin/problems/{id}/try, and clears it when it leaves. `shell.help`
 * opens the Help dialog the shell owns, so any screen can offer a button
 * for it; a contest page puts its rules in `shell.rules` while it is shown,
 * so Help lists them first. `shell.theme` opens the "Theme and colours"
 * dialog the same way.
 */
class ShellState {
  workspace = $state(false);
  help = $state(false);
  rules = $state.raw<string[] | null>(null);
  theme = $state(false);
}

/** Shared shell layout state. */
export const shell = new ShellState();
