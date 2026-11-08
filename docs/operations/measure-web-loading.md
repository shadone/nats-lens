# Measure production web loading

Use this procedure to compare production bundle and browser costs before changing route boundaries or dependencies. The [2026-11-08 baseline](../research/web-loading-2026-11-08.md) records the first measurements and proposed budgets.

Use a clean, isolated worktree, installed lockfile-matching web dependencies, Node, Go, and an existing Chromium browser. Browser automation additionally needs an already installed Playwright Core; it is not a project dependency. Use only the generic fixed demo and a free loopback port. This procedure does not require a database, collector, NATS connection, or dependency installation.

## Record the source and build

1. Record the exact source and toolchain from the worktree root.

   ```sh
   git status --short
   git rev-parse HEAD
   node --version
   npm --version
   go version
   shasum -a 256 web/package-lock.json
   ```

   Continue with a clean source tree and the intended dependencies. A version mismatch means the result needs its own baseline identity.

2. Create private evidence storage, then run the production TypeScript check and Vite build. The observer below records metadata without source maps or a permanent analysis dependency.

   ```sh
   export BUNDLE_EVIDENCE="$(mktemp -d /private/tmp/natslens-bundle.XXXXXX)"
   cd web
   ./node_modules/.bin/tsc --noEmit
   node --input-type=module > "$BUNDLE_EVIDENCE/build.log" 2>&1 <<'JS'
   import { build } from 'vite';
   import { writeFileSync } from 'node:fs';
   import { gzipSync, brotliCompressSync, constants } from 'node:zlib';
   const evidence = process.env.BUNDLE_EVIDENCE;
   await build({
     configLoader: 'runner',
     cacheDir: `${evidence}/vite-cache`,
     plugins: [{ name: 'private-bundle-evidence', generateBundle(_, bundle) {
       const chunks = Object.values(bundle).filter(x => x.type === 'chunk').map(x => ({
         file: x.fileName, entry: x.isEntry, imports: x.imports,
         dynamicImports: x.dynamicImports,
         minified: Buffer.byteLength(x.code),
         gzip9: gzipSync(x.code, { level: 9 }).length,
         brotli11: brotliCompressSync(x.code, {
           params: { [constants.BROTLI_PARAM_QUALITY]: 11 },
         }).length,
         modules: Object.entries(x.modules).map(([id, m]) => ({
           id, renderedLength: m.renderedLength,
         })),
       }));
       writeFileSync(`${evidence}/chunks.json`, JSON.stringify(chunks, null, 2));
     }}],
   });
   JS
   touch ../internal/webui/dist/.gitkeep
   cd ..
   tail -8 "$BUNDLE_EVIDENCE/build.log"
   ```

   Check both command exit statuses before using the evidence. Keep the chunk warning in the log. `renderedLength` ranks modules before final chunk minification; it does not allocate compressed bytes among dependencies. The observer writes absolute module paths privately; normalize them before committing any summary.

3. Inspect `internal/webui/dist/index.html` and `chunks.json`. Start with every HTML module script and module-preload link, then recursively include static `imports`, deduplicating filenames. Sum their minified, gzip, and Brotli bytes. Report dynamically fetched chunks separately, adding any fetched during initial loading to the initial total. Do not use only the largest chunk as the initial size.

   Record the HTML/CSS/font costs separately. Verify the generated files against metadata and repeat the build when establishing reproducibility. For the baseline, both builds produced the same JavaScript SHA-256.

## Measure the embedded server

1. Build the binary with the completed production assets and a private Go cache, then start the generic demo in a dedicated terminal.

   ```sh
   GOCACHE="$BUNDLE_EVIDENCE/go-cache" GOPROXY=off GOTOOLCHAIN=local \
     go build -o "$BUNDLE_EVIDENCE/natslens" ./cmd/natslens
   "$BUNDLE_EVIDENCE/natslens" demo -fixed -listen 127.0.0.1:18439
   ```

   A successful start reports `serving` on the chosen port. Stop if dependencies are missing or the port is occupied; do not install or stop another process as part of measurement. A restricted environment may need permission for this isolated listener. Keep the binary running until browser measurements finish.

2. In another terminal, request the actual JavaScript filename from the generated HTML. The baseline command was:

   ```sh
   curl -sS -H 'Accept-Encoding: gzip, br' \
     -D "$BUNDLE_EVIDENCE/js-headers.txt" \
     -o "$BUNDLE_EVIDENCE/transferred.js" \
     http://127.0.0.1:18439/assets/index-C79PO9cE.js
   wc -c "$BUNDLE_EVIDENCE/transferred.js"
   cat "$BUNDLE_EVIDENCE/js-headers.txt"
   ```

   Compare `Content-Encoding`, `Content-Length`, and body size with the file. Do not add `--compressed`: decoding would obscure the encoded body size. A missing encoding means gzip/Brotli remain estimates. Browser `encodedBodySize` and `decodedBodySize` provide another check; `transferSize` includes a browser header allowance, not exact wire bytes.

