//! Prompt destination reservation and creation-race safety.
use super::*;

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
