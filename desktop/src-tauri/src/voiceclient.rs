//! The app's side of a spoken conversation with the main's Reeve: the voice
//! session (`voiceproto`) over a WebSocket with the app's pinned TLS, the
//! microphone streamed to it, the Reeve's lines fetched and played, and
//! what the widget shows sent to it as events (it holds no token).
//!
//! `Conversation` decides everything and does nothing: each message from
//! the main, command from the person and line played gives `Effect`s, which
//! `run` carries out. The main decides when a turn ends and when the
//! conversation listens for a reply; the app streams while it listens.
//!
//! The microphone is muted while a line plays and for 300 ms after, so the
//! Reeve does not hear itself (no echo cancellation yet).

use std::collections::VecDeque;
use std::sync::Arc;
use std::time::{Duration, Instant};

use futures_util::{SinkExt, StreamExt};
use serde_json::{json, Value};
use tokio::sync::mpsc;
use tokio_tungstenite::tungstenite::Message;

use crate::legato::Socket;
use crate::voice::{self, Framer};
use crate::voicekey::VoiceKey;
use crate::voiceproto::{DeviceMessage, MainMessage};

/// How long the microphone stays muted after a line has played.
pub const AFTER_PLAYING: Duration = Duration::from_millis(300);

/// Whether the microphone is muted: while a line plays, and for
/// AFTER_PLAYING after.
#[derive(Debug, Default)]
pub struct Mute {
    playing: bool,
    until: Option<Instant>,
}

impl Mute {
    pub fn playing(&mut self) {
        self.playing = true;
    }

    pub fn played(&mut self, now: Instant) {
        self.playing = false;
        self.until = Some(now + AFTER_PLAYING);
    }

    pub fn muted(&self, now: Instant) -> bool {
        self.playing || self.until.is_some_and(|until| now < until)
    }
}

/// What the person asks of the conversation: the key, and the widget's
/// buttons.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Command {
    Key(VoiceKey),
    /// Stop the Reeve speaking (or thinking), keeping the conversation.
    Stop,
    /// Stop playback and listen.
    Reply,
    /// Send what was said now.
    SendNow,
    /// Drop the turn and end the conversation.
    Cancel,
    /// End the conversation (the widget closed).
    End,
}

impl Command {
    /// A command the widget sends by name.
    pub fn named(name: &str) -> Option<Command> {
        Some(match name {
            "stop" => Command::Stop,
            "reply" => Command::Reply,
            "send" => Command::SendNow,
            "cancel" => Command::Cancel,
            "end" => Command::End,
            _ => return None,
        })
    }
}

/// What the widget is told, each an app event (`name`, `payload`).
#[derive(Debug, Clone, PartialEq)]
pub enum Ui {
    /// `voice-state`: `connecting`, `not_ready`, `idle`, `listening`,
    /// `thinking`, `speaking`; reason says why (not ready: the line to show;
    /// idle: how the conversation ended).
    State { state: String, reason: Option<String>, setting: Option<String> },
    /// `voice-caption`: `partial`, `heard`, `activity` or `written`.
    Caption { kind: &'static str, text: String },
    /// `voice-line`: a line `queued`, `playing`, `played` or `dropped`.
    Line { id: String, text: String, status: &'static str },
    /// `voice-pause`: the pause bar.
    Pause { ms: u64, of: u64 },
    /// `voice-level`: the microphone, 0 to 1, every frame.
    Level(f64),
}

impl Ui {
    pub fn state(state: &str, reason: Option<&str>) -> Ui {
        Ui::State { state: state.into(), reason: reason.map(String::from), setting: None }
    }

    pub fn name(&self) -> &'static str {
        match self {
            Ui::State { .. } => "voice-state",
            Ui::Caption { .. } => "voice-caption",
            Ui::Line { .. } => "voice-line",
            Ui::Pause { .. } => "voice-pause",
            Ui::Level(_) => "voice-level",
        }
    }

    pub fn payload(&self) -> Value {
        match self {
            Ui::State { state, reason, setting } => json!({ "state": state, "reason": reason, "setting": setting }),
            Ui::Caption { kind, text } => json!({ "kind": kind, "text": text }),
            Ui::Line { id, text, status } => json!({ "id": id, "text": text, "status": status }),
            Ui::Pause { ms, of } => json!({ "ms": ms, "of": of }),
            Ui::Level(level) => json!({ "level": level }),
        }
    }
}

/// What `run` does for the conversation.
#[derive(Debug, Clone, PartialEq)]
pub enum Effect {
    Send(DeviceMessage),
    Ui(Ui),
    /// Fetch and play a line, then report it played.
    Play { id: String, url: String },
    /// Stop the line playing now.
    StopPlayback,
    /// Send the microphone's frames kept while the main was not listening.
    FlushAudio,
    /// The conversation is over: stop recording and close.
    Close,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum Phase {
    /// Hello sent, the main not heard yet.
    Opening,
    Idle,
    Listening,
    Thinking,
    Speaking,
}

/// What becomes of a frame of the microphone now.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Audio {
    /// To the main, which listens.
    Send,
    /// Kept until the main listens (it has been asked to).
    Hold,
    Drop,
}

/// Something the person asked before the main said listening, done when it
/// does.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum Pending {
    /// Send what was said (the key again, its release, Send now).
    Send,
    /// Stop listening (Stop).
    Stop,
}

#[derive(Debug, Clone)]
struct Line {
    id: String,
    text: String,
    url: String,
}

/// One conversation, from hello to its end.
#[derive(Debug)]
pub struct Conversation {
    phase: Phase,
    /// The main has been past idle: idle now ends the conversation.
    began: bool,
    /// The person stopped the Reeve: the idle that follows keeps the
    /// conversation, as the web UI's does.
    stopped: bool,
    /// Listen is asked (or about to be) and the main has not said listening.
    awaiting: bool,
    pending: Option<Pending>,
    queue: VecDeque<Line>,
    playing: Option<Line>,
    mute: Mute,
}

impl Default for Conversation {
    fn default() -> Self {
        Conversation::new()
    }
}

impl Conversation {
    pub fn new() -> Conversation {
        Conversation {
            phase: Phase::Opening,
            began: false,
            stopped: false,
            awaiting: true,
            pending: None,
            queue: VecDeque::new(),
            playing: None,
            mute: Mute::default(),
        }
    }

