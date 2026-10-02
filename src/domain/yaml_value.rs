//! yaml.v3's generic values, separate from owned-field string coercion.
use super::{
    frontmatter::{Node, NodeKind},
    issue::Timestamp,
    yaml_decode::duplicate_errors,
};
use std::collections::{BTreeMap, HashMap, HashSet};
mod scalar;
use scalar::decode as scalar;

#[derive(Clone, Debug, PartialEq)]
pub enum YamlValue {
    Null,
    String(Vec<u8>),
    Bool(bool),
    Int(i64),
    Uint(u64),
    Float(f64),
    Timestamp(Timestamp),
    Sequence(Vec<YamlValue>),
    StringMap(BTreeMap<Vec<u8>, YamlValue>),
    Map(Vec<(YamlValue, YamlValue)>),
}

struct Decoder<'a> {
    anchors: HashMap<usize, &'a Node>,
    active: HashSet<usize>,
    depth: usize,
    count: usize,
    aliases: usize,
}
fn anchors<'a>(node: &'a Node, out: &mut HashMap<usize, &'a Node>) {
    if node.kind != NodeKind::Alias && node.anchor_id != 0 {
        out.insert(node.anchor_id, node);
    }
    for child in &node.children {
        anchors(child, out);
    }
}
impl<'a> Decoder<'a> {
    fn count(&mut self) -> Result<(), ()> {
        self.count += 1;
        if self.depth > 0 {
            self.aliases += 1;
        }
        let ratio = if self.count <= 400_000 {
            0.99
        } else if self.count >= 4_000_000 {
            0.10
        } else {
            0.99 - 0.89 * (self.count - 400_000) as f64 / 3_600_000.0
        };
        if self.aliases > 100
            && self.count > 1000
            && self.aliases as f64 / self.count as f64 > ratio
        {
            Err(())
        } else {
            Ok(())
        }
    }
    fn resolve(&self, node: &'a Node) -> Result<&'a Node, ()> {
        if node.kind == NodeKind::Alias {
            self.anchors.get(&node.anchor_id).copied().ok_or(())
        } else {
            Ok(node)
        }
    }
    fn value(&mut self, node: &'a Node) -> Result<Option<YamlValue>, ()> {
        self.count()?;
        if node.kind == NodeKind::Alias {
            let id = std::ptr::from_ref(node).addr();
            if !self.active.insert(id) {
                return Err(());
            }
            self.depth += 1;
            let result = self.value(self.resolve(node)?);
            self.depth -= 1;
            self.active.remove(&id);
            return result;
        }
        Ok(Some(match node.kind {
            NodeKind::Scalar => scalar(node)?,
            NodeKind::Sequence => {
                let mut values = Vec::new();
                for child in &node.children {
                    if let Some(v) = self.value(child)? {
                        values.push(v);
                    }
                }
                YamlValue::Sequence(values)
            }
            NodeKind::Mapping => {
                if !duplicate_errors(node).is_empty() {
                    return Ok(None);
                }
                let strings = node.children.as_chunks::<2>().0.iter().all(|p| {
                    self.resolve(&p[0])
                        .is_ok_and(|key| matches!(key.tag.as_str(), "!!str" | "!!merge"))
                });
                if strings {
                    let mut map = BTreeMap::new();
                    self.string_map(node, &mut map, &mut None)?;
                    YamlValue::StringMap(map)
                } else {
                    let mut map = Vec::new();
                    self.general_map(node, &mut map, &mut None)?;
                    YamlValue::Map(map)
                }
            }
            NodeKind::Alias => unreachable!(),
        }))
    }
    fn key(&mut self, node: &'a Node) -> Result<Option<Vec<u8>>, ()> {
        self.count()?;
        if node.kind == NodeKind::Alias {
            let id = std::ptr::from_ref(node).addr();
            if !self.active.insert(id) {
                return Err(());
            }
            self.depth += 1;
            let result = self.key(self.resolve(node)?);
            self.depth -= 1;
            self.active.remove(&id);
            return result;
        }
        if node.kind != NodeKind::Scalar {
            return Ok(None);
        }
        let value = scalar(node)?;
        Ok(Some(match value {
            YamlValue::Null => return Ok(None),
            YamlValue::String(bytes) => bytes,
            _ => node.value.as_deref().unwrap_or_default().as_bytes().into(),
        }))
    }
    fn string_map(
        &mut self,
        node: &'a Node,
        map: &mut BTreeMap<Vec<u8>, YamlValue>,
        seen: &mut Option<Vec<YamlValue>>,
    ) -> Result<(), ()> {
        if !duplicate_errors(node).is_empty() {
            return Ok(());
        }
        let mut merge = None;
        for pair in node.children.as_chunks::<2>().0 {
            if pair[0].tag == "!!merge" {
                merge = Some(&pair[1]);
                continue;
            }
            if let Some(key) = self.key(&pair[0])? {
                if let Some(seen) = seen {
                    let value = YamlValue::String(key.clone());
                    if seen.contains(&value) {
                        continue;
                    }
                    seen.push(value);
                }
                if let Some(value) = self.value(&pair[1])? {
                    map.insert(key, value);
                }
            }
        }
        if let Some(merge) = merge {
            if seen.is_none() {
                *seen = Some(self.merge_keys(node)?);
            }
            for item in self.merge_maps(merge)? {
                if self.resolve(item)?.kind != NodeKind::Mapping {
                    return Err(());
                }
                self.string_map_alias(item, map, seen)?;
            }
        }
        Ok(())
    }
    fn string_map_alias(
        &mut self,
        node: &'a Node,
        map: &mut BTreeMap<Vec<u8>, YamlValue>,
        seen: &mut Option<Vec<YamlValue>>,
    ) -> Result<(), ()> {
        self.count()?;
        if node.kind == NodeKind::Alias {
            let id = std::ptr::from_ref(node).addr();
            if !self.active.insert(id) {
                return Err(());
            }
            self.depth += 1;
            self.count()?;
            let result = self.string_map(self.resolve(node)?, map, seen);
            self.depth -= 1;
            self.active.remove(&id);
            result
        } else {
            self.string_map(node, map, seen)
        }
    }
    fn general_map(
        &mut self,
        node: &'a Node,
        map: &mut Vec<(YamlValue, YamlValue)>,
        seen: &mut Option<Vec<YamlValue>>,
    ) -> Result<(), ()> {
        if !duplicate_errors(node).is_empty() {
            return Ok(());
        }
        let mut merge = None;
        for pair in node.children.as_chunks::<2>().0 {
            if pair[0].tag == "!!merge" {
                merge = Some(&pair[1]);
                continue;
            }
            if let Some(key) = self.value(&pair[0])? {
                if matches!(
                    key,
                    YamlValue::Sequence(..) | YamlValue::Map(..) | YamlValue::StringMap(..)
                ) {
                    return Err(());
                }
                if let Some(seen) = seen {
                    if seen.contains(&key) {
                        continue;
                    }
                    seen.push(key.clone());
                }
                if let Some(value) = self.value(&pair[1])? {
                    if let Some(entry) = map.iter_mut().find(|(k, _)| *k == key) {
                        entry.1 = value;
                    } else {
                        map.push((key, value));
                    }
                }
            }
        }
        if let Some(merge) = merge {
            if seen.is_none() {
                *seen = Some(self.merge_keys(node)?);
            }
            for item in self.merge_maps(merge)? {
                if self.resolve(item)?.kind != NodeKind::Mapping {
                    return Err(());
                }
                self.count()?;
                if item.kind == NodeKind::Alias {
                    let id = std::ptr::from_ref(item).addr();
                    if !self.active.insert(id) {
                        return Err(());
                    }
                    self.depth += 1;
                    self.count()?;
                    self.general_map(self.resolve(item)?, map, seen)?;
                    self.depth -= 1;
                    self.active.remove(&id);
                } else {
                    self.general_map(item, map, seen)?;
                }
            }
        }
        Ok(())
    }
    fn merge_keys(&mut self, node: &'a Node) -> Result<Vec<YamlValue>, ()> {
        let mut keys = Vec::new();
        for pair in node.children.as_chunks::<2>().0 {
            if let Some(key) = self.value(&pair[0])? {
                if matches!(
                    key,
                    YamlValue::Sequence(..) | YamlValue::Map(..) | YamlValue::StringMap(..)
                ) {
                    return Err(());
                }
                if !keys.contains(&key) {
                    keys.push(key);
                }
            }
        }
        Ok(keys)
    }
    fn merge_maps(&self, node: &'a Node) -> Result<Vec<&'a Node>, ()> {
        let maps = match node.kind {
            NodeKind::Mapping | NodeKind::Alias => vec![node],
            NodeKind::Sequence => node.children.iter().collect(),
            _ => return Err(()),
        };
        Ok(maps)
    }
}
pub(crate) fn string_map(data: &[u8]) -> Option<BTreeMap<Vec<u8>, YamlValue>> {
    let text = std::str::from_utf8(data).ok()?;
    let text = text.strip_prefix('\u{feff}').unwrap_or(text);
    // The generic decoder accepts alias keys; use the shared parser's alias
    // key adaptation. Syntax errors leave the original doc body available.
    let node = super::yaml::parse_optional_with_syntax("document", text, |_, _| {
        super::frontmatter::Error::new("invalid YAML".into())
    })
    .ok()??;
    if node.kind != NodeKind::Mapping || !duplicate_errors(&node).is_empty() {
        return None;
    }
    let mut decoder = Decoder {
        anchors: HashMap::new(),
        active: HashSet::new(),
        depth: 0,
        count: 2,
        aliases: 0,
    };
    anchors(&node, &mut decoder.anchors);
    let mut result = BTreeMap::new();
    // yaml.Unmarshal errors are deliberately ignored by the doc loader. A
    // fatal value error keeps previously assigned top-level map entries.
    let _ = decoder.string_map(&node, &mut result, &mut None);
    Some(result)
}
