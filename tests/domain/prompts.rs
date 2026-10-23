use beans::domain::prompt;

const STATUSES: [&str; 5] = [
    prompt::OPEN,
    prompt::IN_PROGRESS,
    prompt::COMPLETED,
    prompt::DECLINED,
    prompt::CANCELLED,
];

#[test]
fn prompt_ids_use_the_prompt_namespace() {
    for valid in ["beans-p-a3f2", "a-p-0", "bean-counter-p-x1"] {
        assert!(prompt::valid_id(valid), "{valid:?}");
    }
    for invalid in [
        "beans-a3f2",
        "beans-p-",
        "-p-a3f2",
        "Beans-p-a3f2",
        "beans-P-a3f2",
        "beans-p-A3F2",
        "beans-p-a3f2-extra",
        "beans-p-a3f2.1",
        "beans-p-a_f2",
        "βeans-p-a3f2",
    ] {
        assert!(!prompt::valid_id(invalid), "{invalid:?}");
    }

    let generated = prompt::new_id("beans", Some(&mut |_| false), 7);
    assert_eq!(generated.len(), "beans-p-".len() + 7);
    assert!(prompt::valid_id(&generated));
}

#[test]
fn lifecycle_matrix_and_force_are_fixed_but_force_never_accepts_unknown_statuses() {
    for from in STATUSES {
        for to in STATUSES {
            let ordinary = from == to
                || from == prompt::OPEN
                    && matches!(
                        to,
                        prompt::IN_PROGRESS | prompt::DECLINED | prompt::CANCELLED
                    )
                || from == prompt::IN_PROGRESS
                    && matches!(to, prompt::COMPLETED | prompt::DECLINED | prompt::CANCELLED);
            assert_eq!(
                prompt::validate_transition(from, to, false).is_ok(),
                ordinary,
                "ordinary {from} -> {to}"
            );
            assert!(
                prompt::validate_transition(from, to, true).is_ok(),
                "forced {from} -> {to}"
            );
        }
    }

    for invalid in ["", "OPEN", "accepted", " open", "completed "] {
        assert!(!prompt::valid_status(invalid));
        assert!(prompt::validate_transition(invalid, prompt::OPEN, true).is_err());
        assert!(prompt::validate_transition(prompt::OPEN, invalid, true).is_err());
    }
}

#[test]
fn identities_are_trimmed_nonblank_case_preserving_coordination_strings() {
    assert_eq!(
        prompt::normalize_identity("  Agent-Σ/One  ").unwrap(),
        "Agent-Σ/One"
    );
    for blank in ["", "   ", "\t\n", "\u{2003}"] {
        assert!(prompt::normalize_identity(blank).is_err(), "{blank:?}");
    }
}

#[test]
fn claimant_state_invariants_hold_for_every_status() {
    assert!(prompt::State::new(prompt::OPEN, None, " Agent A ").is_ok());
    assert!(prompt::State::new(prompt::OPEN, Some("Agent A"), "Agent B").is_err());
    assert!(prompt::State::new(prompt::IN_PROGRESS, None, "Agent B").is_err());
    assert!(prompt::State::new(prompt::COMPLETED, None, "Agent B").is_err());
    for status in [prompt::IN_PROGRESS, prompt::COMPLETED] {
        let state = prompt::State::new(status, Some(" Agent A "), " Agent B ").unwrap();
        assert_eq!(state.claimed_by.as_deref(), Some("Agent A"));
        assert_eq!(state.to, "Agent B");
    }
    for status in [prompt::DECLINED, prompt::CANCELLED] {
        assert!(prompt::State::new(status, None, "Agent B").is_ok());
        assert!(prompt::State::new(status, Some("Agent A"), "Agent B").is_ok());
    }
    assert!(prompt::State::new(prompt::DECLINED, Some("  "), "Agent B").is_err());
    assert!(prompt::State::new("unknown", None, "Agent B").is_err());
}

