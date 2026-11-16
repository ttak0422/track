#!/usr/bin/env python3
"""Synthetic engine admission matrix; DOES NOT enable ordinary native admission."""
import argparse
import json
import os
from pathlib import Path
import shutil
import signal
import tempfile

from process import Fixture, Server, inventory, unchanged, wait_for


def close_owned(s):
    try:
        if s.p.poll() is None:
            s.close()
    finally:
        s.log.close()
        def group_exited():
            try:
                os.killpg(s.p.pid, 0)
                return False
            except ProcessLookupError:
                return True
        try:
            wait_for(group_exited, 'owned process group exit')
        except AssertionError:
            os.killpg(s.p.pid, signal.SIGKILL)
            raise


def shaped_fixture(base):
    f = Fixture(base)
    f.config.write_text(f'default_vault: main\ncache_dir: {base / "ordinary"}\nvaults:\n  main: {base / "active"}\n  work: {base / "peer"}\n')
    for name in ('active', 'peer'):
        (base / name / '.track/config.yml').write_text(
            'link_format: id-v1\nextensions: [.md]\n'
            'properties:\n  priority:\n    type: string\n    values: [low, high]\n'
            'queries:\n  all_notes: "TABLE title"\n')
    f.env.pop('TRACK_VAULT')
    f.env['TMPDIR'] = str(f.cache)
    f.paths.append(base / 'home')
    return f


def note_ready(s, vault, expected_body, expected_path, active):
    code, body = s.request('/api/note?id=100&vault=' + vault)
    if code != 200:
        return False
    note = json.loads(body).get('note', {})
    return note.get('vault') == ('' if vault == active else vault) and note.get('path') == str(expected_path / 'note/100.md') and note.get('note_id') == 100 and expected_body in note.get('body', '')


def healthy(binary, base, mode):
    f = shaped_fixture(base)
    active = 'main'
    peers = ['main', 'work']
    if mode == 'home':
        for name in ('active', 'peer', 'ordinary'):
            (base / name).rename(base / 'home' / name)
        f.paths = [base / 'home/active', base / 'home/peer', f.config, base / 'home/ordinary', base / 'home']
        f.config.write_text('default_vault: main\ncache_dir: ~/normal\nvaults:\n  main: ~/active\n  work: ~/peer\n')
    if mode == 'home':
        (base / 'home/ordinary').rename(base / 'home/normal')
        f.paths[3] = base / 'home/normal'
    if mode == 'xdg':
        f.config.write_text(f'default_vault: main\nvaults:\n  main: {base / "active"}\n  work: {base / "peer"}\n')
        (base / 'xdg-cache').mkdir(mode=0o700)
        f.env['XDG_CACHE_HOME'] = str(base / 'xdg-cache')
        f.paths.append(base / 'xdg-cache')
    if mode == 'environment':
        shutil.copytree(base / 'peer', base / 'third')
        (base / 'third/note/100.md').write_text('third sentinel\n')
        (base / 'environment-cache').mkdir(mode=0o700)
        f.paths += [base / 'third', base / 'environment-cache']
        f.env.update(TRACK_VAULT=str(base / 'peer'), TRACK_VAULTS_THIRD=str(base / 'third'),
                     TRACK_CACHE_DIR=str(base / 'environment-cache'))
        active = 'work'
        peers.append('third')
    expected_paths = {'main': base / 'active', 'work': base / 'peer'}
    if mode == 'home':
        expected_paths = {'main': base / 'home/active', 'work': base / 'home/peer'}
    if mode == 'environment':
        expected_paths['third'] = base / 'third'
    bodies = {'main': 'active sentinel', 'work': 'peer sentinel', 'third': 'third sentinel'}
    before = inventory(f.paths)
    s = Server(binary, f)
    try:
        try:
            wait_for(s.ready, 'real-shaped configured startup')
        except AssertionError as e:
            raise AssertionError((mode, s.request('/api/storage'), s.logs())) from e
        code, body = s.request('/api/vaults')
        registry = json.loads(body)
        assert code == 200 and registry['active']['name'] == active, registry
        observed_paths = {item['name']: item['path'] for item in registry['vaults'] + registry.get('unavailable', []) }
        for name, path in expected_paths.items():
            assert observed_paths.get(name) == str(path), registry
        for name in peers:
            wait_for(lambda: note_ready(s, name, bodies[name], expected_paths[name], active), name + ' verified read')
        unchanged(before, f.paths)
        generation = json.loads(s.request('/api/storage')[1])['generation']
        attempts = 0
        def retry_started():
            nonlocal attempts
            attempts += 1
            assert s.request('/api/storage', 'POST', {})[0] == 200
            unchanged(before, f.paths)
            return json.loads(s.request('/api/storage')[1])['generation'] > generation
        wait_for(retry_started, 'retry accepted after in-flight startup completion')
        wait_for(s.ready, 'real-shaped retry')
        new_generation = json.loads(s.request('/api/storage')[1])['generation']
        assert new_generation > generation
        print(f'{mode}: retry attempts={attempts}, generation={generation}->{new_generation}')
        for name in peers:
            wait_for(lambda: note_ready(s, name, bodies[name], expected_paths[name], active), name + ' read after retry')
        unchanged(before, f.paths)
        assert len(list(f.cache.rglob('*.db'))) >= len(peers), 'missing isolated peer DB'
    finally:
        close_owned(s)
    unchanged(before, f.paths)
    assert not list(f.cache.rglob('*.pid')), 'private PID leaked'


