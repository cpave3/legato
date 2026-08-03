import { AlertCircle, AlertTriangle, CheckCircle2, Info } from "lucide-react"
import type { HTMLAttributes } from "react"

import { cn } from "../../lib/utils"

type InlineNoticeVariant = "info" | "success" | "warning" | "error"

const variants = {
  info: { className: "border-indigo-800/70 bg-indigo-950/40 text-indigo-200", Icon: Info },
  success: { className: "border-emerald-800/70 bg-emerald-950/40 text-emerald-200", Icon: CheckCircle2 },
  warning: { className: "border-amber-800/70 bg-amber-950/40 text-amber-200", Icon: AlertTriangle },
  error: { className: "border-red-800/70 bg-red-950/40 text-red-200", Icon: AlertCircle },
} satisfies Record<InlineNoticeVariant, { className: string; Icon: typeof Info }>

interface InlineNoticeProps extends HTMLAttributes<HTMLDivElement> {
  variant?: InlineNoticeVariant
}

export function InlineNotice({ variant = "info", className, children, role, ...props }: InlineNoticeProps) {
  const { className: variantClassName, Icon } = variants[variant]

  return (
    <div
      role={role ?? (variant === "error" ? "alert" : "status")}
      className={cn("flex items-start gap-2 rounded-md border px-3 py-2 text-sm", variantClassName, className)}
      {...props}
    >
      <Icon aria-hidden="true" className="mt-0.5 size-4 shrink-0" />
      <div className="min-w-0">{children}</div>
    </div>
  )
}
