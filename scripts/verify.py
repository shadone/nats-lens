#!/usr/bin/env python3
"""Read-only fast/full verification; heavy tiers are always explicit."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import signal
import subprocess
import sys
import tempfile
import time
from datetime import datetime, timezone

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parent.parent
COMMANDS = {
    'gate-tests': ([sys.executable, '-B', '-m', 'unittest', 'discover', '-s', 'scripts', '-p', 'verify_test.py'], '.'),
    'build-tests': (['node', '--test', 'scripts/build-web.test.mjs'], '.'),
    'types': (['npm', 'run', 'typecheck'], 'web'),
    'web-tests': (['npm', 'test', '--', '--configLoader', 'runner', '--maxWorkers=2'], 'web'),
    'vet-fast': (['go', 'vet', './internal/config', './internal/routehealth', './internal/collect', './cmd/natslens'], '.'),
    'vet-full': (['go', 'vet', './...'], '.'),
    'go-fast': (['./scripts/test-go-fast.sh'], '.'),
    'go-full': (['go', 'test', '-p', '1', '-count=1', '-timeout', '40m', './...'], '.'),
    'race-api': (['go', 'test', '-race', '-short', '-count=1', '-timeout', '5m', './internal/api', '-run', '^(TestEntityPagesLimitsCancellationAndEviction|TestBatchHasABudget|TestBatchReviewClientCancelNoLeak)$'], '.'),
    'race-k8s': (['go', 'test', '-race', '-short', '-count=1', '-timeout', '5m', './internal/k8s', '-run', '^(TestRetryPaginationAndCancellation|TestRetryAuthLoggingAndRecovery|TestRetryQuietWatchRecoversWhileOpen)$'], '.'),
    # The upgrade, archive and recovery compatibility matrix: every schema
    # version as a starting point (COMPAT_ENV), with the migration, archive,
    # crash and recovery tests it builds on. See
    # docs/operations/upgrade-compatibility.md.
    'compat-store': (['go', 'test', '-count=1', '-timeout', '40m', './internal/store', '-run', '^(' + '|'.join([
        'TestCompat[A-Za-z]+', 'TestNewerDatabaseIsRefusedBeforeAnyWrite', 'TestCheckSchemaNamesANewerDatabase',
        'TestEditedMigrationRefusedBeforeBackfill', 'TestMigrationsMatchGolden', 'TestAcceptedEarlierChecksumOpens',
        'TestMigrationChecksumsRecorded', 'TestEditedMigrationFailsStart', 'TestMissingChecksumsBackfilled',
        'TestEditedMigrationStopsPendingOnes', 'TestChecksumRecordedWithMigration', 'TestUpgradeFromVersion9',
        'TestMigrationsFrom21OnAPopulatedDatabase', 'TestMigrationsFrom26OnAPopulatedDatabase', 'TestMigrationsFrom30OnMutes',
        'TestMigrationsFrom31OnAPopulatedDatabase', 'TestMigrationsFrom32OnAPopulatedDatabase', 'TestSourceHealthUpgrade',
        'TestNotificationIdentityUpgrade', 'TestNotifyAcknowledgementMemoryUpgrade', 'TestNotifyAcknowledgementUpgradeEvidence',
        'TestNotifyAcknowledgementUpgradeBaselineBoundaries', 'TestReopenAfterCrashAfterOpen', 'TestReopenAfterSchemaWALReplay',
        'TestUnreplayableWALIsErrWALReplay', 'TestIndexSurvivesWALReplayShutdown', 'TestIndexReplayProcess',
        'TestFatalBoundaryRefusesNativeOperations', 'TestFatalEngineContainment', 'TestArchiveRoundTrip', 'TestRebuildNeedsManifest', 'TestRebuildChecksRowCounts',
        'TestReexportCrashKeepsPreviousExport', 'TestExportIDNotReused', 'TestConcurrentExportsKeepRebuildWorking',
        'TestCleanupKeepsLaterExports', 'TestStateRaisesSequences', 'TestStateRaisesMuteSequence',
        'TestRebuildMixesMutesFilesFromBeforeAndAfterAcceptances', 'TestRebuildFromOldMutesFilesOnlyAddsNoRow',
        'TestRebuildBeforeAcceptancesLoadsPlainMutes', 'TestRebuildBeforeAcknowledgements', 'TestRebuildBeforeServerLimitsLoadsNull',
        'TestArchiveBeforeSourceHealth', 'TestDatasetCatalogAndContentsConserved']) + ')$'], '.'),
    'compat-cmd': (['go', 'test', '-count=1', '-timeout', '10m', './cmd/natslens', '-run', '^(TestFailedRebuildIsRetried|TestEmptyDatabaseIsRebuilt|TestDatabaseWithDataIsKept|TestUnreplayableWALIsMovedAsideAndRebuilt)$'], '.'),
}
# COMPAT_ENV makes the compatibility tests run every schema version rather
# than their seed versions; a non-UTC zone catches time-zone shifts.
COMPAT_ENV = {'NATSLENS_COMPAT_MATRIX': '1', 'TZ': 'Pacific/Honolulu'}
# Steps whose -run selector names tests the runner first checks exist.
SELECTED = ('race-', 'compat-')

def plan(profile):
    common = ['format', 'go-version', 'gate-tests', 'build-tests', 'types', 'proto', 'docs']
    return {
        'fast': common + ['vet-fast', 'go-fast', 'web-tests'],
        'full': common + ['vet-full', 'web-tests', 'go-full', 'race-api', 'race-k8s'],
        'release': common + ['vet-full', 'web-tests', 'go-full', 'race-api', 'race-k8s', 'compat-store', 'compat-cmd'],
        'web': ['types', 'web-tests'], 'proto': ['proto'], 'race': ['race-api', 'race-k8s'],
        'compat': ['compat-store', 'compat-cmd'],
    }[profile]

def format_failed(code, output):
    return code != 0 or bool(output.strip())

def unexpected_audit_issues(report, docs):
    if not isinstance(report.get('issues'), list):
        raise ValueError('Feature auditor returned no issues array')
    unexpected = []
    for issue in report['issues']:
        expected = False
        if issue.get('severity') == 'warning' and issue.get('kind') == 'bad-status':
            path = (docs / issue['location']).resolve()
            if path.is_relative_to(docs.resolve()) and path.is_file():
                statuses = re.findall(r'^- \*\*Status:\*\*\s*(.*?)\s*$', path.read_text(), re.M)
                expected = len(statuses) == 1 and statuses[0].lower() == 'planned'
        if not expected:
            unexpected.append(issue)
    return unexpected

def generated_drift(actual, expected):
    def files(root):
        return {p.relative_to(root).as_posix(): p.read_bytes() for p in root.rglob('*') if p.is_file() and p.name != '.DS_Store'}
    a, b = files(actual), files(expected)
    return sorted(name for name in a.keys() | b.keys() if a.get(name) != b.get(name))

def run_steps(steps, execute):
    results = []
    for step in steps:
        code = execute(step)
        results.append({'name': step, 'code': code})
        if code:
            break
    return results

def command(argv, cwd, log, environment=None):
    log.write('$ ' + ' '.join(map(str, argv)) + '\n'); log.flush()
    child = subprocess.Popen(list(map(str, argv)), cwd=cwd, stdout=log, stderr=subprocess.STDOUT, start_new_session=True, env={**os.environ, **(environment or {})})
    try:
        return child.wait()
    except BaseException:
        if child.poll() is None:
            os.killpg(child.pid, signal.SIGTERM)
            try:
                child.wait(timeout=5)
            except subprocess.TimeoutExpired:
                os.killpg(child.pid, signal.SIGKILL); child.wait()
        raise

def check_format(root, log):
    result = subprocess.run(['git', 'ls-files', '-z', '--cached', '--others', '--exclude-standard', '--', '*.go'], cwd=root, capture_output=True)
    if result.returncode:
        log.write(result.stderr.decode()); return result.returncode
    files = [name for name in result.stdout.decode().split('\0') if name and (root / name).is_file()]
    if not files:
        return 0
    result = subprocess.run(['gofmt', '-l', *files], cwd=root, text=True, capture_output=True)
    log.write(result.stdout + result.stderr)
    return 1 if format_failed(result.returncode, result.stdout) else 0

def go_release(gomod):
    """The Go release that builds the module, as (release, why not) with one
    of them None. It is the toolchain line's when there is one, else the go
    line's: setup-go's go-version-file reads the toolchain line first and
    then sets GOTOOLCHAIN=local, and go mod tidy drops a toolchain line that
    is not above the go line. A go line above the toolchain line would make
    setup-go install the older release, which then refuses the module."""
    go = re.findall(r'^go\s+(\S+)\s*(?://.*)?$', gomod, re.M)
    toolchain = re.findall(r'^toolchain\s+go(\S+)\s*(?://.*)?$', gomod, re.M)
    if len(go) != 1 or len(toolchain) > 1:
        return None, 'go.mod needs one go line and at most one toolchain line'
    full = lambda v: re.fullmatch(r'\d+\.\d+\.\d+', v)
    key = lambda v: tuple(int(n) for n in v.split('.'))
    if not toolchain:
        if not full(go[0]):
            return None, f'go.mod\'s go line {go[0]} names no full release (X.Y.Z) and there is no toolchain line'
        return go[0], None
    if not full(toolchain[0]):
        return None, f'go.mod\'s toolchain line go{toolchain[0]} names no full release (goX.Y.Z)'
    if full(go[0]) and key(go[0]) > key(toolchain[0]):
        return None, (f'go.mod\'s go line {go[0]} is above its toolchain line go{toolchain[0]}: setup-go would '
                      f'install {toolchain[0]} with GOTOOLCHAIN=local, which refuses the module; remove the '
                      'toolchain line (go mod tidy does) or raise it')
    return toolchain[0], None

def go_version_mismatch(gomod, dockerfile):
    """Why the Go release go.mod builds with (CI and Pages, through setup-go)
    and the Dockerfile's golang image (the image, the release binaries and
    the weekly govulncheck) differ, or None when they agree."""
    release, why = go_release(gomod)
    if why:
        return why
    images = sorted(set(re.findall(r'\bgolang:(\d+\.\d+\.\d+)', dockerfile)))
    if len(images) != 1:
        return f'the Dockerfile needs one golang:X.Y.Z image, found {images or "none"}'
    if release != images[0]:
        source = 'toolchain line' if re.search(r'^toolchain\s', gomod, re.M) else 'go line (no toolchain line)'
        return (f'go.mod builds with Go {release} (its {source}) and the Dockerfile with golang:{images[0]}: '
                'bump the older one to the newer release (go.mod: go mod edit -toolchain=goX.Y.Z, or the go '
                'line when it is the newer; the Dockerfile: the image with its digest and cache ids)')
    return None

def check_go_version(root, log):
    reason = go_version_mismatch((root / 'go.mod').read_text(), (root / 'Dockerfile').read_text())
    log.write((reason or 'go.mod and the Dockerfile name the same Go release') + '\n')
    return 1 if reason else 0

def template_outputs(text):
    outputs = [line.strip().removeprefix('-').strip().split(':', 1)[1].split('#', 1)[0].strip().strip('"\'') for line in text.splitlines() if re.match(r'^\s+(?:-\s+)?out:', line)]
    if sorted(outputs) != ['internal/gen', 'internal/gen', 'web/src/gen']:
        raise ValueError('Proto verifier requires the reviewed relative output directories internal/gen and web/src/gen; inspect changed generation configuration first')

def check_proto(root, log):
    for argv in [['buf', 'lint'], ['buf', 'format', '--diff', '--exit-code']]:
        code = command(argv, root, log)
        if code: return code
    template_outputs((root / 'buf.gen.yaml').read_text())
    inputs = [root / name for name in ['go.mod', 'go.sum', 'buf.yaml', 'buf.gen.yaml']] + sorted((root / 'proto').rglob('*.proto'))
    before = {p: p.read_bytes() for p in inputs}
    plugin = root / 'web/node_modules/.bin/protoc-gen-es'
    if not plugin.is_file():
        raise ValueError('Installed protoc-gen-es is missing; install the locked web dependencies explicitly')
    with tempfile.TemporaryDirectory(prefix='natslens-proto-check-') as name:
        temp = Path(name)
        for path, data in before.items():
            target = temp / path.relative_to(root); target.parent.mkdir(parents=True, exist_ok=True); target.write_bytes(data)
        (temp / 'web').mkdir(exist_ok=True)
        (temp / 'web/node_modules').symlink_to((root / 'web/node_modules').resolve(), target_is_directory=True)
        code = command(['buf', 'generate'], temp, log)
        if code: return code
        current = [root / name for name in ['go.mod', 'go.sum', 'buf.yaml', 'buf.gen.yaml']] + sorted((root / 'proto').rglob('*.proto'))
        if current != inputs or any(p.read_bytes() != data for p, data in before.items()):
            raise ValueError('Proto inputs changed during verification; retry with stable inputs')
        drift = []
        for directory in ['internal/gen', 'web/src/gen']:
            drift.extend(directory + '/' + p for p in generated_drift(root / directory, temp / directory))
        if drift:
            log.write('Generated-code drift (checkout was not modified):\n' + '\n'.join(drift[:30]) + '\n')
            return 1
    return 0

def find_auditor(explicit=None):
    if explicit or os.environ.get('FEATURE_DOCS_AUDITOR'):
        path = Path(explicit or os.environ['FEATURE_DOCS_AUDITOR']).expanduser()
        if path.is_file(): return path
        raise ValueError('FEATURE_DOCS_AUDITOR does not name a readable script')
    cache = Path.home() / '.codex/plugins/cache/ddenis/ddenis-core'
    def version(path): return tuple(int(x) for x in re.findall(r'\d+', path.parts[-5]))
    candidates = sorted(cache.glob('*/skills/feature-docs/scripts/audit_feature_docs.py'), key=version, reverse=True)
    if candidates: return candidates[0]
    raise ValueError('Set FEATURE_DOCS_AUDITOR to the installed feature-docs audit_feature_docs.py; verification never downloads it')

def check_docs(root, log, auditor=None):
    result = subprocess.run([sys.executable, str(find_auditor(auditor)), 'docs/features/', '--json', '--map-refs', 'require'], cwd=root, text=True, capture_output=True)
    log.write(result.stdout + result.stderr)
    if result.returncode: return result.returncode
    unexpected = unexpected_audit_issues(json.loads(result.stdout), root / 'docs/features')
    if unexpected:
        log.write('Unexpected feature-doc issues: ' + json.dumps(unexpected) + '\n'); return 1
    return 0

def step_environment(step):
    if step.startswith('race-'):
        return {'TZ': 'Pacific/Honolulu'}
    if step.startswith('compat-'):
        return COMPAT_ENV
    return None

def selected_missing(selector, listed):
    """The names in an anchored -run selector that match no listed test.
    A name with a pattern (TestCompat[A-Za-z]+) must match at least one."""
    inner = selector.removeprefix('^(').removesuffix(')$')
    return sorted(name for name in inner.split('|') if not any(re.fullmatch(name, test) for test in listed))

def execute(step, root, log, auditor=None):
    if step == 'format': return check_format(root, log)
    if step == 'go-version': return check_go_version(root, log)
    if step == 'proto': return check_proto(root, log)
    if step == 'docs': return check_docs(root, log, auditor)
    argv, directory = COMMANDS[step]
    environment = step_environment(step)
    if step.startswith(SELECTED):
        listing = [('-list' if value == '-run' else value) for value in argv]
        code = command(listing, root / directory, log, environment)
        if code:
            return code
        log.flush()
        listed = Path(log.name).read_text().splitlines()
        missing = selected_missing(argv[-1], listed)
        if missing:
            log.write('Selected tests are missing: ' + ', '.join(missing) + '\n')
            return 1
    return command(argv, root / directory, log, environment)

def tool_metadata(root):
    tools = {}
    for name, argv in {'go':['go','version'], 'node':['node','--version'], 'npm':['npm','--version'], 'buf':['buf','--version'], 'python':[sys.executable,'--version']}.items():
        try:
            result = subprocess.run(argv, cwd=root, text=True, capture_output=True, timeout=10, env={**os.environ, 'GOTOOLCHAIN':'local'})
            tools[name] = (result.stdout + result.stderr).strip()
        except (OSError, subprocess.TimeoutExpired): tools[name] = 'unavailable'
    try:
        revision = subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True).strip()
        dirty = bool(subprocess.check_output(['git','status','--porcelain'],cwd=root))
    except subprocess.CalledProcessError: revision, dirty = 'unknown', True
    return {'tools':tools,'revision':revision,'dirty':dirty,'module_hashes':{name:hashlib.sha256((root/name).read_bytes()).hexdigest() for name in ['go.mod','go.sum']},'go_proxy_off':os.environ.get('GOPROXY')=='off'}

def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('profile', choices=['fast', 'full', 'release', 'web', 'proto', 'race', 'compat'])
    parser.add_argument('--only', help='Explicit comma-separated stage subset; never reported as a complete profile')
    parser.add_argument('--dry-run', action='store_true')
    parser.add_argument('--log-dir', type=Path)
    parser.add_argument('--auditor', help='Path to the required installed feature-docs auditor')
    args = parser.parse_args(argv)
    steps = plan(args.profile)
    if args.only:
        wanted = args.only.split(',')
        if any(step not in steps for step in wanted): parser.error('Unknown stage; available: ' + ', '.join(steps))
        steps = [step for step in steps if step in wanted]
    print(f'Scope: {args.profile}' + (f' subset ({", ".join(steps)}); NOT a complete profile' if args.only else ' complete profile'), flush=True)
    if args.dry_run:
        for step in steps: print(step + ': ' + ' '.join(COMMANDS.get(step, ([f'read-only {step} check'], '.'))[0]))
        return 0
    directory = args.log_dir or Path(tempfile.mkdtemp(prefix='natslens-verify-'))
    directory.mkdir(parents=True, exist_ok=True)
    print('Artifacts: ' + str(directory), flush=True)
    metadata = {'profile': args.profile, 'complete_profile': not bool(args.only), 'planned': steps, 'started': datetime.now(timezone.utc).isoformat(), 'results': [], **tool_metadata(ROOT)}
    def run(step):
        print('START ' + step, flush=True); started = time.monotonic()
        with (directory / (step + '.log')).open('w') as log:
            try: code = execute(step, ROOT, log, args.auditor)
            except KeyboardInterrupt: code = 130; log.write('Interrupted\n')
            except Exception as error: code = 1; log.write(type(error).__name__ + ': ' + str(error) + '\n')
        metadata['results'].append({'name': step, 'code': code, 'seconds': time.monotonic()-started})
        (directory / 'result.json').write_text(json.dumps(metadata, indent=2) + '\n')
        print(('PASS ' if code == 0 else 'FAIL ') + step + f' ({time.monotonic()-started:.2f}s)', flush=True)
        return code
    results = run_steps(steps, run)
    metadata['finished'] = datetime.now(timezone.utc).isoformat()
    metadata['success'] = len(results) == len(steps) and all(row['code'] == 0 for row in results)
    (directory / 'result.json').write_text(json.dumps(metadata, indent=2) + '\n')
    return 0 if metadata['success'] else (results[-1]['code'] or 1)

def interrupt(_signum, _frame):
    raise KeyboardInterrupt()

if __name__ == '__main__':
    signal.signal(signal.SIGTERM, interrupt)
    sys.exit(main())
