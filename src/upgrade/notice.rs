use super::{fetch, manifest, version};
use crate::{domain::frontmatter::Error, vault::paths};
use serde::{Deserialize, Serialize};
use std::{
    fs::{File, OpenOptions},
    io::{Read, Seek, Write},
    os::{fd::AsRawFd, unix::fs::OpenOptionsExt},
    path::Path,
    time::{SystemTime, UNIX_EPOCH},
};

const TTL: u64 = 60 * 60;
const CACHE_LIMIT: u64 = 64 * 1024;

#[derive(Default, Deserialize, Serialize)]
struct Cache {
    base: String,
    attempted_at: u64,
    latest: Option<String>,
}

struct LockedCache(File);
impl Drop for LockedCache {
    fn drop(&mut self) {
        unsafe { libc::flock(self.0.as_raw_fd(), libc::LOCK_UN) };
    }
}
impl LockedCache {
    fn open(directory: &Path) -> Option<Self> {
        std::fs::create_dir_all(directory).ok()?;
        let directory = OpenOptions::new()
            .read(true)
            .custom_flags(libc::O_DIRECTORY | libc::O_NOFOLLOW | libc::O_CLOEXEC)
            .open(directory)
            .ok()?;
        let fd = unsafe {
            libc::openat(
                directory.as_raw_fd(),
                c"update-check.json".as_ptr(),
                libc::O_RDWR
                    | libc::O_CREAT
                    | libc::O_NOFOLLOW
                    | libc::O_NONBLOCK
                    | libc::O_CLOEXEC,
                0o600,
            )
        };
        if fd < 0 {
            return None;
        }
        use std::os::fd::FromRawFd;
        let file = unsafe { File::from_raw_fd(fd) };
        if !file.metadata().ok()?.is_file()
            || unsafe { libc::flock(fd, libc::LOCK_EX | libc::LOCK_NB) } != 0
        {
            return None;
        }
        Some(Self(file))
    }
    fn read(&mut self) -> Cache {
        let mut raw = Vec::new();
        if Read::by_ref(&mut self.0)
            .take(CACHE_LIMIT + 1)
            .read_to_end(&mut raw)
            .is_err()
            || raw.len() as u64 > CACHE_LIMIT
        {
            return Cache::default();
        }
        serde_json::from_slice(&raw).unwrap_or_default()
    }
    fn write(&mut self, cache: &Cache) -> Result<(), Error> {
        let bytes = serde_json::to_vec(cache).map_err(|e| Error::new(e.to_string()))?;
        self.0
            .rewind()
            .and_then(|_| self.0.set_len(0))
            .and_then(|_| self.0.write_all(&bytes))
            .map_err(|e| Error::new(e.to_string()))
    }
}

fn check(
    directory: &Path,
    base: &str,
    current: &str,
    now: u64,
    latest: impl FnOnce() -> Option<String>,
) -> Option<String> {
    let mut file = LockedCache::open(directory)?;
    let mut cache = file.read();
    if cache.base != base {
        cache = Cache {
            base: base.into(),
            ..Default::default()
        };
    }
    if cache.attempted_at == 0 || now < cache.attempted_at || now - cache.attempted_at >= TTL {
        cache.attempted_at = now;
        file.write(&cache).ok()?;
        if let Some(tag) = latest().filter(|tag| version::parse_tag(tag).is_some()) {
            cache.latest = Some(tag);
            file.write(&cache).ok()?;
        }
    }
    let tag = cache.latest?;
    let published = version::parse_tag(&tag)?;
    (version::decide(current, published) == version::Decision::Upgrade).then(|| {
        format!("bn: {tag} is available; current CLI is {current}. Run `bn upgrade` to receive new coordination features. For a Cargo or source installation, rebuild with `make install`.\n")
    })
}

