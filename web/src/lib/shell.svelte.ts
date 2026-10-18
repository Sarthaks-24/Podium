/**
 * Layout requests from a screen to the app shell. A contest page sets
 * `shell.workspace` while it shows the workspace, so the shell trims its
 * vertical padding the way it does for /admin/problems/{id}/try, and
 * clears it when it leaves. `shell.shortcuts` opens the "Keyboard
 * shortcuts" dialog the shell owns, so any screen can offer a button for it.
 */
class ShellState {
  workspace = $state(false);
  shortcuts = $state(false);
}

/** Shared shell layout state. */
export const shell = new ShellState();
