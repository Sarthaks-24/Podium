import { describe, expect, it } from 'vitest';
import { mayChange, rolesFor } from './people';

// The screen's copy of server/internal/auth/rules.go (V4-16).
describe('who may change whose account', () => {
  const admin = { id: 5, role: 'admin' as const };
  const owner = { id: 1, role: 'owner' as const };
  const row = (id: number, role: 'student' | 'organiser' | 'admin' | 'owner', locked = false) => ({ id, role, locked });

  it('lets an admin change students and organisers only, never themself', () => {
    expect(mayChange(admin, row(9, 'student'))).toBe(true);
    expect(mayChange(admin, row(9, 'organiser'))).toBe(true);
    expect(mayChange(admin, row(9, 'admin'))).toBe(false);
    expect(mayChange(admin, row(9, 'owner'))).toBe(false);
    expect(mayChange(admin, row(5, 'admin'))).toBe(false);
  });

  it('lets an owner change anyone but the configured owner', () => {
    expect(mayChange(owner, row(9, 'admin'))).toBe(true);
    expect(mayChange(owner, row(2, 'owner'))).toBe(true);
    expect(mayChange(owner, row(1, 'owner', true))).toBe(false);
  });

  it('lets nobody below admin change anything', () => {
    expect(mayChange({ id: 3, role: 'organiser' }, row(9, 'student'))).toBe(false);
  });

  it('offers an admin only the student and organiser roles', () => {
    expect(rolesFor(admin)).toEqual(['student', 'organiser']);
    expect(rolesFor(owner)).toEqual(['student', 'organiser', 'admin', 'owner']);
    expect(rolesFor({ id: 3, role: 'organiser' })).toEqual([]);
  });
});
