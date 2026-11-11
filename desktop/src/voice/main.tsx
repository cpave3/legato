import { StrictMode } from "react"
import { createRoot } from "react-dom/client"
import "@fontsource/geist-mono/400.css"
import "./widget.css"
import { Widget } from "./Widget"

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <Widget />
  </StrictMode>,
)
