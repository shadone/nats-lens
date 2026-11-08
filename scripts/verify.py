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
}

def plan(profile):
    common = ['format', 'gate-tests', 'build-tests', 'types', 'proto', 'docs']
    return {
        'fast': common + ['vet-fast', 'go-fast', 'web-tests'],
        'full': common + ['vet-full', 'web-tests', 'go-full', 'race-api', 'race-k8s'],
        'web': ['types', 'web-tests'], 'proto': ['proto'], 'race': ['race-api', 'race-k8s'],
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

def template_outputs(text):
    outputs = [line.strip().split(':', 1)[1].split('#', 1)[0].strip().strip('"\'') for line in text.splitlines() if re.match(r'^\s+out:', line)]
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

def execute(step, root, log, auditor=None):
    if step == 'format': return check_format(root, log)
    if step == 'proto': return check_proto(root, log)
    if step == 'docs': return check_docs(root, log, auditor)
    argv, directory = COMMANDS[step]
    environment = {'TZ': 'Pacific/Honolulu'} if step.startswith('race-') else None
    if step.startswith('race-'):
        listing = [('-list' if value == '-run' else value) for value in argv]
        code = command(listing, root / directory, log, environment)
        if code:
            return code
        log.flush()
        listed = set(Path(log.name).read_text().splitlines())
        missing = set(re.findall(r'Test[A-Za-z0-9_]+', argv[-1])) - listed
        if missing:
            log.write('Selected race tests are missing: ' + ', '.join(sorted(missing)) + '\n')
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
    parser.add_argument('profile', choices=['fast', 'full', 'web', 'proto', 'race'])
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
