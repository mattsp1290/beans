//! Only requested-ref porcelain records can authorize contention replay.
use super::GitOutput;
use crate::domain::frontmatter::Error;

#[derive(Debug)]
pub(super) enum PushFailure {
    Contention(Error),
    Failed(Error),
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum PushOutcome {
    Published,
    Contention,
    Failed,
}
pub fn classify_push(output: &GitOutput, branch: &str) -> PushOutcome {
    if output.status.success() {
        return PushOutcome::Published;
    }
    if output.status.code().is_none() {
        return PushOutcome::Failed;
    }
    let destination = format!("HEAD:refs/heads/{branch}");
    let mut records = output.stdout.split(|b| *b == b'\n').filter_map(|line| {
        let fields: Vec<_> = line.split(|b| *b == b'\t').collect();
        (fields.len() == 3 && fields[1] == destination.as_bytes()).then_some(fields)
    });
    let Some(record) = records.next() else {
        return PushOutcome::Failed;
    };
    if records.next().is_some() || record[0] != b"!" {
        return PushOutcome::Failed;
    }
    if matches!(
        record[2],
        b"[rejected] (non-fast-forward)" | b"[rejected] (fetch first)"
    ) {
        return PushOutcome::Contention;
    }
    if matches!(
        record[2],
        b"[remote rejected] (failed to update ref)"
            | b"[remote rejected] (incorrect old value provided)"
    ) && receiver_divergence(&output.stderr, branch)
    {
        return PushOutcome::Contention;
    }
    PushOutcome::Failed
}
fn receiver_divergence(stderr: &[u8], branch: &str) -> bool {
    let prefix = format!("remote: error: cannot lock ref 'refs/heads/{branch}': is at ");
    stderr.split(|b| *b == b'\n').any(|line| {
        let line = line.trim_ascii_end();
        let Some(rest) = line.strip_prefix(prefix.as_bytes()) else {
            return false;
        };
        let Some(split) = rest
            .windows(b" but expected ".len())
            .position(|w| w == b" but expected ")
        else {
            return false;
        };
        let actual = &rest[..split];
        let expected = rest[split + b" but expected ".len()..]
            .strip_suffix(b"\r")
            .unwrap_or(&rest[split + b" but expected ".len()..]);
        let oid =
            |s: &[u8]| (s.len() == 40 || s.len() == 64) && s.iter().all(u8::is_ascii_hexdigit);
        oid(actual) && oid(expected) && actual.len() == expected.len() && actual != expected
    })
}
#[cfg(test)]
mod tests {
    use super::*;
    use std::os::unix::process::ExitStatusExt;
    fn output(record: &str, err: &str) -> GitOutput {
        GitOutput {
            stdout: record.as_bytes().to_vec(),
            stderr: err.as_bytes().to_vec(),
            status: std::process::ExitStatus::from_raw(256),
        }
    }
    #[test]
    fn requested_ref_and_receiver_evidence_are_required() {
        let evidence = format!(
            "remote: error: cannot lock ref 'refs/heads/main': is at {} but expected {}\n",
            "a".repeat(40),
            "b".repeat(40)
        );
        for cause in ["failed to update ref", "incorrect old value provided"] {
            let record = format!("!\tHEAD:refs/heads/main\t[remote rejected] ({cause})\n");
            assert_eq!(
                classify_push(&output(&record, &evidence), "main"),
                PushOutcome::Contention
            );
            for bad in [
                "",
                "remote: hook says incorrect old value provided",
                "remote: error: cannot lock ref 'refs/heads/main': Permission denied",
            ] {
                assert_eq!(
                    classify_push(&output(&record, bad), "main"),
                    PushOutcome::Failed
                );
            }
        }
        for cause in ["fetch first", "non-fast-forward"] {
            let record = format!("!\tHEAD:refs/heads/main\t[rejected] ({cause})\n");
            assert_eq!(
                classify_push(&output(&record, ""), "main"),
                PushOutcome::Contention
            );
            assert_eq!(
                classify_push(&output(&record.replace("main", "other"), &evidence), "main"),
                PushOutcome::Failed
            );
        }
        for record in [
            "remote: !\tHEAD:refs/heads/main\t[rejected] (fetch first)",
            "! [rejected] HEAD -> main (fetch first)",
            "!\tHEAD:refs/heads/main\t[remote rejected] (pre-receive hook declined)",
            "!\tHEAD:refs/heads/main\t[rejected] (disk full)",
        ] {
            assert_eq!(
                classify_push(&output(record, &evidence), "main"),
                PushOutcome::Failed
            );
        }
    }
}
