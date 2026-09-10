# bn UI

Svelte 5 + Vite app embedded into the `bn` binary by `ui/embed.go`. The
issues board and wiki are rewritten in WP6 of the hub vault redesign.

## Development

```sh
npm install
npm run dev
```

The Vite dev server proxies `/api` to `http://127.0.0.1:7777` (`bn serve`)
by default. Set `VITE_API_PROXY_TARGET` to point at a different address.

## Checks

```sh
npm run test
npm run check
npm run build     # writes dist/, which go:embed picks up on the next go build
```
