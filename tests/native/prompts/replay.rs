//! Prompt replay, future-timestamp, and conflicting-claim races.
use super::*;

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
