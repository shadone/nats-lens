import tempfile
import unittest
from pathlib import Path
import verify

class VerificationContracts(unittest.TestCase):
    def test_gofmt_output_is_failure_even_with_zero_exit(self):
        self.assertTrue(verify.format_failed(0, 'internal/example.go\n'))
        self.assertFalse(verify.format_failed(0, ''))
        self.assertTrue(verify.format_failed(2, ''))

    def test_only_explicit_planned_status_warnings_are_expected(self):
        with tempfile.TemporaryDirectory() as name:
            docs = Path(name)
            (docs / 'planned.md').write_text('- **Status:** planned\n')
            (docs / 'broken.md').write_text('- **Status:** typo\n')
            def issue(kind, location, severity='warning'):
                return dict(kind=kind, location=location, severity=severity, message='synthetic')
            expected = issue('bad-status', 'planned.md')
            bad = [issue('bad-status', 'broken.md'), issue('missing-doc', 'planned.md'), issue('bad-status', 'planned.md', 'error')]
            self.assertEqual(verify.unexpected_audit_issues({'issues': [expected]}, docs), [])
            self.assertEqual(verify.unexpected_audit_issues({'issues': bad}, docs), bad)

    def test_generated_comparison_detects_content_missing_and_extra_without_writes(self):
        with tempfile.TemporaryDirectory() as name:
            actual, expected = Path(name)/'actual', Path(name)/'expected'
            actual.mkdir(); expected.mkdir()
            (actual/'changed.ts').write_text('user edit')
            (expected/'changed.ts').write_text('generated')
            (actual/'stale.ts').write_text('unrelated local file')
            (expected/'new.ts').write_text('new')
            self.assertEqual(verify.generated_drift(actual, expected), ['changed.ts', 'new.ts', 'stale.ts'])
            self.assertEqual((actual/'changed.ts').read_text(), 'user edit')
            self.assertEqual((actual/'stale.ts').read_text(), 'unrelated local file')

    def test_failure_stops_later_checks_and_preserves_exit_status(self):
        called = []
        def execute(step):
            called.append(step)
            return 7 if step == 'broken' else 0
        results = verify.run_steps(['first', 'broken', 'must-not-run'], execute)
        self.assertEqual(called, ['first', 'broken'])
        self.assertEqual(results[-1]['code'], 7)


class TierScopeContracts(unittest.TestCase):
    def test_fast_and_full_keep_expensive_work_explicit(self):
        fast, full = verify.plan('fast'), verify.plan('full')
        for name in ['format', 'types', 'proto', 'docs', 'web-tests']:
            self.assertIn(name, fast)
            self.assertIn(name, full)
        self.assertIn('go-fast', fast)
        self.assertNotIn('go-full', fast)
        self.assertNotIn('race-api', fast)
        self.assertIn('go-full', full)
        self.assertIn('race-api', full)
        self.assertIn('race-k8s', full)
        self.assertNotIn('go-fast', full)
        self.assertIn('-count=1', verify.COMMANDS['go-full'][0])
        self.assertNotIn('govulncheck', repr(verify.COMMANDS))

    def test_unsafe_generation_destinations_are_rejected(self):
        valid = 'plugins:\n  - out: internal/gen\n  - out: internal/gen\n  - out: web/src/gen\n'
        verify.template_outputs(valid)
        for bad in ['/tmp/user-work', '../user-work', 'other/gen']:
            with self.assertRaises(ValueError):
                verify.template_outputs(valid.replace('web/src/gen', bad))

    def test_planned_aliases_and_duplicate_headers_are_not_silently_ignored(self):
        with tempfile.TemporaryDirectory() as name:
            docs = Path(name)
            (docs/'ambiguous.md').write_text('- **Status:** planned\n- **Status:** bad\n')
            issue = dict(kind='bad-status',location='ambiguous.md',severity='warning',message='synthetic')
            self.assertEqual(verify.unexpected_audit_issues({'issues':[issue]},docs),[issue])
            with self.assertRaises(ValueError): verify.unexpected_audit_issues({},docs)

if __name__ == '__main__':
    unittest.main()
