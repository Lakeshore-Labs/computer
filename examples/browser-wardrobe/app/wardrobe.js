const $ = (id) => document.getElementById(id);
const cast = ["Alex", "Blair", "Casey", "Drew", "Eden", "Frankie", "Jules"];
const root = await navigator.storage.getDirectory();
const rack = await root.getDirectoryHandle("wardrobe", { create: true });
let channel = null;
const updates = new BroadcastChannel("wardrobe-file-updates");
async function read(name) {
  try {
    const handle = await rack.getFileHandle(name);
    return JSON.parse(await (await handle.getFile()).text());
  } catch (e) {
    if (e.name === "NotFoundError") return null;
    throw e;
  }
}
async function write(name, value) {
  const handle = await rack.getFileHandle(name, { create: true });
  const writer = await handle.createWritable();
  await writer.write(JSON.stringify(value, null, 2) + "\n");
  await writer.close();
}
async function revision(stock) {
  const bytes = new TextEncoder().encode(JSON.stringify(stock));
  return Array.from(
    new Uint8Array(await crypto.subtle.digest("SHA-256", bytes)),
    (n) => n.toString(16).padStart(2, "0"),
  ).join("");
}
async function inventory() {
  const stock = await read("inventory.json");
  return { stock, revision: await revision(stock) };
}
await navigator.locks.request("wardrobe-write", async () => {
  if (!(await read("inventory.json")))
    await write("inventory.json", { S: 4, M: 5, L: 3 });
});
async function refresh() {
  const { stock, revision: current } = await inventory(),
    plan = await read("allocation.json");
  $("stock").replaceChildren();
  for (const [size, n] of Object.entries(stock)) {
    const cell = document.createElement("div");
    cell.className = "size" + (size === "M" ? " medium" : "");
    const label = document.createElement("span"),
      value = document.createElement("b");
    label.textContent = size;
    value.textContent = n;
    cell.append(label, value);
    $("stock").append(cell);
  }
  $("medium").value = stock.M;
  const stale = plan && plan.revision !== current;
  $("plan-summary").className = stale ? "stale" : "";
  $("plan-summary").textContent = stale
    ? "Stock changed. Run Computer to update the fitting list."
    : plan
      ? plan.allocated +
        " fitted · " +
        (plan.short
          ? plan.short + " still need jackets"
          : plan.spare + " spare on the rack")
      : "Run Computer to make the first plan.";
  $("cast").replaceChildren();
  cast.forEach((name, i) => {
    const li = document.createElement("li"),
      b = document.createElement("b"),
      span = document.createElement("span");
    b.textContent = name;
    const ready = plan && i < plan.allocated;
    li.className = "tag " + (!plan || stale ? "" : ready ? "ready" : "waiting");
    span.textContent = !plan
      ? "Unassigned"
      : stale
        ? "Recounted"
        : ready
          ? "Fitted"
          : "Needs jacket";
    li.append(b, span);
    $("cast").append(li);
  });
  const names = [];
  for await (const name of rack.keys()) names.push(name);
  $("files").textContent = names.sort().join(" · ");
  $("json").textContent = plan
    ? JSON.stringify(plan, null, 2)
    : "No plan saved.";
  document.body.dataset.ready = "true";
}
updates.onmessage = () => refresh();
$("count").onsubmit = async (e) => {
  e.preventDefault();
  const n = $("medium").valueAsNumber;
  if (!Number.isInteger(n) || n < 0 || n > 99) return;
  await navigator.locks.request("wardrobe-write", async () => {
    const { stock } = await inventory();
    stock.M = n;
    await write("inventory.json", stock);
  });
  await refresh();
  updates.postMessage("inventory");
  $("saved").textContent = "Saved " + n + " medium jackets in this browser.";
};
$("connect").onclick = () => {
  if (channel) return;
  channel = new EventSource("/events");
  channel.onopen = () => {
    $("status").textContent = "This tab is connected.";
    $("run").disabled = false;
    $("connect").disabled = true;
  };
  channel.onerror = () => {
    $("run").disabled = true;
    $("status").textContent = "Waiting for the local connection.";
  };
  channel.onmessage = async (event) => {
    const { id, op, input } = JSON.parse(event.data);
    let reply;
    try {
      const value = await navigator.locks.request(
        "wardrobe-write",
        async () => {
          const current = await inventory();
          if (op === "read") return current;
          if (op !== "save" || input.revision !== current.revision)
            throw new Error("Inventory changed. Recompute the plan.");
          await write("allocation.json", input);
          await refresh();
          updates.postMessage("plan");
          return { saved: true };
        },
      );
      reply = { id, ok: true, value };
    } catch (e) {
      reply = { id, ok: false, error: e.message };
    }
    await fetch("/reply", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(reply),
    });
  };
};
$("run").onclick = async () => {
  $("run").disabled = true;
  $("status").textContent = "Computer is reading the browser inventory…";
  try {
    const result = await fetch("/plan", { method: "POST" }),
      saved = await result.json();
    if (!result.ok) throw new Error(saved.error || "Plan failed");
    await refresh();
    $("status").textContent = "Computer saved the fitting list.";
  } catch (e) {
    $("status").textContent = e.message;
  } finally {
    $("run").disabled = channel?.readyState !== EventSource.OPEN;
  }
};
await refresh();
