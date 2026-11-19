import { readProbedComponent } from "./main-world"

// The entry of scope-source.js, which the service worker runs in the page's
// MAIN world with chrome.scripting.executeScript. The build makes the bundle
// one expression whose value is this, and executeScript answers a script's
// last value; so the reading goes back through Chrome, not through a message
// other scripts in the page could listen to. The page controls this world
// and can forge it all the same: the service worker checks it
// (scope-source/lookup.ts).
export default readProbedComponent(document)
