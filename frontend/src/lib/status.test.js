import { describe, expect, it } from 'vitest';
import { artifactLine, chatLine, lastSyncLine, memoryLine, nextButton, selectionLine, skillLine, progress, pullLine, pushLine, timeLeft, verifyLine } from './status.js';

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
  it('running', () => expect(pullLine(s({ pull_done: 80, pull_complete: false, job: 'pull', pull_phase: 'projects' }))).toBe('Downloading… 80 of 172 projects'));
  it('reading chats', () => expect(pullLine(s({ pull_complete: false, job: 'pull', pull_phase: 'chats', chats: 392, artifacts: 427, phase_done: 0, phase_total: 0 }))).toBe('Projects done. Reading chats… 392 read, 427 artifacts found'));
  it('reading chats with total and time left', () => {
    const started = new Date(Date.now() - 10 * 60 * 1000).toISOString(); // 10 min ago
    expect(pullLine(s({ pull_complete: false, job: 'pull', pull_phase: 'chats', artifacts: 427, phase_done: 400, phase_total: 1000, phase_started_at: started })))
      .toBe('Reading chats: 400 of 1000 (40%), about 15 min left. 427 artifacts found.');
  });
  it('downloading projects with time left', () => {
    const started = new Date(Date.now() - 2 * 60 * 1000).toISOString();
    expect(pullLine(s({ pull_complete: false, job: 'pull', pull_phase: 'projects', pull_done: 86, pull_total: 172, phase_done: 86, phase_total: 172, phase_started_at: started })))
      .toBe('Downloading projects: 86 of 172 (50%), about 2 min left.');
  });
  it('stopped while reading chats', () => expect(pullLine(s({ pull_complete: false, pull_phase: 'chats', chats: 392, artifacts: 427 }))).toBe('Projects done. Stopped while reading chats (392 read). You can resume.'));
  it('complete', () => expect(pullLine(s({}))).toMatch(/^172 projects downloaded on /));
  it('complete with artifacts', () => expect(pullLine(s({ artifacts: 57, chats: 300 }))).toMatch(/^172 projects and 57 artifacts from 300 chats downloaded on /));
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
  it('stale', () => expect(verifyLine(s({ verified_at: 'x', verify_ok: 2, verify_total: 3, verify_unchecked: 1, verify_stale: true }))).toBe('1 project changed since the last check. Verify again.'));
  it('not selected note', () => expect(verifyLine(s({ verified_at: 'x', verify_ok: 3, verify_total: 3, verify_not_selected: 12 }))).toBe('3 of 3 projects match the target. 12 sent earlier are not selected, so not checked.'));
  it('ok', () => expect(verifyLine(s({ verified_at: 'x', verify_ok: 156, verify_total: 156 }))).toBe('156 of 156 projects match the target.'));
  it('only unsent artifacts', () => expect(verifyLine(s({ verified_at: 'x', verify_ok: 33, verify_total: 124, verify_waiting: 91 }))).toBe('33 of 124 projects match. The other 91 only need their artifacts sent.'));
  it('mixed', () => expect(verifyLine(s({ verified_at: 'x', verify_ok: 30, verify_total: 124, verify_waiting: 90 }))).toBe('30 of 124 projects match. 90 only need their artifacts sent; 4 have other differences.'));
});

describe('nextButton', () => {
  it('resume pull', () => expect(nextButton(s({ next: 'pull', pull_done: 80, pull_complete: false }))).toEqual({ action: 'pull', label: 'Resume pull (80 of 172)' }));
  it('start pull', () => expect(nextButton(s({ next: 'pull', pull_done: 0, pull_total: 0, pull_complete: false }))).toEqual({ action: 'pull', label: 'Start pull' }));
  it('resume push', () => expect(nextButton(s({}))).toEqual({ action: 'push', label: 'Resume push (36 projects left)' }));
  it('start push', () => expect(nextButton(s({ pushed: 0, pending_projects: 156 }))).toEqual({ action: 'push', label: 'Start push (156 projects)' }));
  it('retry', () => expect(nextButton(s({ pending_projects: 0, pending_items: 0, failed: 2 }))).toEqual({ action: 'push', label: 'Retry 2 failed items' }));
  it('wait', () => expect(nextButton(s({ next: 'wait', job: 'push' }))).toEqual({ action: '', label: 'Push running…' }));
  it('connect', () => expect(nextButton(s({ next: 'connect_target' }))).toEqual({ action: 'connect_target', label: 'Connect target account' }));
  it('review mismatches', () => expect(nextButton(s({ next: 'review', verify_ok: 33, verify_total: 124, verify_waiting: 0 }))).toEqual({ action: 'verify', label: 'Verify again (91 projects did not match)' }));
  it('review: only unsent artifacts', () => expect(nextButton(s({ next: 'review', verify_ok: 33, verify_total: 124, verify_waiting: 91 }))).toEqual({ action: 'select', label: 'Choose projects to send the remaining artifacts' }));
});

