<script>
  import { onMount } from 'svelte';
  import * as Go from '../../wailsjs/go/app/App.js';

  // Where migrated content lives in the target account, shown as the path
  // someone clicks through in claude.ai, with real names from this migration.
  let { done } = $props();
  let g = $state(null);
  let error = $state('');

  onMount(async () => {
    try {
      g = await Go.Guide();
    } catch (e) {
      error = String(e);
    }
  });

  const fail = (e) => (error = String(e));
  const project = () => g.example?.name || 'your project';
  const trim = (name) => (name || '').replace(/\.md$/, '');
</script>

<section class="where" aria-labelledby="where-title">
  <button class="link back" onclick={done}>Back</button>
  <h2 id="where-title">Where to find everything{g?.target_name ? ` in ${g.target_name}` : ''}</h2>
  <p class="lead">
    Open claude.ai signed in to the account you moved to. Everything is where you would put it yourself.
  </p>

  {#if g}
    <dl>
      <div class="kind">
        <dt>Projects</dt>
        <dd>
          <span class="path"><span>Projects</span><span>{project()}</span></span>
          <span class="note">{g.projects} projects, with the same names as before.</span>
        </dd>
        <div class="act">
          {#if g.example}
            <button onclick={() => Go.OpenTargetProject(g.example.source_uuid).catch(fail)}>Open project</button>
          {:else}
            <button onclick={() => Go.OpenLink('projects').catch(fail)}>Open Projects</button>
          {/if}
        </div>
      </div>

      <div class="kind">
        <dt>Artifacts</dt>
        <dd>
          <span class="path"><span>{project()}</span><span>Project knowledge</span><span class="doc">{trim(g.example?.artifact) || 'Artifact - …'}</span></span>
          <span class="note">Each artifact is a document in its project's knowledge, at its final version.</span>
        </dd>
      </div>

      <div class="kind">
        <dt>Chats</dt>
        <dd>
          <span class="path"><span>{project()}</span><span>Project knowledge</span><span class="doc">{trim(g.example?.chat) || 'Chat - title (date)'}</span></span>
          <span class="note">If you chose to copy chats. Ask Claude about them in a new chat in the project.</span>
        </dd>
      </div>

      <div class="kind">
        <dt>Skills</dt>
        <dd>
          <span class="path"><span>Customize</span><span>Skills</span></span>
          <span class="note">{g.skills ? `${g.skills} of your own skills, with all their files.` : 'Your own skills, with all their files.'}</span>
        </dd>
      </div>

      <div class="kind">
        <dt>Memory</dt>
        <dd>
          <span class="path"><span>Settings</span><span>Memory</span></span>
          <span class="note">{g.memory_sent ? 'Sent. claude.ai merges it in the background, so it can take a few minutes to appear.' : 'Not sent yet.'}</span>
        </dd>
      </div>

      <div class="kind local">
        <dt>On this computer</dt>
        <dd>
          <span class="note">
            Artifacts from chats outside any project have no project to go to, so they stay here as readable files{g.local_only ? ` (${g.local_only})` : ''}.
          </span>
        </dd>
        <div class="act"><button onclick={() => Go.OpenArtifactsFolder().catch(fail)}>Open folder</button></div>
      </div>
    </dl>
    <p class="muted small">
      Links open in your default browser. If a project does not open, that browser is signed in to a different account:
      switch to {g.target_name || 'the target account'} in claude.ai and try again.
    </p>
  {/if}
  {#if error}<p class="error">{error}</p>{/if}
</section>

<style>
  .where { max-width: 680px; }
  .back { margin-bottom: 8px; }
  h2 { font-size: 20px; margin: 0 0 4px; }
  .lead { color: var(--slate); margin: 0 0 16px; }
  dl { margin: 0 0 12px; border-top: 1px solid var(--rule); }
  .kind { display: grid; grid-template-columns: 8.5rem 1fr auto; gap: 4px 14px; align-items: start; padding: 12px 0; border-bottom: 1px solid var(--rule); }
  dt { font-weight: 600; padding-top: 2px; }
  dd { margin: 0; display: flex; flex-direction: column; gap: 4px; min-width: 0; }
  .act { padding-top: 1px; }
  /* The path is the memorable part: the clicks you make in claude.ai. */
  .path { display: flex; flex-wrap: wrap; align-items: center; gap: 2px 0; font-size: 13px; color: var(--ink); }
  .path span:not(:last-child)::after { content: '›'; margin: 0 7px; color: var(--slate); }
  .path .doc { padding: 1px 7px; border-radius: 5px; background: color-mix(in srgb, var(--brand) 10%, transparent); color: var(--ink); overflow-wrap: anywhere; }
  .note { color: var(--slate); font-size: 13px; }
  .local dd { grid-column: 2; }
  @media (max-width: 560px) {
    .kind { grid-template-columns: 1fr; }
    .local dd { grid-column: auto; }
  }
</style>
