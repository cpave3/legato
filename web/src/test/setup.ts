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