## Measure browser work

Use T3 Preview first when available. After two identical inspection failures, record the raw errors and use an isolated installed browser. Do not mix browser versions into one timing series.

For a manual comparison, use Chrome DevTools Network and Performance with a fresh profile, service workers blocked, cache disabled, and viewport 1280 × 800. Measure both native CPU/unthrottled loopback and the constrained profile: 1.6 Mbps download, 0.75 Mbps upload, 150 ms latency, and 4× CPU slowdown. Record browser version, host, CPU conditions, cache state, and all settings with the results.

The baseline's automation used the following Chrome DevTools Protocol (CDP) calls, in order after creating each fresh context. These exact values avoid ambiguity between bits and bytes or preset names:

```js
await cdp.send('Network.enable');
await cdp.send('Network.setCacheDisabled', { cacheDisabled: true });
await cdp.send('Emulation.setCPUThrottlingRate', { rate: constrained ? 4 : 1 });
await cdp.send('Network.emulateNetworkConditions', {
  offline: false,
  latency: constrained ? 150 : 0,
  downloadThroughput: constrained ? 200000 : -1,
  uploadThroughput: constrained ? 93750 : -1,
});
await cdp.send('Performance.enable');
```

The baseline used `chromium.launch` with installed Google Chrome, `headless: true`, and `--disable-background-networking`. Each context used `viewport: { width: 1280, height: 800 }` and `serviceWorkers: 'block'`. Browser-process startup was excluded. No external traffic shaping proxy modified the server responses.

1. Navigate to `/fleet`. Observe **3 systems, worst first**, the completed `ListSystems` resource, and two animation frames. Record navigation-start-to-observation time, DOMContentLoaded, paint entries, all resource timings, and `Performance.getMetrics`.
2. Record `performance.now()`, then click the sidebar link containing `/streams`. Observe `order-jobs` inside `main`, then two animation frames. Record the elapsed time and resources whose `startTime` follows the mark.
3. Record another start mark, then click the sidebar link containing `/checks`. Observe the **Search checks** input, then two animation frames. Capture the same fields.
4. Close the context. Repeat the complete sequence three times per profile, with a fresh context each time. Keep all samples and report medians and ranges. Separate cold deep links from client-side navigation if adding more routes.

The baseline's readiness frame wait was:

```js
await page.evaluate(() => new Promise(resolve =>
  requestAnimationFrame(() => requestAnimationFrame(resolve))));
```

These markers measure DOM readiness observed by automation. They do not prove a paint has completed or all fonts have arrived. Preserve raw paint entries; if FCP is absent at readiness, report it as unavailable rather than substituting first paint.

Capture `performance.getEntriesByType('resource')`, `performance.getEntriesByType('navigation')`, and `performance.getEntriesByType('paint')`. Keep resource path, initiator type, `startTime`, `requestStart`, `responseStart`, `responseEnd`, `transferSize`, `encodedBodySize`, `decodedBodySize`, status, and encoding. Register a buffered `PerformanceObserver` for `longtask` before loading the page. Read CDP `ScriptDuration` and `TaskDuration`; convert seconds to milliseconds and subtract consecutive cumulative samples for navigation costs.

Keep the following measurements distinct:

| Measurement | Interpretation |
| --- | --- |
| Asset `responseEnd - responseStart` | Browser-observed body interval, including delivery waits |
| API `requestStart` → `responseEnd` | Complete observed request, including transport/provider work |
| API earliest start → latest end | Overlapping request window; do not sum parallel durations |
| CDP `ScriptDuration` | Aggregate execution through observation, including render/data callbacks |
| CDP `TaskDuration` | Main-thread task time; not exclusively JavaScript |
| Diagnostic parser trace events | Inclusive parse/compile observations, not an additive CPU breakdown |

For parser investigation, record a separate diagnostic trace using categories `devtools.timeline,v8,v8.execute,disabled-by-default-v8.compile`. Inspect `V8.ParseProgram`, `V8.ParseFunction`, and compile events separately from execution. Nested events overlap; background streaming-parser wall spans can include network waits. Keep the trace private unless a sanitized artifact is needed. Tracing overhead means diagnostic runs are separate from the timing series.

## Finish the comparison

Record exact initial JS totals, biggest chunks, compression settings, actual encoding, browser samples, request counts, and limits. Compare the complete initial static/preload closure and representative first navigations. A smaller entry chunk with the same initial shared downloads does not satisfy a reduction target.

Stop only the demo and browser processes started for this measurement. Remove the borrowed dependency symlink and temporary configuration if used. Keep a sanitized result record with the source commit, toolchain, and proposed follow-up budgets; keep raw traces and module paths private. This workflow changes operational evidence, so it does not create a shipped feature claim in `docs/features/`.
