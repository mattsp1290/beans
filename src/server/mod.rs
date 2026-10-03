//! Native HTTP adapter. Git and parsing effects stay off Tokio's reactor threads.
mod api;
mod files;
mod mutations;
mod wire;
use crate::{
    domain::frontmatter::Error,
    gitops::Hub,
    vault::{Index, LoadOptions},
};
use axum::{
    Router,
    body::{Bytes, HttpBody},
    extract::State,
    http::{Method, Uri},
    response::{
        IntoResponse, Response,
        sse::{Event, KeepAlive, Sse},
    },
    routing::get,
};
use notify::Watcher;
use std::{
    sync::{
        Arc, RwLock,
        atomic::{AtomicBool, Ordering},
    },
    time::Duration,
};
use tokio::sync::{mpsc, watch};
include!(concat!(env!("OUT_DIR"), "/assets.rs"));
pub struct App {
    pub hub: Hub,
    pub project: String,
    pub index: RwLock<Index>,
    pub reload: watch::Sender<u64>,
    pub shutdown: watch::Sender<bool>,
    pub options: LoadOptions,
    refresh_lock: std::sync::Mutex<()>,
}
impl App {
    pub fn new(hub: Hub, project: String) -> Result<Arc<Self>, Error> {
        let options = LoadOptions {
            explicit_workflow: std::env::var_os("BN_CONFIG")
                .filter(|s| !s.is_empty())
                .map(Into::into),
        };
        let index = Index::load_snapshot(&hub.dir, options.clone())?;
        let (reload, _) = watch::channel(0);
        let (shutdown, _) = watch::channel(false);
        Ok(Arc::new(Self {
            hub,
            project,
            index: RwLock::new(index),
            reload,
            shutdown,
            options,
            refresh_lock: std::sync::Mutex::new(()),
        }))
    }
    pub fn refresh(&self) -> Result<(), Error> {
        let _lock = self.refresh_lock.lock().unwrap();
        let index = Index::load_snapshot(&self.hub.dir, self.options.clone())?;
        *self.index.write().unwrap() = index;
        self.reload.send_modify(|n| *n += 1);
        Ok(())
    }
}
pub fn router(app: Arc<App>) -> Router {
    Router::new()
        .route("/api/events", get(events))
        .fallback(handle)
        .with_state(app)
}
async fn handle(
    State(app): State<Arc<App>>,
    method: Method,
    uri: Uri,
    body: Result<Bytes, axum::extract::rejection::BytesRejection>,
) -> Response {
    let body = match body {
        Ok(body) => body,
        Err(e) => return api::error(e.status().as_u16(), "invalid_body", &e.to_string()),
    };
    let head = method == Method::HEAD;
    match tokio::task::spawn_blocking(move || {
        let _scope = crate::gitops::diagnostics::scope(app.hub.executor.policy.diagnostics, "http");
        let at = std::time::Instant::now();
        let response = api::handle(&app, &method, &uri, &body);
        crate::gitops::diagnostics::emit(
            app.hub.executor.policy.diagnostics,
            "operation",
            "http",
            at.elapsed(),
            if response.status().is_success() {
                "success"
            } else {
                "failure"
            },
            None,
        );
        response
    })
    .await
    {
        Ok(mut response) => {
            if head {
                if !response.headers().contains_key("content-length")
                    && let Some(size) = response.body().size_hint().exact()
                {
                    response
                        .headers_mut()
                        .insert("content-length", size.to_string().parse().unwrap());
                }
                *response.body_mut() = axum::body::Body::empty();
            }
            response
        }
        Err(_) => api::error(500, "internal_error", "request worker failed"),
    }
}
struct Events(mpsc::Receiver<Result<Event, std::convert::Infallible>>);
impl futures_core::Stream for Events {
    type Item = Result<Event, std::convert::Infallible>;
    fn poll_next(
        mut self: std::pin::Pin<&mut Self>,
        cx: &mut std::task::Context<'_>,
    ) -> std::task::Poll<Option<Self::Item>> {
        self.0.poll_recv(cx)
    }
}
async fn events(State(app): State<Arc<App>>) -> Response {
    let (sender, receiver) = mpsc::channel(8);
    let mut reload = app.reload.subscribe();
    let mut shutdown = app.shutdown.subscribe();
    tokio::spawn(async move {
        if *shutdown.borrow() {
            return;
        }
        loop {
            tokio::select! {
                r = reload.changed() => {
                    if r.is_err() { break; }
                    let event = Event::default().event("reload").id(reload.borrow().to_string()).data("{}");
                    tokio::select! { _ = shutdown.changed() => break, result = sender.send(Ok(event)) => if result.is_err() { break; } }
                }
                _ = shutdown.changed() => break,
                _ = sender.closed() => break,
            }
        }
    });
    Sse::new(Events(receiver))
        .keep_alive(
            KeepAlive::new()
                .interval(Duration::from_secs(15))
                .text("heartbeat"),
        )
        .into_response()
}
/// Own the recursive watcher until shutdown; ignore Git internals and debounce bursts.
pub fn watcher(app: Arc<App>, stop: Arc<AtomicBool>) -> Result<std::thread::JoinHandle<()>, Error> {
    let (sender, receiver) = std::sync::mpsc::channel();
    let mut watcher = notify::recommended_watcher(move |event: notify::Result<notify::Event>| {
        let _ = sender.send(event);
    })
    .map_err(|e| Error::new(e.to_string()))?;
    watcher
        .watch(&app.hub.dir, notify::RecursiveMode::Recursive)
        .map_err(|e| Error::new(e.to_string()))?;
    Ok(std::thread::spawn(move || {
        let _watcher = watcher;
        while !stop.load(Ordering::Relaxed) {
            if let Ok(Ok(event)) = receiver.recv_timeout(Duration::from_millis(100)) {
                if !event.kind.is_modify() && !event.kind.is_create() && !event.kind.is_remove() {
                    continue;
                }
                if event.paths.iter().all(|p| {
                    p.strip_prefix(&app.hub.dir)
                        .is_ok_and(|r| r.components().any(|c| c.as_os_str() == ".git"))
                }) {
                    continue;
                }
                // A bounded debounce cannot starve under continuous external edits.
                std::thread::sleep(Duration::from_millis(100));
                while receiver.try_recv().is_ok() {}
                if let Err(e) = app.refresh() {
                    eprintln!("bn: watcher reload: {e}");
                }
            }
        }
    }))
}
pub fn run(hub: Hub, project: String, host: &str, port: &str) -> Result<(), Error> {
    let address = format!("{host}:{port}");
    let app = App::new(hub, project)?;
    tokio::runtime::Runtime::new()
        .map_err(|e| Error::new(e.to_string()))?
        .block_on(async {
            let listener = tokio::net::TcpListener::bind(&address)
                .await
                .map_err(|e| Error::new(e.to_string()))?;
            let stop = Arc::new(AtomicBool::new(false));
            let watcher = watcher(app.clone(), stop.clone())?;
            eprintln!("Serving http://{}", listener.local_addr().unwrap());
            let shutdown_app = app.clone();
            let result = axum::serve(listener, router(app.clone()))
                .with_graceful_shutdown(async move {
                    let mut term =
                        tokio::signal::unix::signal(tokio::signal::unix::SignalKind::terminate())
                            .expect("install SIGTERM handler");
                    tokio::select! { _ = tokio::signal::ctrl_c() => (), _ = term.recv() => () }
                    shutdown_app.shutdown.send_replace(true);
                })
                .await;
            app.shutdown.send_replace(true);
            stop.store(true, Ordering::Relaxed);
            let _ = watcher.join();
            result.map_err(|e| Error::new(e.to_string()))
        })
}
