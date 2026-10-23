//! End-to-end retained CLI contracts over the same disposable real-Git sandbox.
use super::*;
fn json(s: &Sandbox, args: &[&str]) -> serde_json::Value {
    serde_json::from_str(&s.ok(args)).unwrap()
}
fn fixture() -> Sandbox {
    let s = Sandbox::new();
    let r = s.remote(false);
    s.ok(&["init", r.to_str().unwrap()]);
    s.ok(&["create", "seed"]);
    s
}
fn issue_path(s: &Sandbox, id: &str) -> PathBuf {
    let v = json(s, &["--json", "show", id]);
    s.path("home/hub").join(v["path"].as_str().unwrap())
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

#[test]
fn native_prompt_operations_are_lossless_audited_and_replay_safe() {
    let s = fixture();
    let hub = Hub::new(
        s.path("home/hub"),
        s.path("home/cache"),
        "main".into(),
        "Native Tester".into(),
        false,
        Duration::ZERO,
    )
    .unwrap();
    let resolved = beans::vault::Resolved {
        project: b"demo".to_vec(),
        repo_head: b"feedface".to_vec(),
        repo_branch: b"feature".to_vec(),
        ..Default::default()
    };
    let mutate = |change, actor: &str| {
        let mut op = beans::ops::RecordMutation::new(
            resolved.clone(),
            String::new(),
            change,
            actor.into(),
            None,
        );
        let result = hub.mutate(&mut op);
        (op, result)
    };

    let (created, result) = mutate(
        beans::ops::RecordChange::PromptCreate(prompt_edit(
            Some("Preserve This Prompt"),
            Some("queue"),
        )),
        "creator",
    );
    assert!(result.unwrap().pushed);
    assert!(
        fs::read_dir(s.path("home/hub/projects/demo/prompts"))
            .unwrap()
            .all(|entry| !entry
                .unwrap()
                .file_name()
                .to_string_lossy()
                .starts_with(".bn-write-")),
        "successful prompt publication must remove its temporary link"
    );
    assert_eq!(
        fs::read_dir(s.path("home/hub/.git/bn-temp-owners"))
            .unwrap()
            .count(),
        0,
        "successful prompt publication must remove its ownership evidence"
    );
    let path = s.path("home/hub").join(format!(
        "projects/demo/prompts/{}-preserve-this-prompt.md",
        created.id
    ));
    let doc = beans::domain::prompt::PromptDocument::parse_bytes(
        path.to_str().unwrap(),
        &fs::read(&path).unwrap(),
    )
    .unwrap();
    assert!(
        doc.body
            .as_bytes()
            .starts_with(beans::domain::template::default_prompt_template())
    );
    assert_eq!(doc.log.len(), 1);
    assert_eq!(doc.log[0].event.as_bytes(), b"prompt created");

    // A hand-authored unknown key/comment and body bytes must survive an owned edit.
    let authored = fs::read_to_string(&path).unwrap().replacen(
        "title:",
        "vendor: [keep, me] # authored\ntitle:",
        1,
    );
    fs::write(&path, authored).unwrap();
    // Publish the hand-authored bytes so the retry owns only its mutation commit.
    // The competing clone can then force the discard-and-reapply path safely.
    hub.sync().unwrap();
    let mut edit = beans::ops::PromptEdit {
        title: Some("Renamed".into()),
        note: Some("exact evidence".into()),
        ..Default::default()
    };
    let update = beans::ops::RecordMutation::new(
        resolved.clone(),
        created.id.clone(),
        beans::ops::RecordChange::PromptUpdate(edit.clone()),
        "editor".into(),
        None,
    );
    let competitor = s.clone_hub(&s.path("remote.git"), "prompt-competitor");
    let remote_body = b"remote latest prompt bytes\n".to_vec();
    let competitor_update = beans::ops::RecordMutation::new(
        resolved.clone(),
        created.id.clone(),
        beans::ops::RecordChange::PromptUpdate(beans::ops::PromptEdit {
            body: Some(remote_body.clone()),
            note: Some("remote body evidence".into()),
            ..Default::default()
        }),
        "remote-editor".into(),
        None,
    );
    let mut race = PromptRace {
        primary: update,
        competitor,
        competitor_update,
        fired: false,
        survived: Vec::new(),
    };
    assert!(hub.mutate(&mut race).unwrap().pushed);
    assert_eq!(race.survived, [false]);
    assert!(path.exists(), "title edits must not rename the prompt");
    let bytes = fs::read(&path).unwrap();
    assert!(String::from_utf8_lossy(&bytes).contains("vendor: [keep, me] # authored"));
    let doc =
        beans::domain::prompt::PromptDocument::parse_bytes(path.to_str().unwrap(), &bytes).unwrap();
    assert_eq!(doc.body.as_bytes(), remote_body);
    assert_eq!(doc.log.len(), 3);
    assert_eq!(
        doc.log
            .iter()
            .filter(|entry| entry.event.as_bytes() == b"remote body evidence")
            .count(),
        1
    );
    assert_eq!(
        doc.log
            .iter()
            .filter(|entry| entry.event.as_bytes() == b"exact evidence")
            .count(),
        1
    );

    edit.note = None;
    let mut noop = beans::ops::RecordMutation::new(
        resolved.clone(),
        created.id.clone(),
        beans::ops::RecordChange::PromptUpdate(edit),
        "editor".into(),
        None,
    );
    assert!(hub.mutate(&mut noop).unwrap().sha.is_empty());

    let claim = beans::ops::PromptEdit {
        claim: true,
        ..Default::default()
    };
    let mut first_claim = beans::ops::RecordMutation::new(
        resolved.clone(),
        created.id.clone(),
        beans::ops::RecordChange::PromptUpdate(claim.clone()),
        "worker-a".into(),
        None,
    );
    hub.mutate(&mut first_claim).unwrap();
    let mut replayed_claim = beans::ops::RecordMutation::new(
        resolved.clone(),
        created.id.clone(),
        beans::ops::RecordChange::PromptUpdate(claim.clone()),
        "worker-a".into(),
        None,
    );
    assert!(hub.mutate(&mut replayed_claim).unwrap().sha.is_empty());
    let mut competing_claim = beans::ops::RecordMutation::new(
        resolved.clone(),
        created.id.clone(),
        beans::ops::RecordChange::PromptUpdate(claim),
        "worker-b".into(),
        None,
    );
    assert!(hub.mutate(&mut competing_claim).is_err());
    let latest = beans::domain::prompt::PromptDocument::parse_bytes(
        path.to_str().unwrap(),
        &fs::read(&path).unwrap(),
    )
    .unwrap();
    assert_eq!(latest.metadata.claimed_by.as_deref(), Some("worker-a"));
    assert_eq!(
        latest
            .log
            .iter()
            .filter(|entry| entry.event.as_bytes() == b"prompt claimed")
            .count(),
        1
    );

    let before_log = latest.log.len();
    let mut complete = beans::ops::RecordMutation::new(
        resolved.clone(),
        created.id.clone(),
        beans::ops::RecordChange::PromptUpdate(beans::ops::PromptEdit {
            status: Some("completed".into()),
            note: Some("completion evidence".into()),
            ..Default::default()
        }),
        "reviewer".into(),
        None,
    );
    hub.mutate(&mut complete).unwrap();
    let completed = beans::domain::prompt::PromptDocument::parse_bytes(
        path.to_str().unwrap(),
        &fs::read(&path).unwrap(),
    )
    .unwrap();
    assert_eq!(completed.log.len(), before_log + 1);
    assert_eq!(
        completed.log.last().unwrap().event.as_bytes(),
        b"completion evidence"
    );
    assert!(
        !completed
            .log
            .iter()
            .any(|entry| entry.event.as_bytes() == b"prompt status: completed")
    );

    let mut reassign = beans::ops::RecordMutation::new(
        resolved.clone(),
        created.id.clone(),
        beans::ops::RecordChange::PromptUpdate(beans::ops::PromptEdit {
            to: Some("new-queue".into()),
            force: true,
            ..Default::default()
        }),
        "dispatcher".into(),
        None,
    );
    hub.mutate(&mut reassign).unwrap();
    let reopened = beans::domain::prompt::PromptDocument::parse_bytes(
        path.to_str().unwrap(),
        &fs::read(&path).unwrap(),
    )
    .unwrap();
    assert_eq!(reopened.metadata.status, "open");
    assert_eq!(reopened.metadata.to, "new-queue");
    assert_eq!(reopened.metadata.claimed_by, None);

    let mut blank_note = beans::ops::RecordMutation::new(
        resolved.clone(),
        created.id.clone(),
        beans::ops::RecordChange::PromptUpdate(beans::ops::PromptEdit {
            note: Some(" \t".into()),
            ..Default::default()
        }),
        "editor".into(),
        None,
    );
    assert!(hub.mutate(&mut blank_note).is_err());

    let (fallback, result) = mutate(
        beans::ops::RecordChange::PromptCreate(prompt_edit(Some("!!!"), Some("queue"))),
        "creator",
    );
    result.unwrap();
    assert!(
        s.path("home/hub")
            .join(format!("projects/demo/prompts/{}.md", fallback.id))
            .exists()
    );
}

#[test]
fn prompt_creation_never_overwrites_unindexed_destination_artifacts() {
    let s = fixture();
    let hub = Hub::new(
        s.path("home/hub"),
        s.path("home/cache"),
        "main".into(),
        "Native Tester".into(),
        false,
        Duration::ZERO,
    )
    .unwrap();
    let resolved = beans::vault::Resolved {
        project: b"demo".to_vec(),
        ..Default::default()
    };
    let prompts = s.path("home/hub/projects/demo/prompts");
    fs::create_dir_all(&prompts).unwrap();

    let malformed_path = prompts.join("demo-p-dead-malformed-collision.md");
    let malformed = b"not a prompt at all\n\0original bytes\n".to_vec();
    fs::write(&malformed_path, &malformed).unwrap();
    hub.sync().unwrap();
    let mut malformed_create = beans::ops::RecordMutation::new(
        resolved.clone(),
        String::new(),
        beans::ops::RecordChange::PromptCreate(prompt_edit(
            Some("Malformed Collision"),
            Some("queue"),
        )),
        "creator".into(),
        None,
    );
    malformed_create.id = "demo-p-dead".into();
    assert!(hub.mutate(&mut malformed_create).is_err());
    assert_eq!(fs::read(&malformed_path).unwrap(), malformed);

    let mut source = beans::ops::RecordMutation::new(
        resolved.clone(),
        String::new(),
        beans::ops::RecordChange::PromptCreate(prompt_edit(Some("Source"), Some("queue"))),
        "creator".into(),
        None,
    );
    hub.mutate(&mut source).unwrap();
    let source_path = prompts.join(format!("{}-source.md", source.id));
    let mismatched = fs::read(&source_path).unwrap();
    fs::remove_file(&source_path).unwrap();
    let mismatch_path = prompts.join("demo-p-beef-metadata-mismatch.md");
    fs::write(&mismatch_path, &mismatched).unwrap();
    hub.sync().unwrap();
    let mut mismatch_create = beans::ops::RecordMutation::new(
        resolved,
        String::new(),
        beans::ops::RecordChange::PromptCreate(prompt_edit(
            Some("Metadata Mismatch"),
            Some("queue"),
        )),
        "creator".into(),
        None,
    );
    mismatch_create.id = "demo-p-beef".into();
    assert!(hub.mutate(&mut mismatch_create).is_err());
    assert_eq!(fs::read(&mismatch_path).unwrap(), mismatched);
}

#[test]
fn prompt_creation_loses_a_toctou_race_without_overwriting_the_winner() {
    use std::{
        io::Write as _,
        os::unix::fs::OpenOptionsExt as _,
        sync::mpsc::{self, TryRecvError},
    };

    let s = fixture();
    let hub_dir = s.path("home/hub");
    let templates = hub_dir.join("projects/demo/templates");
    fs::create_dir_all(&templates).unwrap();
    let template = templates.join("prompt.md");
    assert!(
        Command::new("mkfifo")
            .arg(&template)
            .status()
            .unwrap()
            .success()
    );

    let relative = PathBuf::from("projects/demo/prompts/demo-p-race-atomic-collision.md");
    let destination = hub_dir.join(&relative);
    fs::create_dir_all(destination.parent().unwrap()).unwrap();
    let apply_hub = hub_dir.clone();
    let (result_tx, result_rx) = mpsc::sync_channel(1);
    let mutation = std::thread::spawn(move || {
        let mut create = beans::ops::RecordMutation::new(
            beans::vault::Resolved {
                project: b"demo".to_vec(),
                ..Default::default()
            },
            String::new(),
            beans::ops::RecordChange::PromptCreate(prompt_edit(
                Some("Atomic Collision"),
                Some("queue"),
            )),
            "creator".into(),
            None,
        );
        create.id = "demo-p-race".into();
        result_tx.send(create.apply(&apply_hub)).unwrap();
    });

    // A nonblocking writer succeeds only after prompt creation has passed its
    // destination check and is blocked with the FIFO open for reading. Polling
    // the result channel makes an early mutation exit fail promptly.
    let deadline = std::time::Instant::now() + Duration::from_secs(10);
    let mut template_writer = loop {
        match fs::OpenOptions::new()
            .write(true)
            .custom_flags(libc::O_NONBLOCK)
            .open(&template)
        {
            Ok(writer) => break writer,
            Err(error) if error.raw_os_error() == Some(libc::ENXIO) => {}
            Err(error) => panic!("failed to synchronize through template FIFO: {error}"),
        }
        match result_rx.try_recv() {
            Ok(result) => panic!("mutation exited before opening template FIFO: {result:?}"),
            Err(TryRecvError::Disconnected) => {
                panic!("mutation thread exited before opening template FIFO")
            }
            Err(TryRecvError::Empty) => {}
        }
        assert!(
            std::time::Instant::now() < deadline,
            "timed out waiting for mutation to open template FIFO"
        );
        std::thread::park_timeout(Duration::from_millis(10));
    };
    let hand_authored = b"hand-authored bytes win exactly\n\0do not replace\n";
    fs::write(&destination, hand_authored).unwrap();
    template_writer
        .write_all(b"prompt template body\n")
        .unwrap();
    drop(template_writer);

    let error = result_rx
        .recv_timeout(Duration::from_secs(10))
        .expect("mutation did not finish after template release")
        .unwrap_err();
    mutation.join().unwrap();
    assert!(error.to_string().contains("already exists"), "{error}");
    assert_eq!(fs::read(destination).unwrap(), hand_authored);
    assert!(
        fs::read_dir(hub_dir.join("projects/demo/prompts"))
            .unwrap()
            .all(|entry| !entry
                .unwrap()
                .file_name()
                .to_string_lossy()
                .starts_with(".bn-write-"))
    );
    assert_eq!(
        fs::read_dir(hub_dir.join(".git/bn-temp-owners"))
            .unwrap()
            .count(),
        0
    );
}

#[test]
fn generated_prompt_ids_reserve_unparseable_prompt_filenames() {
    let s = fixture();
    let hub = Hub::new(
        s.path("home/hub"),
        s.path("home/cache"),
        "main".into(),
        "Native Tester".into(),
        false,
        Duration::ZERO,
    )
    .unwrap();
    let hub_config = s.path("home/hub/beans.toml");
    let mut config = fs::read_to_string(&hub_config).unwrap_or_default();
    config.push_str("\n[ids]\nlength = 1\n");
    fs::write(&hub_config, config).unwrap();
    let prompts = s.path("home/hub/projects/demo/prompts");
    fs::create_dir_all(&prompts).unwrap();
    let artifact = b"reserved malformed prompt bytes\n";
    for suffix in b"abcdefghijklmnopqrstuvwxyz0123456789" {
        fs::write(
            prompts.join(format!("demo-p-{}-reserved.md", char::from(*suffix))),
            artifact,
        )
        .unwrap();
    }
    hub.sync().unwrap();
    let mut create = beans::ops::RecordMutation::new(
        beans::vault::Resolved {
            project: b"demo".to_vec(),
            ..Default::default()
        },
        String::new(),
        beans::ops::RecordChange::PromptCreate(prompt_edit(Some("Reserved"), Some("queue"))),
        "creator".into(),
        None,
    );
    hub.mutate(&mut create).unwrap();
    assert_eq!(create.id.len(), "demo-p-".len() + 2);
    for suffix in b"abcdefghijklmnopqrstuvwxyz0123456789" {
        assert_eq!(
            fs::read(prompts.join(format!("demo-p-{}-reserved.md", char::from(*suffix)))).unwrap(),
            artifact
        );
    }
}

#[test]
fn replay_revalidates_assigned_prompt_id_against_unparseable_remote_filename() {
    let s = fixture();
    let hub = Hub::new(
        s.path("home/hub"),
        s.path("home/cache"),
        "main".into(),
        "Native Tester".into(),
        false,
        Duration::ZERO,
    )
    .unwrap();
    let competitor = s.clone_hub(&s.path("remote.git"), "malformed-reservation-winner");
    let primary = beans::ops::RecordMutation::new(
        beans::vault::Resolved {
            project: b"demo".to_vec(),
            ..Default::default()
        },
        String::new(),
        beans::ops::RecordChange::PromptCreate(prompt_edit(
            Some("Reserved During Replay"),
            Some("queue"),
        )),
        "creator".into(),
        None,
    );
    let mut race = PromptCreateReservationRace {
        primary,
        competitor,
        fired: false,
        survived: Vec::new(),
        artifact: None,
    };

    let result = hub.mutate(&mut race);
    assert!(result.is_err(), "replay must reject the newly reserved ID");
    assert_eq!(
        race.survived.len(),
        1,
        "the rejected push must enter the non-fast-forward replay path"
    );
    let (artifact, bytes) = race.artifact.as_ref().unwrap();
    assert_eq!(fs::read(s.path("home/hub").join(artifact)).unwrap(), *bytes);
    assert_eq!(
        fs::read(race.competitor.dir.join(artifact)).unwrap(),
        *bytes
    );
    let prompt = race.competitor.dir.join(format!(
        "projects/demo/prompts/{}-reserved-during-replay.md",
        race.primary.id
    ));
    assert!(
        !prompt.exists(),
        "the rejected replay must not publish its prompt"
    );
}

#[test]
fn replay_keeps_remote_future_updated_and_appends_each_prompt_log_once() {
    let s = fixture();
    let hub = Hub::new(
        s.path("home/hub"),
        s.path("home/cache"),
        "main".into(),
        "Native Tester".into(),
        false,
        Duration::ZERO,
    )
    .unwrap();
    let resolved = beans::vault::Resolved {
        project: b"demo".to_vec(),
        ..Default::default()
    };
    let mut create = beans::ops::RecordMutation::new(
        resolved.clone(),
        String::new(),
        beans::ops::RecordChange::PromptCreate(prompt_edit(Some("Monotonic"), Some("queue"))),
        "creator".into(),
        None,
    );
    hub.mutate(&mut create).unwrap();
    let relative = PathBuf::from(format!("projects/demo/prompts/{}-monotonic.md", create.id));
    let future = beans::domain::issue::Timestamp {
        seconds: 4_102_444_800,
        nanoseconds: 0,
        offset_seconds: 0,
    };
    let remote_body = b"remote bytes that must remain latest\n".to_vec();
    let primary = beans::ops::RecordMutation::new(
        resolved,
        create.id,
        beans::ops::RecordChange::PromptUpdate(beans::ops::PromptEdit {
            title: Some("Locally Retitled".into()),
            note: Some("losing operation evidence".into()),
            ..Default::default()
        }),
        "local-editor".into(),
        None,
    );
    let competitor = s.clone_hub(&s.path("remote.git"), "future-prompt-winner");
    let mut race = FuturePromptRace {
        primary,
        competitor,
        competitor_update: FuturePromptUpdate {
            path: relative.clone(),
            body: remote_body.clone(),
            at: future.clone(),
        },
        fired: false,
        survived: Vec::new(),
    };
    hub.mutate(&mut race).unwrap();
    assert_eq!(race.survived, [false]);
    let latest = beans::domain::prompt::PromptDocument::parse_bytes(
        &relative.to_string_lossy(),
        &fs::read(s.path("home/hub").join(&relative)).unwrap(),
    )
    .unwrap();
    assert_eq!(latest.metadata.updated, future);
    assert_eq!(latest.body.as_bytes(), remote_body);
    for event in [
        b"future remote body".as_slice(),
        b"losing operation evidence",
    ] {
        assert_eq!(
            latest
                .log
                .iter()
                .filter(|entry| entry.event.as_bytes() == event)
                .count(),
            1
        );
    }
}

#[test]
fn conflicting_prompt_claim_is_rejected_when_its_commit_loses_the_push_race() {
    let s = fixture();
    let hub = Hub::new(
        s.path("home/hub"),
        s.path("home/cache"),
        "main".into(),
        "Native Tester".into(),
        false,
        Duration::ZERO,
    )
    .unwrap();
    let resolved = beans::vault::Resolved {
        project: b"demo".to_vec(),
        repo_head: b"feedface".to_vec(),
        repo_branch: b"feature".to_vec(),
        ..Default::default()
    };
    let mut create = beans::ops::RecordMutation::new(
        resolved.clone(),
        String::new(),
        beans::ops::RecordChange::PromptCreate(prompt_edit(Some("Raced claim"), Some("queue"))),
        "creator".into(),
        None,
    );
    hub.mutate(&mut create).unwrap();
    let relative = PathBuf::from(format!(
        "projects/demo/prompts/{}-raced-claim.md",
        create.id
    ));
    let competitor = s.clone_hub(&s.path("remote.git"), "claim-winner");
    let entered = s.path("claim-push-entered");
    let release = s.path("claim-push-release");
    hook(
        &hub,
        "pre-push",
        &format!(
            "if test ! -e '{}'; then\n  : > '{}'\n  while test ! -e '{}'; do sleep 0.01; done\nfi",
            entered.display(),
            entered.display(),
            release.display()
        ),
    );

    let claim = beans::ops::PromptEdit {
        claim: true,
        ..Default::default()
    };
    let mut losing_claim = ReplayProbe {
        primary: beans::ops::RecordMutation::new(
            resolved.clone(),
            create.id.clone(),
            beans::ops::RecordChange::PromptUpdate(claim.clone()),
            "worker-a".into(),
            None,
        ),
        survived: Vec::new(),
    };
    let loser = std::thread::spawn(move || {
        let result = hub.mutate(&mut losing_claim);
        (result, losing_claim)
    });
    wait_for_path(&entered);
    assert_eq!(
        git(
            &s.path("home/hub"),
            &["rev-list", "--count", "origin/main..HEAD"]
        ),
        "1",
        "worker A must have committed its claim before worker B pushes"
    );

    let mut winning_claim = beans::ops::RecordMutation::new(
        resolved,
        create.id,
        beans::ops::RecordChange::PromptUpdate(claim),
        "worker-b".into(),
        None,
    );
    let winning_result = competitor.mutate(&mut winning_claim);
    fs::write(&release, b"continue\n").unwrap();
    winning_result.unwrap();
    let winning_head = git(&competitor.dir, &["rev-parse", "HEAD"]);
    let winning_bytes = fs::read(competitor.dir.join(&relative)).unwrap();

    let (losing_result, losing_claim) = loser.join().unwrap();
    assert!(losing_result.is_err());
    assert_eq!(losing_claim.survived, [false]);
    assert_eq!(
        fs::read(s.path("home/hub").join(&relative)).unwrap(),
        winning_bytes
    );
    assert_eq!(
        git(&s.path("remote.git"), &["rev-parse", "main"]),
        winning_head
    );
    let latest = beans::domain::prompt::PromptDocument::parse_bytes(
        relative.to_str().unwrap(),
        &winning_bytes,
    )
    .unwrap();
    assert_eq!(latest.metadata.claimed_by.as_deref(), Some("worker-b"));
    assert_eq!(
        latest
            .log
            .iter()
            .filter(|entry| entry.event.as_bytes() == b"prompt claimed")
            .count(),
        1
    );
    let subject = format!("bn: record {}", latest.metadata.id);
    assert_eq!(
        git(&competitor.dir, &["log", "--format=%s", "origin/main"])
            .lines()
            .filter(|candidate| *candidate == subject)
            .count(),
        2,
        "the remote must contain only prompt creation and worker B's claim"
    );
}

#[test]
fn native_retained_relationships_archive_reopen_and_force_delete() {
    let s = fixture();
    let a = s.ok(&["create", "parent", "--silent"]).trim().to_owned();
    let b = s
        .ok(&[
            "create",
            "child",
            "--parent",
            &a,
            "--blocked-by",
            &a,
            "--label",
            "first",
            "--label",
            "second",
            "--assignee",
            "owner",
            "--url",
            "https://example.org",
            "--silent",
        ])
        .trim()
        .to_owned();
    assert_eq!(json(&s, &["--json", "children", &a])[0]["id"], b);
    assert!(
        json(&s, &["--json", "blocked"])
            .as_array()
            .unwrap()
            .iter()
            .any(|v| v["issue"]["id"] == b)
    );
    let cycle = s.cli(&["dep", "add", &a, &b]);
    assert!(!cycle.status.success());
    assert!(String::from_utf8_lossy(&cycle.stderr).contains("cycle"));
    s.ok(&["sync"]);
    assert_eq!(
        json(&s, &["--json", "dep", "cycles"]),
        serde_json::json!([])
    );
    let tree = json(&s, &["--json", "dep", "tree", &b]);
    assert_eq!(tree["nodes"].as_array().unwrap().len(), 2);
    assert!(!s.cli(&["close", &a]).status.success());
    s.ok(&["sync"]);
    s.ok(&["close", &a, "--force"]);
    s.ok(&[
        "update",
        &b,
        "--unlabel",
        "first",
        "--label",
        "third",
        "--description",
        "new description",
        "--type",
        "bug",
        "--note",
        "retained note",
    ]);
    let changed = json(&s, &["--json", "show", &b]);
    assert_eq!(changed["labels"], serde_json::json!(["second", "third"]));
    assert_eq!(
        changed["description"].as_str().unwrap().trim(),
        "new description"
    );
    assert!(
        changed["log"]
            .as_array()
            .unwrap()
            .iter()
            .any(|l| l["event"] == "retained note")
    );
    s.ok(&["close", &b, "-r", "done"]);
    let before_archive = issue_path(&s, &b);
    let raw = fs::read_to_string(&before_archive)
        .unwrap()
        .replace("2026-", "2025-");
    fs::write(&before_archive, raw).unwrap();
    s.ok(&["archive", "--older-than", "30d"]);
    let archived = issue_path(&s, &b);
    assert!(archived.to_string_lossy().contains("/archive/2025/"));
    assert!(!before_archive.exists());
    let raw = fs::read_to_string(&archived).unwrap().replacen(
        "title:",
        "vendor: {keep: [one, two]} # authored\ntitle:",
        1,
    );
    fs::write(&archived, &raw).unwrap();
    s.ok(&["reopen", &b]);
    let restored = issue_path(&s, &b);
    assert!(restored.to_string_lossy().contains("/issues/"));
    assert!(!archived.exists());
    assert!(
        fs::read_to_string(&restored)
            .unwrap()
            .contains("vendor: {keep: [one, two]} # authored")
    );
    assert!(!s.cli(&["delete", &a]).status.success());
    s.ok(&["sync"]);
    s.ok(&["delete", &a, "--force"]);
    let child = json(&s, &["--json", "show", &b]);
    assert_eq!(child["parent"]["target"], "");
    assert!(child["blocked_by"].as_array().unwrap().is_empty());
}
#[test]
fn native_request_and_continuation_keep_unknown_authored_bytes() {
    let s = fixture();
    let issue = s
        .ok(&["create", "implement request", "--silent"])
        .trim()
        .to_owned();
    let body = s.path("body.md");
    fs::write(&body, "Requested outcome\n\nKeep this prose.\n").unwrap();
    let request = json(
        &s,
        &[
            "--json",
            "request",
            "create",
            "User request",
            "--body-file",
            body.to_str().unwrap(),
            "--issue",
            &issue,
            "--label",
            "milestone",
            "--requested-by",
            "human",
        ],
    )["id"]
        .as_str()
        .unwrap()
        .to_owned();
    let before = json(&s, &["--json", "request", "show", &request]);
    assert!(
        before["body"]
            .as_str()
            .unwrap()
            .contains("Keep this prose.")
    );
    let path = s.path("home/hub").join(before["path"].as_str().unwrap());
    let bytes = fs::read_to_string(&path).unwrap().replacen(
        "title:",
        "unknown: 'verbatim value' # keep comment\ntitle:",
        1,
    );
    fs::write(&path, &bytes).unwrap();
    assert!(
        !s.cli(&["request", "update", &request, "--status", "resolved"])
            .status
            .success()
    );
    s.ok(&["sync"]);
    for status in ["accepted", "in_progress", "resolved"] {
        s.ok(&["request", "update", &request, "--status", status]);
    }
    s.ok(&["request", "unlink", &request, &issue]);
    assert!(
        json(&s, &["--json", "request", "show", &request])["issues"]
            .as_array()
            .unwrap()
            .is_empty()
    );
    s.ok(&["request", "link", &request, &issue]);
    assert!(
        fs::read_to_string(&path)
            .unwrap()
            .contains("unknown: 'verbatim value' # keep comment")
    );
    let handoff = json(
        &s,
        &[
            "--json",
            "handoff",
            "create",
            "Next session",
            "--file",
            body.to_str().unwrap(),
            "--issue",
            &issue,
        ],
    )["id"]
        .as_str()
        .unwrap()
        .to_owned();
    let handoff_before = json(&s, &["--json", "handoff", "show", &handoff]);
    let path = s
        .path("home/hub")
        .join(handoff_before["path"].as_str().unwrap());
    fs::write(
        &path,
        fs::read_to_string(&path).unwrap().replacen(
            "title:",
            "unknown: [keep, this] # handoff\ntitle:",
            1,
        ),
    )
    .unwrap();
    s.ok(&["handoff", "detach", &handoff]);
    s.ok(&["handoff", "attach", &handoff, &issue]);
    s.ok(&["handoff", "archive", &handoff]);
    assert!(!path.exists());
    assert!(
        json(&s, &["--json", "handoff", "list"])
            .as_array()
            .unwrap()
            .is_empty()
    );
    s.ok(&["handoff", "restore", &handoff]);
    let raw = s.ok(&["handoff", "show", &handoff, "--raw"]);
    assert!(raw.contains("unknown: [keep, this] # handoff"));
    assert!(raw.ends_with("Requested outcome\n\nKeep this prose.\n"));
    let clone = s.clone_hub(&s.path("remote.git"), "observer");
    assert!(
        clone
            .dir
            .join("projects/demo/requests")
            .read_dir()
            .unwrap()
            .count()
            > 1
    );
    assert!(
        clone
            .dir
            .join("projects/demo/handoffs")
            .join(path.file_name().unwrap())
            .exists()
    );
}
#[test]
fn native_plan_bundle_stale_revision_roundtrip_and_node_workflows() {
    let s = fixture();
    let issue = s
        .ok(&["create", "plan implementation", "--silent"])
        .trim()
        .to_owned();
    let draft = s.path("draft");
    let plan = json(
        &s,
        &[
            "--json",
            "plan",
            "init",
            "Bounded plan",
            "--output",
            draft.to_str().unwrap(),
        ],
    )["id"]
        .as_str()
        .unwrap()
        .to_owned();
    let manifest = draft.join("plan.md");
    let raw = fs::read_to_string(&manifest)
        .unwrap()
        .replace("title:", "# authored frontmatter comment\ntitle:")
        .replace(
            "nodes: []",
            "nodes:\n  - id: implement\n    label: Implement work\n    kind: workflow",
        );
    fs::write(&manifest, raw).unwrap();
    s.ok(&["plan", "validate", draft.to_str().unwrap()]);
    s.ok(&["plan", "put", draft.to_str().unwrap()]);
    let stale = s.path("stale");
    s.ok(&["plan", "get", &plan, "--output", stale.to_str().unwrap()]);
    s.ok(&["plan", "link", &plan, "implement", &issue]);
    let stale_result = s.cli(&["plan", "put", stale.to_str().unwrap()]);
    assert!(!stale_result.status.success());
    assert!(String::from_utf8_lossy(&stale_result.stderr).contains("stale plan"));
    s.ok(&["sync"]);
    let detail = json(&s, &["--json", "plan", "show", &plan]);
    assert_eq!(detail["graph"]["nodes"][0]["ref"], issue);
    let published = s.path("home/hub").join(detail["path"].as_str().unwrap());
    assert!(
        fs::read_to_string(&published)
            .unwrap()
            .contains("# authored frontmatter comment")
    );
    s.ok(&["plan", "status", &plan]);
    s.ok(&["plan", "unlink", &plan, "implement", &issue]);
    let fresh = s.path("fresh");
    s.ok(&["plan", "get", &plan, "--output", fresh.to_str().unwrap()]);
    fs::create_dir(fresh.join("sections")).unwrap();
    fs::write(
        fresh.join("sections/design.md"),
        "# Design\n\nAuthored design.\n",
    )
    .unwrap();
    let raw = fs::read_to_string(fresh.join("plan.md")).unwrap().replacen(
        "---\n\n",
        "sections: [sections/design.md]\n---\n\n",
        1,
    );
    fs::write(fresh.join("plan.md"), raw).unwrap();
    s.ok(&["plan", "put", fresh.to_str().unwrap()]);
    let roundtrip = s.path("roundtrip");
    s.ok(&[
        "plan",
        "get",
        &plan,
        "--output",
        roundtrip.to_str().unwrap(),
    ]);
    assert_eq!(
        fs::read(roundtrip.join("sections/design.md")).unwrap(),
        b"# Design\n\nAuthored design.\n"
    );
    let raw = fs::read_to_string(roundtrip.join("plan.md"))
        .unwrap()
        .replace("sections: [sections/design.md]\n", "");
    fs::write(roundtrip.join("plan.md"), raw).unwrap();
    fs::remove_file(roundtrip.join("sections/design.md")).unwrap();
    s.ok(&["plan", "put", roundtrip.to_str().unwrap()]);
    assert!(
        !published
            .parent()
            .unwrap()
            .join("sections/design.md")
            .exists()
    );
    let head = git(&s.path("home/hub"), &["rev-parse", "HEAD"]);
    s.ok(&["plan", "put", roundtrip.to_str().unwrap()]);
    assert_eq!(git(&s.path("home/hub"), &["rev-parse", "HEAD"]), head);
    s.ok(&["project", "create", "another"]);
    assert!(
        !s.cli(&[
            "--project",
            "another",
            "plan",
            "put",
            roundtrip.to_str().unwrap()
        ])
        .status
        .success()
    );
}
#[test]
fn native_memory_docs_projects_import_diagnostics_cache_and_root() {
    let s = fixture();
    s.ok(&[
        "remember",
        "Project fact",
        "--key",
        "project-fact",
        "--tag",
        "docs",
        "--type",
        "project",
    ]);
    s.ok(&[
        "remember",
        "Global fact",
        "--key",
        "global-fact",
        "--global",
    ]);
    assert_eq!(
        json(&s, &["--json", "memories", "--all"])
            .as_array()
            .unwrap()
            .len(),
        2
    );
    s.ok(&["forget", "global-fact", "--global"]);
    s.ok(&["doc", "new", "design/overview"]);
    assert!(s.ok(&["doc", "list"]).contains("overview"));
    assert!(s.ok(&["search", "Project fact"]).contains("memory"));
    s.ok(&["project", "create", "another"]);
    assert!(s.ok(&["project", "list"]).contains("another"));
    s.ok(&["project", "show", "another"]);
    let export = s.path("export.jsonl");
    fs::write(&export,"# bd export\n{bad}\n{\"id\":\"old-ab12\",\"title\":\"Imported\",\"status\":\"open\",\"issue_type\":\"task\",\"dependencies\":[{\"issue_id\":\"old-ab12\",\"depends_on_id\":\"missing-ab12\",\"type\":\"relates\"}]}\n{\"_type\":\"memory\",\"key\":\"bd-fact\",\"value\":\"Imported fact\"}\n").unwrap();
    let dry = json(
        &s,
        &[
            "--json",
            "import",
            "bd",
            export.to_str().unwrap(),
            "--dry-run",
        ],
    );
    assert_eq!(dry["report"]["issues"], 1);
    assert_eq!(dry["report"]["warnings"].as_array().unwrap().len(), 2);
    assert!(!s.cli(&["show", "old-ab12"]).status.success());
    s.ok(&["import", "bd", export.to_str().unwrap()]);
    assert_eq!(json(&s, &["--json", "show", "old-ab12"])["id"], "old-ab12");
    let malformed = s.path("home/hub/projects/demo/issues/bad.md");
    fs::write(&malformed, "---\nid: broken\n---\n").unwrap();
    let doctor = s.cli(&["--json", "doctor"]);
    assert!(!doctor.status.success());
    let diagnostics: serde_json::Value = serde_json::from_slice(&doctor.stdout).unwrap();
    assert!(!diagnostics["warnings"].as_array().unwrap().is_empty());
    let cache = s.path("home/.beans-state/hub");
    fs::write(cache.join("last-fetch"), "cache").unwrap();
    fs::write(cache.join("derived"), "preserved unknown").unwrap();
    fs::write(cache.join("op-journal.json"), "retained recovery evidence").unwrap();
    s.ok(&["cache", "clear"]);
    assert!(!cache.join("last-fetch").exists());
    assert!(cache.join("derived").exists());
    assert_eq!(
        fs::read_to_string(cache.join("op-journal.json")).unwrap(),
        "retained recovery evidence"
    );
    assert!(s.ok(&[]).contains("Usage:"));
    assert!(s.ok(&["--version"]).contains("bn"));
    assert!(s.ok(&["man"]).contains("bn"));
    assert_eq!(s.ok(&["prime"]), include_str!("../../docs/prime.md"));
}
#[test]
fn retained_command_and_flag_census_is_callable() {
    let baseline: serde_json::Value =
        serde_json::from_str(include_str!("../fixtures/expected/commands.json")).unwrap();
    let mut root = beans::cli::command();
    root.build();
    for command in baseline["commands"].as_array().unwrap() {
        if command["retired"] == true {
            continue;
        }
        let path: Vec<_> = command["path"]
            .as_array()
            .unwrap()
            .iter()
            .map(|s| s.as_str().unwrap())
            .collect();
        if path.first().is_some_and(|s| matches!(*s, "serve" | "help")) {
            continue;
        }
        let mut actual = &root;
        for name in &path {
            actual = actual
                .find_subcommand(name)
                .unwrap_or_else(|| panic!("missing retained command {path:?}"));
        }
        for flag in command["local_flags"].as_array().unwrap() {
            let name = flag["name"].as_str().unwrap();
            if name == "help" {
                continue;
            }
            assert!(
                actual.get_arguments().any(|a| a.get_long() == Some(name)),
                "missing retained flag {path:?} --{name}"
            );
        }
    }
}
#[test]
fn stdin_bodies_and_atomic_validation_preserve_new_and_existing_projects() {
    use std::io::Write;
    use std::process::Stdio;
    let s = fixture();
    let issue = s
        .ok(&["create", "stdin issue", "--silent"])
        .trim()
        .to_owned();
    let stdin_run = |args: &[&str], body: &str| {
        let mut c = Command::new(env!("CARGO_BIN_EXE_bn"))
            .args(args)
            .env("BEANS_HOME", s.path("home"))
            .env("BEANS_PROJECT", "demo")
            .env_remove("BEANS_HUB")
            .env_remove("BN_CONFIG")
            .current_dir(&s.0)
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped())
            .spawn()
            .unwrap();
        c.stdin.take().unwrap().write_all(body.as_bytes()).unwrap();
        let o = c.wait_with_output().unwrap();
        assert!(o.status.success(), "{}", String::from_utf8_lossy(&o.stderr));
        serde_json::from_slice::<serde_json::Value>(&o.stdout).unwrap()
    };
    let handoff = stdin_run(
        &[
            "--json", "handoff", "create", "--file", "-", "--issue", &issue,
        ],
        "Verbatim continuation\n\n\n",
    );
    let id = handoff["id"].as_str().unwrap();
    assert_eq!(
        json(&s, &["--json", "handoff", "show", id])["body"],
        "Verbatim continuation\n\n\n"
    );
    let request = stdin_run(
        &["--json", "request", "create", "stdin request", "--stdin"],
        "stdin requested outcome\n",
    );
    let id = request["id"].as_str().unwrap();
    let before = json(&s, &["--json", "request", "show", id]);
    assert!(
        before["body"]
            .as_str()
            .unwrap()
            .contains("stdin requested outcome")
    );
    assert!(
        !s.cli(&["request", "link", id, &issue, "missing-ab12"])
            .status
            .success()
    );
    s.ok(&["sync"]);
    assert_eq!(
        json(&s, &["--json", "request", "show", id])["issues"],
        before["issues"]
    );
    assert!(
        !s.cli(&[
            "--project",
            "uncreated",
            "handoff",
            "create",
            "--file",
            "-",
            "--issue",
            "missing-ab12"
        ])
        .status
        .success()
    );
    assert!(!s.path("home/hub/projects/uncreated").exists());
    let explicit_empty = json(
        &s,
        &[
            "--json",
            "request",
            "create",
            "empty body",
            "--description",
            "",
        ],
    )["id"]
        .as_str()
        .unwrap()
        .to_owned();
    assert!(
        !json(&s, &["--json", "request", "show", &explicit_empty])["body"]
            .as_str()
            .unwrap()
            .contains("Outcome")
    );
}
#[test]
fn native_new_families_reject_symlink_destinations_without_external_writes() {
    use std::os::unix::fs::symlink;
    for family in ["requests", "handoffs", "memories", "docs", "plans"] {
        let s = fixture();
        let outside = s.path("authored-external");
        fs::create_dir(&outside).unwrap();
        fs::write(outside.join("keep.md"), "authored external bytes\n").unwrap();
        let destination = s.path(&format!("home/hub/projects/demo/{family}"));
        if destination.exists() {
            fs::remove_dir_all(&destination).unwrap();
        }
        symlink(&outside, &destination).unwrap();
        let body = s.path("body.md");
        fs::write(&body, "body\n").unwrap();
        let draft = s.path("draft");
        let output = match family {
            "requests" => s.cli(&["request", "create", "unsafe"]),
            "handoffs" => s.cli(&["handoff", "create", "--file", body.to_str().unwrap()]),
            "memories" => s.cli(&["remember", "unsafe", "--key", "unsafe"]),
            "docs" => s.cli(&["doc", "new", "unsafe"]),
            _ => {
                s.ok(&[
                    "plan",
                    "init",
                    "unsafe",
                    "--output",
                    draft.to_str().unwrap(),
                ]);
                s.cli(&["plan", "put", draft.to_str().unwrap()])
            }
        };
        assert!(!output.status.success(), "{family}");
        assert_eq!(
            fs::read_to_string(outside.join("keep.md")).unwrap(),
            "authored external bytes\n"
        );
        assert_eq!(fs::read_dir(&outside).unwrap().count(), 1);
    }
}
#[test]
fn native_alias_registry_closed_filters_and_partial_close_failure() {
    let s = fixture();
    let a = s.ok(&["create", "first", "--silent"]).trim().to_owned();
    let b = s
        .ok(&["create", "second", "--priority", "0", "--silent"])
        .trim()
        .to_owned();
    assert_eq!(json(&s, &["--json", "ready"])[0]["id"], b);
    let out = s.cli(&["close", &a, "missing-ab12", "-r", "finished"]);
    assert!(!out.status.success());
    s.ok(&["sync"]);
    assert_eq!(json(&s, &["--json", "show", &a])["status"], "closed");
    assert!(
        !s.ok(&["--json=false", "list", "--closed=false", "--limit", "-1"])
            .contains(&a)
    );
    let closed = json(&s, &["--json", "list", "--closed"]);
    assert!(closed.as_array().unwrap().iter().any(|i| i["id"] == a));
    assert!(!closed.as_array().unwrap().iter().any(|i| i["id"] == b));
    s.ok(&["reopen", &a]);
    let head = git(&s.path("home/hub"), &["rev-parse", "HEAD"]);
    s.ok(&["reopen", &a]);
    assert_eq!(git(&s.path("home/hub"), &["rev-parse", "HEAD"]), head);
    s.ok(&["doc", "new", "demo-collision", "--global"]);
    let hub = Hub::new(
        s.path("home/hub"),
        s.path("home/cache"),
        "main".into(),
        "Native Tester".into(),
        false,
        Duration::ZERO,
    )
    .unwrap();
    let resolved = beans::vault::Resolved {
        project: b"demo".to_vec(),
        ..Default::default()
    };
    let mut op = beans::ops::IssueMutation::new(
        resolved.clone(),
        String::new(),
        beans::ops::IssueChange::Create {
            title: "collision".into(),
            kind: "task".into(),
            priority: 2,
            description: None,
        },
        "Native Tester".into(),
        None,
    );
    op.id = "demo-collision".into();
    assert!(hub.mutate(&mut op).is_err());
    assert!(
        fs::read_to_string(s.path("home/hub/docs/demo-collision.md"))
            .unwrap()
            .contains("# demo-collision")
    );
    let mut request = beans::ops::RecordMutation::new(
        resolved,
        String::new(),
        beans::ops::RecordChange::RequestCreate(beans::ops::RequestEdit {
            title: Some("collision".into()),
            ..Default::default()
        }),
        "Native Tester".into(),
        None,
    );
    request.id = "demo-collision".into();
    assert!(hub.mutate(&mut request).is_err());
}
#[test]
fn native_live_bd_export_force_rerun_is_no_change_and_no_hub_errors() {
    let empty = Sandbox::new();
    assert!(!empty.cli(&["ready"]).status.success());
    assert!(!empty.cli(&["request", "list"]).status.success());
    assert!(empty.ok(&["man"]).starts_with(".TH BN 1"));
    assert_eq!(empty.ok(&["prime"]), include_str!("../../docs/prime.md"));
    assert_eq!(empty.cli(&["create"]).status.code(), Some(2));
    let s = fixture();
    let path = PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("tests/fixtures/native-baseline/cmd/bn/testdata/beans_export_2026-09-10.jsonl");
    let report = json(
        &s,
        &[
            "--json",
            "--project",
            "beans",
            "import",
            "bd",
            path.to_str().unwrap(),
        ],
    );
    assert!(report["report"]["issues"].as_u64().unwrap() > 10);
    assert!(report["report"]["rejected"].as_array().unwrap().is_empty());
    let hub = s.path("home/hub");
    let before = git(&hub, &["rev-parse", "HEAD"]);
    let repeated = json(
        &s,
        &[
            "--json",
            "--project",
            "beans",
            "import",
            "bd",
            path.to_str().unwrap(),
            "--force",
        ],
    );
    assert!(
        repeated["report"]["rejected"]
            .as_array()
            .unwrap()
            .is_empty()
    );
    assert_eq!(
        git(&hub, &["rev-parse", "HEAD"]),
        before,
        "{}",
        git(&hub, &["show", "--format=", "--"])
    );
}
#[test]
fn native_basename_parent_cycle_alias_registry_and_second_import_fixture() {
    let s = fixture();
    let a = s
        .ok(&["create", "alias owner", "--silent"])
        .trim()
        .to_owned();
    let b = s.ok(&["create", "dependent", "--silent"]).trim().to_owned();
    let path = issue_path(&s, &a);
    let basename = path.file_stem().unwrap().to_str().unwrap();
    s.ok(&["dep", "add", &b, basename]);
    s.ok(&["dep", "add", &b, basename, "--type", "parent-child"]);
    let cycle = s.cli(&["dep", "add", &a, &b, "--type", "parent-child"]);
    assert!(!cycle.status.success());
    assert!(String::from_utf8_lossy(&cycle.stderr).contains("cycle"));
    s.ok(&["sync"]);
    let source = fs::read(&path).unwrap();
    let mut authored = beans::domain::issue::IssueDocument::parse_bytes(
        "projects/demo/issues/authored.md",
        &source,
    )
    .unwrap();
    authored.metadata.aliases.push("demo-alias".into());
    fs::write(&path, authored.encode().unwrap().bytes).unwrap();
    let hub = Hub::new(
        s.path("home/hub"),
        s.path("home/cache"),
        "main".into(),
        "Native Tester".into(),
        false,
        Duration::ZERO,
    )
    .unwrap();
    let mut op = beans::ops::IssueMutation::new(
        beans::vault::Resolved {
            project: b"demo".to_vec(),
            ..Default::default()
        },
        String::new(),
        beans::ops::IssueChange::Create {
            title: "collision".into(),
            kind: "task".into(),
            priority: 2,
            description: None,
        },
        "Native Tester".into(),
        None,
    );
    op.id = "demo-alias".into();
    assert!(hub.mutate(&mut op).is_err());
    assert!(fs::read_to_string(&path).unwrap().contains("demo-alias"));
    s.ok(&["sync"]);
    let export = PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("tests/fixtures/native-baseline/cmd/bn/testdata/gastownhall_beads_export.jsonl");
    let report = json(
        &s,
        &[
            "--json",
            "--project",
            "gastownhall",
            "import",
            "bd",
            export.to_str().unwrap(),
        ],
    );
    assert!(report["report"]["issues"].as_u64().unwrap() > 0);
    assert!(report["report"]["rejected"].as_array().unwrap().is_empty());
    let ready = s.ok(&["create", "unblocked", "--silent"]).trim().to_owned();
    let blocker = s.ok(&["create", "blocker", "--silent"]).trim().to_owned();
    let waiting = s
        .ok(&["create", "waiting", "--blocked-by", &blocker, "--silent"])
        .trim()
        .to_owned();
    let result = json(
        &s,
        &["--json", "close", &blocker, "-r", "done", "--suggest-next"],
    );
    assert!(
        result["next_ready"]
            .as_array()
            .unwrap()
            .iter()
            .any(|v| v["id"] == waiting)
    );
    assert!(
        !result["next_ready"]
            .as_array()
            .unwrap()
            .iter()
            .any(|v| v["id"] == ready)
    );
}
