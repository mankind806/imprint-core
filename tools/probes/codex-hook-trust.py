#!/usr/bin/env python3
"""Bounded, auth-free native hook trust probe. Writes only a new temporary directory."""
import json, os, pathlib, select, subprocess, tempfile, time

root = pathlib.Path(tempfile.mkdtemp(prefix='imprint-codex-hook-probe-'))
home = root / 'codex-home'
home.mkdir()
script = root / 'hook.sh'
marker = root / 'marker'
config = home / 'config.toml'
hooks = home / 'hooks.json'
command = 'sh ' + str(script)
base = 'approval_policy = "never"\nsandbox_mode = "read-only"\n'
config.write_text(base)

def fixture(value):
    script.write_text("#!/bin/sh\nprintf '%s' " + value + ' > ' + str(marker) + '\n')

def definition(cmd):
    hooks.write_text(json.dumps({'hooks': {'SessionStart': [{'hooks': [{'type': 'command', 'command': cmd}]}]}}))

class Server:
    def __init__(self):
        self.p = subprocess.Popen(['codex', 'app-server', '--stdio'], env={**os.environ, 'CODEX_HOME': str(home)}, cwd=root, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True)
        self.i = 0
        self.request('initialize', {'clientInfo': {'name': 'imprint-native-probe', 'version': '1'}, 'capabilities': {'experimentalApi': True}})
        self.p.stdin.write(json.dumps({'method': 'initialized'}) + '\n'); self.p.stdin.flush()
    def request(self, method, params):
        self.i += 1
        self.p.stdin.write(json.dumps({'id': self.i, 'method': method, 'params': params}) + '\n'); self.p.stdin.flush()
        deadline = time.monotonic() + 20
        while time.monotonic() < deadline:
            if not select.select([self.p.stdout], [], [], 1)[0]: continue
            line = self.p.stdout.readline()
            if not line: raise RuntimeError('server exited')
            value = json.loads(line)
            if value.get('id') == self.i:
                if 'error' in value: raise RuntimeError(value['error'])
                return value['result']
        raise TimeoutError(method)
    def close(self):
        self.p.terminate()
        self.p.wait(timeout=5)

def measure(label):
    server = Server()
    try:
        listing = server.request('hooks/list', {'cwds': [str(root)]})
        entries = listing.get('data', listing.get('entries', []))
        if not entries: raise RuntimeError(listing)
        hook = entries[0]['hooks'][0]
        thread = server.request('thread/start', {'cwd': str(root), 'ephemeral': True, 'sandbox': 'read-only', 'approvalPolicy': 'never'})
        server.request('turn/start', {'threadId': thread['thread']['id'], 'input': [{'type': 'text', 'text': 'DELEGATED BY: native-probe ROLE: inert probe. Reply OK only.'}]})
        time.sleep(1)
        result = {k: hook[k] for k in ('key', 'currentHash', 'trustStatus')}
        result.update(label=label, marker=marker.read_text() if marker.exists() else None)
        print(json.dumps(result), flush=True)
        return result
    finally: server.close()

print(json.dumps({'version': subprocess.check_output(['codex', '--version'], text=True).strip(), 'utc': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()), 'mode': 'isolated CODEX_HOME; no auth copied; turn requested without credentials'}))
fixture('A'); definition(command)
a = measure('untrusted')
config.write_text(base + '\n[hooks.state.' + json.dumps(a['key']) + ']\ntrusted_hash = ' + json.dumps(a['currentHash']) + '\n')
b = measure('trusted-A')
fixture('B')
c = measure('same-definition-script-B')
fixture('C')
definition(command + ' # changed-definition')
d = measure('changed-definition')
assert a['marker'] is None and a['trustStatus'] == 'untrusted'
assert b['marker'] == 'A' and b['trustStatus'] == 'trusted'
assert c['marker'] == 'B' and c['currentHash'] == b['currentHash'] and c['trustStatus'] == 'trusted'
assert d['marker'] == 'B' and d['currentHash'] != c['currentHash'] and d['trustStatus'] == 'modified'
print('PASS: script bytes are outside this hook-definition trust hash; changed definition is skipped')
