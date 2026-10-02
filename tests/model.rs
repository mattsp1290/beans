//! Bounded decision exploration, not a Git/OS model. Both clients perform the
//! same idempotent operation. Git outcomes are injected; native CLI tests exercise real effects.
use beans::kernel::retry::{
    AttemptBudget, RetryAction, RetryFacts, decide_retry, take_push_attempt,
};
use stateright::{Checker, Model, Property};

#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash)]
enum Stage {
    Apply,
    Push,
    Rebase,
    Done,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash)]
enum Outcome {
    Initial,
    Applied,
    Empty,
    Success,
    Rejected,
    NetworkFailure,
    Rebased,
    Dropped,
    Conflict,
    Exhausted,
    Restart,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash)]
struct Client {
    stage: Stage,
    base_version: u8,
    base_effects: u8,
    // Four distinct bits identify each client's offline and hand-edit commits.
    users: u8,
    operation: bool,
    owned_by_run: bool,
    total_pushes: u8,
    run_pushes: u8,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash)]
struct State {
    clients: [Client; 2],
    initial_users: u8,
    remote_users: u8,
    remote_version: u8,
    // A counter, not a set: a duplicate operation effect is observable.
    remote_effects: u8,
    restarted: bool,
    bad_discard: bool,
    last: Outcome,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum Event {
    Apply,
    Success,
    Rejected,
    NetworkFailure,
    Rebased,
    Conflict,
    Restart,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
struct Action(usize, Event);

struct Transactions;

fn retry_facts(client: &Client) -> RetryFacts {
    RetryFacts {
        operation_present: client.operation,
        head_is_operation: client.operation,
        local_commits: client.users.count_ones() as usize + usize::from(client.operation),
        owned_by_run: client.owned_by_run,
        push_attempts: usize::from(client.run_pushes),
    }
}

/// The checker stores a scalar history because budgets cannot be cloned. Every
/// transition reconstructs it by replaying grants through the real helper;
/// budget arithmetic is not duplicated in a separate proof-only implementation.
fn grant(client: &mut Client) {
    let mut budget = AttemptBudget::for_mutation();
    for _ in 0..client.run_pushes {
        assert!(take_push_attempt(&mut budget));
    }
    assert!(take_push_attempt(&mut budget));
    client.run_pushes = 3 - budget.remaining();
    client.total_pushes += 1;
}

impl Model for Transactions {
    type State = State;
    type Action = Action;

    fn init_states(&self) -> Vec<State> {
        // Each clone has zero/one offline commit and zero/one hand-edit commit.
        (0..16)
            .map(|users| State {
                clients: std::array::from_fn(|index| Client {
                    stage: Stage::Apply,
                    base_version: 0,
                    base_effects: 0,
                    users: users & (3 << (index * 2)),
                    operation: false,
                    owned_by_run: false,
                    total_pushes: 0,
                    run_pushes: 0,
                }),
                initial_users: users,
                remote_users: 0,
                remote_version: 0,
                remote_effects: 0,
                restarted: false,
                bad_discard: false,
                last: Outcome::Initial,
            })
            .collect()
    }

    fn actions(&self, state: &State, actions: &mut Vec<Action>) {
        for (index, client) in state.clients.iter().enumerate() {
            match client.stage {
                Stage::Apply => actions.push(Action(index, Event::Apply)),
                Stage::Push if client.total_pushes < 3 => {
                    if client.base_version == state.remote_version {
                        actions.push(Action(index, Event::Success));
                    }
                    // Rejection and network failure are abstract injected Git
                    // outcomes, not claims about a real server's rejection cause.
                    actions.push(Action(index, Event::Rejected));
                    actions.push(Action(index, Event::NetworkFailure));
                }
                Stage::Rebase => {
                    actions.push(Action(index, Event::Rebased));
                    actions.push(Action(index, Event::Conflict));
                }
                _ => {}
            }
            if !state.restarted && client.stage != Stage::Done {
                actions.push(Action(index, Event::Restart));
            }
        }
    }

    fn next_state(&self, prior: &State, Action(index, event): Action) -> Option<State> {
        let mut next = *prior;
        let client = &mut next.clients[index];
        match event {
            Event::Apply => {
                // Apply re-reads the current tree, including existing local
                // operation commits. Replaying an existing effect is a no-op.
                if client.base_effects == 0 && !client.operation {
                    client.operation = true;
                    client.owned_by_run = true;
                    next.last = Outcome::Applied;
                } else {
                    next.last = Outcome::Empty;
                }
                client.stage = if client.operation || client.users != 0 {
                    Stage::Push
                } else {
                    Stage::Done
                };
            }
            Event::Success => {
                grant(client);
                next.remote_effects += u8::from(client.operation);
                next.remote_users |= client.users;
                next.remote_version += client.users.count_ones() as u8 + u8::from(client.operation);
                client.users = 0;
                client.operation = false;
                client.owned_by_run = false;
                client.stage = Stage::Done;
                next.last = Outcome::Success;
            }
            Event::Rejected => {
                grant(client);
                if decide_retry(retry_facts(client)) == RetryAction::Exhausted {
                    client.stage = Stage::Done;
                    next.last = Outcome::Exhausted;
                } else {
                    client.stage = Stage::Rebase;
                    next.last = Outcome::Rejected;
                }
            }
            Event::NetworkFailure => {
                grant(client);
                client.stage = Stage::Done;
                next.last = Outcome::NetworkFailure;
            }
            Event::Rebased => {
                client.base_version = next.remote_version;
                client.base_effects = next.remote_effects;
                if client.operation && next.remote_effects != 0 {
                    client.operation = false;
                    client.owned_by_run = false;
                    next.last = Outcome::Dropped;
                } else {
                    next.last = Outcome::Rebased;
                }
                client.stage = Stage::Apply;
            }
            Event::Conflict => {
                match decide_retry(retry_facts(client)) {
                    RetryAction::DiscardOwnedAndReplay => {
                        next.bad_discard |=
                            !client.operation || !client.owned_by_run || client.users != 0;
                        // A real reset discards all local commits. This exposes
                        // a weakened sole-commit guard as lost user commits.
                        client.users = 0;
                        client.operation = false;
                        client.owned_by_run = false;
                        client.base_version = next.remote_version;
                        client.base_effects = next.remote_effects;
                        client.stage = Stage::Apply;
                    }
                    RetryAction::PreserveAndFail | RetryAction::Exhausted => {
                        client.stage = Stage::Done;
                    }
                }
                next.last = Outcome::Conflict;
            }
            Event::Restart => {
                // A new run has a new nonce and cannot claim the prior run's
                // operation commit. Existing local content remains intact.
                client.owned_by_run = false;
                client.run_pushes = 0;
                client.stage = Stage::Rebase;
                next.restarted = true;
                next.last = Outcome::Restart;
            }
        }
        Some(next)
    }

    fn properties(&self) -> Vec<Property<Self>> {
        vec![
            Property::<Self>::always("user commits survive", |_, s| {
                s.remote_users | s.clients[0].users | s.clients[1].users == s.initial_users
            }),
            Property::<Self>::always("discard targets only this run's operation", |_, s| {
                !s.bad_discard
            }),
            Property::<Self>::always(
                "identical operations have at most one remote effect",
                |_, s| s.remote_effects <= 1,
            ),
            Property::<Self>::always("pushes stay within the declared bound", |_, s| {
                s.clients
                    .iter()
                    .all(|client| client.total_pushes <= 3 && client.run_pushes <= 3)
            }),
            Property::<Self>::sometimes("success is explored", |_, s| s.last == Outcome::Success),
            Property::<Self>::sometimes("rejection is explored", |_, s| {
                s.last == Outcome::Rejected
            }),
            Property::<Self>::sometimes("network failure is explored", |_, s| {
                s.last == Outcome::NetworkFailure
            }),
            Property::<Self>::sometimes("conflicting rebase is explored", |_, s| {
                s.last == Outcome::Conflict
            }),
            Property::<Self>::sometimes("dropped operation is explored", |_, s| {
                s.last == Outcome::Dropped
            }),
            Property::<Self>::sometimes("empty apply is explored", |_, s| s.last == Outcome::Empty),
            Property::<Self>::sometimes("exhaustion is explored", |_, s| {
                s.last == Outcome::Exhausted
            }),
            Property::<Self>::sometimes("interruption and restart are explored", |_, s| {
                s.last == Outcome::Restart
            }),
        ]
    }
}

#[test]
fn bounded_transactions_use_actual_kernel_decisions() {
    println!(
        "bound: two clients; three total pushes each (including after restart); \
        zero/one offline and zero/one hand-edit commit per clone; one restart per trace"
    );
    let checked = Transactions.checker().spawn_bfs().join();
    println!("states checked: {}", checked.unique_state_count());
    checked.assert_properties();
}
