// Pi persists OpenAI reasoning_details with the originating assistant message. Gemini's
// OpenAI endpoint puts the same opaque replay data on a particular function call.
const format = "google-gemini-v1";

export function geminiRequest(params, google) {
  for (const message of params.messages) {
    if (!Array.isArray(message.reasoning_details)) continue;
    const other = [];
    for (const detail of message.reasoning_details) {
      if (detail.type !== "reasoning.encrypted" || detail.format !== format) {
        other.push(detail);
        continue;
      }
      if (!google) continue; // Only Gemini accepts its stored call signatures.
      const call = message.tool_calls?.find((call) => call.id === detail.id);
      if (call) call.extra_content = { ...call.extra_content, google: { ...call.extra_content?.google, thought_signature: detail.data } };
    }
    if (other.length) message.reasoning_details = other;
    else delete message.reasoning_details;
  }
}

// The identifier and signature may arrive in different deltas of a parallel call.
export function geminiChunks() {
  const choices = new Map();
  return (chunk) => {
    for (const [i, choice] of (chunk.choices || []).entries()) {
      const number = choice.index ?? i;
      let calls = choices.get(number);
      for (const [j, call] of (choice.delta?.tool_calls || []).entries()) {
        if (!calls) {
          calls = new Map();
          choices.set(number, calls);
        }
        const index = call.index ?? j;
        const saved = calls.get(index) ?? {};
        calls.set(index, saved);
        if (call.id) saved.id = call.id;
        if (call.extra_content?.google?.thought_signature) saved.signature = call.extra_content.google.thought_signature;
        if (saved.id && saved.signature) {
          (choice.delta.reasoning_details ??= []).push({
            type: "reasoning.encrypted", format, id: saved.id, data: saved.signature,
          });
          delete saved.signature;
        }
      }
      if (calls && choice.finish_reason === "stop") choice.finish_reason = "tool_calls";
    }
  };
}
