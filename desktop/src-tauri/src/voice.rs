//! Recording the microphone for a server's page. A page served over plain
//! HTTP has no microphone of its own, so its dictation asks the app to
//! record in its place (see `bridge`), as the Android app does.
//!
//! The same recorder streams the microphone to the main's voice session
//! (`Recorder::stream`, `Framer`), for talking to the Reeve.
//!
//! The recording is made by the system's own recorder (`parecord`, else
//! `arecord`), which writes 16 kHz mono 16-bit samples, the transcriber's
//! format, to its output. Nothing here links an audio library, so the app
//! builds without the sound system's development files.

use std::io::Read;
use std::process::{Child, Command, Stdio};
use std::sync::{Arc, Mutex};
use std::thread::JoinHandle;

/// Samples a second, as Legato's transcriber takes them.
pub const RATE: u32 = 16000;

/// The level is of the newest tenth of a second.
const LEVEL_WINDOW: usize = RATE as usize / 10;

/// The system's recorder and how to have it write 16 kHz mono 16-bit
/// little-endian samples to its output; None when there is none (or not on
/// Linux), and then the app does not offer to record.
pub fn system_recorder() -> Option<(&'static str, &'static [&'static str])> {
    if !cfg!(target_os = "linux") {
        return None;
    }
    const RECORDERS: [(&str, &[&str]); 2] = [
        ("parecord", &["--raw", "--format=s16le", "--rate=16000", "--channels=1", "--latency-msec=50"]),
        ("arecord", &["-q", "-t", "raw", "-f", "S16_LE", "-r", "16000", "-c", "1"]),
    ];
    let path = std::env::var_os("PATH")?;
    RECORDERS.into_iter().find(|(name, _)| std::env::split_paths(&path).any(|dir| dir.join(name).is_file()))
}

/// Where the recorder's output goes: kept whole (dictation), or handed on
/// as it arrives (the voice session).
enum Sink {
    Keep(Arc<Mutex<Vec<i16>>>),
    Forward(Box<dyn FnMut(&[u8]) + Send>),
}

/// A recording in progress: the recorder running, and what it has written so far.
pub struct Recorder {
    program: String,
    child: Child,
    samples: Arc<Mutex<Vec<i16>>>,
    reader: Option<JoinHandle<()>>,
    errors: Arc<Mutex<String>>,
}

impl Recorder {
    /// Starts program, which writes samples to its output until stopped.
    pub fn start(program: &str, args: &[&str]) -> Result<Recorder, String> {
        let samples = Arc::new(Mutex::new(Vec::new()));
        Recorder::spawn(program, args, samples.clone(), Sink::Keep(samples))
    }

    /// Starts program and hands each piece of its output to on_bytes as it
    /// arrives, keeping nothing; an empty piece when the output ends. The
    /// pieces are raw s16le bytes and need not hold whole samples (see
    /// `Framer`).
    pub fn stream(program: &str, args: &[&str], on_bytes: impl FnMut(&[u8]) + Send + 'static) -> Result<Recorder, String> {
        Recorder::spawn(program, args, Arc::default(), Sink::Forward(Box::new(on_bytes)))
    }

    fn spawn(program: &str, args: &[&str], samples: Arc<Mutex<Vec<i16>>>, mut sink: Sink) -> Result<Recorder, String> {
        let mut child = Command::new(program)
            .args(args)
            .stdin(Stdio::null())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped())
            .spawn()
            .map_err(|e| format!("Could not start {program} to record: {e}"))?;
        let mut out = child.stdout.take().expect("piped");
        let reader = std::thread::spawn(move || {
            let mut buf = [0u8; 4096];
            let mut odd: Option<u8> = None;
            while let Ok(n) = out.read(&mut buf) {
                if n == 0 {
                    break;
                }
                let into = match &mut sink {
                    Sink::Forward(on_bytes) => {
                        on_bytes(&buf[..n]);
                        continue;
                    }
                    Sink::Keep(into) => into,
                };
                let mut bytes = odd.take().into_iter().chain(buf[..n].iter().copied());
                let mut got = Vec::with_capacity(n / 2 + 1);
                while let Some(lo) = bytes.next() {
                    match bytes.next() {
                        Some(hi) => got.push(i16::from_le_bytes([lo, hi])),
                        None => odd = Some(lo),
                    }
                }
                into.lock().unwrap().extend(got);
            }
            if let Sink::Forward(on_bytes) = &mut sink {
                on_bytes(&[]);
            }
        });
        let errors = Arc::new(Mutex::new(String::new()));
        let mut err = child.stderr.take().expect("piped");
        let into = errors.clone();
        std::thread::spawn(move || {
            let mut text = String::new();
            let _ = err.read_to_string(&mut text);
            *into.lock().unwrap() = text;
        });
        Ok(Recorder { program: program.into(), child, samples, reader: Some(reader), errors })
    }

    /// How loud the newest tenth of a second was, from 0 (-50 dB or quieter)
    /// to 1 (full scale); an error when
    /// the recorder has ended by itself.
    pub fn level(&mut self) -> Result<f64, String> {
        self.still_running()?;
        let samples = self.samples.lock().unwrap();
        Ok(level_of(&samples[samples.len().saturating_sub(LEVEL_WINDOW)..]))
    }

    /// Whether the recorder still runs: an error saying why when it ended
    /// by itself.
    pub fn check(&mut self) -> Result<(), String> {
        self.still_running()
    }

    /// Why a recorder whose output has ended stopped, in one line: what it
    /// said last, waiting up to a second for it to exit.
    pub fn why_stopped(&mut self) -> String {
        let deadline = std::time::Instant::now() + std::time::Duration::from_secs(1);
        loop {
            if let Err(why) = self.still_running() {
                return why;
            }
            if std::time::Instant::now() > deadline {
                return format!("{} stopped recording.", self.program);
            }
            std::thread::sleep(std::time::Duration::from_millis(20));
        }
    }

    /// Ends the recording and gives it back as a WAV file.
    pub fn stop(mut self) -> Result<Vec<u8>, String> {
        self.still_running()?;
        let _ = self.child.kill();
        let _ = self.child.wait();
        if let Some(reader) = self.reader.take() {
            let _ = reader.join();
        }
        let samples = std::mem::take(&mut *self.samples.lock().unwrap());
        if samples.is_empty() {
            return Err("Nothing was recorded.".into());
        }
        Ok(wav(&samples))
    }

    /// Ends the recording and throws it away.
    pub fn cancel(self) {}

    fn still_running(&mut self) -> Result<(), String> {
        let Ok(Some(status)) = self.child.try_wait() else { return Ok(()) };
        // What it said last, once it has all been read.
        if let Some(reader) = self.reader.take() {
            let _ = reader.join();
        }
        let deadline = std::time::Instant::now() + std::time::Duration::from_millis(500);
        let said = loop {
            let said = self.errors.lock().unwrap().trim().lines().last().unwrap_or_default().to_string();
            if !said.is_empty() || std::time::Instant::now() > deadline {
                break said;
            }
            std::thread::sleep(std::time::Duration::from_millis(10));
        };
        Err(if said.is_empty() { format!("{} stopped recording ({status}).", self.program) } else { format!("{} stopped recording: {said}", self.program) })
    }
}

