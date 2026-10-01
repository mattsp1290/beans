use super::*;
impl Index {
    pub fn reload_all(&mut self) -> Result<(), Error> {
        let fresh = Self::load_with_options(
            &self.hub_dir,
            LoadOptions {
                explicit_workflow: self.explicit_workflow.clone(),
            },
        )?;
        *self = fresh;
        Ok(())
    }
    pub fn reload(&mut self, paths: &[PathBuf]) -> Result<(), Error> {
        let mut rels = Vec::new();
        for p in paths {
            let rel = self.relative(p)?;
            if rel.rsplit(|&b| b == b'/').next() == Some(b"beans.toml") {
                return self.reload_all();
            }
            rels.push(rel);
        }
        for rel in rels {
            self.reload_one(&rel);
        }
        self.rebuild();
        Ok(())
    }
    fn relative(&self, p: &Path) -> Result<Vec<u8>, Error> {
        let raw = p.as_os_str().as_bytes();
        let input = clean(raw);
        let rel = if input.starts_with(b"/") {
            let root = self.hub_dir.as_os_str().as_bytes();
            if input == root {
                b".".to_vec()
            } else if root == b"/" {
                input[1..].into()
            } else if let Some(rest) = input.strip_prefix(root).and_then(|s| s.strip_prefix(b"/")) {
                rest.into()
            } else {
                return Err(Error::from_bytes(
                    [
                        b"vault: ".as_slice(),
                        p.as_os_str().as_bytes(),
                        b" is not under hub ",
                        self.hub_dir.as_os_str().as_bytes(),
                    ]
                    .concat(),
                ));
            }
        } else {
            input
        };
        if rel == b".." || rel.starts_with(b"../") {
            return Err(Error::from_bytes(
                [
                    b"vault: ".as_slice(),
                    p.as_os_str().as_bytes(),
                    b" is not under hub ",
                    self.hub_dir.as_os_str().as_bytes(),
                ]
                .concat(),
            ));
        }
        Ok(rel)
    }
    fn reload_one(&mut self, rel: &[u8]) {
        let parts: Vec<_> = rel.split(|&b| b == b'/').collect();
        if parts.len() >= 5 && parts[0] == b"projects" && parts[2] == b"plans" {
            self.reload_plan(parts[1], &parts[..4].join(&b'/'));
            return;
        }
        self.remove(rel);
        self.graph.clear_parse_warning(rel);
        self.assets.remove(rel);
        let full = self.hub_dir.join(path(rel));
        if !fs::metadata(&full).is_ok_and(|m| !m.is_dir()) {
            return;
        }
        self.load_path(rel);
    }
    fn reload_plan(&mut self, project: &[u8], root: &[u8]) {
        let manifest = [root, b"/plan.md"].concat();
        let full = self.hub_dir.join(path(root));
        if let Err(e) = crate::gitops::recover_tree(&full) {
            self.warning(&manifest, format!("recover plan tree: {e}"));
            return;
        }
        if fs::metadata(&full).is_err_and(|e| e.kind() == std::io::ErrorKind::NotFound) {
            self.remove(&manifest);
            self.graph.clear_parse_warning(&manifest);
            return;
        }
        if let Err(e) = crate::domain::plan::load_path(&full) {
            self.warning(&manifest, e);
            return;
        }
        self.remove(&manifest);
        self.graph.clear_parse_warning(&manifest);
        self.load_plan(project, root);
    }
}
