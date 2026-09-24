<script>
  import { onMount } from 'svelte';
  import { GetSelection, SaveSelection } from '../../wailsjs/go/app/App.js';
  import { countSelected, filterRows, selectionMap } from '../lib/format.js';
  let { done } = $props();
  let rows = $state([]);
  let query = $state('');
  let error = $state('');
  let saving = $state(false);

  onMount(async () => {
    try {
      rows = await GetSelection();
    } catch (e) {
      error = String(e);
    }
  });
  const visible = $derived(filterRows(rows, query));
  const setShown = (value) => visible.forEach((r) => (r.selected = value));

  async function save() {
    saving = true;
    error = '';
    try {
      await SaveSelection(selectionMap(rows));
      done();
    } catch (e) {
      error = String(e);
    } finally {
      saving = false;
    }
  }
</script>

<h2>Choose projects to migrate</h2>
<p class="muted">{countSelected(rows)} of {rows.length} selected. Projects starting with Personal:, Family: or Travel start unticked.</p>
<input placeholder="Search projects…" bind:value={query} />
<button onclick={() => setShown(true)}>Tick shown</button>
<button onclick={() => setShown(false)}>Untick shown</button>
<div class="list">
  {#each visible as r (r.uuid)}
    <label class="row">
      <input type="checkbox" bind:checked={r.selected} />
      <span class="name">{r.name}</span>
      <span class="meta">{r.docs} docs · {r.files} files</span>
    </label>
  {/each}
</div>
{#if error}<p class="error">{error}</p>{/if}
<button onclick={done}>Cancel</button>
<button class="primary" onclick={save} disabled={saving || countSelected(rows) === 0}>Save selection</button>
