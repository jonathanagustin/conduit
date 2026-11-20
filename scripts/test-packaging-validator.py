"""Negative-contract tests. Tool mocks below are not live validation receipts."""
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("validator", Path(__file__).with_name("verify-packaging-scaffold.py"))
v = importlib.util.module_from_spec(spec)
spec.loader.exec_module(v)

class ReceiptTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.envelope = self.root / "envelope.json"
        self.helm = self.root / "helm-fixture"
        self.helm.write_bytes(b"tool fixture, never executed")
        self.digest = v.file_sha(self.helm)
        self.files = [{"path": p, "content": "{}\n"} for p in sorted(v.EXPECTED)]
        self.write_bundle()
        schemas = {"zarf-v1.0.1.json": b'{"type":"object"}', "uds-package-v1alpha1.yaml": b'{"spec":{"versions":[{"name":"v1alpha1","schema":{"openAPIV3Schema":{"type":"object"}}}]}}'}
        self.pins = {}
        for name, raw in schemas.items():
            (self.root / name).write_bytes(raw)
            self.pins[name] = v.sha(raw)
        self.addCleanup(patch.stopall)
        patch.object(v, "SCHEMAS", self.pins).start()
        self.tool = patch.object(v, "tool", side_effect=self.fake_tool).start()

    def write_bundle(self):
        canonical = json.dumps(self.files, separators=(",", ":"), ensure_ascii=False).encode()
        self.envelope.write_text(json.dumps({"status":"scaffold-needs-validation", "files":self.files, "sha256":v.sha(canonical)}))

    @staticmethod
    def fake_tool(helm, args, env):
        if args[0] == "version":
            return v.HELM_VERSION
        if args[0] == "template":
            return "apiVersion: v1\nkind: Service\nmetadata:\n  name: fixture\n"
        return "fixture lint"

    def check(self):
        return v.validate(self.envelope, self.root, self.helm, self.digest)

    def test_success_requires_all_checks_and_exact_bindings(self):
        receipt = self.check()
        self.assertEqual(receipt["status"], "passed")
        self.assertEqual(receipt["inputSHA256"], v.sha(self.envelope.read_bytes()))
        self.assertTrue(all(c["status"] == "passed" for c in receipt["checks"]))
        self.assertFalse(receipt["deploymentAuthorized"])
        self.assertEqual(receipt["producer"], "local-process-unsigned")
        self.assertEqual(receipt["helm"]["sha256"], self.digest)
        env = self.tool.call_args.args[2]
        self.assertNotIn("HELM_KUBETOKEN", env)
        self.assertNotEqual(env["KUBECONFIG"], os.environ.get("KUBECONFIG"))

    def test_tamper_fails_before_tools(self):
        bundle = json.loads(self.envelope.read_text())
        bundle["files"][0]["content"] = '{"changed":true}'
        self.envelope.write_text(json.dumps(bundle))
        result = self.check()
        self.assertEqual(result["status"], "failed")
        self.assertFalse(self.tool.called)
        self.assertTrue(any(c["status"] == "not-run" for c in result["checks"]))

    def test_schema_or_tool_identity_mismatch(self):
        for target in [self.root / "zarf-v1.0.1.json", self.helm]:
            original = target.read_bytes()
            target.write_bytes(b"changed")
            self.assertEqual(self.check()["status"], "failed")
            target.write_bytes(original)

    def test_timeout_and_failed_tool_do_not_pass_or_leak(self):
        for error in [subprocess.TimeoutExpired("secret-command", 20), subprocess.CalledProcessError(1, "secret-command", stderr=b"credential-do-not-echo")]:
            def tool(helm, args, env):
                if args[0] == "version":
                    return v.HELM_VERSION
                raise error
            self.tool.side_effect = tool
            result = self.check()
            self.assertEqual(result["status"], "failed")
            self.assertNotIn("secret-command", json.dumps(result))
            self.assertNotIn("credential-do-not-echo", json.dumps(result))
            self.assertTrue(any(c["status"] == "not-run" for c in result["checks"]))

    def test_template_expression_path_escape_duplicate_keys_rejected(self):
        self.files[0]["content"] = '{"x":"{{ lookup }}"}'
        self.write_bundle()
        self.assertEqual(self.check()["status"], "failed")
        self.files[0]["content"] = "{}"
        self.files[0]["path"] = "../escape"
        self.write_bundle()
        self.assertEqual(self.check()["status"], "failed")
        self.envelope.write_text('{"files":[],"files":[]}')
        self.assertEqual(self.check()["status"], "failed")

    def test_wrong_version_and_empty_render_fail(self):
        self.tool.side_effect = lambda *args: "v0.0.0"
        self.assertEqual(self.check()["status"], "failed")
        self.tool.side_effect = lambda helm,args,env: v.HELM_VERSION if args[0]=="version" else ""
        self.assertEqual(self.check()["status"], "failed")

class ToolBoundsTests(unittest.TestCase):
    def test_real_process_output_bound(self):
        with self.assertRaises(ValueError):
            v.tool(Path(sys.executable), ["-c", "import sys;sys.stdout.write('x'*1048576)"], dict(os.environ))
    def test_real_process_timeout(self):
        with patch.object(v, "TOOL_TIMEOUT", 0.05):
            with self.assertRaises(subprocess.TimeoutExpired):
                v.tool(Path(sys.executable), ["-c", "import time;time.sleep(5)"], dict(os.environ))



class BrokenCaptureTests(unittest.TestCase):
    def test_stdout_or_stderr_read_failure_is_not_success(self):
        import io
        class BrokenStream:
            def __init__(self): self.first=True
            def read(self, size):
                if self.first:
                    self.first=False
                    return b'apiVersion: v1\nkind: Service\n'
                raise OSError('private-sensitive-diagnostic')
            def close(self): pass
        class Process:
            def __init__(self, broken_stdout):
                self.stdout=BrokenStream() if broken_stdout else io.BytesIO(b'apiVersion: v1\nkind: Service\n')
                self.stderr=io.BytesIO(b'') if broken_stdout else BrokenStream()
            def wait(self, timeout=None):return 0
            def kill(self):pass
        for broken_stdout in [True,False]:
            with patch.object(v.subprocess,'Popen',return_value=Process(broken_stdout)):
                with self.assertRaisesRegex(RuntimeError,'tool output capture failed'):
                    v.tool(Path('never-executed'),[],{})

if __name__ == "__main__":
    unittest.main()
