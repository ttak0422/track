#!/usr/bin/env python3
"""Bounded Dependabot npm hash repair. Never imports or executes PR code."""
import base64
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import urllib.parse
import urllib.request

REPO = 'ttak0422/track'
ALLOWED = {'web/package.json', 'web/package-lock.json', 'flake.nix'}
HASH = re.compile(r'npmDepsHash = "(sha256-[A-Za-z0-9+/]{43}=)";')
VERSION = re.compile(r'([~^]?)(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)')


class Refused(Exception):
    pass


def require(value, reason):
    if not value:
        raise Refused(reason)


def decode_json(data):
    def unique(pairs):
        result = {}
        for key, value in pairs:
            require(key not in result, 'duplicate JSON key')
            result[key] = value
        return result
    return json.loads(data, object_pairs_hook=unique)


def api(path, method='GET', body=None):
    require(path.startswith('/'), 'invalid API path')
    data = None if body is None else json.dumps(body).encode()
    request = urllib.request.Request(
        'https://api.github.com/repos/' + REPO + path, data=data, method=method,
        headers={'Authorization': 'Bearer ' + os.environ['GH_TOKEN'],
                 'Accept': 'application/vnd.github+json',
                 'Content-Type': 'application/json',
                 'X-GitHub-Api-Version': '2022-11-28'})
    with urllib.request.urlopen(request, timeout=60) as response:
        result = response.read(8_000_001)
    require(len(result) <= 8_000_000, 'API response too large')
    return decode_json(result)


def version_update(old, new):
    """Keep existing range syntax; reject prereleases, downgrades and breaking updates."""
    a, b = VERSION.fullmatch(old), VERSION.fullmatch(new)
    require(a and b and a[1] == b[1], 'unsupported version/range change')
    av, bv = tuple(map(int, a.groups()[1:])), tuple(map(int, b.groups()[1:]))
    require(bv >= av and av[0] == bv[0], 'major update or downgrade')
    require(av[0] != 0 or av[1] == bv[1], 'breaking 0.x minor update')


def manifest_policy(old, new):
    old, new = dict(old), dict(new)
    for section in ('dependencies', 'devDependencies'):
        a, b = old.pop(section, {}), new.pop(section, {})
        require(a.keys() == b.keys(), 'direct dependencies added or removed')
        for name in a:
            if a[name] != b[name]:
                version_update(a[name], b[name])
    require(old == new, 'non-dependency manifest change')


def lock_policy(old, new, manifest):
    require(old.get('lockfileVersion') == new.get('lockfileVersion') == 3,
            'only npm lockfile v3 is supported')
    require(set(old) == set(new), 'lockfile metadata structure changed')
    require({k: v for k, v in old.items() if k != 'packages'} ==
            {k: v for k, v in new.items() if k != 'packages'}, 'lockfile metadata changed')
    packages = new['packages']
    require('' in packages and len(packages) <= 5000, 'invalid package count')
    for section in ('dependencies', 'devDependencies'):
        require(packages[''].get(section, {}) == manifest.get(section, {}),
                'manifest/lock root mismatch')
    manifest_policy(old['packages'][''], packages[''])
    for path, package in packages.items():
        if not path:
            continue
        require(path.startswith('node_modules/') and '..' not in path.split('/'), 'non-npm path')
        require(not package.get('link'), 'linked package')
        url = urllib.parse.urlsplit(package.get('resolved', ''))
        require(url.scheme == 'https' and url.netloc == 'registry.npmjs.org'
                and not url.query and not url.fragment, 'non-registry package URL')
        require(re.fullmatch(r'sha512-[A-Za-z0-9+/]{86}==', package.get('integrity', '')),
                'missing sha512 integrity')
        require(VERSION.fullmatch(package.get('version', '')), 'non-stable package version')
    # A changed install tree needs human review; do not infer safety from the PR title.
    require(old['packages'].keys() == packages.keys(), 'install tree changed; manual review required')
    for path, package in packages.items():
        if path and old['packages'][path] != package:
            require(old['packages'][path]['version'] != package['version'],
                    'package content changed without a version change')
            version_update(old['packages'][path]['version'], package['version'])


def replace_hash(text, value):
    require(re.fullmatch(r'sha256-[A-Za-z0-9+/]{43}=', value), 'invalid npm hash')
    require(len(HASH.findall(text)) == 1, 'expected one npmDepsHash')
    return HASH.sub(lambda _: 'npmDepsHash = "' + value + '";', text)


def validate_metadata(pr, expected_head=None):
    require(pr['state'] == 'open' and not pr['draft'], 'PR is closed or draft')
    require(pr['user']['login'] == 'dependabot[bot]' and pr['user']['type'] == 'Bot', 'not Dependabot')
    require(pr['base']['repo']['full_name'] == pr['head']['repo']['full_name'] == REPO,
            'foreign repository')
    require(pr['base']['ref'] == 'main', 'wrong base branch')
    require(re.fullmatch(r'dependabot/npm_and_yarn/web/[A-Za-z0-9_.-]+', pr['head']['ref']),
            'unexpected bot branch')
    if expected_head:
        require(pr['head']['sha'] == expected_head, 'stale head')


