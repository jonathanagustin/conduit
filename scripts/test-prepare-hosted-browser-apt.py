#!/usr/bin/env python3
"""Exercise only temporary fixtures; never inspect or modify this worker's APT."""
import contextlib
import importlib.util
import io
from pathlib import Path
import stat
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location("hosted_apt", Path(__file__).with_name("prepare-hosted-browser-apt.py"))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class HostedArchiveTests(unittest.TestCase):
    def test_hosted_archive_mirrorlist_retains_security_and_trust(self):
        original = "Types: deb\nURIs: mirror+file:/etc/apt/apt-mirrors.txt\nSuites: resolute resolute-updates resolute-backports\nArchitectures: amd64v3\nSigned-By: /usr/share/keyrings/ubuntu-archive-keyring.gpg\n\nTypes: deb\nURIs: mirror+file:/etc/apt/apt-security-mirrors.txt\nSuites: resolute-security\nSigned-By: /usr/share/keyrings/ubuntu-archive-keyring.gpg\n"
        expected = original.replace("mirror+file:/etc/apt/apt-mirrors.txt", "https://archive.ubuntu.com/ubuntu")
        self.assertEqual(module.rewrite_sources(original), expected)
        self.assertEqual(module.rewrite_sources(expected), expected)
        classic = "deb [arch=amd64v3 signed-by=/keyring] mirror+file:/etc/apt/apt-mirrors.txt resolute main\n"
        self.assertEqual(module.rewrite_sources(classic), classic.replace("mirror+file:/etc/apt/apt-mirrors.txt", module.CANONICAL))

    def test_mirrorlist_is_an_exact_uri_token_only(self):
        for uri in ["https://example.invalid/mirror+file:/etc/apt/apt-mirrors.txt", "mirror+file:/etc/apt/apt-mirrors.txt.extra", "mirror+file:/etc/apt/apt-mirrors.txt?query=1", "mirror+file:/etc/apt/custom-mirrors.txt"]:
            text = "URIs: " + uri + "\n"
            self.assertEqual(module.rewrite_sources(text), text)
        for field in ["# URIs:", "Description:", "Signed-By:"]:
            text = field + " mirror+file:/etc/apt/apt-mirrors.txt\n"
            self.assertEqual(module.rewrite_sources(text), text)

    def test_deb822_preserves_signed_distribution_and_architectures(self):
        original = "Types: deb\nURIs: http://us-east-2.ec2.archive.ubuntu.com/ubuntu/\nSuites: resolute resolute-updates resolute-backports\nComponents: main restricted universe multiverse\nArchitectures: amd64 amd64v3\nSigned-By: /usr/share/keyrings/ubuntu-archive-keyring.gpg\n"
        expected = original.replace("http://us-east-2.ec2.archive.ubuntu.com", "https://archive.ubuntu.com")
        self.assertEqual(module.rewrite_sources(original), expected)
        self.assertEqual(module.rewrite_sources(expected), expected)

    def test_classic_sources_retain_options_and_pockets(self):
        original = "deb [arch=amd64 signed-by=/keyring] https://eu-west-1.ec2.archive.ubuntu.com/ubuntu resolute main\ndeb-src http://us-east-2.ec2.archive.ubuntu.com/ubuntu resolute-updates universe\n"
        expected = original.replace("https://eu-west-1.ec2.archive.ubuntu.com", "https://archive.ubuntu.com").replace("http://us-east-2.ec2.archive.ubuntu.com", "https://archive.ubuntu.com")
        self.assertEqual(module.rewrite_sources(original), expected)

    def test_comments_security_third_party_and_unrelated_fields_are_untouched(self):
        original = "# URIs: http://us-east-2.ec2.archive.ubuntu.com/ubuntu\nURIs: http://security.ubuntu.com/ubuntu https://packages.example/ubuntu http://us-east-2.ec2.archive.ubuntu.com.attacker.invalid/ubuntu\nDescription: http://us-east-2.ec2.archive.ubuntu.com/ubuntu\nSigned-By: http://us-east-2.ec2.archive.ubuntu.com/ubuntu\n"
        self.assertEqual(module.rewrite_sources(original), original)

    def test_multiline_uris_and_line_endings(self):
        original = "URIs:\r\n http://us-east-2.ec2.archive.ubuntu.com/ubuntu\r\n https://security.ubuntu.com/ubuntu\r\nSuites: resolute\r\n"
        self.assertEqual(module.rewrite_sources(original), original.replace("http://us-east-2.ec2.archive.ubuntu.com", "https://archive.ubuntu.com"))

    def test_paths_queries_and_credentials_are_not_reinterpreted(self):
        for uri in ["http://us-east-2.ec2.archive.ubuntu.com/ubuntu/other", "http://us-east-2.ec2.archive.ubuntu.com/ubuntu?query=1", "http://user@us-east-2.ec2.archive.ubuntu.com/ubuntu"]:
            text = "URIs: " + uri + "\n"
            self.assertEqual(module.rewrite_sources(text), text)

    def test_temporary_file_update_preserves_permissions(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "ubuntu.sources"
            path.write_bytes(b"URIs: http://us-east-2.ec2.archive.ubuntu.com/ubuntu\r\nSuites: resolute\r\n")
            path.chmod(0o640)
            self.assertEqual(module.prepare_sources([path]), 1)
            self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o640)
            self.assertEqual(path.read_bytes(), b"URIs: https://archive.ubuntu.com/ubuntu\r\nSuites: resolute\r\n")
            self.assertEqual(module.prepare_sources([path]), 0)

    def test_symlink_refused_without_modifying_target(self):
        with tempfile.TemporaryDirectory() as directory:
            target = Path(directory) / "target"
            target.write_text("URIs: http://us-east-2.ec2.archive.ubuntu.com/ubuntu\n")
            link = Path(directory) / "ubuntu.sources"
            link.symlink_to(target)
            with self.assertRaises(ValueError):
                module.prepare_sources([link])
            self.assertIn("us-east-2.ec2", target.read_text())

    def test_hosted_setup_uses_only_standard_ubuntu_files(self):
        environment = {"GITHUB_ACTIONS": "true", "RUNNER_ENVIRONMENT": "github-hosted"}
        with patch.dict(module.os.environ, environment, clear=True), patch.object(module.platform, "system", return_value="Linux"), patch.object(module.platform, "machine", return_value="x86_64"), patch.object(module.os, "geteuid", return_value=0), patch.object(Path, "read_text", return_value="ID=ubuntu\n"), patch.object(module, "prepare_sources", return_value=1) as prepare, contextlib.redirect_stdout(io.StringIO()):
            self.assertEqual(module.main(), 0)
            prepare.assert_called_once_with([Path("/etc/apt/sources.list"), Path("/etc/apt/sources.list.d/ubuntu.sources")])

    def test_local_self_hosted_and_act_refuse_before_reading_configuration(self):
        for environment in [{}, {"GITHUB_ACTIONS": "true", "RUNNER_ENVIRONMENT": "self-hosted"}, {"GITHUB_ACTIONS": "true", "RUNNER_ENVIRONMENT": "github-hosted", "ACT": "true"}]:
            with self.subTest(environment=environment), patch.dict(module.os.environ, environment, clear=True), patch.object(module.platform, "system", return_value="Linux"), patch.object(module.platform, "machine", return_value="x86_64"), patch.object(module.os, "geteuid", return_value=0), patch.object(module, "prepare_sources", side_effect=AssertionError("must not prepare")), patch.object(Path, "read_text", side_effect=AssertionError("must not read configuration")), contextlib.redirect_stderr(io.StringIO()):
                self.assertEqual(module.main(), 1)


if __name__ == "__main__":
    unittest.main()
