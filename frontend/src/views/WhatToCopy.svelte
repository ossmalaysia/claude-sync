<script module>
  // Unsaved switches, kept while the user is away choosing projects so they
  // come back to what they had ticked. Cleared on Save and Cancel.
  let draft = null;
</script>

<script>
  import { onMount } from 'svelte';
  import { GetCopySettings, SaveCopySettings } from '../../wailsjs/go/app/App.js';

  // The "What to copy" page: shown once after the first download, and again
  // from Settings. Counts are what the local copy holds.
  let { done, onChooseProjects, cancellable = false } = $props();
  let settings = $state(null);
  let counts = $state(null);
  let error = $state('');
  let saving = $state(false);

  async function load() {
    error = '';
    try {
      const v = await GetCopySettings();
      counts = v.counts;
      settings = draft ?? { ...v.settings };
    } catch (e) {
      error = String(e);
    }
  }

  onMount(load);

  const num = (n) => n.toLocaleString('en-US');
  const plural = (n, word) => `${num(n)} ${word}${n === 1 ? '' : 's'}`;

  // Project names often contain colons, so each is quoted to keep the list readable.
  function names(list, max = 4) {
    const q = list.map((n) => `“${n.trim()}”`);
    if (q.length <= max) return q.join(', ');
    return `${q.slice(0, max).join(', ')} and ${q.length - max} more`;
  }

  function chooseProjects() {
    draft = { ...settings };
    onChooseProjects();
  }

  function cancel() {
    draft = null;
    done();
  }

  async function save() {
    saving = true;
    error = '';
    try {
      await SaveCopySettings({ ...settings });
      draft = null;
      done();
    } catch (e) {
      error = String(e);
    } finally {
      saving = false;
    }
  }
</script>

<section class="copy-page" aria-labelledby="copy-title">
  <h2 id="copy-title">What to copy</h2>
  <p class="lead">Choose what Claude Sync sends to the target account. You can change this later under Settings.</p>

  {#if counts && settings}
    <ul class="opts">
      <li>
        <span></span>
        <div class="text">
          <b>Projects</b>
          <span>{num(counts.selected)} of {plural(counts.projects, 'project')} selected. Projects are always copied; you choose which ones.</span>
        </div>
        <button onclick={chooseProjects}>Choose projects</button>
      </li>

      <li>
        <input id="opt-artifacts" type="checkbox" bind:checked={settings.artifacts} aria-describedby="opt-artifacts-d" />
        <label class="text" for="opt-artifacts">
          <b>Artifacts from chats</b>
          <span id="opt-artifacts-d">
            {#if counts.artifacts}{plural(counts.artifacts, 'artifact')} in chats inside your projects, each added to its project as a document.{:else}No artifacts found in chats inside your projects.{/if}
            {#if counts.artifacts_local}{num(counts.artifacts_local)} from chats outside a project stay on this computer.{/if}
          </span>
        </label>
      </li>

      <li>
        <input id="opt-chats" type="checkbox" bind:checked={settings.chats} aria-describedby="opt-chats-d" />
        <label class="text" for="opt-chats">
          <b>Chats as documents</b>
          <span id="opt-chats-d">
            {#if counts.chats}Each of the {plural(counts.chats, 'chat')} in your projects becomes a document named “Chat - title (date)” in its project, without Claude’s hidden reasoning.{:else}No chats found inside your projects.{/if}
            Off by default because it adds many documents to shared projects.
            {#if counts.chats_local}{plural(counts.chats_local, 'chat')} outside a project stay on this computer.{/if}
          </span>
        </label>
      </li>

      <li>
        <input id="opt-skills" type="checkbox" bind:checked={settings.skills} aria-describedby="opt-skills-d" />
        <label class="text" for="opt-skills">
          <b>Your own skills</b>
          <span id="opt-skills-d">
            {#if counts.skills}{plural(counts.skills, 'skill')} you made or added.{:else}No skills of your own found.{/if}
            Built-in skills are never copied.
          </span>
        </label>
      </li>

      <li>
        <input id="opt-memory" type="checkbox" bind:checked={settings.memory} aria-describedby="opt-memory-d" />
        <label class="text" for="opt-memory">
          <b>Memory</b>
          <span id="opt-memory-d">
            {#if counts.memory}What Claude remembers about you. claude.ai merges it into the target account’s memory.{:else}No memory found in the source account.{/if}
          </span>
        </label>
      </li>

      <li>
        <input id="opt-personal" type="checkbox" bind:checked={settings.personal} aria-describedby="opt-personal-d" />
        <label class="text" for="opt-personal">
          <b>Personal projects</b>
          <span id="opt-personal-d">
            {#if counts.personal_projects.length}
              {counts.personal_projects.length === 1 ? '1 project looks' : `${num(counts.personal_projects.length)} projects look`} personal (the name starts with Personal:, Family: or Travel): {names(counts.personal_projects)}.
              Leave them out when moving to a company or team account. Changing this switch ticks or unticks them in Choose projects.
            {:else}
              No project names start with Personal:, Family: or Travel.
            {/if}
          </span>
        </label>
      </li>
    </ul>

    <p class="note">Turning something off stops it being sent from now on. Nothing already in the target is removed.</p>
  {:else if !error}
    <p class="muted">Loading…</p>
  {/if}

  {#if error}<p class="error">{error}</p>{/if}
  <div class="buttons">
    {#if settings}
      <button class="primary" onclick={save} disabled={saving}>{saving ? 'Saving…' : 'Save'}</button>
    {:else if error}
      <button class="primary" onclick={load}>Try again</button>
    {/if}
    {#if cancellable}<button onclick={cancel} disabled={saving}>Cancel</button>{/if}
  </div>
</section>

<style>
  .copy-page { max-width: 640px; }
  h2 { font-size: 20px; margin: 4px 0 6px; }
  .lead { color: var(--slate); margin: 0 0 14px; }
  .opts { list-style: none; margin: 0; padding: 0; border-top: 1px solid var(--rule); }
  .opts li { display: grid; grid-template-columns: 18px 1fr auto; gap: 12px; align-items: start; padding: 11px 0; border-bottom: 1px solid var(--rule); }
  .opts input { margin: 3px 0 0; width: 16px; height: 16px; accent-color: var(--brand); cursor: pointer; }
  .text { display: flex; flex-direction: column; min-width: 0; grid-column: 2 / -1; }
  li:first-child .text { grid-column: 2; }
  label.text { cursor: pointer; }
  .text b { font-weight: 600; font-size: 14px; }
  .text span { font-size: 13px; color: var(--slate); }
  .note { font-size: 13px; color: var(--slate); margin: 12px 0 16px; }
  .buttons { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
  @media (max-width: 560px) {
    .opts li:first-child { grid-template-columns: 18px 1fr; }
    .opts li:first-child button { grid-column: 2; width: fit-content; }
  }
</style>