def tree(sha):
    result = api('/git/trees/' + sha + '?recursive=1')
    require(not result.get('truncated'), 'truncated tree')
    return {e['path']: e for e in result['tree'] if e['type'] != 'tree'}


def blob(entry):
    require(entry['type'] == 'blob' and entry['mode'] == '100644', 'non-regular file')
    require(entry.get('size', 0) <= 2_000_000, 'file too large')
    result = api('/git/blobs/' + entry['sha'])
    require(result['encoding'] == 'base64', 'unexpected blob encoding')
    return base64.b64decode(result['content']).decode('utf-8')


def snapshot(number, trusted_base, expected_head=None):
    pr = api('/pulls/' + str(number))
    validate_metadata(pr, expected_head)
    base, head = pr['base']['sha'], pr['head']['sha']
    require(base == trusted_base == api('/git/ref/heads/main')['object']['sha'], 'main moved')
    comparison = api('/compare/' + base + '...' + head)
    require(comparison['status'] == 'ahead' and comparison['merge_base_commit']['sha'] == base,
            'PR needs rebase onto current main')
    a, b = tree(base), tree(head)
    changed = {p for p in a.keys() | b.keys() if a.get(p, {}).get('sha') != b.get(p, {}).get('sha')
               or a.get(p, {}).get('mode') != b.get(p, {}).get('mode')}
    require(changed <= ALLOWED and 'web/package-lock.json' in changed, 'unexpected changed files')
    require(all(p in a and p in b for p in ALLOWED), 'required file missing')
    old_manifest, manifest = (decode_json(blob(t['web/package.json'])) for t in (a, b))
    manifest_policy(old_manifest, manifest)
    lock = blob(b['web/package-lock.json'])
    lock_policy(decode_json(blob(a['web/package-lock.json'])), decode_json(lock), manifest)
    old_flake, flake = (blob(t['flake.nix']) for t in (a, b))
    require(replace_hash(old_flake, 'sha256-' + 'A' * 43 + '=') ==
            replace_hash(flake, 'sha256-' + 'A' * 43 + '='), 'non-hash Nix change')
    return pr, b, flake, lock


def calculate(number, base):
    pr, _, flake, lock = snapshot(number, base)
    # This flake.lock is checked out from the trusted workflow commit, never from the PR.
    nixpkgs = decode_json(Path('flake.lock').read_text())['nodes']['nixpkgs']['locked']
    require(nixpkgs['type'] == 'github' and nixpkgs['owner'].lower() == 'nixos'
            and nixpkgs['repo'] == 'nixpkgs', 'unexpected trusted nixpkgs source')
    require(re.fullmatch(r'[a-f0-9]{40}', nixpkgs['rev']), 'invalid pinned nixpkgs revision')
    with tempfile.TemporaryDirectory() as directory:
        path = Path(directory) / 'package-lock.json'
        path.write_text(lock)
        # Explicit process environment; no credentials or arbitrary workflow environment.
        env = {k: os.environ[k] for k in ('PATH', 'HOME', 'TMPDIR', 'NIX_REMOTE',
               'NIX_SSL_CERT_FILE', 'SSL_CERT_FILE') if k in os.environ}
        value = subprocess.check_output(
            ['nix', 'run', 'github:nixos/nixpkgs/' + nixpkgs['rev'] + '#prefetch-npm-deps',
             '--', str(path)], env=env, text=True, timeout=900).strip()
    updated = replace_hash(flake, value)
    # Output public GitHub metadata, not values read from the workflow environment.
    result = {'number': number, 'base': pr['base']['sha'], 'head': pr['head']['sha'], 'hash': value,
              'lock_sha256': hashlib.sha256(lock.encode()).hexdigest(), 'changed': updated != flake}
    with open(os.environ['GITHUB_OUTPUT'], 'a') as output:
        output.write('result=' + json.dumps(result, separators=(',', ':')) + '\n')


def checks_pass(head, run_id):
    run = api('/actions/runs/' + str(run_id))
    require(run['path'] == '.github/workflows/ci.yml' and run['event'] == 'pull_request'
            and run['head_sha'] == head and run['conclusion'] == 'success'
            and run['head_repository']['full_name'] == REPO, 'exact-head CI has not succeeded')
    jobs = api('/actions/runs/' + str(run_id) + '/jobs?filter=latest&per_page=100')
    require(jobs['total_count'] <= 100, 'too many CI jobs')
    successful = {job['name'] for job in jobs['jobs'] if job['conclusion'] == 'success'}
    require({'test', 'macOS desktop'} <= successful, 'required CI jobs did not pass')
    checks = api('/commits/' + head + '/check-runs?per_page=100&filter=latest')
    require(checks['total_count'] <= 100, 'too many checks')
    require(checks['check_runs'], 'no checks')
    for check in checks['check_runs']:
        require(check['status'] == 'completed' and check['conclusion'] in ('success', 'skipped', 'neutral'),
                'checks are pending or failed')
    status = api('/commits/' + head + '/status?per_page=100')
    require(status['total_count'] <= 100 and
            (status['total_count'] == 0 or status['state'] == 'success'), 'commit statuses not successful')



