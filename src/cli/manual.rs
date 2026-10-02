//! A portable roff manual generated from the actual Clap command tree.
pub(super) fn render() -> String {
    let mut root = super::command();
    root.build();
    let mut out = format!(
        ".TH BN 1 \"\" \"Beans {}\"\n.SH NAME\nbn \\ - git-backed issues and wiki\n.SH SYNOPSIS\n.nf\n{}\n.fi\n.SH COMMANDS\n",
        env!("CARGO_PKG_VERSION"),
        escape(&root.render_usage().to_string())
    );
    fn walk(command: &clap::Command, path: &str, out: &mut String) {
        for child in command.get_subcommands() {
            let path = format!("{path} {}", child.get_name());
            out.push_str(&format!(
                ".SS {}\n{}\n",
                escape(&path),
                escape(
                    &child
                        .get_about()
                        .map(ToString::to_string)
                        .unwrap_or_default()
                )
            ));
            let mut copy = child.clone();
            out.push_str(&format!(
                ".nf\n{}\n.fi\n",
                escape(&copy.render_long_help().to_string())
            ));
            walk(child, &path, out);
        }
    }
    walk(&root, "bn", &mut out);
    out
}
fn escape(s: &str) -> String {
    s.replace('\\', "\\e")
        .replace('-', "\\-")
        .lines()
        .map(|line| {
            if line.starts_with(['.', '\'']) {
                format!("\\&{line}")
            } else {
                line.to_owned()
            }
        })
        .collect::<Vec<_>>()
        .join("\n")
}
