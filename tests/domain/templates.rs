use beans::domain::template::{
    default_request_template, default_template, load_request_template, load_template,
};
use serde::Deserialize;
use std::path::{Path, PathBuf};

struct Scratch(PathBuf);
impl Scratch {
    fn new() -> Self {
        let mut nonce = [0; 16];
        getrandom::fill(&mut nonce).unwrap();
        let path =
            std::env::temp_dir().join(format!("beans-templates-{:x}", u128::from_ne_bytes(nonce)));
        std::fs::create_dir(&path).unwrap();
        Self(path)
    }
    fn write(&self, name: &str, data: &[u8]) {
        let path = self.0.join(name);
        std::fs::create_dir_all(path.parent().unwrap()).unwrap();
        std::fs::write(path, data).unwrap();
    }
}
impl Drop for Scratch {
    fn drop(&mut self) {
        std::fs::remove_dir_all(&self.0).unwrap();
    }
}

// Independent ports of issue/template_test.go. Keep the original assertions,
// rather than treating a generated differential corpus as their replacement.
#[test]
fn default_template_builtins() {
    for (kind, expected) in [
        ("task", "## Acceptance\n- [ ] \n"),
        (
            "bug",
            "## Steps\n1. \n\n## Expected\n\n## Actual\n\n## Acceptance\n- [ ] \n",
        ),
        ("feature", "## Motivation\n\n## Acceptance\n- [ ] \n"),
        ("epic", "## Goal\n\n## Scope\n\n"),
        ("chore", ""),
    ] {
        assert_eq!(default_template(kind), expected.as_bytes(), "{kind}");
    }
}

#[test]
fn default_template_unknown_type_falls_back_to_task() {
    assert_eq!(default_template("no-such-type"), default_template("task"));
}

#[test]
fn load_template_prefers_project_then_hub_then_builtin() {
    let scratch = Scratch::new();
    scratch.write("project/templates/task.md", b"## Project Template\n");
    scratch.write("hub/templates/task.md", b"## Hub Template\n");
    scratch.write("hub/templates/bug.md", b"## Hub Template\n");
    let project = scratch.0.join("project");
    let hub = scratch.0.join("hub");
    let skip = Path::new("");
    assert_eq!(
        load_template("task", &project, &hub),
        b"## Project Template\n"
    );
    assert_eq!(load_template("task", skip, &hub), b"## Hub Template\n");
    assert_eq!(load_template("bug", &project, &hub), b"## Hub Template\n");
    assert_eq!(load_template("task", skip, skip), default_template("task"));
    assert_eq!(
        load_template("epic", &project, &hub),
        default_template("epic")
    );
}

#[derive(Deserialize)]
struct Entry {
    path: String,
    kind: String,
    data: Option<Vec<u8>>,
    target: String,
}
#[derive(Deserialize)]
struct Case {
    name: String,
    #[serde(rename = "type")]
    kind: String,
    default: bool,
    request: bool,
    project: String,
    hub: String,
    entries: Option<Vec<Entry>>,
    expected: Vec<u8>,
}
#[derive(Deserialize)]
struct Corpus {
    cases: Vec<Case>,
}

#[test]
fn template_bytes_and_precedence_match_committed_contract() {
    let corpus: Corpus =
        serde_json::from_str(include_str!("../fixtures/expected/templates.json")).unwrap();
    assert_eq!(corpus.cases.len(), 42);
    for case in corpus.cases {
        let scratch = Scratch::new();
        for entry in case.entries.unwrap_or_default() {
            let path = scratch.0.join(&entry.path);
            std::fs::create_dir_all(path.parent().unwrap()).unwrap();
            match entry.kind.as_str() {
                "file" => std::fs::write(path, entry.data.unwrap()).unwrap(),
                "directory" => std::fs::create_dir(path).unwrap(),
                "symlink" => std::os::unix::fs::symlink(entry.target, path).unwrap(),
                other => panic!("unexpected fixture kind {other}"),
            }
        }
        let resolve = |directory: &str| {
            if directory.is_empty() {
                PathBuf::new()
            } else {
                // Preserve the missing intermediate component to qualify the
                // loader's clean-before-open behavior against filepath.Join.
                scratch.0.join(directory)
            }
        };
        let actual = if case.default {
            if case.request {
                default_request_template().to_vec()
            } else {
                default_template(&case.kind).to_vec()
            }
        } else if case.request {
            load_request_template(&resolve(&case.project), &resolve(&case.hub))
        } else {
            load_template(&case.kind, &resolve(&case.project), &resolve(&case.hub))
        };
        assert_eq!(actual, case.expected, "{}", case.name);
    }
}
