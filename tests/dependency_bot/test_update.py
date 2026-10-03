import importlib.util
import os
from pathlib import Path
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('update', Path(__file__).parents[2] / 'scripts/dependency_bot/update.py')
u = importlib.util.module_from_spec(spec)
spec.loader.exec_module(u)
H = 'sha256-' + 'A' * 43 + '='
H2 = 'sha256-' + 'B' * 43 + '='


def metadata():
    return {'state': 'open', 'draft': False, 'user': {'login': 'dependabot[bot]', 'type': 'Bot'},
            'base': {'ref': 'main', 'sha': 'base', 'repo': {'full_name': u.REPO}},
            'head': {'ref': 'dependabot/npm_and_yarn/web/pkg-1.2.4', 'sha': 'head', 'repo': {'full_name': u.REPO}},
            'mergeable_state': 'clean'}


def lock():
    return {'name': 'example', 'lockfileVersion': 3, 'requires': True, 'packages': {
        '': {'name': 'example', 'dependencies': {'pkg': '^1.2.3'}},
        'node_modules/pkg': {'version': '1.2.3', 'resolved': 'https://registry.npmjs.org/pkg/-/pkg-1.2.3.tgz',
                             'integrity': 'sha512-' + 'A' * 86 + '=='}}}


class PolicyTests(unittest.TestCase):
    def test_ranges(self):
        for a, b in [('^1.2.3', '^1.3.0'), ('~1.2.3', '~1.2.4'), ('0.2.3', '0.2.4')]:
            u.version_update(a, b)
        for a, b in [('^1.2.3', '1.2.4'), ('1.2.3', '2.0.0'), ('0.2.3', '0.3.0'),
                     ('1.2.3', '1.2.2'), ('1.2.3', '1.3.0-beta'), ('1.2.3', 'https://bad')]:
            with self.subTest(a=a, b=b), self.assertRaises(u.Refused):
                u.version_update(a, b)

    def test_manifest(self):
        old = {'dependencies': {'pkg': '^1.2.3'}, 'scripts': {'test': 'vitest'}}
        for extra in [{'scripts': {'test': 'evil'}}, {'overrides': {'x': '1.0.0'}},
                      {'dependencies': {'pkg': '^1.2.3', 'new': '1.0.0'}}]:
            with self.subTest(extra=extra), self.assertRaises(u.Refused):
                u.manifest_policy(old, old | extra)

    def test_lock_patch(self):
        old, new = lock(), lock()
        new['packages']['node_modules/pkg']['version'] = '1.2.4'
        u.lock_policy(old, new, old['packages'][''])

    def test_lock_guards(self):
        for change in [{'resolved': 'http://registry.npmjs.org/a'}, {'resolved': 'https://evil.example/a'},
                       {'resolved': 'https://registry.npmjs.org@evil.example/a'}, {'link': True},
                       {'integrity': ''}, {'version': '2.0.0'}]:
            old, new = lock(), lock()
            new['packages']['node_modules/pkg'].update(change)
            with self.subTest(change=change), self.assertRaises(u.Refused):
                u.lock_policy(old, new, old['packages'][''])
        old, new = lock(), lock()
        del new['packages']['node_modules/pkg']
        with self.assertRaises(u.Refused):
            u.lock_policy(old, new, old['packages'][''])

    def test_same_version_artifact_change_refused(self):
        old, new = lock(), lock()
        new['packages']['node_modules/pkg']['integrity'] = 'sha512-' + 'B' * 86 + '=='
        with self.assertRaises(u.Refused):
            u.lock_policy(old, new, old['packages'][''])

    def test_metadata(self):
        u.validate_metadata(metadata(), 'head')
        for path, value in [(('user', 'login'), 'someone'), (('user', 'type'), 'User'),
                            (('head', 'ref'), 'feature/test'), (('base', 'ref'), 'other'),
                            (('head', 'sha'), 'new')]:
            pr = metadata()
            pr[path[0]][path[1]] = value
            with self.subTest(path=path), self.assertRaises(u.Refused):
                u.validate_metadata(pr, 'head')
        pr = metadata()
        pr['head']['repo']['full_name'] = 'foreign/repo'
        with self.assertRaises(u.Refused):
            u.validate_metadata(pr)

    def test_hash(self):
        source = 'before\nnpmDepsHash = "' + H + '";\nafter'
        self.assertEqual(u.replace_hash(source, H2), source.replace(H, H2))
        for text in [source + source, 'no hash']:
            with self.assertRaises(u.Refused):
                u.replace_hash(text, H2)
        with self.assertRaises(u.Refused):
            u.replace_hash(source, 'injected\noutput=value')

    def test_duplicate_json(self):
        with self.assertRaises(u.Refused):
            u.decode_json('{"scripts":{},"scripts":{"test":"evil"}}')


