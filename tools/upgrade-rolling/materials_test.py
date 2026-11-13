import io
from pathlib import Path
import tarfile
import tempfile
import unittest
from rolling import materials_archive


class ContainerMaterialTest(unittest.TestCase):
    def test_archive_uses_private_container_root_ownership(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory)
            (path / "postgres.dsn").write_bytes(b"fixture-only")
            with tarfile.open(fileobj=io.BytesIO(materials_archive(path))) as archive:
                members = archive.getmembers()
                self.assertEqual([entry.name for entry in members], ["fixture", "fixture/postgres.dsn"])
                self.assertEqual([(entry.uid, entry.gid, entry.mode) for entry in members], [(0, 0, 0o700), (0, 0, 0o600)])
                self.assertEqual(archive.extractfile("fixture/postgres.dsn").read(), b"fixture-only")


if __name__ == "__main__":
    unittest.main()