pub(crate) fn startup() {
    if std::env::var_os("BN_NO_UPDATE_CHECK").is_some_and(|v| v == "1") {
        return;
    }
    let Ok(paths) = paths::default_paths(std::ffi::OsStr::new("")) else {
        return;
    };
    let base = std::env::var(super::BASE_URL_ENV)
        .ok()
        .filter(|s| !s.is_empty())
        .unwrap_or_else(|| super::DEFAULT_BASE_URL.into());
    let Ok(base) = manifest::release_base(&base) else {
        return;
    };
    let Ok(now) = SystemTime::now().duration_since(UNIX_EPOCH) else {
        return;
    };
    if let Some(message) = check(
        &paths.cache,
        base,
        env!("BN_VERSION"),
        now.as_secs(),
        || {
            let target = manifest::target().ok()?;
            let url = format!("{base}/latest/download/bn-manifest.json");
            let raw = fetch::fetch_notice_manifest(base, &url, &paths.cache).ok()?;
            manifest::validate(&raw, base, &target, None)
                .ok()
                .map(|release| release.tag)
        },
    ) {
        crate::gitops::diagnostics::message(message.into_bytes());
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::{cell::Cell, os::unix::fs::symlink};
    struct Temp(std::path::PathBuf);
    impl Temp {
        fn path(&self) -> &Path {
            &self.0
        }
    }
    impl Drop for Temp {
        fn drop(&mut self) {
            let _ = std::fs::remove_dir_all(&self.0);
        }
    }
    fn dir() -> Temp {
        let mut random = [0; 8];
        getrandom::fill(&mut random).unwrap();
        let path = std::env::temp_dir().join(format!(
            "bn-notice-{}-{}",
            std::process::id(),
            u64::from_ne_bytes(random)
        ));
        std::fs::create_dir(&path).unwrap();
        Temp(path)
    }
    #[test]
    fn expired_ttl_refreshes_and_cached_newer_release_keeps_notice() {
        let dir = dir();
        let calls = Cell::new(0);
        let fetch = || {
            calls.set(calls.get() + 1);
            Some("v9.0.0".into())
        };
        assert!(
            check(dir.path(), "https://release", "v1.0.0", 100, fetch)
                .unwrap()
                .contains("bn upgrade")
        );
        assert!(check(dir.path(), "https://release", "v1.0.0", 3699, fetch).is_some());
        assert_eq!(calls.get(), 1);
        assert!(check(dir.path(), "https://release", "v1.0.0", 3700, fetch).is_some());
        assert_eq!(calls.get(), 2);
        assert!(check(dir.path(), "https://release", "v9.0.0", 3701, fetch).is_none());
        assert!(check(dir.path(), "https://release", "dev", 3702, fetch).is_none());
        assert!(check(dir.path(), "https://release", "v10.0.0-3-gabc", 3703, fetch).is_none());
    }
    #[test]
    fn failed_check_is_throttled_and_previous_success_survives() {
        let dir = dir();
        assert!(check(dir.path(), "base", "v1.0.0", 100, || None).is_none());
        assert!(
            check(dir.path(), "base", "v1.0.0", 101, || panic!(
                "failure TTL must apply"
            ))
            .is_none()
        );
        assert!(check(dir.path(), "base", "v1.0.0", 3700, || Some("v2.0.0".into())).is_some());
        assert!(check(dir.path(), "base", "v1.0.0", 7300, || None).is_some());
    }
    #[test]
    fn cache_corruption_clock_rollback_and_source_change_trigger_check() {
        let dir = dir();
        std::fs::write(dir.path().join("update-check.json"), b"broken").unwrap();
        assert!(check(dir.path(), "one", "v1.0.0", 100, || Some("v2.0.0".into())).is_some());
        let path = dir.path().join("update-check.json");
        let mut newer: serde_json::Value =
            serde_json::from_slice(&std::fs::read(&path).unwrap()).unwrap();
        newer["future_metadata"] = serde_json::json!({"ignored": true});
        std::fs::write(path, newer.to_string()).unwrap();
        assert!(
            check(dir.path(), "one", "v1.0.0", 101, || panic!(
                "unknown fields must not expire a cached check"
            ))
            .is_some()
        );
        assert!(
            check(dir.path(), "one", "v1.0.0", 99, || Some("v3.0.0".into()))
                .unwrap()
                .contains("v3.0.0")
        );
        assert!(check(dir.path(), "two", "v1.0.0", 100, || None).is_none());
    }
    #[test]
    fn symlink_and_busy_cache_are_skipped_without_writes() {
        let dir = dir();
        let victim = dir.path().join("victim");
        std::fs::write(&victim, "preserve").unwrap();
        symlink(&victim, dir.path().join("update-check.json")).unwrap();
        assert!(check(dir.path(), "base", "v1.0.0", 100, || panic!("unsafe cache")).is_none());
        assert_eq!(std::fs::read_to_string(&victim).unwrap(), "preserve");
        let locked = super::tests::dir();
        let _lock = LockedCache::open(locked.path()).unwrap();
        assert!(
            check(locked.path(), "base", "v1.0.0", 100, || panic!(
                "busy cache"
            ))
            .is_none()
        );
        let link = dir.path().join("cache-link");
        symlink(locked.path(), &link).unwrap();
        assert!(
            check(&link, "base", "v1.0.0", 100, || panic!(
                "symlink cache root"
            ))
            .is_none()
        );
    }
}
