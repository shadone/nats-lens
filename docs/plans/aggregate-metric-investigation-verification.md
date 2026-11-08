# Aggregate metric destination verification

Base: 97fa1bdefc2126e580786aff2a742b336653fa89. Board task bXMlS0kOLTauMBDTEL280 remains in progress for traffic volumes and global ranking. This slice adds eight aggregate pages for nine count/meta metric IDs and canonical Overview tile routing. No Go/proto/dependency/migration changes. Proposed, not yet confirmed: at most two aggregate histories per read, through the existing bounded series API.

## Runtime checkpoints

- `07a57a50`: fourteen valid runtime failures for aggregate definitions/reads/navigation. The catalogue source-file test initially had an incompatible import.meta URL in jsdom; that setup failure is excluded. Its corrected relative path independently reaches runtime RED with navigation removed, then GREEN. `ade4cc69`: fifteen focused tests and types pass.
- `31438f17`: eleven runtime page/tile-link failures.`97ef6de6`: forty-eight focused tests including existing Overview tiles pass, with types. A test assertion was made plural for multiple legitimate Unknown cells, not counted as a production correction.
- `d109ba2b` adds real-query timer tests (live clock advances; fixed bounds do not), copied aggregate components and two-query work bounds. Existing tile-navigation assertions now use exact canonical paths rather than permissive suffix matching; all context assertions remain.
-Five independent fix-removal controls fail at runtime: page, catalogue/navigation, API composition, tile identity mapping, copied context. Production files restore byte-for-byte in finally blocks. Restored five suites: 30 tests pass. Logs /private/tmp/nats-aggregate-removal/{page,catalogue,comparison,tiles,prompt}.log and results.json; /private/tmp/nats-aggregate-restored.log.

## Verification

Commands from web/: `npm test -- --config vite.metric.config.ts --configLoader runner --maxWorkers=2`; `npm run typecheck`; `npx vite build --config vite.metric.config.ts --configLoader runner`. Temporary config only redirects cache outside borrowed dependencies.

- Full web: 201 files, 1,904 passed, 24 existing skips, 78.64s. /private/tmp/nats-aggregate-full-web.log.
- Types and production build pass; existing 500 kB warning unchanged. /private/tmp/nats-aggregate-final-types.log and /private/tmp/nats-aggregate-build.log.
- Feature audit clean: 46 capabilities; both index sections updated.
- Catalogue conformance checks all 37 accepted metric IDs using the entity identity required by each scope. Combined asset metrics share a destination; a system aggregate cannot narrow itself to an attached server context.
- API tests cover fresh fixed frames, missing live clocks, fixed ranges without clocks, exact native integer totals, per-query failures, wrong identities, incomplete responses and oversized work. No coverage percentage, broad Go/check-engine or new performance claim.

## Actual browser

Only task-owned fixed synthetic demo on port 18692. No private port 18900 access. T3 native navigation/read/pointer/keyboard worked: all 9 Overview title destinations, Meta leader→observed changes, native last/total/peak and timestamps, fixed inventory/event links. Shift+Right then Enter selected 14:00:00–14:00:38; Open metric details retained exactly that system interval and removed the live range. Derived transitions and advisory/event evidence remain explicitly distinct.

T3 twice reported requested freeform 360x800 but actual viewport/evaluated innerWidth 1280. Those are not mobile proof. The installed isolated agent-browser fallback initially hit a sandbox socket-directory denial, then an approved retry controlled its own temporary browser session against loopback only. Actual 360px assets view: document 360px, two 294px histories, contained table and no API alerts. Screenshot /private/tmp/nats-aggregate-assets-360.png was visually inspected: real fonts, readable controls, no document overflow. Additional account/stream/meta viewport checks are recorded in terminal receipts; no physical touch or native clipboard certification.

## Scope limits

Counts do not identify contributors, exact historical sets or causes. Aggregate inventory links explain newest-collection and count-definition differences. Meta observations are derived majority transitions, not complete captured elections or independent-group health. Resource/replica-setting counts do not prove healthy placement. Known/returned rows do not certify capture. Server/resource pages and existing storage/detection specialists remain intact; precise traffic volumes and global ranking are still open.