    /// The first frame: who listens, and whether it plays lines.
    pub fn hello(device: &str, play: bool) -> DeviceMessage {
        DeviceMessage::Hello { device: device.into(), client: "desktop".into(), play }
    }

    /// What becomes of a frame of the microphone now: sent while the main
    /// listens (or has just stopped, to catch speech that carries on); kept
    /// while it has been asked to listen and has not yet, so a short phrase
    /// is not lost; dropped while a line plays or just after.
    pub fn audio(&self, now: Instant) -> Audio {
        if self.mute.muted(now) {
            Audio::Drop
        } else if self.awaiting {
            Audio::Hold
        } else if matches!(self.phase, Phase::Listening | Phase::Thinking) {
            Audio::Send
        } else {
            Audio::Drop
        }
    }

    /// Asks the main to listen.
    fn listen(&mut self) -> Effect {
        self.awaiting = true;
        self.pending = None;
        Effect::Send(DeviceMessage::Listen)
    }

    pub fn on_main(&mut self, message: MainMessage, now: Instant) -> Vec<Effect> {
        let _ = now;
        match message {
            MainMessage::State { state, reason } => match state.as_str() {
                "idle" if !self.began => {
                    self.phase = Phase::Idle;
                    let pending = self.pending.take();
                    let listen = self.listen();
                    self.pending = pending;
                    vec![listen]
                }
                "idle" if std::mem::take(&mut self.stopped) => {
                    self.phase = Phase::Idle;
                    self.awaiting = false;
                    self.pending = None;
                    vec![Effect::Ui(Ui::state("idle", None))]
                }
                "idle" => {
                    self.phase = Phase::Idle;
                    self.end(reason.as_deref().unwrap_or("done"), true)
                }
                "listening" => {
                    self.began = true;
                    self.phase = Phase::Listening;
                    self.awaiting = false;
                    self.stopped = false;
                    let mut out = vec![Effect::Ui(Ui::state(&state, None)), Effect::FlushAudio];
                    match self.pending.take() {
                        Some(Pending::Send) => out.push(Effect::Send(DeviceMessage::Send)),
                        Some(Pending::Stop) => {
                            self.stopped = true;
                            out.push(Effect::Send(DeviceMessage::Cancel));
                        }
                        None => {}
                    }
                    out
                }
                "thinking" | "speaking" => {
                    self.began = true;
                    self.phase = if state == "thinking" { Phase::Thinking } else { Phase::Speaking };
                    vec![Effect::Ui(Ui::state(&state, None))]
                }
                _ => vec![],
            },
            MainMessage::Pause { ms, of } => vec![Effect::Ui(Ui::Pause { ms, of })],
            MainMessage::Partial { text } => vec![Effect::Ui(Ui::Caption { kind: "partial", text })],
            MainMessage::Heard { text } => vec![Effect::Ui(Ui::Caption { kind: "heard", text })],
            MainMessage::Activity { text } => vec![Effect::Ui(Ui::Caption { kind: "activity", text })],
            MainMessage::Written => vec![Effect::Ui(Ui::Caption { kind: "written", text: String::new() })],
            MainMessage::Line { id, text, url } => {
                let mut out = vec![Effect::Ui(Ui::Line { id: id.clone(), text: text.clone(), status: "queued" })];
                self.queue.push_back(Line { id, text, url });
                out.extend(self.play_next());
                out
            }
            MainMessage::Ended { reason } => {
                let mut out = self.drop_lines(now);
                out.extend(self.end(&reason, false));
                out
            }
            MainMessage::Unknown => vec![],
        }
    }

    /// A line has finished playing (or could not be played).
    pub fn on_played(&mut self, id: &str, now: Instant) -> Vec<Effect> {
        if self.playing.as_ref().is_none_or(|l| l.id != id) {
            return vec![];
        }
        let line = self.playing.take().expect("checked");
        self.mute.played(now);
        let mut out = vec![Effect::Send(DeviceMessage::Played { line: line.id.clone() }), Effect::Ui(Ui::Line { id: line.id, text: line.text, status: "played" })];
        out.extend(self.play_next());
        out
    }

    /// The recorder stopped by itself: the conversation ends, saying why.
    pub fn on_microphone_lost(&mut self, why: &str, now: Instant) -> Vec<Effect> {
        let mut out = self.drop_lines(now);
        out.extend(self.end(why, true));
        out
    }

    pub fn on_command(&mut self, command: Command, now: Instant) -> Vec<Effect> {
        use Phase::*;
        // Before the main listens, what would act on listening waits for it.
        if self.awaiting {
            match command {
                Command::Key(VoiceKey::Talk | VoiceKey::Release) | Command::SendNow => {
                    self.pending = Some(Pending::Send);
                    return vec![];
                }
                Command::Key(VoiceKey::Hold) | Command::Reply => {
                    self.pending = None;
                    return vec![];
                }
                Command::Stop => {
                    self.pending = Some(Pending::Stop);
                    return vec![];
                }
                Command::Key(VoiceKey::Cancel) | Command::Cancel | Command::End => {}
            }
        }
        match (command, self.phase) {
            (Command::Key(VoiceKey::Talk), Idle) | (Command::Key(VoiceKey::Hold), Idle) => vec![self.listen()],
            (Command::Key(VoiceKey::Talk), Listening) | (Command::Key(VoiceKey::Release), Listening) | (Command::SendNow, Listening) => {
                vec![Effect::Send(DeviceMessage::Send)]
            }
            (Command::Key(VoiceKey::Talk), Speaking) | (Command::Key(VoiceKey::Hold), Speaking) | (Command::Reply, Speaking | Idle | Thinking) => {
                let mut out = self.drop_lines(now);
                out.push(self.listen());
                out
            }
            (Command::Stop, Listening) => {
                self.stopped = true;
                vec![Effect::Send(DeviceMessage::Cancel)]
            }
            (Command::Stop, Thinking | Speaking) => {
                self.stopped = true;
                let mut out = self.drop_lines(now);
                out.push(Effect::Send(DeviceMessage::Stop));
                out
            }
            (Command::Key(VoiceKey::Cancel) | Command::Cancel, phase) => {
                let mut out = self.drop_lines(now);
                match phase {
                    Listening => out.push(Effect::Send(DeviceMessage::Cancel)),
                    Thinking | Speaking => out.push(Effect::Send(DeviceMessage::Stop)),
                    Opening | Idle => {}
                }
                out.extend(self.end("cancelled", true));
                out
            }
            (Command::End, _) => {
                let mut out = self.drop_lines(now);
                out.extend(self.end("bye", true));
                out
            }
            _ => vec![],
        }
    }

