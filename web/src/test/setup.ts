import { cleanup } from "@testing-library/react"
import { afterEach } from "vitest"

// Testing Library unmounts after each test only when vitest's globals are on,
// and they are off here. Without this a test's components stay mounted into
// the next test, and the last test's into the end of its file, where React's
// pending work runs after jsdom is gone and fails the run with "window is not
// defined" though every test passed.
afterEach(() => cleanup())

// jsdom has no canvas: getContext returns null after printing that it is not
// implemented. xterm asks for one as soon as it loads, so any test that loads
// a page with a terminal printed that. Answer the same null, quietly.
HTMLCanvasElement.prototype.getContext = (() => null) as typeof HTMLCanvasElement.prototype.getContext

// jsdom has no media playback either: play, pause and load print that they are
// not implemented. A page that holds a video or a take calls them; tests that
// care spy on them. Do nothing, quietly, as a paused element would.
HTMLMediaElement.prototype.pause = () => {}
HTMLMediaElement.prototype.load = () => {}
HTMLMediaElement.prototype.play = () => Promise.resolve()
