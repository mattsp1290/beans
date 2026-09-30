//! Decisions for the rejected-push/conflicting-rebase path in `gitops/hub.go`.
use vstd::prelude::*;

verus! {

/// Observations supplied by the Git adapter after aborting a conflicting rebase.
/// `owned_by_run` requires an operation SHA created in this run or re-established
/// from this run's nonce. A SHA or nonce supplied by a user is insufficient.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct RetryFacts {
    pub operation_present: bool,
    pub head_is_operation: bool,
    pub local_commits: usize,
    pub owned_by_run: bool,
    pub push_attempts: usize,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum RetryAction {
    Exhausted,
    DiscardOwnedAndReplay,
    PreserveAndFail,
}

/// Exhaustion is checked before the Git adapter attempts another fetch/rebase.
/// At a conflicting rebase, discard is permitted only for the sole local HEAD
/// commit whose ownership has been established for this run.
pub fn decide_retry(facts: RetryFacts) -> (action: RetryAction)
    ensures
        (action == RetryAction::Exhausted) == (facts.push_attempts >= 3),
        (action == RetryAction::DiscardOwnedAndReplay) == (
            facts.push_attempts < 3
            && facts.operation_present
            && facts.head_is_operation
            && facts.local_commits == 1
            && facts.owned_by_run
        ),
        !facts.owned_by_run ==> action != RetryAction::DiscardOwnedAndReplay,
        facts.local_commits != 1 ==> action != RetryAction::DiscardOwnedAndReplay,
        action == RetryAction::PreserveAndFail ==> facts.push_attempts < 3,
{
    if facts.push_attempts >= 3 {
        RetryAction::Exhausted
    } else if facts.operation_present
        && facts.head_is_operation
        && facts.local_commits == 1
        && facts.owned_by_run
    {
        RetryAction::DiscardOwnedAndReplay
    } else {
        RetryAction::PreserveAndFail
    }
}

/// One mutation invocation owns one budget. Private state and absence of Clone
/// prevent callers from replenishing or duplicating a live budget. WP4 must
/// create it once and obtain a grant immediately before every real push.
pub struct AttemptBudget {
    remaining: u8,
}

impl AttemptBudget {
    pub closed spec fn available(&self) -> nat {
        self.remaining as nat
    }

    pub fn for_mutation() -> (budget: Self)
        ensures budget.available() == 3,
    {
        Self { remaining: 3 }
    }

    pub fn remaining(&self) -> (remaining: u8)
        ensures remaining == self.available(),
    {
        self.remaining
    }
}

/// A granted attempt consumes exactly one slot, including an attempt that
/// fails without reaching the remote. Once empty, repeated requests stay empty.
pub fn take_push_attempt(budget: &mut AttemptBudget) -> (granted: bool)
    ensures
        granted == (old(budget).available() > 0),
        granted ==> final(budget).available() + 1 == old(budget).available(),
        !granted ==> final(budget).available() == old(budget).available(),
        final(budget).available() <= old(budget).available(),
        old(budget).available() <= 3 ==> final(budget).available() <= 3,
{
    if budget.remaining == 0 {
        false
    } else {
        budget.remaining -= 1;
        true
    }
}
}

// The constructor deliberately has no Default/Clone implementation: a mutation
// must initialize its own budget explicitly at the pipeline boundary.

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn every_discard_guard_and_exhaustion_boundary_is_observable() {
        for operation_present in [false, true] {
            for head_is_operation in [false, true] {
                for owned_by_run in [false, true] {
                    for local_commits in [0, 1, 2, usize::MAX] {
                        for push_attempts in [0, 1, 2, 3, 4, usize::MAX] {
                            let facts = RetryFacts {
                                operation_present,
                                head_is_operation,
                                local_commits,
                                owned_by_run,
                                push_attempts,
                            };
                            let action = decide_retry(facts);
                            if push_attempts >= 3 {
                                assert_eq!(action, RetryAction::Exhausted);
                            } else if operation_present
                                && head_is_operation
                                && owned_by_run
                                && local_commits == 1
                            {
                                assert_eq!(action, RetryAction::DiscardOwnedAndReplay);
                            } else {
                                assert_eq!(action, RetryAction::PreserveAndFail);
                            }
                        }
                    }
                }
            }
        }
    }

    #[test]
    fn budget_grants_exactly_three_attempts_then_stays_exhausted() {
        let mut budget = AttemptBudget::for_mutation();
        for expected in [3, 2, 1] {
            assert_eq!(budget.remaining(), expected);
            assert!(take_push_attempt(&mut budget));
        }
        for _ in 0..10 {
            assert!(!take_push_attempt(&mut budget));
            assert_eq!(budget.remaining(), 0);
        }
    }
}
