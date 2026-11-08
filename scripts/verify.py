"""Initial verification contracts; runners are implemented after runtime RED."""

def format_failed(code, output):
    return code != 0

def unexpected_audit_issues(report, docs):
    return []

def generated_drift(actual, expected):
    return []

def run_steps(steps, execute):
    results = []
    for step in steps:
        results.append({'name': step, 'code': execute(step)})
    return results
