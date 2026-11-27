import { useEffect, useRef, useState } from "react"
import { Terminal as XTerm } from "@xterm/xterm"
import { FitAddon } from "@xterm/addon-fit"

import { commandEnded, resize, socketUrl, theme } from "./protocol"

export type TerminalProps = {
  /** The WebSocket of computer.TerminalHandler: ws: or wss:, or a path, which is from the page's origin. */
  url: string
  className?: string
  /** Called when a command at the prompt ends, with its exit code: 130 for one ended by Ctrl-C. */
  onCommandEnd?: (exitCode: number) => void
}

const dark = () => document.documentElement.classList.contains("dark")

/** Terminal is a computer's prompt in the page: it fills its parent, follows the page's dark class, and when the
 * connection closes offers to connect again, which it does only when asked. Its styles are xterm.js's: import
 * "@lakeshore-labs/computer-react/terminal.css" once. */
export function Terminal({ url, className, onCommandEnd }: TerminalProps) {
  const parent = useRef<HTMLDivElement>(null)
  const ended = useRef(onCommandEnd)
  const [connection, setConnection] = useState(0) // a new one connects again
  const [closed, setClosed] = useState(false)
  const [isDark, setDark] = useState(dark)
  const term = useRef<XTerm | null>(null)

  useEffect(() => {
    ended.current = onCommandEnd
  }, [onCommandEnd])

  useEffect(() => {
    const themed = new MutationObserver(() => setDark(dark()))
    themed.observe(document.documentElement, { attributes: true, attributeFilter: ["class"] })
    return () => themed.disconnect()
  }, [])
  useEffect(() => {
    if (term.current) term.current.options.theme = theme(isDark)
  }, [isDark])

  useEffect(() => {
    const element = parent.current
    if (!element) return
    const xterm = new XTerm({
      cursorBlink: true,
      fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Consolas, "Liberation Mono", monospace',
      fontSize: 13,
      theme: theme(dark()),
    })
    const fit = new FitAddon()
    xterm.loadAddon(fit)
    xterm.open(element)
    term.current = xterm

    const socket = new WebSocket(socketUrl(url, window.location))
    socket.binaryType = "arraybuffer"
    const open = () => socket.readyState === WebSocket.OPEN
    const encoder = new TextEncoder()
    const refit = () => {
      if (element.clientWidth > 0 && element.clientHeight > 0) fit.fit()
    }
    socket.onopen = () => {
      refit()
      socket.send(resize(xterm.cols, xterm.rows))
      xterm.focus()
    }
    socket.onmessage = (event) => {
      if (event.data instanceof ArrayBuffer) xterm.write(new Uint8Array(event.data))
    }
    socket.onclose = () => setClosed(true)
    const typed = xterm.onData((data) => {
      if (open()) socket.send(encoder.encode(data))
    })
    const resized = xterm.onResize(({ cols, rows }) => {
      if (open()) socket.send(resize(cols, rows))
    })
    const mark = xterm.parser.registerOscHandler(133, (data) => {
      const code = commandEnded(data)
      if (code !== undefined) ended.current?.(code)
      return true
    })
    const sized = new ResizeObserver(refit)
    sized.observe(element)

    return () => {
      sized.disconnect()
      typed.dispose()
      resized.dispose()
      mark.dispose()
      socket.onclose = null
      socket.close()
      term.current = null
      xterm.dispose()
    }
  }, [url, connection])

  const colors = theme(isDark)
  return (
    <div className={className} style={{ position: "relative", width: "100%", height: "100%", overflow: "hidden", background: colors.background }}>
      <div ref={parent} style={{ position: "absolute", inset: 4 }} />
      {closed && (
        <div style={{ position: "absolute", right: 12, bottom: 12, display: "flex", gap: 8, alignItems: "center", font: "13px system-ui, sans-serif" }}>
          <span style={{ opacity: 0.7, color: colors.foreground }}>Disconnected</span>
          <button
            type="button"
            onClick={() => {
              setClosed(false)
              setConnection((n) => n + 1)
            }}
            style={{ font: "inherit", padding: "4px 10px", borderRadius: 6, border: "1px solid #8888", background: "transparent", color: colors.foreground, cursor: "pointer" }}
          >
            Reconnect
          </button>
        </div>
      )}
    </div>
  )
}
