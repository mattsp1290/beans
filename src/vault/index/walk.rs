use super::*;
use crate::vault::{is_plans_directory, plan_bundle_project, plan_manifest_project, skip_dir_name};
impl Index {
    pub(super) fn walk(&mut self) -> Result<(), Error> {
        let root = self.hub_dir.clone();
        let info = fs::symlink_metadata(&root).map_err(|e| path_error("lstat", &root, e))?;
        if info.is_dir() {
            if !self.recover {
                self.snapshot_reader = Some(crate::vault::public_read::SnapshotReader::new(&root)?);
            }
            let result = self.walk_directory(b"");
            self.snapshot_reader = None;
            result?;
        }
        Ok(())
    }
    fn walk_directory(&mut self, rel: &[u8]) -> Result<(), Error> {
        let full = self.hub_dir.join(path(rel));
        if !rel.is_empty() {
            if self.recover && is_plans_directory(rel) {
                crate::gitops::recover_trees(&full).map_err(|e| {
                    e.context(&[b"vault: recover plan trees in ".as_slice(), rel].concat())
                })?;
            }
            if skip_dir_name(rel.rsplit(|&b| b == b'/').next().unwrap()) {
                return Ok(());
            }
            if let Some(project) = plan_bundle_project(rel) {
                self.load_plan(&project, rel);
                return Ok(());
            }
        }
        let mut entries = fs::read_dir(&full)
            .map_err(|e| path_error("open", &full, e))?
            .collect::<Result<Vec<_>, _>>()
            .map_err(|e| path_error("readdirent", &full, e))?;
        entries.sort_by_key(|e| e.file_name().as_bytes().to_vec());
        for entry in entries {
            let name = entry.file_name();
            let name = name.as_bytes();
            let child = if rel.is_empty() {
                name.to_vec()
            } else {
                [rel, b"/", name].concat()
            };
            let kind = entry
                .file_type()
                .map_err(|e| path_error("lstat", &entry.path(), e))?;
            if kind.is_dir() {
                self.walk_directory(&child)?;
            } else if !name.starts_with(b".") {
                if plan_bundle_project(&child).is_some() {
                    self.warning(&child, "plan root must be a directory");
                } else if let Some(project) = plan_manifest_project(&child) {
                    let root = child.rsplitn(2, |&b| b == b'/').nth(1).unwrap();
                    self.load_plan(&project, root);
                } else {
                    self.load_path(&child);
                }
            }
        }
        Ok(())
    }
}
