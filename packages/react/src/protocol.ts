// What the Terminal and computer.TerminalHandler say to each other, apart from the DOM.

/** socketUrl is the WebSocket URL for url: as it is when it is ws: or wss:, and otherwise from the page's origin,
 * wss: on an https: page and ws: on an http: one. */
export function socketUrl(url: string, page: { href: string }): string {
  const u = new URL(url, page.href)
  if (u.protocol === "http:") u.protocol = "ws:"
  if (u.protocol === "https:") u.protocol = "wss:"
  return u.href
}

/** commandEnded is the exit code in the data of an OSC 133 sequence the prompt writes when a command ends ("D;0"),
 * or undefined for another of its marks. */
export function commandEnded(data: string): number | undefined {
  const [mark, code] = data.split(";")
  if (mark !== "D") return undefined
  const n = Number(code)
  return code !== undefined && code !== "" && Number.isInteger(n) ? n : 0
}

/** resize is the message that tells the prompt the terminal's size. */
export function resize(cols: number, rows: number): string {
  return JSON.stringify({ cols, rows })
}

/** theme is the terminal's colours, light or dark as the page is. */
export function theme(dark: boolean) {
  return dark
    ? { background: "#0b0b0c", foreground: "#e6e6e6", cursor: "#e6e6e6", selectionBackground: "#3a3d41" }
    : { background: "#ffffff", foreground: "#1f2328", cursor: "#1f2328", selectionBackground: "#c8d7f1", cyan: "#0b7285" }
}
