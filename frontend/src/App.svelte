<script>
  import { onDestroy, onMount } from 'svelte';
  import * as Go from '../wailsjs/go/app/App.js';
  import { BrowserOpenURL, ClipboardSetText, EventsOn } from '../wailsjs/runtime/runtime.js';
  import Select from './views/Select.svelte';
  import logo from './assets/logo.png';
  import { pullProgressText } from './lib/format.js';
  import { nextButton, progress, pullLine, pushLine, verifyLine } from './lib/status.js';

  // Everything shown comes from Status(), which the backend rebuilds from the
  // files on disk every poll. Nothing here depends on what was clicked before.
  let status = $state(null);
  let view = $state('home'); // 'home' | 'select'
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

  async function refresh() {
    try {
      status = await Go.Status();
    } catch (e) {
      error = String(e);
    }
  }

  onMount(() => {
    refresh();
    const timer = setInterval(refresh, 3000);
    return () => clearInterval(timer);
  });

  const off = EventsOn('progress', (e) => {
    if (e.level === 'info') live = e.stage === 'pull' ? pullProgressText(e) : `${e.done} of ${e.total} projects, now ${e.message}`;
    else log = [...log.slice(-200), e.message];
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
    else notice = `${what} finished.`;
  }

  const actions = {
    connect_source: () => run('connect_source', async () => (orgs.source = await Go.ConnectAccount('source'))),
    connect_target: () => run('connect_target', async () => (orgs.target = await Go.ConnectAccount('target'))),
    pull: () => run('pull', async () => outcome('Pull', await Go.Pull(''))),
    push: () => run('push', async () => outcome('Push', await Go.Push(''))),
    verify: () => run('verify', async () => (verifyRows = await Go.Verify(''))),
    select: () => (view = 'select'),
  };

  async function chooseOrg(account, uuid) {
    const o = orgs[account].orgs.find((x) => x.uuid === uuid);
    await Go.SetOrg(account, o.uuid, o.name);
    orgs[account] = null;
    await refresh();
  }

  async function toggleMemory() {
    showMemory = !showMemory;
    copied = false;
    if (showMemory) memory = await Go.MemoryText();
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
    const pushDone = s.pushed > 0 && s.pending_projects + s.pending_items + s.failed === 0;
    return {
      pull: s.pull_complete ? 'done' : s.pull_done || s.job === 'pull' ? 'active' : 'todo',
      select: s.selected > 0 ? 'done' : 'todo',
      push: pushDone ? 'done' : s.pushed || s.job === 'push' ? 'active' : 'todo',
      verify: s.verified_at && !s.verify_stale && s.verify_ok === s.verify_total ? 'done' : 'todo',
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
  </header>

  <main>
    {#if view === 'select'}
      <Select done={() => { view = 'home'; refresh(); }} />
    {:else if status}
      <section class="transfer" aria-label="Accounts">
        <div class="account">
          <span class="role">From</span>
          <strong title={orgLabel(status.source)}>{orgLabel(status.source)}</strong>
          <span class="login">{status.source.saved_login ? 'Saved login' : 'Not logged in'}</span>
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
          <span class="login">{status.target.saved_login ? 'Saved login' : 'Not logged in'}</span>
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
        {:else}
          <span class="verified">{btn.label}</span>
        {/if}
      </div>
      {#if status.job_here}<p class="muted small">Stopping is safe. Everything done so far is saved and Resume continues from there.</p>{/if}
      {#if live}<p class="muted small">{live}</p>{/if}
      {#if error}<p class="error">{error}</p>{/if}
      {#if status.error}<p class="error">{status.error}</p>{/if}
      {#if notice}<p class="notice">{notice}</p>{/if}

      <ol class="steps">
        <li class={rowState.pull}>
          <span class="mark" aria-hidden="true"></span>
          <div><b>Download from source</b><span>{pullLine(status)}</span></div>
          <button onclick={actions.pull} disabled={locked || !status.source.org}>{status.pull_complete ? 'Pull again' : status.pull_done ? 'Resume' : 'Start'}</button>
        </li>
        <li class={rowState.select}>
          <span class="mark" aria-hidden="true"></span>
          <div><b>Choose projects</b><span>{status.selected} of {status.projects} projects selected</span></div>
          <button onclick={actions.select} disabled={locked || !status.projects}>Edit</button>
        </li>
        <li class={rowState.push}>
          <span class="mark" aria-hidden="true"></span>
          <div>
            <b>Send to target</b><span>{pushLine(status)}</span>
            {#if status.skipped}<span class="muted">{status.skipped} files over 30 MB are skipped.</span>{/if}
          </div>
          <button onclick={actions.push} disabled={locked || !status.target.org || !status.pull_complete || rowState.push === 'done'}>{rowState.push === 'done' ? 'Sent' : status.failed && !status.pending_projects && !status.pending_items ? 'Retry' : status.pushed ? 'Resume' : 'Start'}</button>
        </li>
        <li class={rowState.verify}>
          <span class="mark" aria-hidden="true"></span>
          <div><b>Check the target</b><span>{verifyLine(status)}</span></div>
          <button onclick={actions.verify} disabled={locked || !status.pushed}>Verify</button>
        </li>
        <li class="todo">
          <span class="mark" aria-hidden="true"></span>
          <div><b>Copy memory</b><span>Paste it into the target account under Settings, Memory.</span></div>
          <button onclick={toggleMemory} disabled={!status.pull_complete}>{showMemory ? 'Hide' : 'Show'}</button>
        </li>
      </ol>

      {#if showMemory}
        <div class="panel">
          <textarea readonly>{memory}</textarea>
          <button onclick={async () => (copied = await ClipboardSetText(memory))}>{copied ? 'Copied' : 'Copy memory'}</button>
        </div>
      {/if}

      {#if verifyRows.length}
        <div class="panel">
          <p><b>{verifyRows.length - bad.length} of {verifyRows.length} projects match the target.</b></p>
          {#if bad.length}
            <table>
              <thead><tr><th>Project</th><th>Docs</th><th>Files</th><th>Problem</th></tr></thead>
              <tbody>
                {#each bad as r}
                  <tr><td>{r.name}</td><td>{r.got_docs} of {r.want_docs}</td><td>{r.got_files} of {r.want_files}</td><td class="error">{r.error}</td></tr>
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
    Developed by <button class="link" onclick={() => BrowserOpenURL('https://www.anchorsprint.com/')}>Anchor Sprint</button>
  </footer>
</div>
