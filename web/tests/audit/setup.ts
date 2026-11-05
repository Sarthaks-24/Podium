// Clears the previous run's raw results for today, so the report never mixes two runs.
import { mkdirSync, rmSync } from 'node:fs';
import { rawDir } from './paths';

export default function setup() {
  const raw = rawDir();
  rmSync(raw, { recursive: true, force: true });
  mkdirSync(raw, { recursive: true });
}
