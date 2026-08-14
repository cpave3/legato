declare module "@wiremark/core" {
  export interface WiremarkDiagnostic {
    level?: string
    message: string
    line?: number
  }

  export interface WiremarkRenderOptions {
    theme?: "light" | "dark"
    interactive?: boolean
  }

  export function render(source: string, options?: WiremarkRenderOptions): {
    svg: string
    diagnostics: WiremarkDiagnostic[]
  }
}