    fn play_next(&mut self) -> Vec<Effect> {
        if self.playing.is_some() {
            return vec![];
        }
        let Some(line) = self.queue.pop_front() else { return vec![] };
        self.mute.playing();
        let out = vec![
            Effect::Send(DeviceMessage::Playing { line: line.id.clone() }),
            Effect::Ui(Ui::Line { id: line.id.clone(), text: line.text.clone(), status: "playing" }),
            Effect::Play { id: line.id.clone(), url: line.url.clone() },
        ];
        self.playing = Some(line);
        out
    }

    /// Stops the line playing and forgets the ones queued.
    fn drop_lines(&mut self, now: Instant) -> Vec<Effect> {
        let mut out = vec![];
        if let Some(line) = self.playing.take() {
            self.mute.played(now);
            out.push(Effect::StopPlayback);
            out.push(Effect::Send(DeviceMessage::Played { line: line.id.clone() }));
            out.push(Effect::Ui(Ui::Line { id: line.id, text: line.text, status: "dropped" }));
        }
        for line in self.queue.drain(..) {
            out.push(Effect::Ui(Ui::Line { id: line.id, text: line.text, status: "dropped" }));
        }
        out
    }

    fn end(&mut self, reason: &str, bye: bool) -> Vec<Effect> {
        let mut out = vec![];
        if bye {
            out.push(Effect::Send(DeviceMessage::Bye));
        }
        out.push(Effect::Ui(Ui::state("idle", Some(reason))));
        out.push(Effect::Close);
        out
    }
}

/// The program that plays a line (an MP3, WAV or Ogg file), and its
/// arguments before the file; None when there is none.
pub fn system_player() -> Option<(&'static str, &'static [&'static str])> {
    const LINUX: [(&str, &[&str]); 2] = [("pw-play", &[]), ("paplay", &[])];
    const MAC: [(&str, &[&str]); 1] = [("afplay", &[])];
    let candidates: &[(&str, &[&str])] = if cfg!(target_os = "macos") { &MAC } else if cfg!(target_os = "linux") { &LINUX } else { &[] };
    let path = std::env::var_os("PATH")?;
    candidates.iter().copied().find(|(name, _)| std::env::split_paths(&path).any(|dir| dir.join(name).is_file()))
}

/// Plays the Reeve's lines: fetches one (a path on the main) and plays it
/// to the end.
pub trait Lines: Send + Sync + 'static {
    fn play(&self, url: String) -> futures_util::future::BoxFuture<'static, Result<(), String>>;
}

/// How the app tells a main that has gone from one that is quiet: it pings
/// every `ping_every`, and gives up when nothing at all (a pong included)
/// has come for `silent_for`. The main's WebSocket answers pings while it
/// reads, which it always does.
#[derive(Debug, Clone, Copy)]
pub struct Liveness {
    pub ping_every: Duration,
    pub silent_for: Duration,
}

impl Default for Liveness {
    fn default() -> Self {
        Liveness { ping_every: Duration::from_secs(5), silent_for: Duration::from_secs(15) }
    }
}

/// How the conversation ends when the connection goes.
pub const LOST: &str = "Lost the connection to the main";

/// The most of the microphone kept while the main is asked to listen: 10 s.
const HELD_FRAMES: usize = 200;

/// Carries out the conversation on an open session until it ends: hello,
/// then the main's messages, the person's commands and the microphone's
/// bytes. `microphone` says why the recorder stopped, when its output ends.
/// `ui` hears what the widget should show.
pub async fn run(
    socket: Socket,
    device: &str,
    lines: Option<Arc<dyn Lines>>,
    commands: mpsc::UnboundedReceiver<Command>,
    audio: mpsc::UnboundedReceiver<Vec<u8>>,
    microphone: impl FnOnce() -> String + Send,
    ui: impl Fn(Ui) + Send,
) {
    run_with(socket, device, lines, commands, audio, microphone, Liveness::default(), ui).await
}

