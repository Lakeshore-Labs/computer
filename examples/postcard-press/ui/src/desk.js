import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StreamableHTTPClientTransport } from "@modelcontextprotocol/sdk/client/streamableHttp.js";

const $ = (id) => document.getElementById(id);
const el = (tag, props = {}, ...kids) => {
  const n = Object.assign(document.createElement(tag), props);
  n.append(...kids);
  return n;
};
const ORDER_PATH = "/desk/order.json";
const SVG_PATH = "/desk/postcard.svg";
const COMMAND = "set -eo pipefail\npython3 /recipe/postcard.py";

// Public trace for audits: tool names and abbreviated arguments only.
const trace = (window.pressTrace = []);
let client = null;
let tools = [];
let lastOrderText = null; // null until this page has written an order
let busy = false;
let issuedCount = 0;

function brief(v) {
  if (typeof v === "string")
    return v.length > 80 ? v.slice(0, 77) + `… (${v.length} chars)` : v;
  if (Array.isArray(v)) return v.map(brief);
  if (v && typeof v === "object")
    return Object.fromEntries(Object.entries(v).map(([k, x]) => [k, brief(x)]));
  return v;
}

function setStatus(state, text) {
  $("status").dataset.state = state;
  $("status-text").textContent = text;
}

async function call(name, args) {
  const entry = {
    tool: name,
    args: brief(args),
    at: new Date().toISOString(),
    ok: null,
  };
  trace.push(entry);
  const li = el(
    "li",
    {},
    el("code", { textContent: name }),
    " ",
    el("span", { className: "args", textContent: JSON.stringify(entry.args) }),
  );
  if ($("trace").firstElementChild?.classList.contains("muted"))
    $("trace").replaceChildren();
  $("trace").append(li);
  li.dataset.state = "run";
  let res;
  try {
    res = await client.callTool({ name, arguments: args });
  } catch (e) {
    entry.ok = false;
    li.dataset.state = "fail";
    throw new Error(`${name} failed: ${e.message || e}`);
  }
  const text = (res.content || [])
    .filter((c) => c.type === "text")
    .map((c) => c.text)
    .join("");
  if (res.isError) {
    entry.ok = false;
    li.dataset.state = "fail";
    throw new Error(`${name} returned an error: ${text.slice(0, 300)}`);
  }
  entry.ok = true;
  li.dataset.state = "ok";
  return text;
}

function editArgs(path, oldText, newText) {
  const props =
    tools.find((t) => t.name === "edit")?.inputSchema?.properties || {};
  return props.edits
    ? { path, edits: [{ oldText, newText }] }
    : { path, oldText, newText };
}

const hex = async (bytes) =>
  [...new Uint8Array(await crypto.subtle.digest("SHA-256", bytes))]
    .map((b) => b.toString(16).padStart(2, "0"))
    .join("");

function validate() {
  const v = $("inscription").value;
  let msg = "";
  if (!v.trim()) msg = "Write at least one character.";
  else if (v.length > 48) msg = "Keep it to 48 characters.";
  else if (/[\p{C}\p{Z}]/u.test(v.replaceAll(" ", "")))
    msg = "Use a single line of printable text.";
  $("err").textContent = msg;
  $("inscription").setAttribute("aria-invalid", msg ? "true" : "false");
  $("count").textContent = `${[...v].length} / 48`;
  return !msg;
}
function refreshButton() {
  $("issue").disabled = !client || busy || !validate();
}

async function issue() {
  if (!client || busy || !validate()) return;
  busy = true;
  refreshButton();
  $("issue").textContent = "Issuing…";
  $("trace").replaceChildren();
  const museum = document.querySelector("input[name=museum]:checked").value;
  const inscription = $("inscription").value;
  const next = JSON.stringify({ museum, inscription }, null, 2) + "\n";
  try {
    if (lastOrderText === null) {
      await call("write", { path: ORDER_PATH, content: next });
    } else if (lastOrderText !== next) {
      await call("edit", editArgs(ORDER_PATH, lastOrderText, next));
    }
    lastOrderText = next;
    const out = await call("bash", { command: COMMAND });
    let receipt;
    try {
      receipt = JSON.parse(out.trim().split("\n").pop());
    } catch {
      throw new Error(
        "The press did not return a receipt: " + out.slice(0, 200),
      );
    }
    if (receipt.museum !== museum || receipt.inscription !== inscription)
      throw new Error(
        "The shared desk has a different order. Reload to start a new visit.",
      );
    const svg = await call("read", { path: SVG_PATH });
    const bytes = new TextEncoder().encode(svg);
    const sha = await hex(bytes);
    if (!svg.startsWith("<svg") || !svg.endsWith("</svg>"))
      throw new Error("The read result is not a complete SVG.");
    if (bytes.length !== receipt.svg_bytes || sha !== receipt.svg_sha256)
      throw new Error(
        `Read bytes (${bytes.length}, ${sha.slice(0, 12)}…) differ from the press receipt (${receipt.svg_bytes}, ${String(receipt.svg_sha256).slice(0, 12)}…).`,
      );
    show(svg, bytes, sha, receipt);
  } catch (e) {
    $("stage").replaceChildren(
      el("p", { className: "error", role: "alert", textContent: e.message }),
    );
    $("actions").hidden = true;
    $("receipt").hidden = true;
  } finally {
    busy = false;
    $("issue").textContent = "Issue postcard";
    refreshButton();
  }
}

