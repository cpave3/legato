//! Recording the microphone for a server's page. A page served over plain
//! HTTP has no microphone of its own, so its dictation asks the app to
//! record in its place (see `bridge`), as the Android app does.
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
        let mut child = Command::new(program)
            .args(args)
            .stdin(Stdio::null())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped())
            .spawn()
            .map_err(|e| format!("Could not start {program} to record: {e}"))?;
        let samples = Arc::new(Mutex::new(Vec::new()));
        let mut out = child.stdout.take().expect("piped");
        let into = samples.clone();
        let reader = std::thread::spawn(move || {
            let mut buf = [0u8; 4096];
            let mut odd: Option<u8> = None;
            while let Ok(n) = out.read(&mut buf) {
                if n == 0 {
                    break;
                }
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

    /// How loud the newest tenth of a second was, from 0 to 1; an error when
    /// the recorder has ended by itself.
    pub fn level(&mut self) -> Result<f64, String> {
        self.still_running()?;
        let samples = self.samples.lock().unwrap();
        let newest = &samples[samples.len().saturating_sub(LEVEL_WINDOW)..];
        if newest.is_empty() {
            return Ok(0.0);
        }
        let sum: f64 = newest.iter().map(|&s| (s as f64 / 32768.0).powi(2)).sum();
        // Speech is quiet next to full scale; scaled up to be seen, as the web UI does.
        Ok(((sum / newest.len() as f64).sqrt() * 6.0).min(1.0))
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

    #[test]
    fn the_level_rises_with_a_louder_voice() {
        let mut quiet = writes(&[40, -40].repeat(800));
        let mut loud = writes(&[2000, -2000].repeat(800));
        let (quiet_level, loud_level) = (settled_level(&mut quiet), settled_level(&mut loud));
        assert!(quiet_level > 0.0 && quiet_level < 0.1, "quiet: {quiet_level}");
        assert!(loud_level > 0.3 && loud_level <= 1.0, "loud: {loud_level}");
        quiet.cancel();
        loud.cancel();
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
    fn a_recording_with_nothing_in_it_is_an_error() {
        let recorder = writes(&[]);
        assert!(recorder.stop().is_err());
    }
}
