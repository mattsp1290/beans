//! Prompt-specific native CLI support and cohesive journey modules.
use super::*;

fn json(s: &Sandbox, args: &[&str]) -> serde_json::Value {
    serde_json::from_str(&s.ok(args)).unwrap()
}

fn fixture() -> Sandbox {
    let s = Sandbox::new();
    let remote = s.remote(false);
    s.ok(&["init", remote.to_str().unwrap()]);
    s.ok(&["create", "seed"]);
    s
}

fn prompt_edit(title: Option<&str>, to: Option<&str>) -> beans::ops::PromptEdit {
    beans::ops::PromptEdit {
        title: title.map(str::to_owned),
        from: title.map(|_| "sender".to_owned()),
        to: to.map(str::to_owned),
        ..Default::default()
    }
}

struct PromptRace {
    primary: beans::ops::RecordMutation,
    competitor: Hub,
    competitor_update: beans::ops::RecordMutation,
    fired: bool,
    survived: Vec<bool>,
}
impl Operation for PromptRace {
    fn subject(&self) -> String {
        self.primary.subject()
    }
    fn after_rebase(&mut self, present: bool) {
        self.survived.push(present);
        self.primary.after_rebase(present);
    }
    fn apply(&mut self, hub: &Path) -> Result<Vec<PathBuf>, Error> {
        let paths = self.primary.apply(hub)?;
        if !self.fired {
            self.fired = true;
            self.competitor.mutate(&mut self.competitor_update)?;
        }
        Ok(paths)
    }
}

struct FuturePromptUpdate {
    path: PathBuf,
    body: Vec<u8>,
    at: beans::domain::issue::Timestamp,
}
impl Operation for FuturePromptUpdate {
    fn subject(&self) -> String {
        "future prompt update".into()
    }
    fn apply(&mut self, hub: &Path) -> Result<Vec<PathBuf>, Error> {
        let absolute = hub.join(&self.path);
        let mut document = beans::domain::prompt::PromptDocument::parse_bytes(
            &self.path.to_string_lossy(),
            &fs::read(&absolute).map_err(|error| Error::new(error.to_string()))?,
        )?;
        document.set_body_bytes(&self.body);
        document.append_log(beans::domain::log::LogEntry {
            at: self.at.clone(),
            actor: "remote-editor".into(),
            event: "future remote body".into(),
            ..Default::default()
        });
        fs::write(&absolute, document.encode()?.bytes)
            .map_err(|error| Error::new(error.to_string()))?;
        Ok(vec![self.path.clone()])
    }
}

struct FuturePromptRace {
    primary: beans::ops::RecordMutation,
    competitor: Hub,
    competitor_update: FuturePromptUpdate,
    fired: bool,
    survived: Vec<bool>,
}
impl Operation for FuturePromptRace {
    fn subject(&self) -> String {
        self.primary.subject()
    }
    fn after_rebase(&mut self, present: bool) {
        self.survived.push(present);
        self.primary.after_rebase(present);
    }
    fn apply(&mut self, hub: &Path) -> Result<Vec<PathBuf>, Error> {
        let paths = self.primary.apply(hub)?;
        if !self.fired {
            self.fired = true;
            self.competitor.mutate(&mut self.competitor_update)?;
        }
        Ok(paths)
    }
}

struct ReplayProbe {
    primary: beans::ops::RecordMutation,
    survived: Vec<bool>,
}
impl Operation for ReplayProbe {
    fn subject(&self) -> String {
        self.primary.subject()
    }
    fn after_rebase(&mut self, present: bool) {
        self.survived.push(present);
        self.primary.after_rebase(present);
    }
    fn apply(&mut self, hub: &Path) -> Result<Vec<PathBuf>, Error> {
        self.primary.apply(hub)
    }
}

struct MalformedPromptArtifact {
    path: PathBuf,
    bytes: Vec<u8>,
}
impl Operation for MalformedPromptArtifact {
    fn subject(&self) -> String {
        "concurrent malformed prompt artifact".into()
    }
    fn apply(&mut self, hub: &Path) -> Result<Vec<PathBuf>, Error> {
        let absolute = hub.join(&self.path);
        fs::create_dir_all(absolute.parent().unwrap())
            .map_err(|error| Error::new(error.to_string()))?;
        fs::write(&absolute, &self.bytes).map_err(|error| Error::new(error.to_string()))?;
        Ok(vec![self.path.clone()])
    }
}

struct PromptCreateReservationRace {
    primary: beans::ops::RecordMutation,
    competitor: Hub,
    fired: bool,
    survived: Vec<bool>,
    artifact: Option<(PathBuf, Vec<u8>)>,
}
impl Operation for PromptCreateReservationRace {
    fn subject(&self) -> String {
        self.primary.subject()
    }
    fn after_rebase(&mut self, present: bool) {
        self.survived.push(present);
        self.primary.after_rebase(present);
    }
    fn apply(&mut self, hub: &Path) -> Result<Vec<PathBuf>, Error> {
        let paths = self.primary.apply(hub)?;
        if !self.fired {
            self.fired = true;
            let path = PathBuf::from(format!(
                "projects/demo/prompts/{}-other.md",
                self.primary.id
            ));
            let bytes = b"malformed concurrent winner bytes\n\0preserve exactly\n".to_vec();
            let mut winner = MalformedPromptArtifact {
                path: path.clone(),
                bytes: bytes.clone(),
            };
            self.competitor.mutate(&mut winner)?;
            self.artifact = Some((path, bytes));
        }
        Ok(paths)
    }
}

fn wait_for_path(path: &Path) {
    let start = std::time::Instant::now();
    while !path.exists() {
        assert!(
            start.elapsed() < Duration::from_secs(10),
            "timed out waiting for {}",
            path.display()
        );
        std::thread::sleep(Duration::from_millis(10));
    }
}

mod cli_contract;
mod creation_safety;
mod lifecycle;
mod replay;