describe('progress', () => {
  it('fraction of selected projects created', () => {
    expect(progress(s({}))).toBeCloseTo(120 / 156);
    expect(progress(s({ pushed: 0, pending_projects: 0 }))).toBe(0);
  });
});

describe('artifactLine', () => {
  const a = (over) => s({ chats: 300, artifacts: 57, artifacts_sent: 0, artifacts_pending: 50, artifacts_no_project: 7, ...over });
  it('not pulled', () => expect(artifactLine(s({ chats: 0, artifacts: 0 }))).toBe('Not read yet. Pull reads your chats for artifacts.'));
  it('none found', () => expect(artifactLine(s({ chats: 12, artifacts: 0 }))).toBe('No artifacts found in 12 chats.'));
  it('waiting', () => expect(artifactLine(a({}))).toBe('57 found in 300 chats. 0 sent, 50 waiting, 7 kept locally (chats outside a project).'));
  it('all sent', () => expect(artifactLine(a({ artifacts_sent: 50, artifacts_pending: 0 }))).toBe('57 found in 300 chats. 50 sent, 7 kept locally (chats outside a project).'));
  it('switched off', () => expect(artifactLine(a({ skip_artifacts: true }))).toBe('57 found in 300 chats. Not copied. Turn it on in Settings.'));
  it('switched off after some were sent', () => expect(artifactLine(a({ skip_artifacts: true, artifacts_sent: 20 }))).toBe('57 found in 300 chats. 20 sent earlier; the rest are not copied. Turn it on in Settings.'));
  it('no loose', () => expect(artifactLine(a({ artifacts_sent: 57, artifacts_pending: 0, artifacts_no_project: 0 }))).toBe('57 found in 300 chats. 57 sent.'));
});

describe('timeLeft', () => {
  const ago = (ms) => new Date(Date.now() - ms).toISOString();
  it('needs a few items and some time first', () => {
    expect(timeLeft(ago(60000), 2, 100)).toBe('');
    expect(timeLeft(ago(3000), 50, 100)).toBe('');
  });
  it('minutes and hours', () => {
    expect(timeLeft(ago(10 * 60000), 400, 1000)).toBe('about 15 min left');
    expect(timeLeft(ago(60 * 60000), 100, 400)).toBe('about 3 h left');
    expect(timeLeft(ago(60 * 60000), 100, 250)).toBe('about 1 h 30 min left');
    expect(timeLeft(ago(60000), 95, 100)).toBe('less than a minute left');
    expect(timeLeft(ago(60000), 100, 100)).toBe('');
  });
});

describe('memoryLine', () => {
  it('not pulled', () => expect(memoryLine(s({ pull_complete: false, pull_done: 0, pull_total: 0, memory_pending: false, memory_sent_at: '' }))).toBe('Pull first to read your memory.'));
  it('ready to send', () => expect(memoryLine(s({ memory_pending: true, memory_sent_at: '' }))).toBe('Ready to send. claude.ai merges it into the target account’s memory.'));
  it('changed since sent', () => expect(memoryLine(s({ memory_pending: true, memory_sent_at: '2026-09-25T00:20:00Z' }))).toMatch(/^Changed since it was sent on .+\. Send again to update\.$/));
  it('sent', () => expect(memoryLine(s({ memory_pending: false, memory_sent_at: '2026-09-25T00:20:00Z' }))).toMatch(/^Sent on .+\. claude\.ai adds it to memory in the background\.$/));
  it('switched off', () => expect(memoryLine(s({ skip_memory: true, memory_pending: false, memory_sent_at: '' }))).toBe('Not copied. Turn it on in Settings.'));
  it('switched off after it was sent', () => expect(memoryLine(s({ skip_memory: true, memory_pending: false, memory_sent_at: '2026-09-25T00:20:00Z' }))).toMatch(/^Sent on .+\. Changes are not copied\. Turn it on in Settings\.$/));
  it('pull first even when switched off', () => expect(memoryLine(s({ skip_memory: true, pull_complete: false, pull_done: 0, pull_total: 0, memory_pending: false, memory_sent_at: '' }))).toBe('Pull first to read your memory.'));
});

describe('nextButton memory', () => {
  it('memory', () => expect(nextButton(s({ next: 'memory' }))).toEqual({ action: 'memory', label: 'Send memory to target' }));
});

