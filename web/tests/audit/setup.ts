// Clears the previous run's raw results for today, so the report never mixes two runs.
import type { FullConfig } from '@playwright/test';
import { mkdirSync, rmSync } from 'node:fs';
import { rawDir } from './paths';

export default function setup(config: FullConfig) {
  const raw = rawDir(config.rootDir);
  rmSync(raw, { recursive: true, force: true });
  mkdirSync(raw, { recursive: true });
}
