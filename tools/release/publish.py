"""Publish locally verified platform archives to the Mimic repository."""
from __future__ import annotations

import argparse
import json
from pathlib import Path
import re
import subprocess
import tempfile

from prepare import ROOT, clean_revision, digest, run

REPO = 'mimic-browser/runtime'
CHECKS = {'runtimecheck:v8', 'runtimecheck:quickjs', 'runtimecheck:goja',
          'examples:puppeteer', 'examples:playwright', 'examples:concurrency'}


def verified_archive(output, version, host, source_revision):
    receipt = json.loads((output / f'{host}-amd64.receipt.json').read_text())
    if (receipt['version'] != version or receipt['platform'] != f'{host}-amd64'
            or receipt.get('packagingRevision', receipt['sourceRevision']) != source_revision
            or receipt['verified'] is not True or set(receipt['checks']) != CHECKS):
        raise RuntimeError(f'Stale or unverified {host} receipt')
    filename = receipt['archive']
    suffix = '.zip' if host == 'windows' else '.tar.gz'
    if filename != f'mimic-{version}-{host}-amd64{suffix}':
        raise RuntimeError('Unexpected archive name')
    archive = output / filename
    if digest(archive) != receipt['sha256'] or archive.stat().st_size != receipt['size']:
        raise RuntimeError(f'Archive changed: {filename}')
    return archive, receipt


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--version', required=True)
    args = parser.parse_args()
    if not re.fullmatch(r'v\d+\.\d+\.\d+(?:-beta\.\d+)?', args.version):
        parser.error('Expected vMAJOR.MINOR.PATCH or vMAJOR.MINOR.PATCH-beta.NUMBER')
    is_prerelease = '-beta.' in args.version
    source_revision = clean_revision(ROOT)
    remote_revision = run('gh', 'api', f'repos/{REPO}/commits/main', '--jq', '.sha', capture=True).strip()
    if source_revision != remote_revision:
        raise RuntimeError('Push the release commit before publishing')
    output = ROOT / '.build/releases' / args.version
    assets, public_assets = [], []
    for host in ('windows', 'linux'):
        archive, receipt = verified_archive(output, args.version, host, source_revision)
        assets.append(archive)
        public_assets.append({key: receipt[key] for key in
                              ('platform', 'archive', 'sha256', 'size', 'binarySha256', 'checks')})
    receipts = [json.loads((output / f'{host}-amd64.receipt.json').read_text())
                for host in ('windows', 'linux')]
    binary_revisions = {receipt['sourceRevision'] for receipt in receipts}
    ci_runs = {receipt.get('ciRun') for receipt in receipts}
    reused = {receipt.get('binariesReusedUnchanged', False) for receipt in receipts}
    if len(binary_revisions) != 1 or len(ci_runs) != 1 or len(reused) != 1:
        raise RuntimeError('Platform receipts disagree on binary provenance')
    binary_revision = binary_revisions.pop()
    ci_run = ci_runs.pop()
    binaries_reused = reused.pop()
    if binaries_reused:
        if not ci_run:
            raise RuntimeError('Reused binaries require a CI run')
        run_id = ci_run.rsplit('/', 1)[-1]
        run_data = json.loads(run('gh', 'run', 'view', run_id, '--repo', REPO,
                                  '--json', 'headSha,status,conclusion', capture=True))
        if (run_data['headSha'] != binary_revision or run_data['status'] != 'completed'
                or run_data['conclusion'] != 'success'):
            raise RuntimeError('Binary source CI run is not successful')
    elif binary_revision != source_revision or ci_run:
        raise RuntimeError('Fresh binaries must come from the packaging revision')
    manifest = output / 'release-manifest.json'
    manifest.write_text(json.dumps({
        'version': args.version, 'sourceRevision': binary_revision,
        'packagingRevision': source_revision, 'ciRun': ci_run,
        'binariesReusedUnchanged': binaries_reused,
        'license': 'Prosperity-3.0.0', 'exampleLicense': 'MIT',
        'requirements': {'windows-amd64': 'Windows x64; validated on Windows 11',
                         'linux-amd64': 'glibc 2.39+, libgcc_s, installed fonts; Ubuntu 24.04 / WSL2'},
        'clients': json.loads((ROOT / 'examples/package.json').read_text())['dependencies'],
        'artifacts': public_assets,
    }, indent=2) + '\n', encoding='utf-8')
    assets.append(manifest)
    sums = output / 'SHA256SUMS'
    sums.write_text(''.join(f'{digest(path)}  {path.name}\n' for path in assets), encoding='utf-8')
    assets.append(sums)
    body = (ROOT / 'RELEASE_NOTES.md').read_text(encoding='utf-8')
    if args.version not in body.splitlines()[0]:
        raise RuntimeError('Release notes do not match the release version')
    body = re.sub(r'\]\((?!https?://)([^)]+)\)',
                  lambda m: f'](https://github.com/{REPO}/blob/{source_revision}/{m[1]})', body)
    notes = output / 'release-body.md'
    notes.write_text(body, encoding='utf-8')
    existing = subprocess.run(['gh', 'release', 'view', args.version, '--repo', REPO,
                               '--json', 'isDraft,targetCommitish'], capture_output=True, text=True)
    if existing.returncode == 0:
        current = json.loads(existing.stdout)
        raise RuntimeError('Release already exists; delete it before publishing its replacement')
    else:
        # Verify repository access independently; do not hide an authentication failure.
        run('gh', 'repo', 'view', REPO, '--json', 'name', capture=True)
        create_args = ['gh', 'release', 'create', args.version, '--repo', REPO, '--draft',
                       '--target', source_revision, '--title', f'Mimic {args.version} — Public Beta',
                       '--notes-file', str(notes)]
        if is_prerelease:
            create_args.append('--prerelease')
        run(*create_args)
    run('gh', 'release', 'upload', args.version, '--repo', REPO, '--clobber', *map(str, assets))
    uploaded = json.loads(run('gh', 'release', 'view', args.version, '--repo', REPO,
                              '--json', 'assets', capture=True))['assets']
    if {asset['name'] for asset in uploaded} != {path.name for path in assets}:
        raise RuntimeError('Draft contains unexpected assets; refusing to publish')
    with tempfile.TemporaryDirectory(prefix='mimic-release-download-') as temp:
        run('gh', 'release', 'download', args.version, '--repo', REPO, '--dir', temp)
        for path in assets:
            if digest(Path(temp) / path.name) != digest(path):
                raise RuntimeError(f'GitHub download verification failed: {path.name}')
    run('gh', 'release', 'edit', args.version, '--repo', REPO,
        '--draft=false', f'--prerelease={str(is_prerelease).lower()}', '--notes-file', str(notes))
    print(f'Published and download-verified: https://github.com/{REPO}/releases/tag/{args.version}')


if __name__ == '__main__':
    main()
