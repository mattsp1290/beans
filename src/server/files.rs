pub use crate::vault::{read_public_file as read, valid_public_path as valid};
use axum::response::{IntoResponse, Response};
pub fn mime(path: &str) -> &'static str {
    match path.rsplit('.').next().unwrap_or_default() {
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