def require_strict_protection():
    # Effective active rules are readable with normal repository metadata access.
    # Unlike a client-side base-SHA check, strict required checks protect the merge
    # atomically if main advances after the final API read.
    rules = api('/rules/branches/main?per_page=100')
    require(len(rules) < 100, 'too many effective rules')
    for rule in rules:
        parameters = rule.get('parameters', {})
        checks = parameters.get('required_status_checks', [])
        if (rule['type'] != 'required_status_checks'
                or parameters.get('strict_required_status_checks_policy') is not True
                or not any(c.get('context') == 'test' and c.get('integration_id') == 15368 for c in checks)
                or rule.get('ruleset_source_type') != 'Repository'
                or rule.get('ruleset_source') != REPO):
            continue
        ruleset_id = rule['ruleset_id']
        require(type(ruleset_id) is int and ruleset_id > 0, 'invalid ruleset id')
        ruleset = api('/rulesets/' + str(ruleset_id))
        if (ruleset.get('enforcement') == 'active' and ruleset.get('bypass_actors') == []
                and any(r.get('type') == 'required_status_checks' and r.get('parameters') == parameters
                        for r in ruleset.get('rules', []))):
            return
    raise Refused('auto-merge blocked: main needs an active no-bypass ruleset with strict GitHub Actions test checks')


def publish(result, trusted_base, run_id):
    require(result['base'] == trusted_base, 'unexpected calculation base')
    number, head = result['number'], result['head']
    pr, files, flake, lock = snapshot(number, trusted_base, head)
    require(hashlib.sha256(lock.encode()).hexdigest() == result['lock_sha256'], 'lock data changed')
    updated = replace_hash(flake, result['hash'])
    if updated != flake:
        # Contents API uses the old blob SHA and branch as a conditional update. A concurrent
        # unrelated commit could still be preserved, so use an explicit single-parent git commit
        # and a non-force ref update instead: any divergent head causes rejection.
        new_blob = api('/git/blobs', 'POST', {'content': updated, 'encoding': 'utf-8'})
        commit = api('/git/commits/' + head)
        new_tree = api('/git/trees', 'POST', {'base_tree': commit['tree']['sha'], 'tree': [
            {'path': 'flake.nix', 'mode': '100644', 'type': 'blob', 'sha': new_blob['sha']}]})
        new_commit = api('/git/commits', 'POST', {
            'message': 'build(deps): synchronize Nix npm dependency hash',
            'tree': new_tree['sha'], 'parents': [head]})
        # Recheck closure/base/head immediately before the conditional write.
        latest = api('/pulls/' + str(number))
        validate_metadata(latest, head)
        require(api('/git/ref/heads/main')['object']['sha'] == trusted_base, 'main moved')
        branch = urllib.parse.quote(pr['head']['ref'], safe='/')
        api('/git/refs/heads/' + branch, 'PATCH', {'sha': new_commit['sha'], 'force': False})
        print('Hash updated. Approve the resulting CI runs in GitHub if requested; no automatic rerun loop.')
        return
    if os.environ.get('AUTO_MERGE') != 'true':
        print('Auto-merge disabled by workflow configuration.')
        return
    if not run_id:
        print('Hash is current. Manual runs never merge; wait for successful exact-head CI.')
        return
    checks_pass(head, run_id)
    require_strict_protection()
    latest = api('/pulls/' + str(number))
    validate_metadata(latest, head)
    require(latest['base']['sha'] == trusted_base and latest.get('mergeable_state') == 'clean',
            'branch protection, review, or base update blocks merge')
    require(api('/git/ref/heads/main')['object']['sha'] == trusted_base, 'main moved')
    # GitHub enforces the validated strict ruleset at merge time. Never bypass it.
    response = api('/pulls/' + str(number) + '/merge', 'PUT', {'sha': head, 'merge_method': 'squash'})
    require(response.get('merged'), 'GitHub declined merge')
    print('Merged validated Dependabot update at ' + head)


def main():
    require(os.environ['GITHUB_REPOSITORY'] == REPO, 'unexpected repository')
    base = os.environ['TRUSTED_BASE']
    require(re.fullmatch(r'[a-f0-9]{40}', base), 'invalid trusted base')
    if os.environ['MODE'] == 'calculate':
        number = int(os.environ['PR_NUMBER'])
        require(number > 0, 'invalid PR number')
        calculate(number, base)
    else:
        publish(decode_json(os.environ['RESULT']), base, os.environ.get('CI_RUN_ID', ''))


if __name__ == '__main__':
    try:
        main()
    except Refused as error:
        # Fail closed but distinguish a normal policy skip in the job log.
        print('Dependency automation refused: ' + str(error))
        raise SystemExit(1)
