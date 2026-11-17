import { plainOutput, type NativeBackgroundShell } from "../lib/backgroundShells"

// ShellOutput is what a background shell printed, newest last, as its tile
// shows it: a closed tile keeps the end in view, an opened one follows it.
// Read-only: nothing is sent to the shell.
export function ShellOutput({ shell }: { shell: NativeBackgroundShell }) {
  const output = shell.output
  if (!output?.available) {
    return <p className="m-0 text-xs text-lw-faint">Output isn’t available for this agent yet. Its command, state and exit code are.</p>
  }
  const text = plainOutput(output.text)
  if (!text.trim()) {
    return <p className="m-0 text-xs text-lw-faint">{shell.status === "running" ? "No output yet." : "It printed nothing."}</p>
  }
  return (
    <>
      {output.truncated && <p className="m-0 mb-1 text-[11px] text-lw-faint">Earlier output is not kept.</p>}
      <pre aria-label="Output" className="m-0 whitespace-pre-wrap break-words font-lw-mono text-[11.5px] leading-[1.45] text-lw-text-2">{text}</pre>
    </>
  )
}
