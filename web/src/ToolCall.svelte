<script>
  import { createEventDispatcher } from "svelte";

  export let entry;
  export let open = false;
  export let details = null;

  const dispatch = createEventDispatcher();
</script>

<div class="tool">
  <button class="head" on:click={() => dispatch("toggle", entry.toolId)}>
    <span class="chevron">{open ? "▾" : "▸"}</span>
    {#if entry.toolName && entry.toolName.toLowerCase() !== (entry.text || "").toLowerCase()}
      <span class="tool-kind">{entry.toolName}</span>
    {/if}
    <span class="title">{entry.text || entry.toolName || "tool"}</span>
    {#if entry.status}
      <span class="status {entry.status}">{entry.status}</span>
    {/if}
  </button>
  {#if open}
    <div class="body">
      {#if entry.todos?.length}
        <ul class="todos">
          {#each entry.todos as todo}
            <li class={todo.status}>{todo.content}</li>
          {/each}
        </ul>
      {/if}
      {#if details}
        {#if details.input}
          <div class="label">Input</div>
          <pre>{details.input}</pre>
        {/if}
        {#if details.output}
          <div class="label">Output</div>
          <pre>{details.output}</pre>
        {/if}
        {#if details.truncated}
          <p class="truncated">Output truncated.</p>
        {/if}
      {:else}
        <p class="loading">Loading…</p>
      {/if}
    </div>
  {/if}
</div>

<style>
  .tool {
    border: 1px solid var(--border);
    border-radius: 12px;
    background: var(--surface);
    overflow: hidden;
    box-shadow: var(--shadow-sm);
  }
  .head {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    border: 0;
    border-radius: 0;
    background: transparent;
    text-align: left;
    padding: 10px 12px;
    font-size: 0.85rem;
  }
  .chevron {
    color: var(--muted);
  }
  .title {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .tool-kind {
    font-size: 0.68rem;
    color: var(--muted);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .status {
    font-size: 0.68rem;
    color: var(--muted);
    text-transform: uppercase;
    letter-spacing: 0.04em;
    font-weight: 600;
  }
  .status.completed {
    color: var(--success);
  }
  .status.failed {
    color: var(--danger);
  }
  .status.running {
    color: var(--warning);
  }
  .body {
    border-top: 1px solid var(--border);
    padding: 10px 12px;
  }
  .label {
    font-size: 0.68rem;
    color: var(--muted);
    text-transform: uppercase;
    letter-spacing: 0.04em;
    font-weight: 600;
    margin: 8px 0 4px;
  }
  .body pre {
    margin: 0;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    font-size: 0.8rem;
    line-height: 1.4;
  }
  .todos {
    margin: 0 0 0.5rem;
    padding-left: 1.2rem;
  }
  .todos .completed {
    color: var(--muted);
    text-decoration: line-through;
  }
  .loading,
  .truncated {
    color: var(--muted);
    font-size: 0.85rem;
    margin: 0;
  }
</style>