describe('lastSyncLine', () => {
  it('never', () => expect(lastSyncLine(s({ last_sync_at: '' }))).toBe(''));
  it('nothing new', () => expect(lastSyncLine(s({ last_sync_at: '2026-09-25T01:00:00Z', last_sync_new_projects: 0, last_sync_changed_projects: 0, last_sync_chats: 0, last_sync_artifacts: 0, last_sync_sent: 0 })))
    .toMatch(/^Last synced .+\. No changes found\.$/));
  it('changes', () => expect(lastSyncLine(s({ last_sync_at: '2026-09-25T01:00:00Z', last_sync_new_projects: 1, last_sync_changed_projects: 2, last_sync_chats: 7, last_sync_artifacts: 5, last_sync_sent: 14 })))
    .toMatch(/^Last synced .+\. Found 1 new project, 2 changed, 7 new or changed chats \(5 artifacts\)\. Sent 14 items\.$/));
});

describe('nextButton sync', () => {
  it('done offers a scan', () => expect(nextButton(s({ next: 'done' }))).toEqual({ action: 'sync', label: 'Scan & sync changes' }));
});

describe('before the first pull', () => {
  const fresh = s({ pull_total: 0, pull_done: 0, pull_complete: false, projects: 0, selected: 0, pushed: 0, pending_projects: 0 });
  it('push line', () => expect(pushLine(fresh)).toBe('Pull first.'));
  it('selection line', () => expect(selectionLine(fresh)).toBe('Pull first to see your projects.'));
  it('selection line, personal left out', () => expect(selectionLine(s({ personal_projects: 16, personal_skipped: 16, personal_choice: 'skip' }))).toBe('156 of 172 projects selected, 16 personal left out'));
  it('selection line, personal included', () => expect(selectionLine(s({ personal_projects: 16, personal_skipped: 0, personal_choice: 'include' }))).toBe('156 of 172 projects selected, personal projects included'));
  it('selection line, personal off but all picked by hand', () => expect(selectionLine(s({ personal_projects: 2, personal_skipped: 0, personal_choice: 'skip' }))).toBe('156 of 172 projects selected'));
  it('selection line after pull', () => expect(selectionLine(s({}))).toBe('156 of 172 projects selected'));
});

describe('skillLine', () => {
  it('before pull', () => expect(skillLine(s({ skills: 0, pull_complete: false, pull_done: 0, pull_total: 0 }))).toBe('Pull first. Only your own skills are copied, not the built-in ones.'));
  it('none', () => expect(skillLine(s({ skills: 0 }))).toBe('No personal skills found. Built-in skills are not copied.'));
  it('waiting', () => expect(skillLine(s({ skills: 5, skills_sent: 1, skills_pending: 4 }))).toBe('5 personal skills found. 1 sent, 4 waiting.'));
  it('failed', () => expect(skillLine(s({ skills: 15, skills_sent: 14, skills_failed: 1, skills_pending: 0 }))).toBe('15 personal skills found. 14 sent, 1 failed. Retry sends it again.'));
  it('a failed skill is not blamed on projects', () => expect(pushLine(s({ pushed: 3, pending_projects: 0, pending_items: 0, failed: 1, skills_failed: 1 }))).toMatch(/Everything is sent\.$/));
  it('switched off', () => expect(skillLine(s({ skills: 5, skills_sent: 0, skills_pending: 0, skip_skills: true }))).toBe('5 personal skills found. Not copied. Turn it on in Settings.'));
  it('switched off after some were sent', () => expect(skillLine(s({ skills: 5, skills_sent: 2, skills_pending: 0, skip_skills: true }))).toBe('5 personal skills found. 2 sent earlier; the rest are not copied. Turn it on in Settings.'));
  it('all sent', () => expect(skillLine(s({ skills: 5, skills_sent: 5, skills_pending: 0 }))).toBe('5 personal skills found. All sent.'));
});

describe('chatLine', () => {
  it('none', () => expect(chatLine(s({ chats_in_projects: 0 }))).toBe('No chats in the selected projects.'));
  it('not reviewed yet counts as off', () => expect(chatLine(s({ chats_in_projects: 605, chat_choice: '' }))).toBe('605 chats in the selected projects. Not copied. Turn it on in Settings.'));
  it('switched off', () => expect(chatLine(s({ chats_in_projects: 1, chat_choice: 'skip' }))).toBe('1 chat in the selected projects. Not copied. Turn it on in Settings.'));
  it('switched off after some were sent', () => expect(chatLine(s({ chats_in_projects: 605, chat_choice: 'skip', chats_sent: 5 }))).toBe('605 chats in the selected projects. 5 sent earlier; the rest are not copied. Turn it on in Settings.'));
  it('waiting', () => expect(chatLine(s({ chats_in_projects: 605, chat_choice: 'include', chats_sent: 5, chats_pending: 600 }))).toBe('605 chats in the selected projects. 5 sent as documents, 600 waiting.'));
  it('done', () => expect(chatLine(s({ chats_in_projects: 605, chat_choice: 'include', chats_sent: 605, chats_pending: 0 }))).toBe('605 chats in the selected projects. All sent as documents.'));
});