class PublishTests(unittest.TestCase):
    def setUp(self):
        self.flake = 'npmDepsHash = "' + H + '";'
        self.result = {'number': 1, 'base': 'base', 'head': 'head', 'hash': H,
                       'lock_sha256': u.hashlib.sha256(b'lock').hexdigest()}
        self.snapshot = patch.object(u, 'snapshot', return_value=(metadata(), {}, self.flake, 'lock'))
        self.snapshot.start()
        self.addCleanup(self.snapshot.stop)

    def test_idempotent_disabled_and_manual_never_write(self):
        with patch.object(u, 'api') as api, patch.dict(os.environ, {'AUTO_MERGE': 'false'}):
            u.publish(self.result, 'base', '123')
            api.assert_not_called()
        with patch.object(u, 'api') as api, patch.dict(os.environ, {'AUTO_MERGE': 'true'}):
            u.publish(self.result, 'base', '')
            api.assert_not_called()

    def test_one_parent_non_force_hash_write(self):
        self.result['hash'] = H2
        responses = [{'sha': 'blob'}, {'tree': {'sha': 'oldtree'}}, {'sha': 'tree'}, {'sha': 'new'},
                     metadata(), {'object': {'sha': 'base'}}, {}]
        with patch.object(u, 'api', side_effect=responses) as api:
            u.publish(self.result, 'base', '123')
        calls = api.call_args_list
        self.assertEqual(calls[2].args[2]['tree'], [{'path': 'flake.nix', 'mode': '100644', 'type': 'blob', 'sha': 'blob'}])
        self.assertEqual(calls[3].args[2]['parents'], ['head'])
        self.assertEqual(calls[-1].args[2], {'sha': 'new', 'force': False})
        self.assertFalse(any('/merge' in c.args[0] for c in calls))

    def test_stale_head_cannot_publish(self):
        self.result['hash'] = H2
        stale = metadata()
        stale['head']['sha'] = 'advanced'
        responses = [{'sha': 'blob'}, {'tree': {'sha': 'oldtree'}}, {'sha': 'tree'}, {'sha': 'new'}, stale]
        with patch.object(u, 'api', side_effect=responses) as api, self.assertRaises(u.Refused):
            u.publish(self.result, 'base', '123')
        self.assertFalse(any(c.args[1:2] == ('PATCH',) for c in api.call_args_list))

    def test_failed_checks_prevent_merge(self):
        with patch.dict(os.environ, {'AUTO_MERGE': 'true'}), patch.object(u, 'api') as api, \
                patch.object(u, 'checks_pass', side_effect=u.Refused('failed')), self.assertRaises(u.Refused):
            u.publish(self.result, 'base', '123')
        api.assert_not_called()

    def test_merge_bound_to_head(self):
        with patch.dict(os.environ, {'AUTO_MERGE': 'true'}), patch.object(u, 'checks_pass') as checks, \
                patch.object(u, 'api', side_effect=[metadata(), {'merged': True}]) as api:
            u.publish(self.result, 'base', '123')
        checks.assert_called_once_with('head', '123')
        self.assertEqual(api.call_args.args, ('/pulls/1/merge', 'PUT', {'sha': 'head', 'merge_method': 'squash'}))


