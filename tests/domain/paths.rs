use beans::{
    domain::frontmatter::Error,
    vault::{Paths, check_hub, default_paths_with},
};
use std::{
    ffi::{OsStr, OsString},
    os::unix::ffi::{OsStrExt, OsStringExt},
    path::PathBuf,
};

fn resolve(home: Vec<u8>, hub: Vec<u8>, flag: Vec<u8>, user_home: Vec<u8>) -> Result<Paths, Error> {
    default_paths_with(
        OsStr::from_bytes(&flag),
        |key| {
            OsString::from_vec(match key {
                "BEANS_HOME" => home.clone(),
                "BEANS_HUB" => hub.clone(),
                "HOME" => user_home.clone(),
                _ => unreachable!(),
            })
        },
        || Ok(PathBuf::from("/oracle/cwd")),
    )
}

#[test]
fn default_paths_match_committed_contract_environment_and_flag_precedence() {
    let fixture: serde_json::Value =
        serde_json::from_str(include_str!("../contract/paths.json")).unwrap();
    for case in fixture["cases"].as_array().unwrap() {
        let input = &case["Input"];
        let bytes = |key| input[key].as_str().unwrap().as_bytes().to_vec();
        let actual = resolve(
            bytes("Home"),
            bytes("Hub"),
            bytes("Flag"),
            bytes("UserHome"),
        );
        let error = case["Error"].as_str().unwrap();
        if !error.is_empty() {
            assert_eq!(actual.unwrap_err().to_string(), error);
            continue;
        }
        let p = actual.unwrap();
        for (key, value) in [
            ("Home", p.home),
            ("Hub", p.hub),
            ("Cache", p.cache),
            ("Config", p.config),
        ] {
            assert_eq!(
                value.as_os_str().as_bytes(),
                case["Paths"][key].as_str().unwrap().as_bytes(),
                "{case}"
            );
        }
    }
    for case in fixture["raw"].as_array().unwrap() {
        let bytes = |key| serde_json::from_value::<Vec<u8>>(case[key].clone()).unwrap();
        let p = resolve(
            bytes("Home"),
            bytes("Hub"),
            bytes("Flag"),
            bytes("UserHome"),
        )
        .unwrap();
        let expected: Vec<Vec<u8>> = serde_json::from_value(case["Paths"].clone()).unwrap();
        for (path, bytes) in [p.home, p.hub, p.cache, p.config].iter().zip(expected) {
            assert_eq!(path.as_os_str().as_bytes(), bytes);
        }
    }
}

#[test]
fn absolute_hub_does_not_require_a_working_directory() {
    let p = default_paths_with(
        OsStr::new("/absolute/../raw/"),
        |_| OsString::from("/home"),
        || panic!("unexpected cwd lookup"),
    )
    .unwrap();
    assert_eq!(p.hub, PathBuf::from("/absolute/../raw/"));
    let error = default_paths_with(
        OsStr::new("relative"),
        |_| OsString::from("/home"),
        || Err(Error::new("getwd failed".into())),
    )
    .unwrap_err();
    assert_eq!(error.to_string(), "getwd failed");
}

#[test]
fn hub_check_follows_directory_symlinks_and_rejects_worktree_files() {
    let dir = std::env::temp_dir().join(format!("beans-paths-{}", std::process::id()));
    std::fs::create_dir(&dir).unwrap();
    struct Cleanup(PathBuf);
    impl Drop for Cleanup {
        fn drop(&mut self) {
            let _ = std::fs::remove_dir_all(&self.0);
        }
    }
    let _cleanup = Cleanup(dir.clone());
    let p = Paths {
        hub: dir.clone(),
        ..Paths::default()
    };
    let expected = format!(
        "no hub found; run bn init <remote> (expected a clone at {})",
        dir.display()
    );
    assert_eq!(check_hub(&p).unwrap_err().to_string(), expected);
    std::fs::write(dir.join(".git"), b"gitdir: elsewhere").unwrap();
    assert_eq!(check_hub(&p).unwrap_err().to_string(), expected);
    std::fs::remove_file(dir.join(".git")).unwrap();
    std::fs::create_dir(dir.join("actual-git")).unwrap();
    std::os::unix::fs::symlink("actual-git", dir.join(".git")).unwrap();
    check_hub(&p).unwrap();
    std::fs::remove_file(dir.join(".git")).unwrap();
    std::fs::create_dir(dir.join(".git")).unwrap();
    check_hub(&p).unwrap();
}

#[test]
fn project_name_matches_committed_contract_without_collapsing_hyphens() {
    let fixture: serde_json::Value =
        serde_json::from_str(include_str!("../contract/paths.json")).unwrap();
    for case in fixture["names"].as_array().unwrap() {
        let input: Vec<u8> = serde_json::from_value(case["Input"].clone()).unwrap();
        assert_eq!(
            beans::vault::project_name(&input),
            case["Name"].as_str().unwrap()
        );
        assert_eq!(
            beans::vault::valid_project_name(&input),
            case["Valid"].as_bool().unwrap()
        );
    }
}

#[test]
fn default_paths_original_regression() {
    let home = b"/tmp/bh".to_vec();
    let p = resolve(home.clone(), vec![], vec![], vec![]).unwrap();
    assert_eq!(p.hub, PathBuf::from("/tmp/bh/hub"));
    assert_eq!(p.cache, PathBuf::from("/tmp/bh/cache"));
    assert_eq!(p.config, PathBuf::from("/tmp/bh/config.toml"));
    let p = resolve(home.clone(), b"/elsewhere/hub".to_vec(), vec![], vec![]).unwrap();
    assert_eq!(p.hub, PathBuf::from("/elsewhere/hub"));
    let p = resolve(
        home,
        b"/elsewhere/hub".to_vec(),
        b"/flag/hub".to_vec(),
        vec![],
    )
    .unwrap();
    assert_eq!(p.hub, PathBuf::from("/flag/hub"));
    let missing = std::env::temp_dir().join(format!("beans-no-hub-{}", std::process::id()));
    assert!(!missing.exists());
    let p = Paths {
        hub: missing.clone(),
        ..Paths::default()
    };
    assert_eq!(
        check_hub(&p).unwrap_err().to_string(),
        format!(
            "no hub found; run bn init <remote> (expected a clone at {})",
            missing.display()
        )
    );
}
