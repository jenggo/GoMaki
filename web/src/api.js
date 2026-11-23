let source;

export function events(onUpdate, onStatus) {
  source = new EventSource("/events");
  source.onmessage = (e) => {
    try {
      onUpdate(JSON.parse(e.data));
    } catch {
      // Ignore a malformed frame; EventSource keeps the stream open.
    }
  };
  source.onopen = () => onStatus?.(true);
  source.onerror = () => onStatus?.(false);
  return () => source && source.close();
}

export async function snapshot() {
  const res = await fetch("/snapshot");
  if (!res.ok) throw new Error(`snapshot ${res.status}`);
  return res.json();
}

export async function post(path, body) {
  const res = await fetch(path, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body ?? {}),
  });
  if (!res.ok) throw new Error((await res.text()).trim() || "request failed");
  return res.json();
}

export async function toolDetails(id) {
  const res = await fetch(`/tool/${encodeURIComponent(id)}`);
  if (!res.ok) throw new Error("tool details failed");
  return res.json();
}

export async function commands() {
  const res = await fetch("/commands");
  if (!res.ok) return [];
  return res.json();
}

export async function upload(file) {
  const body = new FormData();
  body.append("file", file);
  const res = await fetch("/upload", { method: "POST", body });
  if (!res.ok) throw new Error((await res.text()).trim() || "upload failed");
  return res.json();
}
