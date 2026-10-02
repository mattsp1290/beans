pub use crate::vault::{read_public_file as read, valid_public_path as valid};
use axum::response::{IntoResponse, Response};
pub fn mime(path: &str) -> &'static str {
    match path
        .rsplit('.')
        .next()
        .unwrap_or_default()
        .to_ascii_lowercase()
        .as_str()
    {
        "html" => "text/html; charset=utf-8",
        "js" => "text/javascript; charset=utf-8",
        "css" => "text/css; charset=utf-8",
        "json" => "application/json",
        "svg" => "image/svg+xml",
        "png" => "image/png",
        "jpg" | "jpeg" => "image/jpeg",
        "gif" => "image/gif",
        "webp" => "image/webp",
        "ico" => "image/x-icon",
        "woff" => "font/woff",
        "woff2" => "font/woff2",
        _ => "application/octet-stream",
    }
}
pub fn response(path: &str, bytes: Vec<u8>) -> Response {
    (
        [
            ("content-type", mime(path)),
            ("x-content-type-options", "nosniff"),
        ],
        bytes,
    )
        .into_response()
}

/// Hub-authored SVG is an untrusted active document, unlike embedded app assets.
pub fn hub_asset_response(path: &str, bytes: Vec<u8>) -> Response {
    let mut response = response(path, bytes);
    if path
        .rsplit('.')
        .next()
        .is_some_and(|extension| extension.eq_ignore_ascii_case("svg"))
    {
        response.headers_mut().insert("content-security-policy", "sandbox; default-src 'none'; script-src 'none'; connect-src 'none'; img-src data:; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'".parse().unwrap());
    }
    response
}
