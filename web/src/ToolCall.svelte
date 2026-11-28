<script>
  import { createEventDispatcher } from "svelte";

  export let entry;
  export let open = false;
  export let details = null;

  const dispatch = createEventDispatcher();

  // The server stores the tool input as JSON and outputs often arrive
  // JSON-quoted, so decode before showing them.
  function formatInput(raw) {
    try {
      const v = JSON.parse(raw);
      if (v && typeof v === "object") {
        return Object.entries(v)
          .map(([k, val]) => (typeof val === "string" ? `${k}: ${val}` : `${k}: ${JSON.stringify(val, null, 2)}`))
          .join("\n\n");
      }
      return typeof v === "string" ? v : raw;
    } catch {
      return raw;
    }
  }

  function formatOutput(raw) {
    try {
      const v = JSON.parse(raw);
      if (typeof v === "string") return v;
      if (v && typeof v === "object") return JSON.stringify(v, null, 2);
    } catch {
      // Plain text output is shown as is.
    }
    return raw;
  }
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
          <pre>{formatInput(details.input)}</pre>
        {/if}
        {#if details.output}
          <div class="label">Output</div>
          <pre>{formatOutput(details.output)}</pre>
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
    border: 1px solid var(--tool-border);
    border-left: 3px solid var(--tool-accent);
    border-radius: 12px;
    background: var(--tool-bg);
    overflow: hidden;
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
  .tool-kind {
    font-size: 0.68rem;
    color: var(--tool-accent);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .title {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-weight: 500;
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
    border-top: 1px solid var(--tool-border);
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
