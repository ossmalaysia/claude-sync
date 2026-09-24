// Wording for the home screen, derived only from the backend Status (which is
// rebuilt from the files on disk), so it is right after a restart and when
// the CLI did the work.

function when(iso) {
  if (!iso) return '';
  const d = new Date(iso);
  return d.toLocaleString(undefined, { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' });
}

export function pullLine(s) {
  if (s.job === 'pull') return `Downloading… ${s.pull_done} of ${s.pull_total} projects`;
  if (!s.pull_total && !s.pull_done) return 'Not pulled yet';
  if (!s.pull_complete) return `${s.pull_done} of ${s.pull_total} projects downloaded. Stopped, you can resume.`;
  return `${s.pull_done} projects downloaded on ${when(s.pulled_at)}`;
}

function selectedTotal(s) {
  return s.pushed + s.pending_projects;
}

export function pushLine(s) {
  const total = selectedTotal(s);
  if (s.job === 'push') return `Sending… ${s.pushed} of ${total} projects created`;
  if (s.pushed === 0) return `Not started. ${total} projects to create.`;
  const head = `${s.pushed} of ${total} projects created`;
  if (s.pending_projects + s.pending_items > 0) return `${head}. ${s.pending_projects} projects and ${s.pending_items} items left.`;
  if (s.failed > 0) return `${head}. ${s.failed} failed items to retry.`;
  return `${head}. Everything is sent.`;
}

export function verifyLine(s) {
  if (!s.verified_at) return 'Not verified yet';
  if (s.verify_stale) return `${s.verify_ok} of ${s.verify_total} matched before the last push. Verify again.`;
  return `${s.verify_ok} of ${s.verify_total} projects match the target.`;
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
    default:
      return { action: '', label: 'All done' };
  }
}

// progress is the share of selected projects already created in the target.
export function progress(s) {
  const total = selectedTotal(s);
  return total ? s.pushed / total : 0;
}
