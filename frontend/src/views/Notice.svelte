<script>
  import * as Go from '../../wailsjs/go/app/App.js';

  let { done } = $props();
  let agreed = $state(false);
  let error = $state('');

  async function accept() {
    try {
      await Go.AcceptTerms();
      done();
    } catch (e) {
      error = String(e);
    }
  }
</script>

<section class="notice-page" aria-labelledby="notice-title">
  <h2 id="notice-title">Before you start</h2>
  <p class="lead">
    Claude Sync helps you move your work from an old claude.ai account to a new one. Please read this once.
  </p>

  <dl>
    <dt>What it does</dt>
    <dd>
      It reads the account you move <b>from</b> and adds Projects, documents, files, artifacts, skills and memory to
      the account you move <b>to</b>. It only adds: it never edits or deletes anything already in either account.
    </dd>

    <dt>Your responsibility</dt>
    <dd>
      You choose what is copied and where. Make sure you are allowed to copy this content into the target account
      (for example, your employer's rules for a company account), and that the target is the right account. In a
      team account, copied Projects follow that account's sharing settings.
    </dd>

    <dt>Your data</dt>
    <dd>
      Everything stays on this computer. Claude Sync talks only to claude.ai, through a browser window where you sign
      in yourself. It never sees your password.
    </dd>

    <dt>Not an Anthropic product</dt>
    <dd>
      Claude Sync is an independent open-source tool, provided as is, without warranty. It uses claude.ai's web
      pages, so a claude.ai change can break it.
    </dd>
  </dl>

  <label class="agree">
    <input type="checkbox" bind:checked={agreed} />
    <span>I understand, and I am responsible for what Claude Sync copies into my target account.</span>
  </label>
  {#if error}<p class="error">{error}</p>{/if}
  <button class="primary" disabled={!agreed} onclick={accept}>Continue</button>
</section>

<style>
  .notice-page { max-width: 620px; }
  h2 { font-size: 20px; margin: 4px 0 6px; }
  .lead { color: var(--slate); margin: 0 0 18px; }
  dl { margin: 0 0 20px; border-top: 1px solid var(--rule); }
  dt { font-weight: 600; margin-top: 14px; }
  dd { margin: 3px 0 0; color: var(--slate); }
  .agree { display: flex; gap: 10px; align-items: flex-start; margin: 0 0 16px; padding: 12px 14px; border: 1px solid var(--rule); border-radius: 8px; background: var(--surface); cursor: pointer; }
  .agree input { margin-top: 3px; accent-color: var(--brand); }
</style>