impl Drop for Recorder {
    fn drop(&mut self) {
        let _ = self.child.kill();
        let _ = self.child.wait();
    }
}

/// How loud samples are, from 0 (-50 dB or quieter) to 1 (full scale), in
/// decibels as the Android app shows it: without a browser's automatic
/// gain, speech sits around -30 dB.
pub fn level_of(samples: &[i16]) -> f64 {
    if samples.is_empty() {
        return 0.0;
    }
    let sum: f64 = samples.iter().map(|&s| (s as f64 / 32768.0).powi(2)).sum();
    let rms = (sum / samples.len() as f64).sqrt();
    if rms <= 0.0 {
        return 0.0;
    }
    ((20.0 * rms.log10() + 50.0) / 50.0).clamp(0.0, 1.0)
}

/// Samples in a frame sent to the voice session: 50 ms, inside the 20-100
/// ms the main takes.
pub const FRAME_SAMPLES: usize = RATE as usize / 20;

/// Cuts the recorder's output, which comes in pieces of any size, into
/// whole frames of FRAME_SAMPLES samples, keeping the rest for the next.
#[derive(Default)]
pub struct Framer {
    pending: Vec<u8>,
}

impl Framer {
    /// The whole frames the bytes complete, as s16le bytes.
    pub fn push(&mut self, bytes: &[u8]) -> Vec<Vec<u8>> {
        self.pending.extend_from_slice(bytes);
        let size = FRAME_SAMPLES * 2;
        let whole = self.pending.len() / size * size;
        let rest = self.pending.split_off(whole);
        let frames = self.pending.chunks(size).map(<[u8]>::to_vec).collect();
        self.pending = rest;
        frames
    }
}

