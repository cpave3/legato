import { useEffect, useMemo, useRef, useState } from "react"
import { MessageCircleQuestion } from "lucide-react"
import { useServer } from "../hooks/useServer"
import { proposeComposerText, type AgentUIItem } from "../lib/agentUi"
import { clock, momentAt, onScreen, parseVideoManifest, posterAddress, questionAbout, videoAddress } from "../lib/agentVideo"

interface AgentVideoPlayerProps {
  item: AgentUIItem
  // fill: take the container's height (the side panel).
  fill?: boolean
}

// AgentVideoPlayer plays a narrated video an agent rendered, with the
// chapters and captions its manifest describes. It is Legato's own player:
// nothing the agent wrote runs here, and the manifest is read as data.
//
// Asking about the video puts what it showed at that moment in the agent's
// message box: the time, the sentence being said, the file and the lines
// highlighted. The person adds the question and sends it.
export function AgentVideoPlayer({ item, fill = false }: AgentVideoPlayerProps) {
  const { baseUrl } = useServer()
  const manifest = useMemo(() => parseVideoManifest(item.content), [item.content])
  const address = useMemo(() => videoAddress(baseUrl, item), [baseUrl, item])
  // The picture the video carries for itself: its title card. Without one
  // the browser shows the first frame.
  const poster = useMemo(() => posterAddress(baseUrl, item), [baseUrl, item])
  const videoRef = useRef<HTMLVideoElement>(null)
  const [ms, setMs] = useState(0)

  // Captions are drawn by the browser from a text track, so they keep time
  // with the picture and follow it to full screen. A video with captions in
  // its picture already gets none.
  useEffect(() => {
    const video = videoRef.current
    if (!video || !manifest || manifest.captionsBurned) return
    if (typeof video.addTextTrack !== "function" || typeof VTTCue === "undefined") return
    const track = video.addTextTrack("captions", "Captions", "en")
    for (const scene of manifest.scenes) {
      for (const cue of scene.cues ?? []) {
        if (cue.endMs > cue.startMs) track.addCue(new VTTCue(cue.startMs / 1000, cue.endMs / 1000, cue.text))
      }
    }
    track.mode = "showing"
    // A track cannot be removed from a video element; one that is left
    // behind is turned off.
    return () => { track.mode = "disabled" }
  }, [manifest, address])

  const wrapper = fill ? "min-h-0 flex-1 overflow-y-auto p-4 text-zinc-200" : "p-3 text-zinc-200"
  if (!manifest) {
    return <div className={wrapper}><p className="text-sm text-zinc-400">This video's description cannot be read, so it cannot be played here.</p></div>
  }
  if (!address) {
    return <div className={wrapper}><p className="text-sm text-zinc-400">Legato's copy of this video is not stored any more. The agent can render it and show it again.</p></div>
  }

  const moment = momentAt(manifest, ms)
  const title = item.title || "Video from the agent"

  function seek(toMs: number) {
    const video = videoRef.current
    if (!video) return
    video.currentTime = toMs / 1000
    setMs(toMs)
    void video.play()?.catch(() => {})
  }

  function ask() {
    const video = videoRef.current
    // The time is read from the video itself: the last update the page saw
    // may be a quarter of a second old.
    const at = video ? Math.round(video.currentTime * 1000) : ms
    video?.pause()
    proposeComposerText(item.task_id, questionAbout(item.title, momentAt(manifest!, at)))
  }

  return (
    <div className={wrapper}>
      <video
        ref={videoRef}
        src={address}
        poster={poster ?? undefined}
        controls
        playsInline
        preload="metadata"
        aria-label={title}
        onTimeUpdate={(event) => setMs(Math.round(event.currentTarget.currentTime * 1000))}
        onSeeked={(event) => setMs(Math.round(event.currentTarget.currentTime * 1000))}
        className="aspect-video w-full rounded-md border border-zinc-700 bg-black"
      />

      <div className="mt-3 flex items-start gap-3">
        <div className="min-w-0 flex-1 text-xs text-zinc-400" aria-live="off">
          <div className="truncate text-zinc-300">
            <span className="tabular-nums text-zinc-500">{clock(moment.ms)}</span>
            {moment.chapter && <span> · {moment.chapter}</span>}
          </div>
          <div className="truncate" title={onScreen(moment)}>On screen: {onScreen(moment)}</div>
        </div>
        <button
          type="button"
          onClick={ask}
          className="flex min-h-9 shrink-0 items-center gap-1.5 rounded-md border border-indigo-800 bg-indigo-950/50 px-3 text-xs font-medium text-indigo-100 transition-colors hover:border-indigo-600 hover:bg-indigo-900/60"
          title="Puts this moment of the video in your message box, for you to add a question"
        >
          <MessageCircleQuestion size={14} />
          Ask about this moment
        </button>
      </div>

      {manifest.chapters.length > 0 && (
        <nav aria-label="Chapters" className="mt-4">
          <h3 className="mb-1 text-[11px] font-semibold uppercase tracking-wider text-zinc-500">Chapters</h3>
          <ol className="divide-y divide-zinc-800 overflow-hidden rounded-md border border-zinc-800">
            {manifest.chapters.map((chapter) => {
              const current = moment.ms >= chapter.startMs && moment.ms < chapter.endMs
              return (
                <li key={`${chapter.startMs}:${chapter.title}`}>
                  <button
                    type="button"
                    onClick={() => seek(chapter.startMs)}
                    aria-current={current ? "true" : undefined}
                    className={`flex min-h-9 w-full items-center gap-3 px-3 py-1.5 text-left text-sm transition-colors hover:bg-zinc-800 ${current ? "bg-zinc-800/70 text-zinc-100" : "text-zinc-300"}`}
                  >
                    <span className="w-9 shrink-0 text-xs tabular-nums text-zinc-500">{clock(chapter.startMs)}</span>
                    <span className="min-w-0 flex-1 truncate">{chapter.title}</span>
                  </button>
                </li>
              )
            })}
          </ol>
        </nav>
      )}
    </div>
  )
}
