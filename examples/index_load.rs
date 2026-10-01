//! Development-only measurement of the production loader on the WP1 fixture.
use beans::vault::{Index, NoteData};
use serde_json::json;
use std::{path::PathBuf, time::Instant};
fn main() {
    let root = PathBuf::from(std::env::args_os().nth(1).expect("hub path required"));
    let mut samples = Vec::new();
    for i in 0..23 {
        let start = Instant::now();
        let index = Index::load(&root).expect("index load");
        let elapsed = start.elapsed().as_secs_f64() * 1000.0;
        assert_eq!(
            index
                .ordered_notes()
                .iter()
                .filter(|n| matches!(&n.data, NoteData::Issue(_)))
                .count(),
            5001
        );
        assert_eq!(
            index
                .ordered_notes()
                .iter()
                .filter(|n| matches!(&n.data, NoteData::Issue(d) if !d.metadata.archived))
                .count(),
            5000
        );
        if i >= 3 {
            samples.push(elapsed);
        }
        std::hint::black_box(index);
    }
    let mut ordered = samples.clone();
    ordered.sort_by(f64::total_cmp);
    println!(
        "{}",
        json!({"schema":"beans-index-load-v1","issue_count":5000,"total_issue_count":5001,"warmup_runs":3,"os":std::env::consts::OS,"arch":std::env::consts::ARCH,"profile":"release","samples_ms":samples,"median_ms":(ordered[9]+ordered[10])/2.0,"p95_ms":ordered[19],"min_ms":ordered[0],"max_ms":ordered[19]})
    );
}