/// The samples of s16le bytes.
pub fn samples_of(bytes: &[u8]) -> Vec<i16> {
    bytes.chunks_exact(2).map(|b| i16::from_le_bytes([b[0], b[1]])).collect()
}

/// 16 kHz mono 16-bit samples as a WAV file.
fn wav(samples: &[i16]) -> Vec<u8> {
    let data = (samples.len() * 2) as u32;
    let mut out = Vec::with_capacity(44 + data as usize);
    out.extend(b"RIFF");
    out.extend((36 + data).to_le_bytes());
    out.extend(b"WAVEfmt ");
    out.extend(16u32.to_le_bytes());
    out.extend(1u16.to_le_bytes());
    out.extend(1u16.to_le_bytes());
    out.extend(RATE.to_le_bytes());
    out.extend((RATE * 2).to_le_bytes());
    out.extend(2u16.to_le_bytes());
    out.extend(16u16.to_le_bytes());
    out.extend(b"data");
    out.extend(data.to_le_bytes());
    out.extend(samples.iter().flat_map(|s| s.to_le_bytes()));
    out
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::time::{Duration, Instant};

    /// A stand-in recorder that writes the given samples and then waits to
    /// be stopped, as a microphone that went quiet would.
    fn writes(samples: &[i16]) -> Recorder {
        let bytes: String = samples.iter().flat_map(|s| s.to_le_bytes()).map(|b| format!("\\{b:03o}")).collect();
        Recorder::start("sh", &["-c", &format!("printf '{bytes}'; exec sleep 30")]).expect("sh starts")
    }

    /// The level once the recorder has read what was written.
    fn settled_level(recorder: &mut Recorder) -> f64 {
        let deadline = Instant::now() + Duration::from_secs(5);
        loop {
            let level = recorder.level().expect("still recording");
            if level > 0.0 || Instant::now() > deadline {
                return level;
            }
            std::thread::sleep(Duration::from_millis(10));
        }
    }

    #[test]
    fn stopping_gives_a_wav_of_exactly_what_was_recorded() {
        let mut recorder = writes(&[1, -2, 0x1234, i16::MIN, i16::MAX]);
        settled_level(&mut recorder);
        let wav = recorder.stop().expect("a recording");
        let mut want = Vec::new();
        want.extend(b"RIFF");
        want.extend(46u32.to_le_bytes()); // 36 + 10 bytes of samples
        want.extend(b"WAVEfmt ");
        want.extend(16u32.to_le_bytes());
        want.extend(1u16.to_le_bytes()); // PCM
        want.extend(1u16.to_le_bytes()); // mono
        want.extend(16000u32.to_le_bytes());
        want.extend(32000u32.to_le_bytes()); // bytes a second
        want.extend(2u16.to_le_bytes()); // bytes a sample
        want.extend(16u16.to_le_bytes()); // bits a sample
        want.extend(b"data");
        want.extend(10u32.to_le_bytes());
        want.extend([0x01, 0x00, 0xfe, 0xff, 0x34, 0x12, 0x00, 0x80, 0xff, 0x7f]);
        assert_eq!(wav, want);
    }

    /// The level is in decibels from -50 dB (0) to full scale (1), as the
    /// Android app's is: a microphone without the browser's automatic gain
    /// gives speech around -30 dB, which should fill about half the bars.
    #[test]
    fn the_level_is_in_decibels_so_ordinary_speech_fills_half_the_bars() {
        let level_of = |amplitude: i16| {
            let mut r = writes(&[amplitude, -amplitude].repeat(800));
            let deadline = Instant::now() + Duration::from_secs(5);
            while r.samples.lock().unwrap().len() < 1600 && Instant::now() < deadline {
                std::thread::sleep(Duration::from_millis(5));
            }
            let level = r.level().expect("still recording");
            r.cancel();
            level
        };
        let (hiss, speech, shout) = (level_of(40), level_of(1000), level_of(30000));
        assert!(hiss < 0.05, "hiss (-58 dB): {hiss}");
        assert!((0.35..0.5).contains(&speech), "speech (-30 dB): {speech}");
        assert!(shout > 0.95 && shout <= 1.0, "shout: {shout}");
    }

    #[test]
    fn a_recorder_that_is_not_installed_does_not_start() {
        let error = Recorder::start("legato-no-such-recorder", &[]).err().expect("an error");
        assert!(error.contains("legato-no-such-recorder"), "{error}");
    }

    #[test]
    fn a_recorder_that_gives_up_says_why() {
        let mut recorder = Recorder::start("sh", &["-c", "echo 'no microphone here' >&2; exit 1"]).expect("sh starts");
        let deadline = Instant::now() + Duration::from_secs(5);
        let error = loop {
            match recorder.level() {
                Err(error) => break error,
                Ok(_) if Instant::now() > deadline => panic!("still recording"),
                Ok(_) => std::thread::sleep(Duration::from_millis(10)),
            }
        };
        assert!(error.contains("no microphone here"), "{error}");
        let error = recorder.stop().err().expect("no recording");
        assert!(error.contains("no microphone here"), "{error}");
    }

    #[test]
    fn a_stream_that_ends_says_why_the_recorder_stopped() {
        let (tx, rx) = std::sync::mpsc::channel::<Vec<u8>>();
        let mut recorder = Recorder::stream("sh", &["-c", "echo 'no such device' >&2; exit 1"], move |b| {
            let _ = tx.send(b.to_vec());
        })
        .expect("sh starts");
        while !rx.recv_timeout(Duration::from_secs(5)).expect("an end").is_empty() {}
        let why = recorder.why_stopped();
        assert!(why.contains("no such device"), "{why}");
    }

    #[test]
    fn streaming_hands_on_exactly_what_was_recorded_then_an_end() {
        let samples: Vec<i16> = (0..3000).map(|i| (i * 7 % 2000 - 1000) as i16).collect();
        let bytes: String = samples.iter().flat_map(|s| s.to_le_bytes()).map(|b| format!("\\{b:03o}")).collect();
        let (tx, rx) = std::sync::mpsc::channel::<Vec<u8>>();
        let mut recorder = Recorder::stream("sh", &["-c", &format!("printf '{bytes}'")], move |b| {
            let _ = tx.send(b.to_vec());
        })
        .expect("sh starts");
        let mut got = Vec::new();
        loop {
            let piece = rx.recv_timeout(Duration::from_secs(5)).expect("the output, then its end");
            if piece.is_empty() {
                break;
            }
            got.extend(piece);
        }
        assert_eq!(samples_of(&got), samples);
        // The recorder ended by itself: it says so.
        let deadline = Instant::now() + Duration::from_secs(5);
        while recorder.check().is_ok() {
            assert!(Instant::now() < deadline, "still running");
            std::thread::sleep(Duration::from_millis(10));
        }
    }

    #[test]
    fn the_framer_gives_whole_frames_and_keeps_the_rest() {
        let mut framer = Framer::default();
        let bytes: Vec<u8> = (0..FRAME_SAMPLES * 2 * 2 + 3).map(|i| i as u8).collect();
        // Pieces that split samples and frames anywhere.
        assert!(framer.push(&bytes[..5]).is_empty());
        let frames = framer.push(&bytes[5..FRAME_SAMPLES * 2 + 1]);
        assert_eq!(frames, vec![bytes[..FRAME_SAMPLES * 2].to_vec()]);
        let frames = framer.push(&bytes[FRAME_SAMPLES * 2 + 1..]);
        assert_eq!(frames, vec![bytes[FRAME_SAMPLES * 2..FRAME_SAMPLES * 4].to_vec()]);
        assert_eq!(framer.push(&[]).len(), 0);
        // 50 ms a frame.
        assert_eq!(FRAME_SAMPLES, 800);
    }

    #[test]
    fn a_recording_with_nothing_in_it_is_an_error() {
        let recorder = writes(&[]);
        assert!(recorder.stop().is_err());
    }
}
