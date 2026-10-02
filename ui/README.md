# bn UI

Svelte 5 + Vite app embedded into the `bn` binary by Rust `build.rs`. The app provides the
issues board and wiki over the native HTTP API.

## Development

```sh
npm ci
npm run dev
```

The Vite dev server proxies `/api` to `http://127.0.0.1:7777` (`bn serve`)
by default. Set `VITE_API_PROXY_TARGET` to point at a different address.

## Checks

```sh
npm run test
npm run check
npm run build     # writes dist/, embedded by the next Cargo build
```
