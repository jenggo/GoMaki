<script>
  import { onMount, tick } from "svelte";
  import Markdown from "./Markdown.svelte";
  import ToolCall from "./ToolCall.svelte";
  import { events, snapshot, post, toolDetails, commands as fetchCommands, upload } from "./api.js";

  let state = emptyState();
  let draft = "";
  let commandList = [];
  let paletteIndex = 0;
  let expandedTools = {};
  let toolCache = {};
  let expandedThoughts = {};
  let attachName = "";
  let error = "";
  let todoExpanded = false;
  let todoDismissed = "";
  let todoToolID = "";
  let transcriptEl;
  let fileInput;
  let autoScroll = true;
  let theme = "light";
  let sseOpen = true;
  let snapshotError = "";
  let pendingUpdates = [];
  let flushScheduled = false;
  let loadingEarlier = false;
  let lastSnapshotAt = 0;

  // A coarse pointer is a touch-first device: Enter inserts a newline and the
  // send button sends.
  const touchDevice = window.matchMedia("(pointer: coarse)").matches;

  function normalize(s) {
    return {
      ...s,
      transcript: s.transcript || [],
      transcriptStart: s.transcriptStart || 0,
      plan: s.plan || [],
      todos: s.todos || [],
      todoToolId: s.todoToolId || "",
      models: s.models || [],
      modes: s.modes || [],
      sessions: s.sessions || [],
      queue: s.queue || [],
    };
  }

  function emptyState() {
    return {
      transcript: [],
      plan: [],
      todos: [],
      todoToolId: "",
      models: [],
      modes: [],
      queue: [],
      sessions: [],
      permission: null,
      elicitation: null,
      busy: false,
      status: "",
      err: "",
      notice: "",
      model: "",
      mode: "",
      cwd: "",
      branch: "",
      contextUsed: 0,
      contextSize: 0,
      costAmount: 0,
      costCurrency: "",
      sessionId: "",
      title: "",
    };
  }

  $: matches = paletteMatches(draft, commandList);
  $: if (matches.length && paletteIndex >= matches.length) paletteIndex = 0;
  $: dark = theme === "dark";
  $: connection = sseOpen ? "" : "Reconnecting…";
  $: todo = state.todos?.length ? { id: state.todoToolId, todos: state.todos } : null;
  $: if (todo && todo.id !== todoToolID) {
    todoToolID = todo.id;
    todoExpanded = false;
    todoDismissed = "";
  }

  function todoDone(todos) {
    return todos.filter((t) => t.status === "completed" || t.status === "cancelled").length;
  }

  function todoAllDone(todos) {
    return todos.length > 0 && todoDone(todos) === todos.length;
  }

  function todoCurrent(todos) {
    const current = todos.find((t) => t.status === "in_progress");
    return current ? current.content : "";
  }

  function paletteMatches(text, list) {
    if (!text.startsWith("/") || /[\s]/.test(text)) return [];
    const query = text.slice(1).toLowerCase();
    return list
      .filter((c) => !query || c.name.toLowerCase().includes(query))
      .slice(0, 8);
  }

  async function refreshSnapshot() {
    const now = Date.now();
    if (now - lastSnapshotAt < 1000) return;
    lastSnapshotAt = now;
    try {
      state = mergeState(normalize(await snapshot()));
      pendingUpdates = [];
      snapshotError = "";
    } catch (err) {
      snapshotError = err.message || "snapshot failed";
    }
  }

  onMount(() => {
    fetchCommands().then((list) => (commandList = list)).catch(() => {});
    refreshSnapshot().then(fillViewport);    theme = resolvedDark() ? "dark" : "light";
    const media = window.matchMedia("(prefers-color-scheme: dark)");
    const onSystemTheme = () => {
      if (!localStorage.getItem("gomaki-theme")) {
        theme = media.matches ? "dark" : "light";
      }
    };
    media.addEventListener("change", onSystemTheme);
    const close = events(applyUpdate, (open) => {
      sseOpen = open;
      if (open) refreshSnapshot();
      else {
        lastSnapshotAt = 0;
        refreshSnapshot();
      }
    });
    const poll = setInterval(() => {
      if (!sseOpen) refreshSnapshot();
    }, 10000);
    return () => {
      close();
      clearInterval(poll);
      media.removeEventListener("change", onSystemTheme);
    };
  });

  function resolvedDark() {
    const explicit = document.documentElement.dataset.theme;
    if (explicit === "dark") return true;
    if (explicit === "light") return false;
    return window.matchMedia("(prefers-color-scheme: dark)").matches;
  }

  function toggleTheme() {
    theme = theme === "dark" ? "light" : "dark";
    document.documentElement.dataset.theme = theme;
    try {
      localStorage.setItem("gomaki-theme", theme);
    } catch {
      // Private mode may reject storage; the theme still changes for this page.
    }
  }

  function statusText() {
    if (state.busy) return state.status || "Working…";
    if (state.notice) return state.notice;
    if (state.status) return state.status;
    if (state.title) return state.title;
    return "";
  }

  async function toggleTodo() {
    const stick = autoScroll;
    todoExpanded = !todoExpanded;
    await tick();
    if (stick && transcriptEl) transcriptEl.scrollTop = transcriptEl.scrollHeight;
  }

  const MODE_COLORS = { build: "#d9a300", plan: "#3b82f6" };
  const EXTRA_MODE_COLORS = ["#a855f7", "#10b981", "#f97316", "#14b8a6", "#ec4899"];

  function modeColor(mode) {
    if (MODE_COLORS[mode]) return MODE_COLORS[mode];
    let h = 0;
    for (const ch of mode || "") h = (h * 31 + ch.charCodeAt(0)) >>> 0;
    return EXTRA_MODE_COLORS[h % EXTRA_MODE_COLORS.length];
  }

  function modeLabel(mode) {
    if (!mode) return "Build";
    return mode.charAt(0).toUpperCase() + mode.slice(1);
  }

  async function cycleMode() {
    const modes = state.modes?.length ? state.modes : ["build"];
    if (modes.length < 2) return;
    const current = state.mode || "build";
    const next = modes[(modes.indexOf(current) + 1) % modes.length];
    try {
      await post("/mode", { mode: next });
    } catch (err) {
      error = err.message;
    }
  }

  function applyUpdate(u) {
    pendingUpdates.push(u);
    if (!flushScheduled) {
      flushScheduled = true;
      requestAnimationFrame(flushUpdates);
    }
  }

  function flushUpdates() {
    flushScheduled = false;
    const batch = pendingUpdates;
    pendingUpdates = [];
    for (const u of batch) applyUpdateNow(u);
  }

  function mergeState(next) {
    const prev = state;
    const loadedEnd = prev.transcriptStart + prev.transcript.length;
    if (next.transcriptStart > loadedEnd) {
      // More than a window of entries arrived while disconnected; start fresh.
      next.truncated = next.transcriptStart > 0;
      return next;
    }
    if (prev.transcriptStart < next.transcriptStart && prev.transcript.length) {
      const keep = prev.transcript.slice(0, next.transcriptStart - prev.transcriptStart);
      next.transcript = [...keep, ...next.transcript];
      next.transcriptStart = prev.transcriptStart;
    }
    next.truncated = next.transcriptStart > 0;
    return next;
  }

  function applyUpdateNow(u) {
    if (u.kind === "event") {
      if (u.state) state = mergeState(normalize(u.state));
      applyEvent(u.event);
      return;
    }
    state = mergeState(normalize(u.state));
    if (u.kind === "reset") {
      draft = "";
      expandedTools = {};
      toolCache = {};
      expandedThoughts = {};
      attachName = "";
    }
    if (u.kind === "draft") draft = u.text;
  }

  function applyEvent(e) {
    const t = state.transcript;
    switch (e.kind) {
      case "user":
        appendText("user", e.text);
        break;
      case "text":
        appendText("agent", e.text);
        break;
      case "thought":
        appendText("thought", e.text);
        break;
      case "tool":
        t.push({
          kind: "tool",
          toolId: e.toolId,
          text: e.title,
          toolName: e.title,
          status: e.status,
          toolKind: e.toolKind,
          hasDetails: true,
          todos: e.todos || [],
          todoList: !!e.todoList,
        });
        if (e.todoList) {
          state.todos = e.todos || [];
          state.todoToolId = e.toolId;
        }
        break;
      case "toolUpdate": {
        if (e.todoList) {
          state.todos = e.todos || [];
          state.todoToolId = e.toolId;
        }
        const entry = [...t].reverse().find((x) => x.kind === "tool" && x.toolId === e.toolId);
        if (entry) {
          if (e.title) entry.text = e.title;
          if (e.status) entry.status = e.status;
          if (e.todoList) {
            entry.todos = e.todos || [];
            entry.todoList = true;
          }
        }
        break;
      }
      case "plan":
        state.plan = e.plan || [];
        break;
      case "config":
        state.models = e.models || [];
        if (e.model) state.model = e.model;
        break;
      case "commands":
        fetchCommands()
          .then((list) => (commandList = list))
          .catch(() => {});
        break;
      case "mode":
        state.mode = e.mode;
        break;
      case "permission":
        state.permission = e.permission;
        break;
      case "elicitation":
        state.elicitation = e.elicitation;
        break;
      case "usage":
        state.contextUsed = e.contextUsed;
        state.contextSize = e.contextSize;
        state.costAmount = e.costAmount;
        state.costCurrency = e.costCurrency;
        break;
      case "promptDone":
        state.busy = false;
        state.status = "";
        if (e.err) state.err = e.err;
        break;
    }
    state = state;
  }

  function appendText(kind, text) {
    const t = state.transcript;
    if (t.length && t[t.length - 1].kind === kind) t[t.length - 1].text += text;
    else t.push({ kind, text });
    state = state;
  }

  async function send() {
    const text = draft.trim();
    if (!text) return;
    draft = "";
    error = "";
    try {
      await post("/prompt", { text });
      attachName = "";
    } catch (err) {
      error = err.message;
      draft = text;
    }
  }

  async function stop() {
    try {
      await post("/cancel", {});
    } catch (err) {
      error = err.message;
    }
  }

  async function pullQueued() {
    try {
      const res = await post("/queue/pop", {});
      if (res.text) draft = res.text;
    } catch (err) {
      error = err.message;
    }
  }

  async function answerPermission(option) {
    state.permission = null;
    try {
      await post("/permission", { option });
    } catch (err) {
      error = err.message;
    }
  }

  let elicit = {};
  let elicitFor = null;
  $: if (state.elicitation && state.elicitation !== elicitFor) {
    elicitFor = state.elicitation;
    elicit = {};
    for (const f of state.elicitation.fields || []) {
      if (f.multi) elicit[f.name] = [];
      else if (f.options?.length) elicit[f.name] = f.options[0].value;
      else elicit[f.name] = "";
    }
  }

  async function submitElicitation(decline) {
    const req = state.elicitation;
    if (!req) return;
    const answers = {};
    if (!decline) {
      for (const f of req.fields || []) {
        const value = elicit[f.name];
        if (f.multi) {
          if (value?.length) answers[f.name] = value;
        } else if (value) {
          answers[f.name] = value;
        }
      }
    }
    state.elicitation = null;
    try {
      await post("/elicitation", { answers, decline });
    } catch (err) {
      error = err.message;
    }
  }

  async function switchModel(event) {
    try {
      await post("/model", { model: event.target.value });
    } catch (err) {
      error = err.message;
    }
  }

  async function resumeSession(event) {
    const id = event.target.value;
    if (!id) return;
    try {
      await post("/session", { id });
      await refreshSnapshot();
    } catch (err) {
      error = err.message;
    }
  }

  async function pickFile(event) {
    const file = event.target.files?.[0];
    if (!file) return;
    try {
      const res = await upload(file);
      attachName = res.file;
    } catch (err) {
      error = err.message;
    }
    event.target.value = "";
  }

  async function toggleTool(id) {
    expandedTools = { ...expandedTools, [id]: !expandedTools[id] };
    if (expandedTools[id] && !toolCache[id]) {
      try {
        toolCache = { ...toolCache, [id]: await toolDetails(id) };
      } catch (err) {
        error = err.message;
      }
    }
  }

  function toggleThought(index) {
    expandedThoughts = { ...expandedThoughts, [index]: !expandedThoughts[index] };
  }

  // Live thoughts roll upward like end credits: keep the tail pinned while older
  // lines scroll out of the top of the viewport.
  function followThought(node) {
    const stick = () => {
      node.scrollTop = node.scrollHeight;
    };
    const observer = new MutationObserver(stick);
    observer.observe(node, { childList: true, characterData: true, subtree: true });
    stick();
    return { destroy: () => observer.disconnect() };
  }

  function onKeydown(event) {
    if (event.key === "q" && (event.ctrlKey || event.metaKey)) {
      event.preventDefault();
      pullQueued();
      return;
    }
    if (matches.length) {
      if (event.key === "ArrowDown") {
        event.preventDefault();
        paletteIndex = (paletteIndex + 1) % matches.length;
        return;
      }
      if (event.key === "ArrowUp") {
        event.preventDefault();
        paletteIndex = (paletteIndex - 1 + matches.length) % matches.length;
        return;
      }
      if (event.key === "Enter" && !event.shiftKey) {
        event.preventDefault();
        draft = matches[paletteIndex].name + " ";
        return;
      }
    }
    if (event.key === "Enter" && !event.shiftKey && !touchDevice) {
      event.preventDefault();
      send();
    }
  }

  function jumpToBottom() {
    if (!transcriptEl) return;
    transcriptEl.scrollTop = transcriptEl.scrollHeight;
    autoScroll = true;
  }

  function onScroll() {
    if (!transcriptEl) return;
    const gap = transcriptEl.scrollHeight - transcriptEl.scrollTop - transcriptEl.clientHeight;
    autoScroll = gap < 80;
    if (transcriptEl.scrollHeight > transcriptEl.clientHeight + 40 && transcriptEl.scrollTop < 120) {
      loadEarlier();
    }
  }

  async function fillViewport() {
    for (let i = 0; i < 2; i++) {
      await tick();
      if (!transcriptEl) return;
      if (transcriptEl.scrollHeight > transcriptEl.clientHeight + 40) return;
      if (state.transcriptStart <= 0) return;
      if (!(await loadEarlier())) return;
    }
  }

  async function loadEarlier() {
    if (loadingEarlier || state.transcriptStart <= 0) return false;
    loadingEarlier = true;
    const before = state.transcriptStart;
    autoScroll = false;
    try {
      const res = await fetch(`/transcript?before=${before}&limit=50`);
      if (!res.ok) throw new Error(`transcript ${res.status}`);
      const page = await res.json();
      if (!page.entries?.length) return false;
      const el = transcriptEl;
      const prevHeight = el ? el.scrollHeight : 0;
      state.transcript = [...page.entries, ...state.transcript];
      state.transcriptStart = page.start;
      state.truncated = page.start > 0;
      await tick();
      if (el) {
        el.scrollTop += el.scrollHeight - prevHeight;
        const gap = el.scrollHeight - el.scrollTop - el.clientHeight;
        autoScroll = gap < 80;
      }
      return true;
    } catch (err) {
      error = err.message;
      return false;
    } finally {
      loadingEarlier = false;
    }
  }

  $: if (state.transcript.length) {
    tick().then(() => {
      // Assigning through transcriptEl would invalidate the bind:this variable
      // and rerun this statement on every flush.
      const el = transcriptEl;
      if (autoScroll && el) el.scrollTop = el.scrollHeight;
    });
  }

  function tokens(n) {
    if (!n) return "0";
    if (n >= 1e6) return (n / 1e6).toFixed(Number.isInteger(n / 1e6) ? 0 : 1) + "M";
    if (n >= 1000) return (n / 1000).toFixed(Number.isInteger(n / 1000) ? 0 : 1) + "k";
    return String(n);
  }

  $: usageText = (() => {
    if (!state.contextSize) return "—/— (—%)";
    const pct = Math.round((state.contextUsed / state.contextSize) * 100);
    return `${tokens(state.contextUsed)}/${tokens(state.contextSize)} (${pct}%)`;
  })();

  function cost() {
    if (!state.costAmount) return "";
    return `${state.costAmount.toFixed(2)} ${state.costCurrency}`;
  }

  function planDone() {
    return (state.plan || []).filter((p) => p.status === "completed").length;
  }
