import { describe, expect, it } from 'vitest';
import { nextButton, progress, pullLine, pushLine, verifyLine } from './status.js';

const base = {
  source: { org: 's', org_name: 'Personal', saved_login: true },
  target: { org: 't', org_name: 'Example Team', saved_login: true },
  job: '', job_here: false,
  pull_total: 172, pull_done: 172, pull_complete: true, pulled_at: '2026-09-24T14:40:00Z',
  projects: 172, selected: 156,
  pushed: 120, pending_projects: 36, pending_items: 90, failed: 0, skipped: 0,
  verified_at: '', verify_ok: 0, verify_total: 0, verify_stale: false,
  next: 'push', error: '',
};
const s = (over) => ({ ...base, ...over });

describe('pullLine', () => {
  it('not pulled', () => expect(pullLine(s({ pull_total: 0, pull_done: 0, pull_complete: false, pulled_at: '' }))).toBe('Not pulled yet'));
  it('partial', () => expect(pullLine(s({ pull_done: 80, pull_complete: false }))).toBe('80 of 172 projects downloaded. Stopped, you can resume.'));
  it('running', () => expect(pullLine(s({ pull_done: 80, pull_complete: false, job: 'pull' }))).toBe('Downloading… 80 of 172 projects'));
  it('complete', () => expect(pullLine(s({}))).toMatch(/^172 projects downloaded on /));
});

describe('pushLine', () => {
  it('not started', () => expect(pushLine(s({ pushed: 0, pending_projects: 156 }))).toBe('Not started. 156 projects to create.'));
  it('partial', () => expect(pushLine(s({}))).toBe('120 of 156 projects created. 36 projects and 90 items left.'));
  it('running', () => expect(pushLine(s({ job: 'push' }))).toBe('Sending… 120 of 156 projects created'));
  it('failed', () => expect(pushLine(s({ pending_projects: 0, pending_items: 0, pushed: 156, failed: 2 }))).toBe('156 of 156 projects created. 2 failed items to retry.'));
  it('all sent', () => expect(pushLine(s({ pending_projects: 0, pending_items: 0, pushed: 156 }))).toBe('156 of 156 projects created. Everything is sent.'));
});

describe('verifyLine', () => {
  it('never', () => expect(verifyLine(s({}))).toBe('Not verified yet'));
  it('stale', () => expect(verifyLine(s({ verified_at: 'x', verify_ok: 3, verify_total: 3, verify_stale: true }))).toBe('3 of 3 matched before the last push. Verify again.'));
  it('ok', () => expect(verifyLine(s({ verified_at: 'x', verify_ok: 156, verify_total: 156 }))).toBe('156 of 156 projects match the target.'));
});

describe('nextButton', () => {
  it('resume pull', () => expect(nextButton(s({ next: 'pull', pull_done: 80, pull_complete: false }))).toEqual({ action: 'pull', label: 'Resume pull (80 of 172)' }));
  it('start pull', () => expect(nextButton(s({ next: 'pull', pull_done: 0, pull_total: 0, pull_complete: false }))).toEqual({ action: 'pull', label: 'Start pull' }));
  it('resume push', () => expect(nextButton(s({}))).toEqual({ action: 'push', label: 'Resume push (36 projects left)' }));
  it('start push', () => expect(nextButton(s({ pushed: 0, pending_projects: 156 }))).toEqual({ action: 'push', label: 'Start push (156 projects)' }));
  it('retry', () => expect(nextButton(s({ pending_projects: 0, pending_items: 0, failed: 2 }))).toEqual({ action: 'push', label: 'Retry 2 failed items' }));
  it('wait', () => expect(nextButton(s({ next: 'wait', job: 'push' }))).toEqual({ action: '', label: 'Push running…' }));
  it('connect', () => expect(nextButton(s({ next: 'connect_target' }))).toEqual({ action: 'connect_target', label: 'Connect target account' }));
  it('done', () => expect(nextButton(s({ next: 'done' }))).toEqual({ action: '', label: 'All done' }));
});

describe('progress', () => {
  it('fraction of selected projects created', () => {
    expect(progress(s({}))).toBeCloseTo(120 / 156);
    expect(progress(s({ pushed: 0, pending_projects: 0 }))).toBe(0);
  });
});
