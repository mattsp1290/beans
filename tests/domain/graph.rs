use beans::vault::{GraphNote, LinkRef, NoteGraph, NoteKind, RawLink, Warning};
use serde_json::{Value, json};
fn text(bytes: &[u8]) -> String {
    String::from_utf8(bytes.into()).unwrap()
}
fn link(link: &LinkRef) -> Value {
    json!({"From":text(&link.from),"To":text(&link.to),"Kind":link.kind})
}
fn snapshot(graph: &NoteGraph, targets: &Value) -> Value {
    let mut owners = serde_json::Map::new();
    let mut ids = serde_json::Map::new();
    let mut out = serde_json::Map::new();
    for n in graph.ordered_notes() {
        owners.insert(
            text(&n.basename),
            json!(text(&graph.by_basename(&n.basename).unwrap().path)),
        );
        if let Some(id) = &n.id
            && let Some(note) = graph.by_id(n.kind, id)
        {
            let kind = serde_json::to_value(n.kind)
                .unwrap()
                .as_str()
                .unwrap()
                .to_owned();
            ids.insert(format!("{kind}:{}", text(id)), json!(text(&note.path)));
        }
        out.insert(
            text(&n.path),
            Value::Array(n.outlinks.iter().map(link).collect()),
        );
    }
    let aliases: serde_json::Map<_, _> = graph
        .aliases()
        .iter()
        .map(|(key, value)| (text(key), json!(text(value))))
        .collect();
    let backlinks: serde_json::Map<_, _> = graph
        .backlinks()
        .iter()
        .map(|(key, value)| (text(key), Value::Array(value.iter().map(link).collect())))
        .collect();
    let lookups: serde_json::Map<_, _> = targets
        .as_array()
        .unwrap()
        .iter()
        .map(|target| {
            let key = target.as_str().unwrap();
            let path = graph
                .lookup(key.as_bytes())
                .map_or(String::new(), |n| text(&n.path));
            (key.into(), json!(path))
        })
        .collect();
    let warnings: Vec<_> = graph
        .warnings()
        .iter()
        .map(|Warning { path, error }| json!({"Path":text(path),"Error":error}))
        .collect();
    json!({"Owners":owners,"IDs":ids,"Aliases":aliases,"Lookups":lookups,"Outlinks":out,"Backlinks":backlinks,"Warnings":warnings})
}
#[test]
fn ordered_graph_rebuild_lookup_links_and_removal_match_fixed_go() {
    let fixture: Value = serde_json::from_str(include_str!("../contract/graph.json")).unwrap();
    for (i, case) in fixture["graphs"].as_array().unwrap().iter().enumerate() {
        let mut graph = NoteGraph::default();
        graph.add_parse_warning(b"docs/z.md".to_vec(), "parse z".into());
        graph.add_parse_warning(b"docs/a.md".to_vec(), "parse a".into());
        for input in case["Notes"].as_array().unwrap() {
            let bytes = |key| input[key].as_str().unwrap().as_bytes().to_vec();
            let aliases = input["Aliases"].as_array().map_or(Vec::new(), |v| {
                v.iter()
                    .map(|a| a.as_str().unwrap().as_bytes().to_vec())
                    .collect()
            });
            let links = input["Links"].as_array().map_or(Vec::new(), |v| {
                v.iter()
                    .map(|l| RawLink {
                        target: l["Target"].as_str().unwrap().as_bytes().to_vec(),
                        kind: serde_json::from_value(l["Kind"].clone()).unwrap(),
                    })
                    .collect()
            });
            let kind: NoteKind = serde_json::from_value(input["Kind"].clone()).unwrap();
            graph.register(GraphNote {
                kind,
                path: bytes("Path"),
                basename: bytes("Basename"),
                id: (!matches!(kind, NoteKind::Doc | NoteKind::Memory)).then(|| bytes("ID")),
                aliases,
                raw_out: links,
                outlinks: Vec::new(),
            });
        }
        graph.rebuild();
        assert_eq!(
            snapshot(&graph, &case["Targets"]),
            case["Before"],
            "case{i} before"
        );
        graph.remove_path(case["Remove"].as_str().unwrap().as_bytes());
        graph.rebuild();
        assert_eq!(
            snapshot(&graph, &case["Targets"]),
            case["After"],
            "case{i} after"
        );
    }
}
#[test]
fn index_path_rules_match_fixed_go_raw_byte_paths() {
    use beans::vault::*;
    let fixture: Value = serde_json::from_str(include_str!("../contract/graph.json")).unwrap();
    for case in fixture["paths"].as_array().unwrap() {
        let bytes = |key| serde_json::from_value::<Vec<u8>>(case[key].clone()).unwrap();
        let path = bytes("Input");
        let classified = classify(&path);
        assert_eq!(classified.is_some(), case["Classified"].as_bool().unwrap());
        if let Some((kind, project)) = classified {
            assert_eq!(serde_json::to_value(kind).unwrap(), case["Kind"]);
            assert_eq!(project, bytes("Project"));
        }
        assert_eq!(is_asset_path(&path), case["Asset"].as_bool().unwrap());
        assert_eq!(skip_dir_name(&path), case["Skip"].as_bool().unwrap());
        assert_eq!(
            is_plans_directory(&path),
            case["PlansDirectory"].as_bool().unwrap()
        );
        for (actual, key, flag) in [
            (plan_bundle_project(&path), "Bundle", "BundleOK"),
            (plan_manifest_project(&path), "Manifest", "ManifestOK"),
        ] {
            assert_eq!(actual.is_some(), case[flag].as_bool().unwrap());
            if let Some(project) = actual {
                assert_eq!(project, bytes(key));
            }
        }
    }
}