</script>

<div class="app">
  <header>
    <div class="bar">
      <div class="where">
        <span class="path" title={state.cwd || ""}>{state.cwd || "No workspace"}</span>
        {#if state.branch}<span class="branch">{state.branch}</span>{/if}
      </div>
      <div class="controls">
        {#if state.models?.length}
          <div class="select-wrap">
            <select value={state.model} on:change={switchModel} aria-label="Model">
              {#each state.models as model}<option value={model}>{model}</option>{/each}
            </select>
          </div>
        {:else}<span class="chip">No model</span>{/if}
        {#if state.sessionId || state.sessions?.length}
          <div class="select-wrap">
            <select value={state.sessionId || ""} on:change={resumeSession} aria-label="Session">
              {#if !state.sessionId}<option value="">No session</option>{/if}
              {#if state.sessionId}
                <option value={state.sessionId}>{state.title || "New session"}</option>
              {/if}
              {#each (state.sessions || []).filter((x) => x.id !== state.sessionId) as session}
                <option value={session.id}>{session.title || session.id}</option>
              {/each}
            </select>
          </div>
        {:else}<span class="chip">No session</span>{/if}
        <button
          class="theme"
          on:click={toggleTheme}
          aria-label={dark ? "Switch to light theme" : "Switch to dark theme"}
          title={dark ? "Switch to light theme" : "Switch to dark theme"}
        >
          {#if dark}
            <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true" focusable="false">
              <circle cx="12" cy="12" r="4" fill="none" stroke="currentColor" stroke-width="2" />
              <path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
            </svg>
          {:else}
            <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true" focusable="false">
              <path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8Z" fill="none" stroke="currentColor" stroke-width="2" stroke-linejoin="round" />
            </svg>
          {/if}
        </button>
      </div>
    </div>
  </header>

  {#if error || state.err || snapshotError}
    <div class="banner">{error || state.err || snapshotError}</div>
  {/if}

  <div class="main-area">
  <main bind:this={transcriptEl} on:scroll={onScroll}>
    <div class="thread">
      {#if state.truncated}
        <div class="truncated-note">
          {#if loadingEarlier}
            Loading earlier…
          {:else}
            <button class="link" on:click={loadEarlier}>Load earlier messages</button>
          {/if}
        </div>
      {/if}
      {#if state.transcript.length === 0}
        <div class="empty">
          <div class="mark">G</div>
          <h1>GoMaki</h1>
          <p>A desktop client for Maki's ACP.</p>
          <p class="hint">Type below to start a new session.</p>
        </div>
      {/if}
      {#each state.transcript as entry, index (state.transcriptStart + index)}
        {#if entry.kind === "user"}
          <div class="entry user">{entry.text}</div>
        {:else if entry.kind === "agent"}
          <div class="entry agent">
            <Markdown text={entry.text} live={state.busy && index === state.transcript.length - 1} />
          </div>
        {:else if entry.kind === "thought"}
          {#if state.busy && index === state.transcript.length - 1}
            <div class="entry thought live">
              <div class="thought-head static">Thinking</div>
              <div class="thought-roll" use:followThought>
                <Markdown text={entry.text} live={true} />
              </div>
            </div>
          {:else}
            <div class="entry thought">
              <button class="thought-head" on:click={() => toggleThought(index)}>
                <span class="chevron">{expandedThoughts[index] ? "▾" : "▸"}</span> Thinking
              </button>
              {#if expandedThoughts[index]}<div class="thought-body"><Markdown text={entry.text} /></div>{/if}
            </div>
          {/if}
        {:else if entry.kind === "tool"}
          <ToolCall
            {entry}
            open={!!expandedTools[entry.toolId]}
            details={toolCache[entry.toolId]}
            on:toggle={() => toggleTool(entry.toolId)}
          />
        {/if}
      {/each}
    </div>
  </main>
  {#if !autoScroll && state.transcript.length}
    <button class="jump" on:click={jumpToBottom} aria-label="Scroll to bottom">
      <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true" focusable="false">
        <path d="M12 5v14M19 12l-7 7-7-7" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" />
      </svg>
    </button>
  {/if}
  </div>

  <div class="panels">
    {#if todo && todo.todos.length && todo.id !== todoDismissed}
      <section class="panel">
        <div
          class="panel-head"
          role="button"
          tabindex="0"
          on:click={toggleTodo}
          on:keydown={(e) => {
            if (e.key === "Enter" || e.key === " ") {
              e.preventDefault();
              toggleTodo();
            }
          }}
        >
          <span class="chevron">{todoExpanded ? "▾" : "▸"}</span>
          <span class="panel-title">Tasks {todoDone(todo.todos)} of {todo.todos.length}</span>
          {#if todoCurrent(todo.todos)}<span class="panel-sub">· {todoCurrent(todo.todos)}</span>{/if}
          {#if todoAllDone(todo.todos)}
            <button class="close" on:click|stopPropagation={() => (todoDismissed = todo.id)} aria-label="Dismiss tasks">×</button>
          {/if}
        </div>
        {#if todoExpanded}
          <ul class="task-list">
            {#each todo.todos as item}<li class={item.status}>{item.content}</li>{/each}
          </ul>
        {/if}
      </section>
    {/if}
    {#if state.plan?.length}
      <section class="panel">
        <div class="panel-head"><span class="panel-title">Plan</span></div>
        <ul class="task-list">
          {#each state.plan as item}<li class={item.status}>{item.content}</li>{/each}
        </ul>
      </section>
    {/if}
    {#if state.queue?.length}
      <section class="panel">
        <div class="panel-head"><span class="panel-title">Queued {state.queue.length}</span></div>
        {#each state.queue as item}<div class="queued">{item.display}</div>{/each}
      </section>
    {/if}
  </div>

  <footer>
    <div class="footer-inner">
      {#if matches.length}
        <div class="palette">
          {#each matches as cmd, i}
            <button
              class:active={i === paletteIndex}
              on:click={() => {
                draft = cmd.name + " ";
                paletteIndex = i;
              }}
            >
              <span class="cmd-name">{cmd.name}</span>
              {#if cmd.description}<span class="cmd-desc">{cmd.description}</span>{/if}
            </button>
          {/each}
        </div>
      {/if}
      {#if attachName}
        <div class="attach">
          <span>Attached: {attachName}</span>
          <button class="link" on:click={() => (attachName = "")}>Remove</button>
        </div>
      {/if}
      <div class="composer-card">
        <textarea bind:value={draft} on:keydown={onKeydown} rows="1" placeholder="Ask Maki…"></textarea>
        <input bind:this={fileInput} type="file" hidden on:change={pickFile} />
        <button class="icon" title="Attach a file" aria-label="Attach a file" on:click={() => fileInput.click()}>
          <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true" focusable="false">
            <path d="M12 5v14M5 12h14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
          </svg>
        </button>
        {#if state.busy && !draft.trim()}
          <button class="stop" on:click={stop} aria-label="Stop">
            <svg viewBox="0 0 24 24" width="16" height="16" aria-hidden="true" focusable="false">
              <rect x="6" y="6" width="12" height="12" rx="2" fill="currentColor" />
            </svg>
          </button>
        {:else}
          <button class="send" on:click={send} disabled={!draft.trim()} aria-label={state.busy ? "Queue prompt" : "Send"}>
            <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true" focusable="false">
              <path d="M12 19V5M5 12l7-7 7 7" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" />
            </svg>
          </button>
        {/if}
      </div>
      <div class="status">
        <span class="meta usage" title="Context: used / window (percent)">{usageText}</span>
        <span class="dot" class:busy={state.busy}></span>
        <span class="state">{connection || statusText()}</span>
        <button
          class="mode-chip"
          style={`--mode-color: ${modeColor(state.mode)}`}
          on:click={cycleMode}
          title={state.modes?.length > 1 ? "Switch mode" : "Current mode"}
          disabled={!state.modes || state.modes.length < 2}
        >
          <span class="mode-dot"></span>
          {modeLabel(state.mode)}
        </button>
        <span class="meta">{cost()}</span>
      </div>
    </div>
  </footer>

  {#if state.permission}
    <div class="modal">
      <div class="dialog">
        <h2>Permission required</h2>
        <p>{state.permission.title || "Maki wants to run a tool."}</p>
        <div class="dialog-actions">
          {#each state.permission.options as option}
            <button
              class:primary={option.kind === "allow_once"}
              class:danger={option.kind?.startsWith("reject")}
              on:click={() => answerPermission(option.id)}
            >
              {option.name}
            </button>
          {/each}
          <button on:click={() => answerPermission("")}>Cancel</button>
        </div>
      </div>
    </div>
  {/if}

  {#if state.elicitation}
    <div class="modal">
      <div class="dialog">
        <h2>Question</h2>
        {#if state.elicitation.message}<p class="muted">{state.elicitation.message}</p>{/if}
        {#each state.elicitation.fields || [] as field}
          <div class="field">
            <label for={`f-${field.name}`}>{field.title || field.name}</label>
            {#if field.description}<p class="muted small">{field.description}</p>{/if}
            {#if field.options?.length}
              {#if field.multi}
                {#each field.options as option}
                  <label class="choice">
                    <input type="checkbox" bind:group={elicit[field.name]} value={option.value} />
                    {option.label}
                  </label>
                {/each}
              {:else}
                <select id={`f-${field.name}`} bind:value={elicit[field.name]}>
                  {#each field.options as option}<option value={option.value}>{option.label}</option>{/each}
                </select>
              {/if}
            {:else}
              <input id={`f-${field.name}`} type="text" bind:value={elicit[field.name]} />
            {/if}
          </div>
        {/each}
        <div class="dialog-actions">
          <button on:click={() => submitElicitation(true)}>Decline</button>
          <button class="primary" on:click={() => submitElicitation(false)}>Submit</button>
        </div>
      </div>
    </div>
  {/if}
</div>

<style>
  .app {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
  }

  header {
    position: sticky;
    top: 0;
    z-index: 5;
    background: var(--bg);
    border-bottom: 1px solid var(--border);
  }
  .bar {
    max-width: 900px;
    margin: 0 auto;
    padding: 10px 16px;
    display: flex;
    align-items: center;
    gap: 12px;
    flex-wrap: wrap;
  }
  .where {
    display: flex;
    align-items: baseline;
    gap: 8px;
    min-width: 0;
    flex: 1 1 240px;
  }
  .path {
    font-weight: 600;
    font-size: 0.9rem;
    letter-spacing: -0.01em;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .branch {
    font-size: 0.72rem;
    color: var(--muted);
    background: var(--surface-2);
    padding: 2px 8px;
    border-radius: 999px;
    white-space: nowrap;
  }
  .controls {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }
  .select-wrap {
    position: relative;
    display: inline-flex;
    min-width: 0;
    max-width: 180px;
  }
  .select-wrap::after {
    content: "";
    position: absolute;
    right: 10px;
    top: 50%;
    width: 6px;
    height: 6px;
    border-right: 1.5px solid var(--muted);
    border-bottom: 1.5px solid var(--muted);
    transform: translateY(-70%) rotate(45deg);
    pointer-events: none;
  }
  .controls select {
    width: 100%;
    min-height: 32px;
    padding: 4px 26px 4px 10px;
    border-radius: 8px;
    font-size: 0.82rem;
    appearance: none;
    -webkit-appearance: none;
  }
  .chip {
    display: inline-flex;
    align-items: center;
    padding: 4px 10px;
    border: 1px solid var(--border);
    border-radius: 999px;
    color: var(--muted);
    font-size: 0.78rem;
    white-space: nowrap;
  }
  .theme {
    width: 34px;
    height: 34px;
    min-height: 34px;
    padding: 0;
    border-radius: 50%;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    color: var(--muted);
  }
  .theme:hover {
    color: var(--text);
  }

  .banner {
    max-width: 900px;
    margin: 12px auto 0;
    width: calc(100% - 32px);
    background: var(--danger-bg);
    color: var(--danger);
    padding: 10px 14px;
    border-radius: 10px;
    font-size: 0.85rem;
  }

  main {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: 24px 16px 8px;
    -webkit-overflow-scrolling: touch;
    scrollbar-width: thin;
    scrollbar-color: var(--border) transparent;
  }
  .main-area {
    position: relative;
    flex: 1;
    display: flex;
    flex-direction: column;
    min-height: 0;
  }
  .jump {
    position: absolute;
    bottom: 12px;
    left: 50%;
    transform: translateX(-50%);
    z-index: 6;
    width: 36px;
    min-height: 36px;
    padding: 0;
    border-radius: 50%;
    display: flex;
    align-items: center;
    justify-content: center;
    background: var(--surface);
    box-shadow: var(--shadow-md);
  }
  .thread {
    max-width: 760px;
    margin: 0 auto;
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .truncated-note {
    text-align: center;
    color: var(--muted);
    font-size: 0.75rem;
    padding: 4px 0;
  }
  .empty {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 6px;
    min-height: 46vh;
    text-align: center;
    padding: 24px;
  }
  .empty .mark {
    width: 56px;
    height: 56px;
    border-radius: 16px;
    background: var(--accent);
    color: #fff;
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: 1.5rem;
    font-weight: 700;
    box-shadow: 0 10px 30px var(--accent-soft);
    margin-bottom: 6px;
  }
  .empty h1 {
    margin: 0;
    font-size: 1.5rem;
    letter-spacing: -0.02em;
    text-wrap: balance;
  }
  .empty p {
    margin: 0;
    color: var(--muted);
    font-size: 0.9rem;
  }
  .empty .hint {
    font-size: 0.8rem;
    margin-top: 4px;
  }

  .entry {
    overflow-wrap: anywhere;
    font-size: 0.95rem;
  }
  .entry.user {
    align-self: flex-end;
    max-width: 82%;
    background: var(--accent);
    color: #fff;
    padding: 10px 14px;
    border-radius: 18px 18px 4px 18px;
    white-space: pre-wrap;
    box-shadow: var(--shadow-sm);
  }
  .entry.agent {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 18px 18px 18px 4px;
    padding: 12px 16px;
    box-shadow: var(--shadow-sm);
  }
  .entry.thought {
    background: var(--thought-bg);
    border: 1px solid var(--thought-border);
    border-left: 3px solid var(--thought-accent);
    border-radius: 12px;
    color: var(--thought-text);
    font-size: 0.85rem;
  }
  .thought-head {
    background: transparent;
    border: 0;
    border-radius: 12px;
    padding: 8px 12px;
    min-height: 0;
    color: var(--thought-text);
    font-size: 0.82rem;
    display: flex;
    width: 100%;
    align-items: center;
    text-align: left;
    gap: 6px;
  }
  .thought-head.static {
    color: var(--thought-text);
    font-size: 0.82rem;
  }
  .thought-roll {
    padding: 0 12px 8px;
    max-height: 9em;
    overflow: hidden;
    -webkit-mask-image: linear-gradient(to bottom, transparent 0, #000 1.6em);
    mask-image: linear-gradient(to bottom, transparent 0, #000 1.6em);
  }
  .thought-body {
    padding: 0 12px 8px;
  }
  .chevron {
    color: var(--muted);
    font-size: 0.75rem;
  }
  .entry.thought .chevron {
    color: var(--thought-accent);
  }

  .panels {
    max-width: 900px;
    width: 100%;
    margin: 0 auto;
    padding: 0 16px;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .panel {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 12px;
    padding: 10px 14px;
    font-size: 0.85rem;
    box-shadow: var(--shadow-sm);
  }
  .panel-head {
    display: flex;
    align-items: center;
    gap: 8px;
    color: var(--muted);
    font-weight: 600;
    cursor: pointer;
  }
  .panel-title {
    font-size: 0.8rem;
  }
  .panel-sub {
    color: var(--muted);
    font-weight: 400;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .panel-head .close {
    margin-left: auto;
    min-height: 0;
    padding: 0 6px;
    background: transparent;
    border: 0;
    color: var(--muted);
    font-size: 1rem;
  }
  .task-list {
    margin: 8px 0 0;
    padding-left: 18px;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .task-list .completed {
    color: var(--muted);
    text-decoration: line-through;
  }
  .task-list .in_progress {
    color: var(--warning);
  }
  .queued {
    color: var(--muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    margin-top: 4px;
  }

  footer {
    position: sticky;
    bottom: 0;
    z-index: 5;
    background: var(--bg);
    border-top: 1px solid var(--border);
    padding: 10px 16px calc(10px + var(--safe-bottom));
  }
  .footer-inner {
    max-width: 900px;
    margin: 0 auto;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .palette {
    display: flex;
    flex-direction: column;
    gap: 2px;
    max-height: 40vh;
    overflow-y: auto;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 12px;
    padding: 4px;
    box-shadow: var(--shadow-md);
  }
  .palette button {
    display: flex;
    gap: 10px;
    align-items: baseline;
    text-align: left;
    background: transparent;
    border: 0;
    border-radius: 8px;
    padding: 8px 10px;
    min-height: 0;
    flex-shrink: 0;
  }
  .palette button.active {
    background: var(--accent-soft);
  }
  .cmd-name {
    font-weight: 600;
    font-size: 0.85rem;
  }
  .cmd-desc {
    color: var(--muted);
    font-size: 0.8rem;
  }
  .attach {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    font-size: 0.8rem;
    color: var(--muted);
  }
  .link {
    background: transparent;
    border: 0;
    color: var(--accent);
    padding: 0;
    min-height: 0;
    font-size: 0.8rem;
  }
  .composer-card {
    display: flex;
    align-items: flex-end;
    gap: 6px;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 18px;
    padding: 6px 6px 6px 14px;
    box-shadow: var(--shadow-md);
  }
  .composer-card textarea {
    flex: 1;
    resize: none;
    min-height: 28px;
    max-height: 30vh;
    padding: 8px 0;
    border: 0;
    background: transparent;
    color: var(--text);
    font-size: 0.95rem;
    line-height: 1.4;
  }
  .composer-card textarea:focus {
    outline: none;
  }
  .composer-card textarea::placeholder {
    color: var(--muted);
  }
  .icon,
  .send,
  .stop {
    width: 36px;
    height: 36px;
    min-height: 36px;
    padding: 0;
    border-radius: 50%;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex: none;
  }
  .icon {
    color: var(--muted);
    background: transparent;
    border: 0;
  }
  .icon:hover {
    background: var(--surface-2);
    color: var(--text);
  }
  .send {
    background: var(--accent);
    border: 0;
    color: #fff;
  }
  .send:disabled {
    opacity: 0.35;
  }
  .stop {
    background: var(--danger);
    border: 0;
    color: #fff;
  }
  .status {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 10px;
    font-size: 0.72rem;
    color: var(--muted);
    min-height: 16px;
    padding: 0 4px;
  }
  .dot.busy {
    background: var(--accent);
  }
  .mode-chip {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    min-height: 0;
    padding: 2px 8px;
    border-radius: 999px;
    border: 1px solid var(--border);
    background: transparent;
    color: var(--mode-color, var(--text));
    font-size: 0.72rem;
    font-weight: 600;
    cursor: pointer;
  }
  .mode-chip:hover:not(:disabled) {
    background: var(--surface-2);
  }
  .mode-chip:disabled {
    opacity: 1;
    cursor: default;
  }
  .mode-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--mode-color, var(--accent));
    flex: none;
  }
  .dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--muted);
    flex: none;
  }
  .status .state {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .meta {
    font-variant-numeric: tabular-nums;
  }
  .status .usage {
    font-weight: 600;
    color: var(--text);
  }

  .modal {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.45);
    backdrop-filter: blur(4px);
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 16px;
    z-index: 20;
  }
  .dialog {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 18px;
    padding: 20px;
    width: min(100%, 480px);
    max-height: 85vh;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 12px;
    box-shadow: var(--shadow-md);
  }
  .dialog h2 {
    margin: 0;
    font-size: 1.1rem;
    letter-spacing: -0.01em;
  }
  .dialog p {
    margin: 0;
  }
  .dialog-actions {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: 8px;
    margin-top: 4px;
  }
  .field {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .field label {
    font-weight: 600;
    font-size: 0.85rem;
  }
  .field input[type="text"] {
    padding: 8px 10px;
    border-radius: 8px;
    border: 1px solid var(--border);
    background: var(--surface-2);
  }
  .choice {
    display: flex;
    gap: 8px;
    align-items: center;
    font-weight: 400;
  }
  .muted {
    color: var(--muted);
  }
  .small {
    font-size: 0.8rem;
  }

  @media (max-width: 640px) {
    .bar {
      padding: 8px 12px;
      gap: 8px;
    }
    .where {
      flex-basis: 100%;
    }
    .controls {
      width: 100%;
    }
    .select-wrap {
      flex: 1 1 120px;
      max-width: none;
    }
    .theme {
      margin-left: auto;
    }
    main {
      padding: 16px 12px 8px;
    }
    .panels {
      padding: 0 12px;
    }
    footer {
      padding: 8px 12px calc(8px + var(--safe-bottom));
    }
    .entry.user {
      max-width: 92%;
    }
    .dialog-actions {
      justify-content: stretch;
    }
    .dialog-actions button {
      flex: 1 1 100%;
      min-height: 44px;
    }
    .palette button {
      flex-direction: column;
      align-items: flex-start;
      gap: 2px;
    }
  }
</style>