def rejected(binary, base, kind):
    f = shaped_fixture(base)
    # All intentional adversarial fixture setup precedes the immutable snapshot.
    if kind == 'missing-peer':
        f.env['TRACK_VAULTS_MISSING'] = str(base / 'missing')
    elif kind == 'namespace-peer':
        namespace = f.cache / 'canonical-read-only-v1'
        (namespace / '.track').mkdir(parents=True)
        f.env['TRACK_VAULTS_INSIDE'] = str(namespace)
        f.paths.append(namespace)
    elif kind == 'namespace-in-vault':
        f.cache = base / 'active'
    elif kind == 'namespace-config':
        namespace = f.cache / 'canonical-read-only-v1'
        namespace.mkdir()
        old = f.config
        f.config = namespace / 'machine.yml'
        shutil.copyfile(old, f.config)
        f.env['TRACK_CONFIG'] = str(f.config)
        f.paths.append(f.config)
    else:
        namespace = f.cache / 'canonical-read-only-v1'
        namespace.mkdir()
        alias = namespace / 'untrusted-entry'
        target = base / 'peer/.track/notes/100.yaml'
        if kind == 'hardlink':
            os.link(target, alias)
        elif kind == 'symlink':
            alias.symlink_to(target)
        else:
            raise AssertionError(kind)
    before = inventory(f.paths)
    s = Server(binary, f)
    try:
        diagnostic = ''
        def failed_closed():
            nonlocal diagnostic
            if s.p.poll() is not None:
                assert s.p.returncode != 0, s.logs()
                diagnostic = s.logs()
                return True
            try:
                code, body = s.request('/api/storage')
                state = json.loads(body)
                diagnostic = state.get('reason', '')
                return code == 200 and state['phase'] == 'unavailable'
            except (OSError, ValueError):
                return False
        wait_for(failed_closed, 'reject ' + kind)
        expected_reason = {'missing-peer': str(base / 'missing'), 'namespace-peer': 'overlaps vault',
                           'namespace-in-vault': 'overlaps vault', 'namespace-config': 'overlaps configuration',
                           'hardlink': 'exactly one link', 'symlink': 'must not be a symlink'}[kind]
        assert expected_reason in diagnostic, (kind, diagnostic)
        if s.p.poll() is None:
            capability = json.loads(s.request('/api/capabilities')[1])
            assert capability['canonical_read_only'] is True
        unchanged(before, f.paths)
        if kind == 'missing-peer':
            assert not (base / 'missing').exists(), 'unavailable peer was created'
        assert not list(f.cache.rglob('*.db')), kind + ' opened cache before validation'
        assert not list(f.cache.rglob('*.pid')), kind + ' acquired lease before validation'
    finally:
        close_owned(s)
    unchanged(before, f.paths)
    if kind == 'missing-peer':
        assert not (base / 'missing').exists(), 'unavailable peer created during shutdown'


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', required=True, type=Path)
    binary = str(parser.parse_args().binary.resolve(strict=True))
    for mode in ('named-default', 'environment', 'home', 'xdg'):
        with tempfile.TemporaryDirectory(prefix='track-admission-shape-') as tmp:
            healthy(binary, Path(tmp).resolve(), mode)
    for kind in ('missing-peer', 'namespace-peer', 'namespace-in-vault', 'namespace-config', 'hardlink', 'symlink'):
        with tempfile.TemporaryDirectory(prefix='track-admission-invalid-') as tmp:
            rejected(binary, Path(tmp).resolve(), kind)
    print('PASS: synthetic canonical engine admission matrix (native gate unchanged)')


if __name__ == '__main__':
    main()
