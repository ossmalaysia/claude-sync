// Wording for the home screen, derived only from the backend Status (which is
// rebuilt from the files on disk), so it is right after a restart and when
// the CLI did the work.

function plural(n, word) {
  return `${n} ${word}${n === 1 ? '' : 's'}`;
}

// off is the wording for a row whose switch is off in "What to copy".
const off = 'Not copied. Turn it on in Settings.';

// offLine tells what was sent before the switch was turned off, if anything.
function offLine(head, sent) {
  return sent ? `${head} ${sent} sent earlier; the rest are not copied. Turn it on in Settings.` : `${head} ${off}`;
}

function when(iso) {
  if (!iso) return '';
  const d = new Date(iso);
  return d.toLocaleString(undefined, { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' });
}

// counted renders "400 of 1000 (40%)" for the running pull phase.
function counted(s) {
  return `${s.phase_done} of ${s.phase_total} (${Math.floor((s.phase_done / s.phase_total) * 100)}%)`;
}

function left(s) {
  const t = timeLeft(s.phase_started_at, s.phase_done, s.phase_total);
  return t ? `, ${t}` : '';
}

// timeLeft estimates the remaining time from the pace so far:
// elapsed / done * (total - done). It stays quiet until there is enough
// signal (at least 5 items and 10 seconds).
export function timeLeft(startedISO, done, total, now = Date.now()) {
  if (!startedISO || done < 5 || done >= total) return '';
  const elapsed = now - new Date(startedISO).getTime();
  if (!(elapsed >= 10000)) return '';
  const mins = Math.round(((elapsed / done) * (total - done)) / 60000);
  if (mins < 1) return 'less than a minute left';
  if (mins < 60) return `about ${mins} min left`;
  const h = Math.floor(mins / 60);
  const m = Math.round((mins % 60) / 10) * 10;
  return m ? `about ${h} h ${m} min left` : `about ${h} h left`;
}

export function pullLine(s) {
  if (s.job === 'pull' && s.pull_phase === 'chats') {
    if (!s.phase_total) return `Projects done. Reading chats… ${s.chats} read, ${s.artifacts} artifacts found`;
    return `Reading chats: ${counted(s)}${left(s)}. ${s.artifacts} artifacts found.`;
  }
  if (s.job === 'pull' && s.phase_total) return `Downloading projects: ${counted(s)}${left(s)}.`;
  if (s.job === 'pull') return `Downloading… ${s.pull_done} of ${s.pull_total} projects`;
  if (!s.pull_complete && s.pull_phase === 'chats') return `Projects done. Stopped while reading chats (${s.chats} read). You can resume.`;
  if (!s.pull_total && !s.pull_done) return 'Not pulled yet';
  if (!s.pull_complete) return `${s.pull_done} of ${s.pull_total} projects downloaded. Stopped, you can resume.`;
  const arts = s.artifacts ? ` and ${s.artifacts} artifacts from ${s.chats} chats` : '';
  return `${s.pull_done} projects${arts} downloaded on ${when(s.pulled_at)}`;
}

function selectedTotal(s) {
  return s.pushed + s.pending_projects;
}

export function pushLine(s) {
  const total = selectedTotal(s);
  if (s.job === 'push') return `Sending… ${s.pushed} of ${total} projects created`;
  if (!s.projects && !s.pushed) return 'Pull first.';
  if (s.pushed === 0) return `Not started. ${total} projects to create.`;
  const head = `${s.pushed} of ${total} projects created`;
  if (s.pending_projects + s.pending_items > 0) return `${head}. ${s.pending_projects} projects and ${s.pending_items} items left.`;
  const failed = s.failed - (s.skills_failed || 0); // failed skills show on their own row
  if (failed > 0) return `${head}. ${failed} failed items to retry.`;
  return `${head}. Everything is sent.`;
}

export function verifyLine(s) {
  if (!s.verified_at) return 'Not verified yet';
  if (s.verify_stale) return `${plural(s.verify_unchecked, 'project')} changed since the last check. Verify again.`;
  const off = s.verify_total - s.verify_ok;
  if (off > 0 && s.verify_waiting) {
    const other = off - s.verify_waiting;
    if (!other) return `${s.verify_ok} of ${s.verify_total} projects match. The other ${off} only need their artifacts sent.`;
    return `${s.verify_ok} of ${s.verify_total} projects match. ${s.verify_waiting} only need their artifacts sent; ${other} have other differences.`;
  }
  const note = s.verify_not_selected ? ` ${s.verify_not_selected} sent earlier are not selected, so not checked.` : '';
  return `${s.verify_ok} of ${s.verify_total} projects match the target.${note}`;
}

// nextButton is the single primary action for the current state.
export function nextButton(s) {
  switch (s.next) {
    case 'wait':
      return { action: '', label: s.job === 'pull' ? 'Pull running…' : 'Push running…' };
    case 'connect_source':
      return { action: 'connect_source', label: 'Connect source account' };
    case 'pull':
      return s.pull_done > 0
        ? { action: 'pull', label: `Resume pull (${s.pull_done} of ${s.pull_total})` }
        : { action: 'pull', label: 'Start pull' };
    case 'select':
      return { action: 'select', label: 'Choose projects' };
    case 'connect_target':
      return { action: 'connect_target', label: 'Connect target account' };
    case 'push':
      if (s.pending_projects + s.pending_items === 0) return { action: 'push', label: `Retry ${s.failed} failed items` };
      if (s.pushed === 0) return { action: 'push', label: `Start push (${selectedTotal(s)} projects)` };
      return { action: 'push', label: `Resume push (${s.pending_projects} projects left)` };
    case 'verify':
      return { action: 'verify', label: 'Verify target' };
    case 'memory':
      return { action: 'memory', label: 'Send memory to target' };
    case 'review':
      if (s.verify_waiting && s.verify_waiting === s.verify_total - s.verify_ok)
        return { action: 'select', label: 'Choose projects to send the remaining artifacts' };
      return { action: 'verify', label: `Verify again (${s.verify_total - s.verify_ok} projects did not match)` };
    default:
      return { action: 'sync', label: 'Scan & sync changes' };
  }
}

// progress is the share of selected projects already created in the target.
export function progress(s) {
  const total = selectedTotal(s);
  return total ? s.pushed / total : 0;
}

// artifactLine summarises artifacts recovered from chats and how many reached
// the target. Artifacts from chats outside a project are exported locally only.
export function artifactLine(s) {
  if (!s.chats) return 'Not read yet. Pull reads your chats for artifacts.';
  if (!s.artifacts) return `No artifacts found in ${s.chats} chats.`;
  if (s.skip_artifacts) return offLine(`${s.artifacts} found in ${s.chats} chats.`, s.artifacts_sent);
  const parts = [`${s.artifacts_sent} sent`];
  if (s.artifacts_pending) parts.push(`${s.artifacts_pending} waiting`);
  if (s.artifacts_no_project) parts.push(`${s.artifacts_no_project} kept locally (chats outside a project)`);
  return `${s.artifacts} found in ${s.chats} chats. ${parts.join(', ')}.`;
}

// memoryLine describes the memory step: send memory.md to the target's
// memory import once, and again only when a later pull changed it.
export function memoryLine(s) {
  if (!s.pull_complete && !s.memory_pending && !s.memory_sent_at) return 'Pull first to read your memory.';
  if (s.skip_memory) return s.memory_sent_at ? `Sent on ${when(s.memory_sent_at)}. Changes are not copied. Turn it on in Settings.` : off;
  if (s.memory_pending && s.memory_sent_at) return `Changed since it was sent on ${when(s.memory_sent_at)}. Send again to update.`;
  if (s.memory_pending) return 'Ready to send. claude.ai merges it into the target account’s memory.';
  if (s.memory_sent_at) return `Sent on ${when(s.memory_sent_at)}. claude.ai adds it to memory in the background.`;
  return 'No memory to send.';
}


// lastSyncLine summarises the last "Scan & sync": what was new in the source
// and how much was sent.
export function lastSyncLine(s) {
  if (!s.last_sync_at) return '';
  const head = `Last synced ${when(s.last_sync_at)}.`;
  const found = [];
  if (s.last_sync_new_projects) found.push(plural(s.last_sync_new_projects, 'new project'));
  if (s.last_sync_changed_projects) found.push(`${s.last_sync_changed_projects} changed`);
  if (s.last_sync_chats) found.push(`${s.last_sync_chats} new or changed chats (${s.last_sync_artifacts} artifacts)`);
  if (!found.length && !s.last_sync_sent) return `${head} No changes found.`;
  return `${head} Found ${found.join(', ') || 'no new data'}. Sent ${plural(s.last_sync_sent, 'item')}.`;
}

export function selectionLine(s) {
  if (!s.projects) return 'Pull first to see your projects.';
  const line = `${s.selected} of ${s.projects} projects selected`;
  if (s.personal_skipped) return `${line}, ${s.personal_skipped} personal left out`;
  if (s.personal_projects && s.personal_choice === 'include') return `${line}, personal projects included`;
  return line;
}

// skillLine describes personal skills: only skills the user made or added are
// copied, never Anthropic's built-in skills.
export function skillLine(s) {
  if (!s.skills && !s.pull_complete) return 'Pull first. Only your own skills are copied, not the built-in ones.';
  if (!s.skills) return 'No personal skills found. Built-in skills are not copied.';
  const found = `${plural(s.skills, 'personal skill')} found.`;
  if (s.skip_skills) return offLine(found, s.skills_sent);
  if (s.skills_failed) return `${found} ${s.skills_sent} sent, ${s.skills_failed} failed. Retry sends ${s.skills_failed === 1 ? 'it' : 'them'} again.`;
  if (!s.skills_pending) return `${found} ${s.skills_sent ? 'All sent.' : 'None sent yet.'}`;
  return `${found} ${s.skills_sent} sent, ${s.skills_pending} waiting.`;
}

// chatLine describes chat transcripts: chats cannot be recreated, but each
// can be added to its project as a document when switched on in Settings.
export function chatLine(s) {
  const n = s.chats_in_projects || 0;
  if (!n) return 'No chats in the selected projects.';
  const found = `${plural(n, 'chat')} in the selected projects.`;
  if (s.chat_choice !== 'include') return offLine(found, s.chats_sent);
  if (s.chats_pending) return `${found} ${s.chats_sent} sent as documents, ${s.chats_pending} waiting.`;
  return `${found} All sent as documents.`;
}
