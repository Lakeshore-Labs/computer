"use strict";

const JOBS = JSON.parse(document.getElementById("jobs-data").textContent);
const $ = (id) => document.getElementById(id);
const decoder = new TextDecoder("utf-8");
const MONO = 'ui-monospace, Menlo, Consolas, monospace';

const state = { job: 0, stage: "inspect" };
// Per job: decoded bytes, text, and whether the bytes match every recorded sha-256.
const tickets = [];
let hashError = null;

function escapeHtml(text) {
  const map = { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" };
  return String(text).replace(/[&<>"]/g, (c) => map[c]);
}

/** Make spaces and newlines visible in the saved label. */
function visible(text) {
  return escapeHtml(text)
    .replace(/ /g, '<span class="sp">·</span>')
    .replace(/\n/g, '<span class="sp">↵</span>');
}

function base64ToBytes(b64) {
  return Uint8Array.from(atob(b64), (c) => c.charCodeAt(0));
}

async function sha256Hex(bytes) {
  const digest = await crypto.subtle.digest("SHA-256", bytes);
  return Array.from(new Uint8Array(digest), (b) => b.toString(16).padStart(2, "0")).join("");
}

/** Decode every ticket and verify it with the browser's own SHA-256. */
async function loadTickets() {
  for (const job of JOBS) {
    if (!job.ticket) {
      tickets.push(null);
      continue;
    }
    const bytes = base64ToBytes(job.ticket.b64);
    const sha = await sha256Hex(bytes);
    tickets.push({
      bytes,
      text: decoder.decode(bytes),
      sha,
      verified: sha === job.ticket.sha256,
    });
  }
}

/** Draw the label from the ZPL text itself (^PW, ^LL, ^FO ... ^A0N ... ^FD ... ^FS). */
function zplPreview(text) {
  const width = Number((/\^PW(\d+)/.exec(text) || [0, 600])[1]);
  const height = Number((/\^LL(\d+)/.exec(text) || [0, 220])[1]);
  const field = /\^FO(\d+),(\d+)\^A0N,(\d+),(\d+)\^FD(.*?)\^FS/g;
  const spoken = [];
  let svg = "";
  for (const m of text.matchAll(field)) {
    const [, x, y, h, w, value] = m;
    const room = Math.min(width - Number(x) - 20, value.length * Number(w) * 0.95);
    spoken.push(value);
    svg +=
      `<text x="${x}" y="${Number(y) + Number(h) * 0.8}" font-size="${h}" font-weight="700" ` +
      `font-family="Helvetica, Arial, sans-serif" textLength="${room}" ` +
      `lengthAdjust="spacingAndGlyphs">${escapeHtml(value)}</text>`;
  }
  const alt = escapeHtml("Label preview: " + spoken.join(", "));
  return `<svg viewBox="0 0 ${width} ${height}" role="img" aria-label="${alt}">${svg}</svg>`;
}

function blankLabel(job, caption) {
  const draft = job.saved.split("\n")[0];
  const draftWidth = Math.min(540, draft.length * 16);
  return (
    `<svg viewBox="0 0 600 220" role="img" aria-label="Blank label: ${escapeHtml(caption)}">` +
    `<text x="30" y="70" font-size="30" fill="#777" font-family="Helvetica, Arial, sans-serif">Waiting at the desk</text>` +
    `<text x="30" y="140" font-size="26" fill="#777" font-family="${MONO}" textLength="${draftWidth}" lengthAdjust="spacingAndGlyphs">${escapeHtml(draft)}</text>` +
    `<text x="30" y="190" font-size="22" fill="#777" font-family="Helvetica, Arial, sans-serif">${escapeHtml(caption)}</text></svg>`
  );
}

function buildControls() {
  $("jobs").innerHTML = JOBS.map(
    (job, i) =>
      `<label><input type="radio" name="job" value="${i}"${i === 0 ? " checked" : ""}>` +
      `<span><span class="dot" style="background:${job.ribbonHex}"></span>` +
      `${escapeHtml(job.shelf)} ${escapeHtml(job.ribbon)}` +
      `<small>${escapeHtml(job.id)}${job.control ? " (control)" : ""}</small></span></label>`
  ).join("");
  $("stages").innerHTML = ["inspect", "issue"]
    .map(
      (key) =>
        `<label><input type="radio" name="stage" value="${key}"${key === "inspect" ? " checked" : ""}>` +
        `<span><b data-title></b><small data-sub></small></span></label>`
    )
    .join("");
}

function currentStage() {
  return JOBS[state.job].stages.find((s) => s.key === state.stage);
}

function stageSubtitle(stage) {
  if (stage.decision === null) return "not needed";
  return stage.decision;
}

function fact(name, key, html) {
  return `<dt>${name}</dt><dd data-fact="${key}">${html}</dd>`;
}

function render() {
  const job = JOBS[state.job];
  const ticket = tickets[state.job];
  const stage = currentStage();
  const missing = stage.decision === null;
  const available = stage.queued && ticket;
  const held = !missing && !stage.queued;

  // Stage radios: names are fixed, subtitles and availability come from the evidence.
  document.querySelectorAll("#stages label").forEach((label, i) => {
    const s = job.stages[i];
    label.querySelector("[data-title]").textContent = s.title;
    label.querySelector("[data-sub]").textContent = stageSubtitle(s);
    const input = label.querySelector("input");
    input.disabled = s.decision === null;
    input.checked = s.key === state.stage;
  });

  $("stripe").style.background = job.ribbonHex;
  $("paper").className = "paper" + (held ? " held" : "") + (available && stage.key === "inspect" ? " accepted" : "");
  $("stamp").textContent = held ? "HELD" : "ACCEPTED";
  $("labelbox").innerHTML = available
    ? zplPreview(ticket.text)
    : blankLabel(job, held ? "held: no ticket queued" : "no ticket queued");

  const sameAsSource = job.saved === job.source;
  const rows = [
    fact("Shelf", "shelf", escapeHtml(job.shelf)),
    fact("Ribbon", "ribbon", `<span class="dot" style="background:${job.ribbonHex}"></span>${escapeHtml(job.ribbon)}`),
    fact(
      "Saved label",
      "saved",
      `<span class="mono">${visible(job.saved)}</span> ` +
        `<span class="${sameAsSource ? "ok" : "bad"}">${sameAsSource ? "matches source" : "differs from source"}</span>`
    ),
    fact("Decision", "decision", escapeHtml(stage.decision)),
    fact("Queue file", "queue", stage.queued ? "present" : "none"),
  ];
  if (job.control) {
    rows.push(fact("Model", "model", "none — desk control, no model run"));
  } else {
    rows.push(
      fact("Model", "model", escapeHtml(job.model)),
      fact("Model digest", "digest", `<span class="mono">${escapeHtml(job.modelDigest.slice(0, 16))}…</span>`)
    );
    rows.push(fact("Tools", "tools", job.tools.map((t) => `<span class="chip mono">${escapeHtml(t)}</span>`).join("")));
  }
  if (available) {
    const verdict = ticket.verified
      ? '<span class="ok">verified</span>'
      : '<span class="bad">MISMATCH</span>';
    rows.push(
      fact("ZPL sha-256", "sha", `<span class="mono">${ticket.sha.slice(0, 16)}…</span> ${verdict}`),
      fact("Size", "size", ticket.bytes.length + " bytes")
    );
  }
  $("facts").innerHTML = rows.join("");

  $("zpl").textContent = available ? ticket.text : "(no ZPL queued at this stage)";
  const button = $("dl");
  button.disabled = !(available && ticket.verified);
  button.textContent = available ? "Download " + job.ticket.name : "Download ZPL (nothing queued)";

  const verb = stage.key === "inspect" ? "Inspect" : "Issue";
  $("status").textContent =
    `${job.shelf} ${job.ribbon} — ${verb}: ${stage.decision}. ` +
    (stage.queued ? "Queue file present." : "No queue file.");
  document.body.dataset.job = job.id;
  document.body.dataset.stage = state.stage;
}

function download() {
  const job = JOBS[state.job];
  const ticket = tickets[state.job];
  if (!ticket || !currentStage().queued || !ticket.verified) return;
  const url = URL.createObjectURL(new Blob([ticket.bytes], { type: "application/octet-stream" }));
  const link = document.createElement("a");
  link.href = url;
  link.download = job.ticket.name;
  document.body.appendChild(link);
  link.click();
  link.remove();
  setTimeout(() => URL.revokeObjectURL(url), 2000);
  $("status").textContent = `Saved ${job.ticket.name} (${ticket.bytes.length} bytes). Nothing was printed.`;
}

async function start() {
  buildControls();
  document.addEventListener("change", (event) => {
    if (event.target.name === "job") {
      state.job = Number(event.target.value);
      const stage = currentStage();
      if (stage.decision === null) state.stage = "inspect";
    }
    if (event.target.name === "stage") state.stage = event.target.value;
    render();
  });
  $("dl").addEventListener("click", download);
  $("status").textContent = "Verifying tickets…";
  try {
    await loadTickets();
  } catch (error) {
    hashError = error;
    JOBS.forEach((_, i) => (tickets[i] = tickets[i] || null));
  }
  render();
  if (hashError) $("status").textContent = "SHA-256 verification is unavailable in this browser; downloads are disabled.";
  document.body.dataset.ready = "true";
}

start();
