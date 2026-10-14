<script>
  import { onDestroy, onMount } from 'svelte';
  import * as Go from '../wailsjs/go/app/App.js';
  import { ClipboardSetText, EventsOn } from '../wailsjs/runtime/runtime.js';
  import Select from './views/Select.svelte';
  import Notice from './views/Notice.svelte';
  import WhatToCopy from './views/WhatToCopy.svelte';
  import Where from './views/Where.svelte';
  import logo from './assets/logo.png';
  import { pullProgressText } from './lib/format.js';
  import { artifactLine, chatLine, lastSyncLine, memoryLine, nextButton, selectionLine, skillLine, progress, pullLine, pushLine, verifyLine } from './lib/status.js';

  // Everything shown comes from Status(), which the backend rebuilds from the
  // files on disk every poll. Nothing here depends on what was clicked before.
  let status = $state(null);
  let view = $state('home'); // 'home' | 'select' | 'where' | 'copy'
  let selectBack = 'home'; // where Choose projects returns to
  let busy = $state(''); // action started from this window
  let error = $state('');
  let notice = $state('');
  let live = $state(''); // latest progress line of a job running in this app
  let log = $state([]);
  let orgs = $state({ source: null, target: null }); // org lists after a connect
  let verifyRows = $state([]);
  let memory = $state('');
  let showMemory = $state(false);
  let copied = $state(false);
  let directions = $state([]);
  let preview = $state(null);
  let pendingSend = $state('');
  let update = $state(null);
  let checkingUpdate = $state(false);
  let updateMessage = $state('');
  let dismissedUpdate = $state('');

  async function refresh() {
    try {
      status = await Go.Status();
      directions = await Go.ListDirections();
    } catch (e) {
      error = String(e);
    }
  }

  let version = $state('');

  async function checkUpdates(manual = false) {
    if (checkingUpdate) return;
    checkingUpdate = true;
    if (manual) { updateMessage = ''; dismissedUpdate = ''; }
    try {
      update = await Go.CheckForUpdates(manual);
      if (manual) updateMessage = {
        available: `${update.latest_version} is available.`,
        current: `You’re up to date (${version || update.current_version}).`,
        development: `Development build. Latest published version: ${update.latest_version}.`,
        unavailable: 'Couldn’t check for updates. Check your connection and try again.',
      }[update.status] || '';
    } catch {
      if (manual) updateMessage = 'Couldn’t check for updates. Try again.';
    } finally { checkingUpdate = false; }
  }

  function openLink(name) {
    Go.OpenLink(name).catch((e) => (error = String(e)));
  }

  onMount(() => {
    refresh();
    Go.Version().then((v) => (version = v)).catch(() => {});
    checkUpdates();
    const timer = setInterval(refresh, 3000);
    const updateTimer = setInterval(() => checkUpdates(), 6 * 60 * 60 * 1000);
    return () => { clearInterval(timer); clearInterval(updateTimer); };
  });

  const off = EventsOn('progress', (e) => {
    if (e.level !== 'info') log = [...log.slice(-200), e.message];
    else if (e.stage === 'pull') live = pullProgressText(e);
    else if (e.stage === 'verify') live = e.done < e.total ? `Checking ${e.done + 1} of ${e.total}: ${e.message}` : '';
    else live = `${e.done} of ${e.total} projects, now ${e.message}`;
  });
  onDestroy(off);

  async function run(name, fn) {
    busy = name;
    error = '';
    notice = '';
    try {
      await fn();
    } catch (e) {
      error = String(e);
    } finally {
      busy = '';
      live = '';
      await refresh();
    }
  }

  function outcome(what, out) {
    if (out.status === 'paused') notice = `${what} stopped. Everything done so far is saved. Press Resume to continue.`;
    else if (out.status === 'needs_login')
      notice = `The ${what === 'Pull' ? 'source' : 'target'} login expired or its browser window was closed. Press Resume and log in to the window that opens.`;
    else notice = `${what} finished.${out.result?.conflicts?.length ? ` ${out.result.conflicts.length} conflicts need review; those items were skipped.` : ''}`;
  }

  const actions = {
    preview: () => run('preview', async () => { preview = await Go.Preview(); }),
    reverse: () => run('reverse', async () => {
      await Go.ReverseDirection();
      clearDirectionView();
      notice = 'Direction reversed. Download the source, review what to copy, then preview the target.';
    }),
    connect_source: () => run('connect_source', async () => (orgs.source = await Go.ConnectAccount('source'))),
    connect_target: () => run('connect_target', async () => (orgs.target = await Go.ConnectAccount('target'))),
    pull: () => run('pull', async () => { preview = null; pendingSend = ''; outcome('Pull', await Go.Pull('')); }),
    push: () => prepareSend('push'),
    push_confirmed: () => run('push', async () => { outcome('Push', await Go.Push('')); preview = null; pendingSend = ''; }),
    verify: () =>
      run('verify', async () => {
        verifyRows = await Go.Verify('');
        // Bring the results into view; they sit below the step list.
        setTimeout(() => document.getElementById('verify-results')?.scrollIntoView({ behavior: 'smooth' }), 50);
      }),
    select: () => {
      selectBack = 'home';
      view = 'select';
    },
    sync: () => prepareSend('sync'),
    sync_confirmed: () =>
      run('sync', async () => {
        const out = await Go.SyncChanges();
        if (out.push?.conflicts?.length) log = [...log, ...out.push.conflicts];
        const where = { pull: 'while scanning the source', push: 'while sending', memory: 'while sending memory' }[out.stage] || '';
        if (out.status === 'completed') notice = 'Sync finished. ' + (lastSyncLine(await Go.Status()) || '');
        else if (out.status === 'paused') notice = `Sync stopped ${where}. Everything done so far is saved; press Scan & sync to continue.`;
        else notice = `A login expired or a browser window was closed ${where}. Press Scan & sync and log in to the window that opens.`;
        preview = null; pendingSend = '';
      }),
    memory: () =>
      run('memory', async () => {
        const out = await Go.SyncMemory();
        notice = {
          sent: 'Memory sent. claude.ai is adding it to the target account’s memory; it appears there within a few minutes.',
          unchanged: 'Memory was already sent and has not changed.',
          empty: 'There is no memory to send. Pull first.',
          skipped: 'Memory is switched off in Settings, so it was not sent.',
          needs_login: 'The target login expired or its window was closed. Press Send again and log in to the window that opens.',
        }[out.status];
      }),
  };

  async function chooseOrg(account, uuid) {
    const o = orgs[account].orgs.find((x) => x.uuid === uuid);
    await Go.SetOrg(account, o.uuid, o.name);
    clearDirectionView();
    orgs[account] = null;
    await refresh();
  }

  function clearDirectionView() {
    preview = null; pendingSend = ''; verifyRows = []; memory = ''; showMemory = false; log = []; orgs = { source: null, target: null };
  }
  function prepareSend(kind) {
    return run('preview', async () => {
      preview = null; pendingSend = '';
      if (kind === 'sync') {
        const out = await Go.Pull('');
        if (out.status !== 'completed') { outcome('Pull', out); return; }
      }
      preview = await Go.Preview(); pendingSend = kind;
      notice = 'Review the preview, then send the missing items. Conflicts are skipped.';
    });
  }
  function selectDirection(value) {
    const d = directions.find((d) => `${d.source_org}|${d.target_org}` === value);
    if (d) run('direction', async () => { await Go.SelectDirection(d.source_org, d.target_org); clearDirectionView(); });
  }

  // Opens the local artifacts-export folder in Finder or Explorer.
  const openExport = () => Go.OpenArtifactsFolder().catch((e) => (error = String(e)));

  async function toggleMemory() {
    showMemory = !showMemory;
    copied = false;
    if (showMemory) memory = await Go.MemoryText();
  }

  // The "What to copy" page, opened from Settings or from a row's Change link.
  const openSettings = () => { preview = null; pendingSend = ''; view = 'copy'; };
  function chooseFromSettings() {
    selectBack = 'copy';
    view = 'select';
  }

  const btn = $derived(status ? nextButton(status) : null);
  const locked = $derived(!!status?.job || !!busy);
  const orgLabel = (a) => a.org_name || (a.org ? `Org ${a.org.slice(0, 8)}` : 'Not connected');
  const bad = $derived(verifyRows.filter((r) => !r.ok));
  const pct = $derived(status ? Math.round(progress(status) * 100) : 0);

  // State of each checklist row: 'done' | 'active' | 'todo'.
  const rowState = $derived.by(() => {
    if (!status) return {};
    const s = status;
    const pushDone = !s.conflicts?.length && s.pushed > 0 && s.pending_projects + s.pending_items + s.failed - (s.skills_failed || 0) === 0;
    return {
      pull: s.pull_complete ? 'done' : s.pull_done || s.job === 'pull' ? 'active' : 'todo',
      select: s.selected > 0 ? 'done' : 'todo',
      push: pushDone ? 'done' : s.pushed || s.job === 'push' ? 'active' : 'todo',
      verify: s.verified_at && !s.verify_stale && s.verify_ok === s.verify_total ? 'done' : 'todo',
      artifacts: !s.artifacts || s.skip_artifacts ? 'todo' : s.artifacts_pending ? (s.artifacts_sent ? 'active' : 'todo') : 'done',
    };
  });
