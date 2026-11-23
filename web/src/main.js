import "./app.css";

const params = new URLSearchParams(location.search);

if (params.has("minimal")) {
  document.getElementById("app").textContent = "minimal mode";
} else {
  import("./App.svelte").then(({ default: App }) => {
    new App({ target: document.getElementById("app") });
  });
}
