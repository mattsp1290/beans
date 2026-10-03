use crate::domain::yaml_string::YamlString;
fn trim(bytes: Vec<u8>) -> Vec<u8> {
    YamlString::from_bytes(bytes).trimmed().as_bytes().into()
}
/// Actor cache retains raw flag/fallback bytes; it is independent of log-token
/// normalization and Git author sanitization.
#[derive(Default)]
pub struct Actor {
    cached: Vec<u8>,
}
impl Actor {
    pub fn new(flag: &[u8]) -> Self {
        Self {
            cached: flag.into(),
        }
    }
    pub fn resolve(&mut self, user_actor: &[u8]) -> &[u8] {
        self.resolve_policy(user_actor, &crate::gitops::GitExecutor::default())
    }
    pub fn resolve_policy(
        &mut self,
        user_actor: &[u8],
        executor: &crate::gitops::GitExecutor,
    ) -> &[u8] {
        self.resolve_with(
            user_actor,
            |key| {
                use std::os::unix::ffi::OsStrExt;
                std::env::var_os(key).unwrap_or_default().as_bytes().into()
            },
            || {
                executor
                    .run(None, ["config", "user.name"], "config")
                    .ok()
                    .filter(|o| o.status.success())
                    .map(|o| o.stdout)
            },
        )
    }
    pub fn resolve_with(
        &mut self,
        user_actor: &[u8],
        env: impl Fn(&str) -> Vec<u8>,
        git_name: impl FnOnce() -> Option<Vec<u8>>,
    ) -> &[u8] {
        if !self.cached.is_empty() {
            return &self.cached;
        }
        let actor = trim(env("BN_ACTOR"));
        if !actor.is_empty() {
            self.cached = actor;
            return &self.cached;
        }
        let actor = trim(user_actor.into());
        if !actor.is_empty() {
            self.cached = actor;
            return &self.cached;
        }
        if let Some(name) = git_name() {
            let name = trim(name);
            if !name.is_empty() {
                self.cached = name;
                return &self.cached;
            }
        }
        self.cached = env("USER");
        &self.cached
    }
}