#[allow(clippy::too_many_arguments)]
pub async fn run_with(
    mut socket: Socket,
    device: &str,
    lines: Option<Arc<dyn Lines>>,
    mut commands: mpsc::UnboundedReceiver<Command>,
    mut audio: mpsc::UnboundedReceiver<Vec<u8>>,
    microphone: impl FnOnce() -> String + Send,
    liveness: Liveness,
    ui: impl Fn(Ui) + Send,
) {
    let mut conversation = Conversation::new();
    let (played_tx, mut played) = mpsc::unbounded_channel::<String>();
    let mut playing: Option<tokio::task::JoinHandle<()>> = None;
    let mut framer = Framer::default();
    let mut held: VecDeque<Vec<u8>> = VecDeque::new();
    let mut microphone = Some(microphone);
    let mut heard = Instant::now();
    let mut pings = tokio::time::interval_at(tokio::time::Instant::now() + liveness.ping_every, liveness.ping_every);
    pings.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Delay);
    let lost = || vec![Effect::Ui(Ui::state("idle", Some(LOST))), Effect::Close];
    let hello = Conversation::hello(device, lines.is_some());
    if let Err(e) = socket.send(Message::text(serde_json::to_string(&hello).expect("encodes"))).await {
        ui(Ui::state("idle", Some(&format!("The session did not open: {e}"))));
        return;
    }
    loop {
        let effects = tokio::select! {
            message = socket.next() => {
                heard = Instant::now();
                match message {
                    Some(Ok(Message::Text(text))) => match serde_json::from_str::<MainMessage>(&text) {
                        Ok(message) => conversation.on_main(message, Instant::now()),
                        Err(e) => {
                            println!("[voice] a message from the main does not read: {e}");
                            vec![]
                        }
                    },
                    Some(Ok(Message::Close(_))) | None => vec![Effect::Ui(Ui::state("idle", Some("The main closed the session"))), Effect::Close],
                    Some(Err(e)) => {
                        println!("[voice] the session failed: {e}");
                        lost()
                    }
                    Some(Ok(_)) => vec![],
                }
            },
            _ = pings.tick() => {
                if heard.elapsed() >= liveness.silent_for {
                    println!("[voice] nothing from the main for {:?}", heard.elapsed());
                    lost()
                } else if socket.send(Message::Ping(Vec::new().into())).await.is_err() {
                    lost()
                } else {
                    vec![]
                }
            },
            command = commands.recv() => match command {
                Some(command) => conversation.on_command(command, Instant::now()),
                None => conversation.on_command(Command::End, Instant::now()),
            },
            bytes = audio.recv() => match bytes {
                Some(bytes) if !bytes.is_empty() => {
                    let mut out = vec![];
                    for frame in framer.push(&bytes) {
                        ui(Ui::Level(voice::level_of(&voice::samples_of(&frame))));
                        match conversation.audio(Instant::now()) {
                            Audio::Send => {
                                if let Err(e) = socket.send(Message::binary(frame)).await {
                                    println!("[voice] a frame was not sent: {e}");
                                    out = lost();
                                    break;
                                }
                            }
                            Audio::Hold => {
                                if held.len() == HELD_FRAMES {
                                    held.pop_front();
                                }
                                held.push_back(frame);
                            }
                            Audio::Drop => held.clear(),
                        }
                    }
                    out
                }
                _ => {
                    let why = microphone.take().map(|f| f()).filter(|w| !w.is_empty()).unwrap_or_else(|| "The microphone stopped".into());
                    println!("[voice] {why}");
                    conversation.on_microphone_lost(&why, Instant::now())
                }
            },
            Some(id) = played.recv() => conversation.on_played(&id, Instant::now()),
        };
        let mut close = false;
        for effect in effects {
            match effect {
                Effect::Send(message) => {
                    if let Err(e) = socket.send(Message::text(serde_json::to_string(&message).expect("encodes"))).await {
                        println!("[voice] {message:?} was not sent: {e}");
                        // A bye is the end anyway; its own reason follows.
                        if message != DeviceMessage::Bye {
                            ui(Ui::state("idle", Some(LOST)));
                            close = true;
                            break;
                        }
                    }
                }
                Effect::Ui(event) => ui(event),
                Effect::Play { id, url } => {
                    let (lines, done) = (lines.clone(), played_tx.clone());
                    playing = Some(tokio::spawn(async move {
                        if let Some(lines) = lines {
                            if let Err(e) = lines.play(url).await {
                                println!("[voice] line {id} did not play: {e}");
                            }
                        }
                        let _ = done.send(id);
                    }));
                }
                Effect::StopPlayback => {
                    if let Some(task) = playing.take() {
                        task.abort();
                    }
                }
                Effect::FlushAudio => {
                    let mut failed = false;
                    for frame in held.drain(..) {
                        if socket.send(Message::binary(frame)).await.is_err() {
                            failed = true;
                            break;
                        }
                    }
                    if failed {
                        ui(Ui::state("idle", Some(LOST)));
                        close = true;
                        break;
                    }
                }
                Effect::Close => close = true,
            }
        }
        if close {
            if let Some(task) = playing.take() {
                task.abort();
            }
            let _ = socket.close(None).await;
            return;
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn line(id: &str) -> MainMessage {
        MainMessage::Line { id: id.into(), text: format!("Line {id}."), url: format!("/api/reeve/voice/lines/{id}") }
    }

    fn state(s: &str) -> MainMessage {
        MainMessage::State { state: s.into(), reason: None }
    }

    fn sent(effects: &[Effect]) -> Vec<DeviceMessage> {
        effects.iter().filter_map(|e| if let Effect::Send(m) = e { Some(m.clone()) } else { None }).collect()
    }

    /// A conversation the main has started listening in.
    fn listening(now: Instant) -> Conversation {
        let mut c = Conversation::new();
        assert_eq!(sent(&c.on_main(state("idle"), now)), [DeviceMessage::Listen]);
        c.on_main(state("listening"), now);
        c
    }

    #[test]
    fn the_microphone_is_muted_while_a_line_plays_and_300_ms_after() {
        let t = Instant::now();
        let mut mute = Mute::default();
        assert!(!mute.muted(t));
        mute.playing();
        assert!(mute.muted(t + Duration::from_secs(60)));
        mute.played(t);
        assert!(mute.muted(t + Duration::from_millis(299)));
        assert!(!mute.muted(t + Duration::from_millis(300)));
    }

    #[test]
    fn the_first_idle_starts_listening_and_audio_goes_only_while_listening() {
        let t = Instant::now();
        let mut c = Conversation::new();
        assert_eq!(c.audio(t), Audio::Hold, "what is said before the main listens is kept for it");
        c.on_main(state("idle"), t);
        assert_eq!(c.audio(t), Audio::Hold, "not sent before the main listens");
        assert!(c.on_main(state("listening"), t).contains(&Effect::FlushAudio));
        assert_eq!(c.audio(t), Audio::Send);
        c.on_main(state("thinking"), t);
        assert_eq!(c.audio(t), Audio::Send, "speech that carries on after a send is the main's to take");
        c.on_main(state("speaking"), t);
        assert_eq!(c.audio(t), Audio::Drop);
    }

    #[test]
    fn stop_keeps_the_conversation_through_the_mains_idle() {
        let t = Instant::now();
        let idle = |reason: Option<&str>| MainMessage::State { state: "idle".into(), reason: reason.map(String::from) };
        // Stopped while it thinks: the main says idle, cancelled.
        let mut c = listening(t);
        c.on_main(state("thinking"), t);
        c.on_command(Command::Stop, t);
        let effects = c.on_main(idle(Some("cancelled")), t);
        assert!(!effects.contains(&Effect::Close), "{effects:?}");
        assert!(sent(&effects).is_empty(), "no bye: {effects:?}");
        assert!(effects.contains(&Effect::Ui(Ui::state("idle", None))));
        assert_eq!(sent(&c.on_command(Command::Reply, t)), [DeviceMessage::Listen]);

        // Stopped while it speaks: thinking until the turn is over, then idle.
        let mut c = listening(t);
        c.on_main(state("speaking"), t);
        c.on_main(line("a"), t);
        c.on_command(Command::Stop, t);
        c.on_main(state("thinking"), t);
        assert!(!c.on_main(idle(None), t).contains(&Effect::Close));
        assert_eq!(sent(&c.on_command(Command::Key(VoiceKey::Talk), t)), [DeviceMessage::Listen]);

        // Stopped while listening: the main says idle, cancelled.
        let mut c = listening(t);
        assert_eq!(sent(&c.on_command(Command::Stop, t)), [DeviceMessage::Cancel]);
        assert!(!c.on_main(idle(Some("cancelled")), t).contains(&Effect::Close));

        // A turn after the stop that ends idle ends the conversation again.
        c.on_command(Command::Reply, t);
        c.on_main(state("listening"), t);
        assert!(c.on_main(idle(Some("no_speech")), t).contains(&Effect::Close));
    }

    #[test]
    fn what_the_person_asks_before_the_main_listens_waits_for_it() {
        let t = Instant::now();
        // Hold to talk, let go before the main has said listening.
        let mut c = Conversation::new();
        assert!(c.on_command(Command::Key(VoiceKey::Release), t).is_empty());
        assert_eq!(sent(&c.on_main(state("idle"), t)), [DeviceMessage::Listen]);
        let effects = c.on_main(state("listening"), t);
        assert_eq!(sent(&effects), [DeviceMessage::Send]);
        let flushed = effects.iter().position(|e| e == &Effect::FlushAudio).expect("flushed");
        let send = effects.iter().position(|e| e == &Effect::Send(DeviceMessage::Send)).unwrap();
        assert!(flushed < send, "what was said goes before the send");

        // The second press of the key, and Send now, after the main said idle.
        for command in [Command::Key(VoiceKey::Talk), Command::SendNow] {
            let mut c = Conversation::new();
            c.on_main(state("idle"), t);
            assert!(c.on_command(command, t).is_empty(), "{command:?}");
            assert_eq!(sent(&c.on_main(state("listening"), t)), [DeviceMessage::Send], "{command:?}");
        }

        // Stop before the main listens stops the turn once it does.
        let mut c = Conversation::new();
        c.on_main(state("idle"), t);
        c.on_command(Command::Stop, t);
        assert_eq!(sent(&c.on_main(state("listening"), t)), [DeviceMessage::Cancel]);
        assert!(!c.on_main(MainMessage::State { state: "idle".into(), reason: Some("cancelled".into()) }, t).contains(&Effect::Close));

        // Reply while the Reeve speaks: a press before listening comes is a send.
        let mut c = listening(t);
        c.on_main(state("speaking"), t);
        assert_eq!(sent(&c.on_command(Command::Reply, t)), [DeviceMessage::Listen]);
        assert_eq!(c.audio(t), Audio::Hold);
        assert!(c.on_command(Command::Key(VoiceKey::Talk), t).is_empty());
        assert_eq!(sent(&c.on_main(state("listening"), t)), [DeviceMessage::Send]);
    }

    #[test]
    fn a_microphone_that_stops_ends_the_conversation_saying_why() {
        let t = Instant::now();
        let mut c = listening(t);
        let effects = c.on_microphone_lost("parecord stopped recording: no such device", t);
        assert_eq!(sent(&effects), [DeviceMessage::Bye]);
        let states: Vec<_> = effects.iter().filter(|e| matches!(e, Effect::Ui(Ui::State { .. }))).collect();
        assert_eq!(states, [&Effect::Ui(Ui::state("idle", Some("parecord stopped recording: no such device")))]);
        assert!(effects.contains(&Effect::Close));
    }

    #[test]
    fn lines_play_in_order_with_the_microphone_muted_and_each_is_acknowledged() {
        let t = Instant::now();
        let mut c = listening(t);
        c.on_main(state("thinking"), t);
        let effects = c.on_main(line("a"), t);
        assert_eq!(sent(&effects), [DeviceMessage::Playing { line: "a".into() }]);
        assert!(effects.contains(&Effect::Play { id: "a".into(), url: "/api/reeve/voice/lines/a".into() }));
        // The second waits for the first.
        let effects = c.on_main(line("b"), t);
        assert!(sent(&effects).is_empty());
        assert!(effects.iter().all(|e| !matches!(e, Effect::Play { .. })));

        // The main says listening for the follow-up before playback ends: still muted.
        c.on_main(state("listening"), t);
        assert_eq!(c.audio(t), Audio::Drop);
        let effects = c.on_played("a", t);
        assert_eq!(sent(&effects), [DeviceMessage::Played { line: "a".into() }, DeviceMessage::Playing { line: "b".into() }]);
        let later = t + Duration::from_secs(2);
        assert_eq!(sent(&c.on_played("b", later)), [DeviceMessage::Played { line: "b".into() }]);
        assert_eq!(c.audio(later + Duration::from_millis(100)), Audio::Drop);
        assert_eq!(c.audio(later + Duration::from_millis(300)), Audio::Send);
        // A line already acknowledged is not again.
        assert!(c.on_played("b", later).is_empty());
    }

    #[test]
    fn the_key_starts_listening_sends_and_replies() {
        let t = Instant::now();
        let mut c = listening(t);
        assert_eq!(sent(&c.on_command(Command::Key(VoiceKey::Talk), t)), [DeviceMessage::Send]);
        c.on_main(state("thinking"), t);
        assert!(c.on_command(Command::Key(VoiceKey::Talk), t).is_empty(), "the key does nothing while the Reeve thinks");
        c.on_main(state("speaking"), t);
        c.on_main(line("a"), t);
        c.on_main(line("b"), t);
        // The key while it speaks is Reply: playback stops, the lines go, and it listens.
        let effects = c.on_command(Command::Key(VoiceKey::Talk), t);
        assert!(effects.contains(&Effect::StopPlayback));
        assert_eq!(sent(&effects), [DeviceMessage::Played { line: "a".into() }, DeviceMessage::Listen]);
        assert_eq!(effects.iter().filter(|e| matches!(e, Effect::Ui(Ui::Line { status: "dropped", .. }))).count(), 2);
    }

    #[test]
    fn hold_to_talk_listens_on_the_press_and_sends_on_the_release() {
        let t = Instant::now();
        let mut c = listening(t);
        assert!(c.on_command(Command::Key(VoiceKey::Hold), t).is_empty(), "already listening");
        assert_eq!(sent(&c.on_command(Command::Key(VoiceKey::Release), t)), [DeviceMessage::Send]);
    }

    #[test]
    fn send_now_reply_and_stop_ask_the_main() {
        let t = Instant::now();
        let mut c = listening(t);
        assert_eq!(sent(&c.on_command(Command::SendNow, t)), [DeviceMessage::Send]);
        c.on_main(state("thinking"), t);
        assert_eq!(sent(&c.on_command(Command::Stop, t)), [DeviceMessage::Stop]);
        c.on_main(state("speaking"), t);
        c.on_main(line("a"), t);
        let effects = c.on_command(Command::Stop, t);
        assert_eq!(sent(&effects), [DeviceMessage::Played { line: "a".into() }, DeviceMessage::Stop]);
        assert!(effects.contains(&Effect::StopPlayback));
        assert!(!effects.contains(&Effect::Close), "stop keeps the conversation");
        assert_eq!(sent(&c.on_command(Command::Reply, t)), [DeviceMessage::Listen]);
    }

    #[test]
    fn cancel_drops_the_turn_and_ends_the_conversation() {
        let t = Instant::now();
        let mut c = listening(t);
        let effects = c.on_command(Command::Cancel, t);
        assert_eq!(sent(&effects), [DeviceMessage::Cancel, DeviceMessage::Bye]);
        assert!(effects.contains(&Effect::Close));

        let mut c = listening(t);
        c.on_main(state("thinking"), t);
        let effects = c.on_command(Command::Key(VoiceKey::Cancel), t);
        assert_eq!(sent(&effects), [DeviceMessage::Stop, DeviceMessage::Bye]);
    }

    #[test]
    fn idle_after_a_turn_ends_the_conversation_with_its_reason() {
        let t = Instant::now();
        let mut c = listening(t);
        let effects = c.on_main(MainMessage::State { state: "idle".into(), reason: Some("no_speech".into()) }, t);
        assert_eq!(sent(&effects), [DeviceMessage::Bye]);
        assert!(effects.contains(&Effect::Ui(Ui::state("idle", Some("no_speech")))));
        assert!(effects.contains(&Effect::Close));
    }

    #[test]
    fn ended_stops_playback_and_closes_without_a_bye() {
        let t = Instant::now();
        let mut c = listening(t);
        c.on_main(state("speaking"), t);
        c.on_main(line("a"), t);
        let effects = c.on_main(MainMessage::Ended { reason: "taken over by Pixel 9".into() }, t);
        assert!(effects.contains(&Effect::StopPlayback));
        assert!(!sent(&effects).contains(&DeviceMessage::Bye));
        assert!(effects.contains(&Effect::Ui(Ui::state("idle", Some("taken over by Pixel 9")))));
        assert!(effects.contains(&Effect::Close));
    }

    #[test]
    fn what_the_main_says_reaches_the_widget() {
        let t = Instant::now();
        let mut c = listening(t);
        let ui = |m: MainMessage, c: &mut Conversation| -> Vec<Ui> {
            c.on_main(m, t).into_iter().filter_map(|e| if let Effect::Ui(u) = e { Some(u) } else { None }).collect()
        };
        assert_eq!(ui(MainMessage::Pause { ms: 300, of: 1000 }, &mut c), [Ui::Pause { ms: 300, of: 1000 }]);
        assert_eq!(ui(MainMessage::Partial { text: "turn the".into() }, &mut c), [Ui::Caption { kind: "partial", text: "turn the".into() }]);
        assert_eq!(ui(MainMessage::Heard { text: "turn the lights off".into() }, &mut c), [Ui::Caption { kind: "heard", text: "turn the lights off".into() }]);
        assert_eq!(ui(MainMessage::Activity { text: "Reading config.yaml".into() }, &mut c), [Ui::Caption { kind: "activity", text: "Reading config.yaml".into() }]);
        assert_eq!(ui(MainMessage::Written, &mut c), [Ui::Caption { kind: "written", text: String::new() }]);
        assert_eq!(ui(MainMessage::Unknown, &mut c), []);
    }

    // ---- A session against a stand-in main ----

    use crate::legato::Client;
    use tokio::net::TcpListener;
    use tokio_tungstenite::tungstenite::handshake::server::{Request, Response};

    const TURN: &str = include_str!(concat!(env!("CARGO_MANIFEST_DIR"), "/../../internal/server/testdata/voice-session/turn.json"));

    type Served = tokio_tungstenite::WebSocketStream<tokio::net::TcpStream>;

    /// Accepts one session, giving the address it was asked for and its Origin.
    async fn accept(listener: &TcpListener) -> (Served, String, String) {
        let (tcp, _) = listener.accept().await.unwrap();
        let asked = Arc::new(std::sync::Mutex::new((String::new(), String::new())));
        let into = asked.clone();
        let ws = tokio_tungstenite::accept_hdr_async(tcp, move |req: &Request, resp: Response| {
            let origin = req.headers().get("Origin").and_then(|v| v.to_str().ok()).unwrap_or_default().to_string();
            *into.lock().unwrap() = (req.uri().to_string(), origin);
            Ok(resp)
        })
        .await
        .unwrap();
        let (uri, origin) = asked.lock().unwrap().clone();
        (ws, uri, origin)
    }

    /// The next text frame the device sends, past any audio.
    async fn next_text(ws: &mut Served) -> Value {
        loop {
            match tokio::time::timeout(Duration::from_secs(5), ws.next()).await.expect("the device says something").unwrap().unwrap() {
                Message::Text(t) => return serde_json::from_str(&t).unwrap(),
                Message::Binary(_) | Message::Ping(_) => continue,
                other => panic!("{other:?}"),
            }
        }
    }

    /// A session client on a socket to the stand-in, with what the widget hears.
    struct Device {
        commands: mpsc::UnboundedSender<Command>,
        audio: mpsc::UnboundedSender<Vec<u8>>,
        heard: mpsc::UnboundedReceiver<Ui>,
        done: tokio::task::JoinHandle<()>,
    }

    impl Device {
        async fn open(url: &str, lines: Option<Arc<dyn Lines>>) -> Device {
            Device::open_with(url, lines, Liveness::default(), || "The microphone stopped".into()).await
        }

        async fn open_with(url: &str, lines: Option<Arc<dyn Lines>>, liveness: Liveness, microphone: impl FnOnce() -> String + Send + 'static) -> Device {
            let client = Client::new(url, None, Some("tok 1".into())).unwrap();
            let socket = client.websocket("/api/reeve/voice/session").await.expect("the session opens");
            let (commands, commands_rx) = mpsc::unbounded_channel();
            let (audio, audio_rx) = mpsc::unbounded_channel();
            let (ui, heard) = mpsc::unbounded_channel();
            let done = tokio::spawn(async move {
                run_with(socket, "Cameron's desktop", lines, commands_rx, audio_rx, microphone, liveness, move |u| {
                    let _ = ui.send(u);
                })
                .await
            });
            Device { commands, audio, heard, done }
        }

        /// Waits until the widget is told what matches, skipping levels and the rest.
        async fn told(&mut self, wanted: impl Fn(&Ui) -> bool) -> Ui {
            loop {
                let u = tokio::time::timeout(Duration::from_secs(5), self.heard.recv()).await.expect("the widget is told").expect("open");
                if wanted(&u) {
                    return u;
                }
            }
        }
    }

    /// ms of a loud tone, or of quiet, as s16le bytes.
    fn audio(ms: u64, loud: bool) -> Vec<u8> {
        (0..ms * 16).flat_map(|i| (if loud { if i % 40 < 20 { 8000i16 } else { -8000 } } else { 0 }).to_le_bytes()).collect()
    }

    #[tokio::test]
    async fn a_spoken_turn_runs_as_the_shared_fixture_has_it() {
        let turn: Value = serde_json::from_str(TURN).unwrap();
        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let url = format!("http://{}", listener.local_addr().unwrap());
        let steps = turn["steps"].as_array().unwrap().clone();
        let main = tokio::spawn(async move {
            let (mut ws, uri, origin) = accept(&listener).await;
            assert_eq!(uri, "/api/reeve/voice/session?token=tok+1");
            assert_eq!(origin, crate::webauthn::ORIGIN);
            for step in steps {
                if let Some(want) = step.get("send") {
                    assert_eq!(&next_text(&mut ws).await, want);
                } else if let Some(say) = step.get("expect") {
                    ws.send(Message::text(say.to_string())).await.unwrap();
                } else if let Some(a) = step.get("audio") {
                    let (ms, loud) = match a.get("speech_ms") {
                        Some(ms) => (ms.as_u64().unwrap(), true),
                        None => (a["quiet_ms"].as_u64().unwrap(), false),
                    };
                    let mut got = 0;
                    while got < ms * 32 {
                        match tokio::time::timeout(Duration::from_secs(5), ws.next()).await.expect("audio").unwrap().unwrap() {
                            Message::Binary(frame) => {
                                assert!((640..=3200).contains(&frame.len()), "20-100 ms a frame: {}", frame.len());
                                let level = voice::level_of(&voice::samples_of(&frame));
                                assert_eq!(level > 0.5, loud, "frame at {got} of {ms} ms");
                                got += frame.len() as u64;
                            }
                            Message::Ping(_) => {}
                            other => panic!("audio, not {other:?}"),
                        }
                    }
                }
            }
        });

        let mut device = Device::open(&url, Some(Arc::new(Noted(Arc::default())))).await;
        device.told(|u| matches!(u, Ui::State { state, .. } if state == "listening")).await;
        // The recorder's pieces split samples and frames anywhere.
        let said = [audio(1000, true), audio(1200, false)].concat();
        for piece in said.chunks(1234) {
            device.audio.send(piece.to_vec()).unwrap();
        }
        device.told(|u| matches!(u, Ui::State { state, .. } if state == "thinking")).await;
        let heard = device.told(|u| matches!(u, Ui::Caption { .. })).await;
        assert_eq!(heard, Ui::Caption { kind: "heard", text: turn["transcript"].as_str().unwrap().into() });
        device.commands.send(Command::End).unwrap();
        main.await.unwrap();
        tokio::time::timeout(Duration::from_secs(5), device.done).await.expect("the client ends").unwrap();
    }

    /// Plays a line by noting it, as the speakers would after a moment.
    struct Noted(Arc<std::sync::Mutex<Vec<String>>>);

    impl Lines for Noted {
        fn play(&self, url: String) -> futures_util::future::BoxFuture<'static, Result<(), String>> {
            self.0.lock().unwrap().push(url);
            Box::pin(async {
                tokio::time::sleep(Duration::from_millis(30)).await;
                Ok(())
            })
        }
    }

    #[tokio::test]
    async fn the_reeves_lines_are_played_in_order_and_acknowledged() {
        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let url = format!("http://{}", listener.local_addr().unwrap());
        let main = tokio::spawn(async move {
            let (mut ws, _, _) = accept(&listener).await;
            let say = |v: Value| Message::text(v.to_string());
            assert_eq!(next_text(&mut ws).await, json!({"type": "hello", "device": "Cameron's desktop", "client": "desktop", "play": true}));
            ws.send(say(json!({"type": "state", "state": "idle"}))).await.unwrap();
            assert_eq!(next_text(&mut ws).await, json!({"type": "listen"}));
            ws.send(say(json!({"type": "state", "state": "speaking"}))).await.unwrap();
            for id in ["a", "b"] {
                ws.send(say(json!({"type": "line", "id": id, "text": "Done.", "url": format!("/api/reeve/voice/lines/{id}")}))).await.unwrap();
            }
            for want in [json!({"type": "playing", "line": "a"}), json!({"type": "played", "line": "a"}), json!({"type": "playing", "line": "b"}), json!({"type": "played", "line": "b"})] {
                assert_eq!(next_text(&mut ws).await, want);
            }
            ws.send(say(json!({"type": "state", "state": "idle", "reason": "no_speech"}))).await.unwrap();
            assert_eq!(next_text(&mut ws).await, json!({"type": "bye"}));
        });

        let noted = Arc::new(std::sync::Mutex::new(Vec::new()));
        let mut device = Device::open(&url, Some(Arc::new(Noted(noted.clone())))).await;
        let ended = device.told(|u| matches!(u, Ui::State { state, .. } if state == "idle")).await;
        assert_eq!(ended, Ui::state("idle", Some("no_speech")));
        main.await.unwrap();
        assert_eq!(*noted.lock().unwrap(), ["/api/reeve/voice/lines/a", "/api/reeve/voice/lines/b"]);
        let _ = device.audio;
    }

    #[tokio::test]
    async fn a_main_that_stops_answering_ends_the_conversation() {
        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let url = format!("http://{}", listener.local_addr().unwrap());
        let main = tokio::spawn(async move {
            let (mut ws, _, _) = accept(&listener).await;
            next_text(&mut ws).await;
            ws.send(Message::text(r#"{"type":"state","state":"idle"}"#)).await.unwrap();
            // Then nothing: the socket is held open but never read, so no pong.
            tokio::time::sleep(Duration::from_secs(10)).await;
            drop(ws);
        });
        let liveness = Liveness { ping_every: Duration::from_millis(50), silent_for: Duration::from_millis(300) };
        let mut device = Device::open_with(&url, None, liveness, || String::new()).await;
        let ended = device.told(|u| matches!(u, Ui::State { state, .. } if state == "idle")).await;
        assert_eq!(ended, Ui::state("idle", Some(LOST)));
        tokio::time::timeout(Duration::from_secs(5), device.done).await.expect("the client ends").unwrap();
        main.abort();
    }

    #[tokio::test]
    async fn a_main_that_answers_pings_keeps_the_conversation() {
        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let url = format!("http://{}", listener.local_addr().unwrap());
        let main = tokio::spawn(async move {
            let (mut ws, _, _) = accept(&listener).await;
            next_text(&mut ws).await;
            ws.send(Message::text(r#"{"type":"state","state":"idle"}"#)).await.unwrap();
            // Reading answers the pings; listen is the one text.
            assert_eq!(next_text(&mut ws).await, json!({"type": "listen"}));
            assert_eq!(next_text(&mut ws).await, json!({"type": "bye"}));
        });
        let liveness = Liveness { ping_every: Duration::from_millis(50), silent_for: Duration::from_millis(300) };
        let device = Device::open_with(&url, None, liveness, || String::new()).await;
        tokio::time::sleep(Duration::from_millis(800)).await;
        assert!(!device.done.is_finished(), "a quiet main that answers pings is kept");
        device.commands.send(Command::End).unwrap();
        main.await.unwrap();
        tokio::time::timeout(Duration::from_secs(5), device.done).await.expect("the client ends").unwrap();
    }

    #[tokio::test]
    async fn a_recorder_that_stops_ends_the_conversation_with_its_reason() {
        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let url = format!("http://{}", listener.local_addr().unwrap());
        let main = tokio::spawn(async move {
            let (mut ws, _, _) = accept(&listener).await;
            next_text(&mut ws).await;
            ws.send(Message::text(r#"{"type":"state","state":"idle"}"#)).await.unwrap();
            assert_eq!(next_text(&mut ws).await, json!({"type": "listen"}));
            ws.send(Message::text(r#"{"type":"state","state":"listening"}"#)).await.unwrap();
            assert_eq!(next_text(&mut ws).await, json!({"type": "bye"}));
        });
        let mut device = Device::open_with(&url, None, Liveness::default(), || "parecord stopped recording: no such device".into()).await;
        device.told(|u| matches!(u, Ui::State { state, .. } if state == "listening")).await;
        device.audio.send(Vec::new()).unwrap();
        let ended = device.told(|u| matches!(u, Ui::State { state, .. } if state == "idle")).await;
        assert_eq!(ended, Ui::state("idle", Some("parecord stopped recording: no such device")));
        main.await.unwrap();
        tokio::time::timeout(Duration::from_secs(5), device.done).await.expect("the client ends").unwrap();
        while let Ok(u) = device.heard.try_recv() {
            assert!(!matches!(u, Ui::State { .. }), "nothing after the reason: {u:?}");
        }
    }

    #[tokio::test]
    async fn the_session_opens_only_over_the_pinned_certificate() {
        let made = rcgen::generate_simple_self_signed(vec!["localhost".into()]).unwrap();
        let pem = crate::legato::certificate_pem(made.cert.der());
        let key = rustls::pki_types::PrivateKeyDer::Pkcs8(made.key_pair.serialize_der().into());
        let tls = rustls::ServerConfig::builder_with_provider(Arc::new(rustls::crypto::ring::default_provider()))
            .with_safe_default_protocol_versions()
            .unwrap()
            .with_no_client_auth()
            .with_single_cert(vec![made.cert.der().clone()], key)
            .unwrap();
        let acceptor = tokio_rustls::TlsAcceptor::from(Arc::new(tls));
        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let url = format!("https://localhost:{}", listener.local_addr().unwrap().port());
        tokio::spawn(async move {
            loop {
                let (tcp, _) = listener.accept().await.unwrap();
                let acceptor = acceptor.clone();
                tokio::spawn(async move {
                    if let Ok(tls) = acceptor.accept(tcp).await {
                        if let Ok(mut ws) = tokio_tungstenite::accept_async(tls).await {
                            let _ = ws.send(Message::text(r#"{"type":"state","state":"idle"}"#)).await;
                        }
                    }
                });
            }
        });

        let client = Client::new(&url, Some(&pem), Some("t".into())).unwrap();
        let mut socket = client.websocket("/api/reeve/voice/session").await.expect("the pinned certificate is trusted");
        let first = socket.next().await.unwrap().unwrap();
        assert_eq!(first, Message::text(r#"{"type":"state","state":"idle"}"#));

        let other = rcgen::generate_simple_self_signed(vec!["localhost".into()]).unwrap();
        let client = Client::new(&url, Some(&crate::legato::certificate_pem(other.cert.der())), Some("t".into())).unwrap();
        let refused = client.websocket("/api/reeve/voice/session").await.err().expect("another certificate is refused");
        assert!(refused.contains("pinned"), "{refused}");
    }

    #[test]
    fn the_widget_names_its_buttons() {
        assert_eq!(Command::named("stop"), Some(Command::Stop));
        assert_eq!(Command::named("reply"), Some(Command::Reply));
        assert_eq!(Command::named("send"), Some(Command::SendNow));
        assert_eq!(Command::named("cancel"), Some(Command::Cancel));
        assert_eq!(Command::named("end"), Some(Command::End));
        assert_eq!(Command::named("talk"), None);
    }
}
