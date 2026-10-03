//! Loss-tolerant diagnostic delivery: producers never wait for the stderr sink.
use crate::domain::frontmatter::Error;
use std::{
    cell::RefCell,
    io::Write,
    sync::{
        OnceLock,
        atomic::{AtomicUsize, Ordering},
        mpsc::{SyncSender, sync_channel},
    },
    time::{Duration, Instant},
};
#[derive(Clone)]
struct Context {
    id: String,
    kind: &'static str,
}
thread_local! { static CONTEXT: RefCell<Option<Context>> = const { RefCell::new(None) }; }
static PENDING: AtomicUsize = AtomicUsize::new(0);
static SINK: OnceLock<Option<SyncSender<Vec<u8>>>> = OnceLock::new();
fn sink() -> Option<&'static SyncSender<Vec<u8>>> {
    SINK.get_or_init(|| {
        let (tx, rx) = sync_channel::<Vec<u8>>(128);
        std::thread::Builder::new()
            .name("bn-git-diagnostics".into())
            .spawn(move || {
                for line in rx {
                    let _ = std::io::stderr().lock().write_all(&line);
                    PENDING.fetch_sub(1, Ordering::Relaxed);
                }
            })
            .ok()
            .map(|_| tx)
    })
    .as_ref()
}
/// Bounded optional drain at CLI exit, after every transaction lock is released.
pub fn flush() {
    let at = Instant::now();
    while PENDING.load(Ordering::Relaxed) != 0 && at.elapsed() < Duration::from_millis(50) {
        std::thread::sleep(Duration::from_millis(1));
    }
}
pub struct Scope {
    previous: Option<Context>,
}
impl Drop for Scope {
    fn drop(&mut self) {
        CONTEXT.with(|c| *c.borrow_mut() = self.previous.take());
    }
}
pub fn scope(enabled: bool, kind: &'static str) -> Scope {
    if !enabled {
        return Scope {
            previous: CONTEXT.with(|c| c.borrow().clone()),
        };
    }
    let mut random = [0u8; 16];
    if getrandom::fill(&mut random).is_err() {
        return Scope {
            previous: CONTEXT.with(|c| c.borrow().clone()),
        };
    }
    let id = random.iter().map(|b| format!("{b:02x}")).collect();
    let previous = CONTEXT.with(|c| c.replace(Some(Context { id, kind })));
    Scope { previous }
}
pub fn emit(
    enabled: bool,
    event: &'static str,
    phase: &str,
    elapsed: Duration,
    result: &str,
    attempt: Option<usize>,
) {
    if !enabled {
        return;
    }
    let context = CONTEXT.with(|c| c.borrow().clone());
    let Some(context) = context else {
        return;
    };
    let value = serde_json::json!({"type":"bn.git", "schema_version":1, "operation_id":context.id, "operation":context.kind, "event":event, "phase":phase, "duration_ms":elapsed.as_secs_f64()*1000.0, "result":result, "push_attempt":attempt});
    let Ok(mut line) = serde_json::to_vec(&value) else {
        return;
    };
    line.push(b'\n');
    if let Some(tx) = sink() {
        PENDING.fetch_add(1, Ordering::Relaxed);
        if tx.try_send(line).is_err() {
            PENDING.fetch_sub(1, Ordering::Relaxed);
        }
    }
}
pub fn result<T>(r: &Result<T, Error>) -> &'static str {
    use crate::domain::error::ErrorCategory::*;
    match r {
        Ok(_) => "success",
        Err(e) => match e.category() {
            Some(LockTimeout) => "lock_timeout",
            Some(GitTimeout) => "git_timeout",
            Some(GitCleanup) => "cleanup_failure",
            Some(GitFailure) => "git_failure",
            Some(GitSignaled) => "signaled",
            Some(GitConflict) => "git_conflict",
            None => "failure",
        },
    }
}
pub fn operation<T>(
    enabled: bool,
    kind: &'static str,
    f: impl FnOnce() -> Result<T, Error>,
) -> Result<T, Error> {
    let missing = CONTEXT.with(|c| c.borrow().is_none());
    let _scope = if missing {
        Some(scope(enabled, kind))
    } else {
        None
    };
    let at = Instant::now();
    let r = f();
    emit(enabled, "operation", kind, at.elapsed(), result(&r), None);
    r
}
