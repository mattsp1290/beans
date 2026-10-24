//! Lossless prompt lifecycle and replay-safe mutation behavior.
use super::*;

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