function show(svg, bytes, sha, receipt) {
  const url = URL.createObjectURL(new Blob([bytes], { type: "image/svg+xml" }));
  const doc = new DOMParser().parseFromString(
    svg,
    "image/svg+xml",
  ).documentElement;
  const [, , vw, vh] = (doc.getAttribute("viewBox") || "").split(/\s+/);
  const n = ++issuedCount;
  const file = `postcard-${receipt.museum}-${n}-${sha.slice(0, 8)}.svg`;
  const img = el("img", {
    src: url,
    alt: `Postcard: ${receipt.title}, inscribed “${receipt.inscription}”. ${receipt.credit}, ${receipt.accession}.`,
  });
  $("stage").replaceChildren(img);
  const dl = $("download");
  dl.href = url;
  dl.download = file;
  dl.textContent = `Download ${file}`;
  $("facts").textContent =
    `${doc.getAttribute("width")} × ${doc.getAttribute("height")} · viewBox ${vw} × ${vh} · ${bytes.length.toLocaleString()} bytes`;
  $("actions").hidden = false;
  const rows = [
    ["Painting", receipt.title],
    ["Credit", `${receipt.credit} · ${receipt.accession}`],
    ["Inscription", receipt.inscription],
    ["SVG bytes", `${receipt.svg_bytes} (read ${bytes.length}, match)`],
    ["SHA-256", sha],
  ];
  $("receipt").replaceChildren(
    ...rows.flatMap(([k, v]) => [
      el("dt", { textContent: k }),
      el("dd", { textContent: v }),
    ]),
  );
  $("receipt").hidden = false;
  if ($("issued").firstElementChild?.classList.contains("muted"))
    $("issued").replaceChildren();
  $("issued").prepend(
    el(
      "li",
      {},
      el("img", { src: url, alt: "" }),
      el("a", { href: url, download: file, textContent: file }),
      el("span", {
        className: "sub",
        textContent: ` “${receipt.inscription}”`,
      }),
    ),
  );
}

function wire() {
  $("inscription").addEventListener("input", refreshButton);
  document.querySelectorAll("input[name=museum]").forEach((r) =>
    r.addEventListener("change", () => {
      document
        .querySelectorAll(".work")
        .forEach((w) =>
          w.classList.toggle(
            "on",
            w.contains(document.querySelector("input[name=museum]:checked")),
          ),
        );
    }),
  );
  $("issue").addEventListener("click", issue);
  $("inscription").addEventListener("keydown", (e) => {
    if (e.key === "Enter") {
      e.preventDefault();
      issue();
    }
  });
  document.querySelector(".work").classList.add("on");
}

async function connect() {
  try {
    const c = new Client({ name: "postcard-press-ui", version: "1.0.0" });
    await c.connect(
      new StreamableHTTPClientTransport(new URL("/mcp", location.href)),
    );
    tools = (await c.listTools()).tools;
    client = c;
    const names = tools.map((t) => t.name);
    const info = c.getServerVersion();
    setStatus(
      "ready",
      `Connected to ${info?.name || "Computer"} · ${names.length} tools ready`,
    );
    $("tools").replaceChildren(
      ...tools.map((t) =>
        el(
          "li",
          {},
          el("code", { textContent: t.name }),
          " ",
          el("span", { textContent: (t.description || "").split(".")[0] }),
        ),
      ),
    );
    window.pressTools = names;
  } catch (e) {
    setStatus("error", `Not connected: ${e.message || e}`);
  }
  refreshButton();
}

wire();
validate();
connect();
