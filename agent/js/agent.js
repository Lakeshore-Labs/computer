// agent: Pi Durable as a program of the computer. `agent "message"` gives the conversation one message and prints the
// run as JSON lines, as Pi Durable's own JSON mode does (its example 18-json): each line holds the transcript
// entries added since the last and the live state, `pi.live`, which is what a view of the conversation draws. The
// last line is {answer} or {unanswered}.
//
// The conversation is kept in Pi Durable's JSONL storage in AGENT_STATE and written at every commit, so the next
// `agent` continues it, and a run cut off by a crash goes on where it stopped.
import { env, exit, run, tell, whenStopped } from "./host.js";
import { BACKGROUND_CONTEXT as ctx } from "@earendil-works/chord/context";
import { createModels, createProvider } from "@earendil-works/pi-ai";
import * as openaiCompletions from "@earendil-works/pi-ai/api/openai-completions";
import { AssistantEntry, createRegistry, Harness } from "@earendil-works/pi-durable";
import { JsonlStorage } from "@earendil-works/pi-durable/storage/jsonl";
import { CodingTools } from "@earendil-works/pi-durable/tools";
import { computerEnv } from "./env.js";

run(async () => {
  const message = scriptArgs.slice(1).join(" ").trim();
  if (!message) { console.log('usage: agent "message"'); exit(2); }

  const home = env("HOME", "/home/user"), state = env("AGENT_STATE", "/var/lib/agent");
  const modelId = env("AGENT_MODEL", "default"), instructions = env("AGENT_INSTRUCTIONS", "");

  // The host knows the model's address and holds its key; the address here only names the provider.
  const model = { id: modelId, name: modelId, api: "openai-completions", provider: "host", baseUrl: "host:", reasoning: false, input: ["text"],
    cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 }, contextWindow: Number(env("AGENT_CONTEXT_WINDOW", "200000")), maxTokens: Number(env("AGENT_MAX_TOKENS", "16384")) };
  if (env("AGENT_MODEL_COMPAT") === "gemini") model.compat = { supportsStore: false };
  const models = createModels();
  models.setProvider(createProvider({ id: "host", name: "host", baseUrl: "host:", models: [model], api: openaiCompletions,
    auth: { apiKey: { name: "host", resolve: async () => ({ auth: { apiKey: "none" } }) } } }));

  const registry = createRegistry();
  registry.install(CodingTools); // read, write, edit and bash, which reach the computer only through the environment
  // Untagged and first, so the model reads the computer's own description as written.
  if (instructions) registry.install({ name: "computer", sections: [{ key: "computer", tag: false, render: () => instructions }] });

  // The conversation is in a directory of the agent program's own. The tools' environment refuses it, and no
  // command of the computer has it, so the model cannot rewrite what the next run reads.
  const storage = await JsonlStorage.open(state, computerEnv({ cwd: "/" }), ctx);
  const computer = computerEnv({ cwd: home, hidden: [state] });
  const harness = await Harness.open(storage, { models, registry, env: () => computer,
    onReport: (report) => console.log("agent:", report.kind, String(report.error?.message ?? report.error ?? "")) }, ctx);
  // A conversation keeps the agent it was made with, so each run sets it again: what this run was started with
  // holds, whatever the store says, and a model or limit the embedder has since changed takes effect.
  const root = await harness.root(ctx);
  await root.configure({ model: { provider: "host", modelId }, cwd: home, maxTurns: Number(env("AGENT_MAX_TURNS", "50")),
    instructions: null, extensions: null, tools: null, modelTools: null, compaction: null, thinkingLevel: null }, ctx);
  harness.resume(); // a run an earlier process was cut off in goes on
  whenStopped(() => void root.abort(ctx)); // the host's stop: queued input is withdrawn and the run's work aborted

  // Attached before the message is given, so the watch covers the whole run.
  const watch = await root.watch(ctx);
  let newest = watch.value.entries.at(-1)?.id ?? 0;
  watch.start(async (call) => {
    if (call.kind !== "value") return;
    const entries = call.value.entries.filter((entry) => entry.id > newest);
    newest = call.value.entries.at(-1)?.id ?? newest;
    tell({ ...(entries.length ? { entries } : {}), live: call.value.docs["pi.live"] });
  });

  const submission = await root.submit({ type: "input", content: message }, ctx);
  const settled = await submission.wait(ctx);
  await harness.waitForIdle(ctx);
  await new Promise((resolve) => setTimeout(resolve, 0)); // the last commits reach the watch before it stops
  await watch.stop();
  let code = 1;
  if (settled.status === "done" && settled.type === "input") {
    const entry = await root.commit((tx) => tx.entry(AssistantEntry, settled.answer), ctx);
    tell({ answer: (entry?.model?.[0]?.content ?? []).flatMap((c) => (c.type === "text" ? [c.text] : [])).join("") });
    code = 0;
  } else {
    tell({ unanswered: settled.status === "unanswered" ? settled.reason : settled.status });
  }
  await harness.close(ctx);
  exit(code);
});
