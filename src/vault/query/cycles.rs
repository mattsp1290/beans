//! Iterative Tarjan traversal avoids depending on the native thread stack.
use super::*;
use std::collections::{BTreeMap, BTreeSet};
impl Index {
    pub fn cycles(&self) -> Vec<Vec<String>> {
        let mut adj = BTreeMap::<String, Vec<String>>::new();
        for note in self.project_issues(b"", true) {
            let m = &issue(note).unwrap().metadata;
            let links = m
                .blocked_by
                .iter()
                .filter_map(|b| {
                    self.lookup(b.target.as_bytes())
                        .and_then(issue)
                        .map(|d| d.metadata.id.clone())
                })
                .collect();
            adj.insert(m.id.clone(), links);
        }
        let mut index = 0;
        let mut indices = BTreeMap::new();
        let mut low = BTreeMap::new();
        let mut on_stack = BTreeSet::new();
        let mut stack = Vec::new();
        let mut groups = Vec::new();
        for seed in adj.keys() {
            if indices.contains_key(seed) {
                continue;
            }
            indices.insert(seed.clone(), index);
            low.insert(seed.clone(), index);
            index += 1;
            stack.push(seed.clone());
            on_stack.insert(seed.clone());
            let mut frames = vec![(seed.clone(), 0)];
            while let Some((v, next)) = frames.last().cloned() {
                if let Some(w) = adj.get(&v).and_then(|edges| edges.get(next)).cloned() {
                    frames.last_mut().unwrap().1 += 1;
                    if !indices.contains_key(&w) {
                        indices.insert(w.clone(), index);
                        low.insert(w.clone(), index);
                        index += 1;
                        stack.push(w.clone());
                        on_stack.insert(w.clone());
                        frames.push((w, 0));
                    } else if on_stack.contains(&w) {
                        let n = low[&v].min(indices[&w]);
                        low.insert(v, n);
                    }
                } else {
                    frames.pop();
                    if low[&v] == indices[&v] {
                        let mut group = Vec::new();
                        loop {
                            let w = stack.pop().unwrap();
                            on_stack.remove(&w);
                            let done = w == v;
                            group.push(w);
                            if done {
                                break;
                            }
                        }
                        if group.len() > 1 || adj.get(&v).is_some_and(|edges| edges.contains(&v)) {
                            group.sort();
                            groups.push(group);
                        }
                    }
                    if let Some((parent, _)) = frames.last() {
                        let n = low[parent].min(low[&v]);
                        low.insert(parent.clone(), n);
                    }
                }
            }
        }
        groups.sort_by_key(|g| g.join(","));
        groups
    }
}