</script>

<div class="shell">
  <header class="masthead">
    <img class="logo" src={logo} alt="" />
    <div>
      <h1>Claude Sync</h1>
      <p>Move Projects from one claude.ai account to another</p>
    </div>
    {#if status?.terms_accepted && status.pull_complete}
      <button class="settings" onclick={openSettings} aria-pressed={view === 'copy'}>Settings</button>
    {/if}
  </header>

  {#if update?.status === 'available' && dismissedUpdate !== update.latest_version}
    <div class="update-banner" role="status">
      <div><strong>Claude Sync {update.latest_version} is available</strong><p>Finish any sync before installing. Your saved logins and transfer history stay on this computer.</p></div>
      <button class="primary" onclick={() => Go.OpenUpdate().catch(() => { updateMessage = 'Couldn’t open the release page. Try again.'; })}>View update</button>
      <button class="link" onclick={() => { dismissedUpdate = update.latest_version; }}>Later</button>
    </div>
  {/if}

  <main>
    {#if status && !status.terms_accepted}
      <Notice done={refresh} />
    {:else if view === 'where'}
      <Where done={() => { view = 'home'; refresh(); }} />
    {:else if view === 'select'}
      <Select done={() => { view = selectBack; refresh(); }} />
    {:else if status && (view === 'copy' || (status.pull_complete && !status.copy_reviewed))}
      <WhatToCopy
        cancellable={status.copy_reviewed}
        onChooseProjects={chooseFromSettings}
        done={() => { view = 'home'; refresh(); }}
      />
    {:else if status}
      <div class="direction-actions">
        <label>Saved direction
          <select aria-label="Saved direction" disabled={locked || !directions.length} value={`${status.source.org}|${status.target.org}`} onchange={(e) => selectDirection(e.currentTarget.value)}>
            {#each directions as d}
              <option value={`${d.source_org}|${d.target_org}`}>{d.source_name || d.source_org} → {d.target_name || d.target_org}</option>
            {/each}
          </select>
        </label>
        <button onclick={actions.reverse} disabled={locked || !status.source.org || !status.target.org}>Reverse direction</button>
      </div>
      <section class="transfer" aria-label="Accounts">
        <div class="account">
          <span class="role">From</span>
          <strong title={orgLabel(status.source)}>{orgLabel(status.source)}</strong>
          <span class="login">{status.source.saved_login ? 'Signed in before' : 'Not signed in yet'}</span>
          <button class="link" onclick={actions.connect_source} disabled={locked}>{status.source.org ? 'Change' : 'Connect'}</button>
          {#if orgs.source}
            <select value={status.source.org} onchange={(e) => chooseOrg('source', e.currentTarget.value)}>
              {#each orgs.source.orgs as o}<option value={o.uuid}>{o.name}</option>{/each}
            </select>
          {/if}
        </div>

        <div class="rail" role="progressbar" aria-valuemin="0" aria-valuemax="100" aria-valuenow={pct} aria-label="Projects created in the target">
          <div class="track"><div class="fill" class:running={status.job === 'push'} style="width: {pct}%"></div></div>
          <span>{status.pushed} of {status.pushed + status.pending_projects} projects</span>
        </div>

        <div class="account">
          <span class="role">To</span>
          <strong title={orgLabel(status.target)}>{orgLabel(status.target)}</strong>
          <span class="login">{status.target.saved_login ? 'Signed in before' : 'Not signed in yet'}</span>
          <button class="link" onclick={actions.connect_target} disabled={locked}>{status.target.org ? 'Change' : 'Connect'}</button>
          {#if orgs.target}
            <select value={status.target.org} onchange={(e) => chooseOrg('target', e.currentTarget.value)}>
              {#each orgs.target.orgs as o}<option value={o.uuid}>{o.name}</option>{/each}
            </select>
          {/if}
        </div>
      </section>

      <div class="next">
        {#if status.job_here}
          <button class="primary" disabled>{btn.label}</button>
          <button onclick={() => Go.StopJob()}>Stop</button>
        {:else if status.job}
          <button class="primary" disabled>{btn.label}</button>
          <span class="muted">Running from the command line. This screen updates every few seconds.</span>
        {:else if btn.action}
          <button class="primary" onclick={actions[btn.action]} disabled={!!busy}>{busy ? 'Working…' : btn.label}</button>
          {#if btn.action !== 'sync' && status.pull_complete && status.target.org}
            <button onclick={actions.sync} disabled={!!busy}>Scan & sync changes</button>
          {/if}
        {:else}
          <span class="verified">{btn.label}</span>
        {/if}
      </div>
      {#if lastSyncLine(status) && !status.job}<p class="muted small">{lastSyncLine(status)}</p>{/if}
      {#if status.job_here}<p class="muted small">Stopping is safe. Everything done so far is saved and Resume continues from there.</p>{/if}
      {#if live}<p class="muted small">{live}</p>{/if}
      {#if error}<p class="error">{error}</p>{/if}
      {#if status.error}<p class="error">{status.error}</p>{/if}
      {#if status.pull_complete && status.target.org}
        <div class="panel">
          <button onclick={actions.preview} disabled={locked}>Preview changes</button>
          <p class="muted small">Checks target content before copying. Identical content is skipped; different versions stay for review.</p>
          {#if preview}
            <p>{preview.new_projects} new project{preview.new_projects === 1 ? '' : 's'} · {preview.new_docs + preview.new_files + preview.new_artifacts + preview.new_chats} new documents/files · {preview.new_skills || 0} new skills · {preview.already_copied} already copied · {preview.conflicts?.length || 0} conflict{preview.conflicts?.length === 1 ? '' : 's'}</p>
            {#if preview.conflicts?.length}
              <p class="error">These items will be skipped. Review the source and target, then preview again after resolving the differences.</p>
              <ul>{#each preview.conflicts as conflict}<li>{conflict}</li>{/each}</ul>
            {/if}
            {#each preview.project_links || [] as link}
              {#if !link.target && preview.target_projects?.length}
                <label class="mapping">Link “{link.name}” to an existing target project
                  <select aria-label={`Target project for ${link.name}`} disabled={locked} onchange={(e) => {
                    const target = e.currentTarget.value;
                    if (target) run('mapping', async () => { await Go.MapProject(link.source, target); preview = await Go.Preview(); });
                  }}>
                    <option value="">Choose a project, or leave unmatched to create one</option>
                    {#each preview.target_projects as target}<option value={target.uuid}>{target.name} ({target.uuid.slice(0, 8)})</option>{/each}
                  </select>
                </label>
              {/if}
            {/each}
            {#if pendingSend}
              <button class="primary" disabled={locked || !status.copy_reviewed} onclick={pendingSend === 'sync' ? actions.sync_confirmed : actions.push_confirmed}>Send missing items</button>
            {/if}
          {/if}
          {#if status.conflicts?.length && !preview}
            <p class="error">{status.conflicts.length} conflicts need review.</p>
            <ul>{#each status.conflicts as conflict}<li>{conflict}</li>{/each}</ul>
          {/if}
        </div>
      {/if}
      {#if notice}<p class="notice">{notice}</p>{/if}

      <ol class="steps">
        <li class={rowState.pull}>
          <span class="mark" aria-hidden="true"></span>
          <div>
            <b>Download from source</b><span>{pullLine(status)}</span>
            {#if status.job === 'pull' && status.phase_total}
              <progress class="bar" max={status.phase_total} value={status.phase_done}></progress>
            {/if}
          </div>
          <button onclick={actions.pull} disabled={locked || !status.source.org}>{status.pull_complete ? 'Pull again' : status.pull_done ? 'Resume' : 'Start'}</button>
        </li>
        <li class={rowState.select}>
          <span class="mark" aria-hidden="true"></span>
          <div>
            <b>Choose projects</b><span>{selectionLine(status)}</span>
            {#if status.personal_skipped}<button class="link" onclick={openSettings}>Change</button>{/if}
          </div>
          <button onclick={actions.select} disabled={locked || !status.projects}>Edit</button>
        </li>
        <li class={rowState.push}>
          <span class="mark" aria-hidden="true"></span>
          <div>
            <b>Send to target</b><span>{pushLine(status)}</span>
            {#if status.pushed}<button class="link" onclick={() => (view = 'where')}>Where to find everything in {status.target.org_name || 'the target'}</button>{/if}
            {#if status.skipped}<span class="muted">{status.skipped} files over 30 MB are skipped.</span>{/if}
          </div>
          <button onclick={actions.push} disabled={locked || !status.target.org || !status.pull_complete || rowState.push === 'done'}>{rowState.push === 'done' ? 'Sent' : status.failed && !status.pending_projects && !status.pending_items ? 'Retry' : status.pushed ? 'Resume' : 'Start'}</button>
        </li>
        <li class={rowState.artifacts}>
          <span class="mark" aria-hidden="true"></span>
          <div>
            <b>Artifacts from chats</b><span>{artifactLine(status)}</span>
            {#if status.skip_artifacts}<button class="link" onclick={openSettings}>Change</button>{/if}
          </div>
          <button onclick={openExport} disabled={!status.artifacts}>Open folder</button>
        </li>
        <li class={!status.chats_in_projects || status.chat_choice !== 'include' ? 'todo' : !status.chats_pending ? 'done' : 'active'}>
          <span class="mark" aria-hidden="true"></span>
          <div>
            <b>Chats as documents</b><span>{chatLine(status)}</span>
            {#if status.chat_choice !== 'include' && status.chats_in_projects}<button class="link" onclick={openSettings}>Change</button>{/if}
          </div>
        </li>
        <li class={!status.skills || status.skip_skills ? 'todo' : status.skills_pending || status.skills_failed ? 'active' : 'done'}>
          <span class="mark" aria-hidden="true"></span>
          <div>
            <b>Skills</b><span>{skillLine(status)}</span>
            {#if status.skip_skills}<button class="link" onclick={openSettings}>Change</button>{/if}
          </div>
          <span></span>
        </li>
        <li class={rowState.verify}>
          <span class="mark" aria-hidden="true"></span>
          <div><b>Check the target</b><span>{verifyLine(status)}</span></div>
          <button onclick={actions.verify} disabled={locked || !status.pushed}>Verify</button>
        </li>
        <li class={status.memory_sent_at && !status.memory_pending && !status.skip_memory ? 'done' : 'todo'}>
          <span class="mark" aria-hidden="true"></span>
          <div>
            <b>Memory</b><span>{memoryLine(status)}</span>
            {#if status.skip_memory}<button class="link" onclick={openSettings}>Change</button>{/if}
          </div>
          <div class="actions">
            <button onclick={toggleMemory} disabled={!status.pull_complete && !status.memory_sent_at}>{showMemory ? 'Hide' : 'Show'}</button>
            <button onclick={actions.memory} disabled={locked || status.skip_memory || !status.memory_pending || !status.target.org}>{status.memory_sent_at && status.memory_pending ? 'Send again' : 'Send'}</button>
          </div>
        </li>
      </ol>

      {#if showMemory}
        <div class="panel">
          <textarea readonly>{memory}</textarea>
          <button onclick={async () => (copied = await ClipboardSetText(memory))}>{copied ? 'Copied' : 'Copy memory'}</button>
        </div>
      {/if}

      {#if verifyRows.length}
        <div class="panel" id="verify-results">
          <p><b>{verifyLine(status)}</b></p>
          {#if bad.length}
            <table>
              <thead><tr><th>Project</th><th>Docs</th><th>Files</th><th>Reason</th></tr></thead>
              <tbody>
                {#each bad as r}
                  <tr><td>{r.name}</td><td>{r.got_docs} of {r.want_docs}</td><td>{r.got_files} of {r.want_files}</td><td class={r.waiting ? 'muted' : 'error'}>{r.error}</td></tr>
                {/each}
              </tbody>
            </table>
          {/if}
        </div>
      {/if}

      {#if log.length}<div class="log">{#each log as l}<div>{l}</div>{/each}</div>{/if}
    {:else}
      <p class="muted">Loading…</p>
    {/if}
  </main>

  <footer>
    Developed by <button class="link" onclick={() => openLink('website')}>Anchor Sprint</button>
    <span class="sep" aria-hidden="true"></span>
    <button class="link" onclick={() => openLink('issues')}>Report an issue</button>
    {#if version}<span class="sep" aria-hidden="true"></span><span class="version" title="Include this when you report an issue">{version}</span>{/if}
    <span class="sep" aria-hidden="true"></span>
    <button class="link" disabled={checkingUpdate} onclick={() => checkUpdates(true)}>{checkingUpdate ? 'Checking for updates…' : 'Check for updates'}</button>
    {#if updateMessage}<span role="status">{updateMessage}</span>{/if}
  </footer>
</div>

<style>
  .update-banner { display: flex; flex-wrap: wrap; align-items: center; gap: 12px; margin: 12px 24px 0; padding: 14px 18px; border: 1px solid var(--rule); border-radius: 12px; background: color-mix(in srgb, var(--brand) 12%, transparent); }
  .update-banner div { flex: 1; min-width: 200px; }
  .update-banner p { margin: 4px 0 0; font-size: 13px; color: var(--slate); }
  .direction-actions { display: flex; flex-wrap: wrap; align-items: end; gap: 12px; margin-bottom: 16px; }
  .direction-actions label { display: flex; flex-direction: column; gap: 5px; flex: 1; min-width: 220px; font-size: 13px; color: var(--slate); }
  .direction-actions select { width: 100%; }
  .mapping { display: flex; flex-direction: column; gap: 6px; margin-top: 12px; }
</style>
