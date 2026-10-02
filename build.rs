use std::{env, fs, path::Path};
fn walk(root: &Path, dir: &Path, out: &mut String) {
    let mut entries: Vec<_> = fs::read_dir(dir)
        .expect("read UI directory")
        .map(|e| e.unwrap().path())
        .collect();
    entries.sort();
    for p in entries {
        if p.is_dir() {
            walk(root, &p, out);
        } else if p.is_file() {
            println!("cargo:rerun-if-changed={}", p.display());
            let name = p.strip_prefix(root).unwrap().to_str().unwrap();
            out.push_str(&format!(
                "({name:?}, include_bytes!({:?})),\n",
                fs::canonicalize(&p).unwrap()
            ));
        }
    }
}
fn main() {
    println!("cargo:rerun-if-changed=ui/dist");
    println!("cargo:rerun-if-changed=src");
    // Worktrees have their own HEAD/index and share branch/tag references.
    let mut paths = vec![
        "HEAD".to_owned(),
        "index".into(),
        "refs/tags".into(),
        "packed-refs".into(),
    ];
    if let Some(branch) = git_output(&["symbolic-ref", "-q", "HEAD"]) {
        paths.push(branch);
    }
    for path in paths {
        if let Some(path) = git_output(&["rev-parse", "--git-path", &path]) {
            println!("cargo:rerun-if-changed={path}");
        }
    }
    // Dirty version metadata changes when any tracked file changes, including
    // docs and build policy outside src. Cargo otherwise caches build scripts.
    if let Some(files) = git_output(&["ls-files", "-z"]) {
        for path in files.split('\0').filter(|path| !path.is_empty()) {
            println!("cargo:rerun-if-changed={path}");
        }
    }
    println!("cargo:rerun-if-env-changed=BN_VERSION");
    let mut out = String::from("pub static ASSETS: &[(&str, &[u8])] = &[\n");
    walk(Path::new("ui/dist"), Path::new("ui/dist"), &mut out);
    out.push_str("];\n");
    fs::write(
        Path::new(&env::var("OUT_DIR").unwrap()).join("assets.rs"),
        out,
    )
    .unwrap();
    let version = env::var("BN_VERSION")
        .ok()
        .filter(|s| !s.trim().is_empty())
        .unwrap_or_else(|| {
            git_output(&["describe", "--tags", "--match", "v*", "--always", "--dirty"])
                .unwrap_or_else(|| "dev".into())
        });
    println!("cargo:rustc-env=BN_VERSION={version}");
}

fn git_output(args: &[&str]) -> Option<String> {
    std::process::Command::new("git")
        .args(args)
        .output()
        .ok()
        .filter(|o| o.status.success())
        .map(|o| String::from_utf8_lossy(&o.stdout).trim().to_owned())
        .filter(|s| !s.is_empty())
}
