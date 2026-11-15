#!/usr/bin/env python3
"""Keep generated banners stable without rewriting authored source."""
import importlib.util
from pathlib import Path
import tempfile
import unittest
import sys

sys.dont_write_bytecode = True

spec = importlib.util.spec_from_file_location("normalize_stubs", Path(__file__).with_name("normalize-connect-stubs.py"))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)

class NormalizeTests(unittest.TestCase):
    def test_authored_contract_test_and_docs_are_untouched(self):
        for suffix, text in [(".go", "package contract_test\n\nfunc Example() {}\n"), (".go", "// Package contract documents an authored package.\npackage contract\n"), (".ts", "export const contract = 'authored';\n")]:
            with self.subTest(text=text), tempfile.TemporaryDirectory() as directory:
                path = Path(directory) / ("authored" + suffix)
                path.write_text(text)
                module.rewrite(path)
                self.assertEqual(path.read_text(), text)

    def test_plugin_output_is_normalized_and_idempotent(self):
        for suffix, body in [(".go", "package contract\n"), (".ts", "export const contract = 1;\n")]:
            with self.subTest(suffix=suffix), tempfile.TemporaryDirectory() as directory:
                path = Path(directory) / ("wire" + suffix)
                path.write_text("// Code " + module.NEEDLE + " protoc. DO NOT EDIT.\n" + body)
                module.rewrite(path)
                first = path.read_text()
                self.assertNotIn(module.NEEDLE, first)
                self.assertIn("Regenerated from proto/conduit. DO NOT EDIT.", first)
                self.assertIn(body, first)
                module.rewrite(path)
                self.assertEqual(path.read_text(), first)

if __name__ == "__main__":
    unittest.main()