class SnapshotTests(unittest.TestCase):
    def snapshot(self, changed=None, ahead=True, regular=True, flake=None):
        manifest = lock()['packages']['']
        old_lock = lock()
        new_lock = lock()
        new_lock['packages']['node_modules/pkg']['version'] = '1.2.4'
        contents = {
            'manifest': u.json.dumps(manifest), 'oldlock': u.json.dumps(old_lock),
            'newlock': u.json.dumps(new_lock), 'flake': 'npmDepsHash = "' + H + '";',
        }
        def entries(which):
            result = {p: {'path': p, 'type': 'blob', 'mode': '100644', 'sha': sha} for p, sha in [
                ('web/package.json', 'manifest'), ('web/package-lock.json', which + 'lock'), ('flake.nix', 'flake')]}
            if which == 'new':
                if changed:
                    result[changed] = {'path': changed, 'type': 'blob', 'mode': '100644', 'sha': 'evil'}
                if not regular:
                    result['web/package-lock.json']['mode'] = '120000'
                if flake:
                    contents['newflake'] = flake
                    result['flake.nix']['sha'] = 'newflake'
            return result
        a, b = entries('old'), entries('new')
        def api(path, *args):
            if path == '/pulls/1': return metadata()
            if path == '/git/ref/heads/main': return {'object': {'sha': 'base'}}
            if path.startswith('/compare/'): return {'status': 'ahead' if ahead else 'diverged', 'merge_base_commit': {'sha': 'base'}}
            if path.startswith('/git/blobs/'):
                return {'encoding': 'base64', 'content': u.base64.b64encode(contents[path.rsplit('/',1)[1]].encode()).decode()}
            raise AssertionError(path)
        with patch.object(u, 'api', side_effect=api), patch.object(u, 'tree', side_effect=[a, b]):
            return u.snapshot(1, 'base', 'head')

    def test_valid_snapshot(self):
        self.snapshot()

    def test_whole_tree_allowlist_and_nonregular_files(self):
        for path in ['.github/workflows/ci.yml', 'scripts/dependency_bot/update.py', 'README.md', 'flake.lock']:
            with self.subTest(path=path), self.assertRaises(u.Refused): self.snapshot(changed=path)
        with self.assertRaises(u.Refused): self.snapshot(regular=False)
        with self.assertRaises(u.Refused): self.snapshot(ahead=False)
        with self.assertRaises(u.Refused): self.snapshot(flake='npmDepsHash = "' + H + '"; evil = true;')


class CheckTests(unittest.TestCase):
    def data(self):
        return [{'path': '.github/workflows/ci.yml', 'event': 'pull_request', 'head_sha': 'head',
                 'conclusion': 'success', 'head_repository': {'full_name': u.REPO}},
                {'total_count': 2, 'jobs': [{'name': 'test', 'conclusion': 'success'},
                                            {'name': 'macOS desktop', 'conclusion': 'success'}]},
                {'total_count': 1, 'check_runs': [{'status': 'completed', 'conclusion': 'success'}]},
                {'total_count': 0, 'state': 'pending'}]

    def test_green(self):
        with patch.object(u, 'api', side_effect=self.data()): u.checks_pass('head', '123')

    def test_pending_failed_wrong_head_and_truncation(self):
        cases = [(0, 'head_sha', 'stale'), (0, 'conclusion', 'failure'),
                 (0, 'event', 'workflow_dispatch'), (0, 'path', '.github/workflows/other.yml'),
                 (1, 'total_count', 101), (2, 'total_count', 101), (3, 'total_count', 1), (1, 'jobs', [])]
        for index, key, value in cases:
            data = self.data(); data[index][key] = value
            with self.subTest(case=(index, key, value)), patch.object(u, 'api', side_effect=data), self.assertRaises(u.Refused):
                u.checks_pass('head', '123')
        for state, conclusion in [('in_progress', None), ('completed', 'failure')]:
            data = self.data(); data[2]['check_runs'][0].update(status=state, conclusion=conclusion)
            with patch.object(u, 'api', side_effect=data), self.assertRaises(u.Refused): u.checks_pass('head', '123')


class CalculationTests(unittest.TestCase):
    def test_prefetch_environment_and_public_output(self):
        import tempfile
        with tempfile.TemporaryDirectory() as directory:
            output = str(Path(directory) / 'output')
            with patch.dict(os.environ, {'GITHUB_OUTPUT': output, 'GH_TOKEN': 'secret',
                                         'UNRELATED_SECRET': 'secret'}), \
                    patch.object(u, 'snapshot', return_value=(metadata(), {}, 'npmDepsHash = "' + H + '";', '{}')), \
                    patch.object(u.subprocess, 'check_output', return_value=H + '\n') as command:
                u.calculate(1, 'base')
            self.assertNotIn('GH_TOKEN', command.call_args.kwargs['env'])
            self.assertNotIn('UNRELATED_SECRET', command.call_args.kwargs['env'])
            result = u.json.loads(Path(output).read_text().removeprefix('result='))
            self.assertEqual(result['base'], 'base')
            self.assertEqual(result['hash'], H)
            self.assertFalse(result['changed'])


if __name__ == '__main__':
    unittest.main()