#[test]
fn claim_is_atomic_conflict_safe_and_idempotent_for_the_same_actor() {
    let open = prompt::State::new(prompt::OPEN, None, "receiver").unwrap();
    let claimed = open.claim(" Agent A ").unwrap();
    assert_eq!(open.status, prompt::OPEN);
    assert_eq!(open.claimed_by, None);
    assert_eq!(claimed.status, prompt::IN_PROGRESS);
    assert_eq!(claimed.claimed_by.as_deref(), Some("Agent A"));
    assert_eq!(claimed.claim("Agent A").unwrap(), claimed);
    assert!(claimed.claim("agent a").is_err());
    assert_eq!(claimed.status, prompt::IN_PROGRESS);
    assert_eq!(claimed.claimed_by.as_deref(), Some("Agent A"));

    for status in [prompt::COMPLETED, prompt::DECLINED, prompt::CANCELLED] {
        let state = prompt::State::new(status, Some("Agent A"), "receiver").unwrap();
        assert!(state.claim("Agent A").is_err(), "{status}");
    }
}

#[test]
fn direct_status_changes_enforce_claim_effect_and_state_invariants_even_when_forced() {
    let open = prompt::State::new(prompt::OPEN, None, "receiver").unwrap();
    assert!(
        open.transition(prompt::IN_PROGRESS, Some("Agent A"), false)
            .is_err()
    );

    let claimed = open.claim("Agent A").unwrap();
    assert_eq!(
        claimed
            .transition(prompt::IN_PROGRESS, Some(" Agent A "), false)
            .unwrap(),
        claimed
    );
    assert!(
        claimed
            .transition(prompt::IN_PROGRESS, Some("Agent B"), false)
            .is_err()
    );
    let completed = claimed.transition(prompt::COMPLETED, None, false).unwrap();
    assert_eq!(completed.claimed_by.as_deref(), Some("Agent A"));

    // Force bypasses the matrix, not the open/unclaimed or invocation-actor rules.
    assert!(completed.transition(prompt::OPEN, None, true).is_err());
    assert!(
        completed
            .transition(prompt::IN_PROGRESS, None, true)
            .is_err()
    );
    assert!(
        completed
            .transition("unknown", Some("Agent A"), true)
            .is_err()
    );
}

#[test]
fn recipient_reassignment_is_open_only_unless_force_resets_claimed_or_terminal_state() {
    let open = prompt::State::new(prompt::OPEN, None, "Agent A").unwrap();
    let reassigned = open.reassign(" Agent B ", false).unwrap();
    assert_eq!(reassigned.to, "Agent B");
    assert_eq!(reassigned.status, prompt::OPEN);

    let claimed = reassigned.claim("Agent B").unwrap();
    assert!(claimed.reassign("Agent C", false).is_err());
    assert_eq!(claimed.reassign("Agent B", false).unwrap(), claimed);
    let same_recipient_reset = claimed.reassign("Agent B", true).unwrap();
    assert_eq!(same_recipient_reset.status, prompt::OPEN);
    assert_eq!(same_recipient_reset.claimed_by, None);
    let reset = claimed.reassign(" Agent C ", true).unwrap();
    assert_eq!(reset.to, "Agent C");
    assert_eq!(reset.status, prompt::OPEN);
    assert_eq!(reset.claimed_by, None);

    for status in [prompt::COMPLETED, prompt::DECLINED, prompt::CANCELLED] {
        let terminal = prompt::State::new(status, Some("Agent B"), "Agent B").unwrap();
        let reset = terminal.reassign("Agent C", true).unwrap();
        assert_eq!(reset.status, prompt::OPEN);
        assert_eq!(reset.claimed_by, None);
    }
    for status in [prompt::DECLINED, prompt::CANCELLED] {
        let terminal = prompt::State::new(status, None, "Agent B").unwrap();
        let reset = terminal.reassign("Agent C", true).unwrap();
        assert_eq!(reset.status, prompt::OPEN);
        assert_eq!(reset.claimed_by, None);
    }
}

#[test]
fn recipient_update_cannot_be_combined_with_claim_or_explicit_status() {
    assert!(prompt::validate_update_combination(true, false, None).is_ok());
    assert!(prompt::validate_update_combination(false, true, Some(prompt::COMPLETED)).is_ok());
    assert!(prompt::validate_update_combination(true, true, None).is_err());
    assert!(prompt::validate_update_combination(true, false, Some(prompt::OPEN)).is_err());
}

#[test]
fn prompt_filename_falls_back_to_the_id_for_non_ascii_or_punctuation_titles() {
    for title in ["!!! --- ???", "雪だるま"] {
        assert_eq!(prompt::slug(title), "");
        assert_eq!(prompt::filename("beans-p-a3f2", title), "beans-p-a3f2.md");
    }
    assert_eq!(
        prompt::filename("beans-p-a3f2", "Hello, Agent!"),
        "beans-p-a3f2-hello-agent.md"
    );
}
