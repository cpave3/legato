import { createRoot } from "react-dom/client"
import { App } from "./App"
import "./styles.css"
import { initializeTheme } from "./theme"

const root = createRoot(document.getElementById("root")!)
initializeTheme().then(() => root.render(<App />)).catch(error => {
  root.render(<p className="error" role="alert">Could not load app theme: {String(error)}</p>)
})
