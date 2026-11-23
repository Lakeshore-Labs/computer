export function checkInscription(v) {
  const length = [...v].length;
  let msg = "";
  if (!v.trim()) msg = "Write at least one character.";
  else if (length > 48) msg = "Keep it to 48 characters.";
  else if (/[\p{C}\p{Z}]/u.test(v.replaceAll(" ", "")))
    msg = "Use a single line of printable text.";
  return { length, error: msg };
}
