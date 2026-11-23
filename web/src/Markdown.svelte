<script context="module">
  import { marked } from "marked";
  import hljs from "highlight.js/lib/common";

  // Diagnostic: ?plain=1 renders text without Markdown or highlighting, to
  // tell a parsing stall apart from a rendering or streaming one.
  const plain =
    typeof location !== "undefined" && new URLSearchParams(location.search).has("plain");

  // The agent writes links to files on disk as file://. A browser page cannot
  // load those, and Firefox throws on them mid-render, so drop the target.
  marked.use({
    walkTokens(token) {
      if ((token.type === "link" || token.type === "image") && /^file:/i.test(token.href || "")) {
        token.href = "";
      }
    },
  });

  // Module scope: use() wraps the global renderer, so calling it per
  // component mount would chain one wrapper (and closure) per rendered entry.
  marked.use({
    renderer: {
      code({ text, lang }) {
        const language = lang && hljs.getLanguage(lang) ? lang : "plaintext";
        const html = hljs.highlight(text, { language }).value;
        return `<pre><code class="hljs language-${language}">${html}</code></pre>`;
      },
    },
  });
</script>

<script>
  import { onDestroy, onMount } from "svelte";

  export let text = "";
  // Live text is still growing; rendering it as plain text is ~8x cheaper
  // than rebuilding its DOM, and it is formatted once when the stream ends.
  export let live = false;

  // A streamed answer arrives one delta at a time; reparsing the whole message
  // for each one pins a CPU core. Reparse at most every 100ms.
  const interval = 100;
  let html = "";
  let shown = null;
  let queued = null;
  let timer = null;
  let last = 0;

  // A restored session mounts many entries at once, and parsing and
  // highlighting every one stalls the page. Render plain text until an entry
  // nears the viewport, then format it once.
  const rootMargin = "600px 0px";
  let root;
  let visible = false;
  let observer;

  onMount(() => {
    if (typeof IntersectionObserver === "undefined") {
      visible = true;
      return;
    }
    observer = new IntersectionObserver(
      (entries) => {
        if (!entries.some((e) => e.isIntersecting)) return;
        visible = true;
        observer.disconnect();
      },
      { rootMargin },
    );
    observer.observe(root);
  });

  $: if (live) {
    clearTimeout(timer);
    timer = null;
    queued = null;
    shown = null;
  } else if (visible && !plain) {
    schedule(text);
  }

  function schedule(value) {
    if (live || shown === value) return;
    queued = value;
    if (timer) return;
    const wait = interval - (Date.now() - last);
    if (wait <= 0) {
      flush();
      return;
    }
    timer = setTimeout(() => {
      timer = null;
      flush();
    }, wait);
  }

  function flush() {
    if (queued === null) return;
    shown = queued;
    queued = null;
    last = Date.now();
    html = marked.parse(shown);
  }

  onDestroy(() => {
    observer?.disconnect();
    clearTimeout(timer);
  });
</script>

<div bind:this={root}>
  {#if plain}
    <div class="markdown pending">{text}</div>
  {:else if live}
    <div class="markdown live">{text}</div>
  {:else if visible}
    <div class="markdown">{@html html}</div>
  {:else}
    <div class="markdown pending">{text}</div>
  {/if}
</div>

<style>
  .live {
    white-space: pre-wrap;
  }
  .pending {
    white-space: pre-wrap;
  }
  .markdown :global(p) {
    margin: 0 0 0.6rem;
  }
  .markdown :global(p:last-child) {
    margin-bottom: 0;
  }
  .markdown :global(ul),
  .markdown :global(ol) {
    padding-left: 1.4rem;
    margin: 0 0 0.6rem;
  }
  .markdown :global(h1),
  .markdown :global(h2),
  .markdown :global(h3) {
    margin: 0.8rem 0 0.4rem;
    line-height: 1.25;
  }
  .markdown :global(table) {
    border-collapse: collapse;
    display: block;
    overflow-x: auto;
  }
  .markdown :global(th),
  .markdown :global(td) {
    border: 1px solid var(--border);
    padding: 0.3rem 0.5rem;
  }
</style>
