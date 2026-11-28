import { Handle, Position } from "@xyflow/react"

// FlowHandles are a node's two hidden handles. React Flow joins edges to
// handles, so every node has one of each; where a line runs comes from the
// layout's points, not from them. across: left to right, else top to bottom.
export function FlowHandles({ across }: { across: boolean }) {
  const hidden = "!h-1 !w-1 !min-w-0 !border-0 !bg-transparent"
  return <>
    <Handle id="t" type="target" position={across ? Position.Left : Position.Top} isConnectable={false} className={hidden} />
    <Handle id="s" type="source" position={across ? Position.Right : Position.Bottom} isConnectable={false} className={hidden} />
  </>
}
