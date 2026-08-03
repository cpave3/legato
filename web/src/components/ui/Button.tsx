import { Slot } from "@radix-ui/react-slot"
import { cva, type VariantProps } from "class-variance-authority"
import type { ButtonHTMLAttributes } from "react"

import { cn } from "../../lib/utils"

const buttonVariants = cva(
  "inline-flex shrink-0 items-center justify-center gap-2 rounded-md text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-indigo-400 focus-visible:ring-offset-2 focus-visible:ring-offset-zinc-950 disabled:pointer-events-none disabled:cursor-not-allowed disabled:opacity-50 [&_svg]:shrink-0",
  {
    variants: {
      variant: {
        primary: "bg-indigo-600 text-white hover:bg-indigo-500",
        secondary: "bg-zinc-800 text-zinc-100 hover:bg-zinc-700",
        outline: "border border-zinc-700 bg-transparent text-zinc-300 hover:bg-zinc-800 hover:text-white",
        ghost: "text-zinc-400 hover:bg-zinc-800 hover:text-zinc-100",
        success: "bg-emerald-600 text-white hover:bg-emerald-500",
        warning: "bg-amber-600 text-white hover:bg-amber-500",
        destructive: "bg-red-600 text-white hover:bg-red-500",
      },
      size: {
        sm: "min-h-9 px-3 text-xs",
        default: "min-h-11 px-4",
        icon: "size-11 p-0",
        "icon-sm": "size-9 p-0",
      },
    },
    defaultVariants: {
      variant: "primary",
      size: "default",
    },
  },
)

export interface ButtonProps
  extends ButtonHTMLAttributes<HTMLButtonElement>,
    VariantProps<typeof buttonVariants> {
  asChild?: boolean
}

export function Button({ asChild = false, className, type, variant, size, ...props }: ButtonProps) {
  const Component = asChild ? Slot : "button"

  return (
    <Component
      className={cn(buttonVariants({ variant, size }), className)}
      {...(!asChild ? { type: type ?? "button" } : {})}
      {...props}
    />
  )
}

export { buttonVariants }
