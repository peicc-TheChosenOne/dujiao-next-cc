import importlib.util
import io
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest
from unittest.mock import patch
import zipfile

spec = importlib.util.spec_from_file_location("release_safety", Path(__file__).resolve().parents[1] / "check-release-safety.py")
safety = importlib.util.module_from_spec(spec)
spec.loader.exec_module(safety)


class ReleaseSafetyTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        subprocess.run(["git", "init", "-q", str(self.root)], check=True)
        self.manifest = "archives:\n  - files:\n" + "".join(f"      - ./{name}\n" for name in sorted(safety.RELEASE_FILES)) + "changelog:\n"
        (self.root / ".goreleaser.yaml").write_text(self.manifest, encoding="utf-8")
        self.env = patch.dict(os.environ, {"VITE_API_BASE_URL": "", "DUJIAO_CONFIG_FILE": "", "DUJIAO_PRIVATE_CONFIG_FILE": ""})
        self.env.start()
        self.addCleanup(self.env.stop)

    def test_example_configuration_is_allowed_but_forced_private_file_is_rejected(self):
        (self.root / "config.private.yml.example").write_text('jwt:\n  secret: ""\n', encoding="utf-8")
        self.assertEqual(safety.check_sources(self.root), [])
        (self.root / ".gitignore").write_text("config.private.yml\n", encoding="utf-8")
        (self.root / "config.private.yml").write_text("jwt:\n  secret: isolated-private-secret\n", encoding="utf-8")
        subprocess.run(["git", "add", "-f", "config.private.yml"], cwd=self.root, check=True)
        self.assertTrue(any("private/runtime" in error for error in safety.check_sources(self.root)))

    def test_private_value_in_source_is_detected_without_printing_it(self):
        (self.root / ".gitignore").write_text("config.private.yml\n", encoding="utf-8")
        (self.root / "config.private.yml").write_text("redis:\n  password: isolated-password-to-detect\n", encoding="utf-8")
        (self.root / "main.go").write_text('package main\nconst password="isolated-password-to-detect"', encoding="utf-8")
        errors = safety.check_sources(self.root)
        self.assertTrue(any("local private value" in error for error in errors))
        self.assertNotIn("isolated-password-to-detect", "\n".join(errors))

    def test_public_ip_in_deployment_template_is_rejected(self):
        (self.root / "deploy").mkdir()
        (self.root / "deploy/nginx.conf").write_text("proxy_pass http://1.1.1.1:8080;", encoding="utf-8")
        self.assertTrue(any("public IPv4" in error for error in safety.check_sources(self.root)))

    def test_expanding_archive_allowlist_is_rejected(self):
        (self.root / ".goreleaser.yaml").write_text(self.manifest.replace("      - ./README.md", "      - ./deploy/**/*"), encoding="utf-8")
        self.assertTrue(any("allowlist" in error for error in safety.check_sources(self.root)))

    def test_extra_release_attachment_cannot_bypass_archive_checks(self):
        (self.root / ".goreleaser.yaml").write_text(self.manifest + "release:\n  extra_files:\n    - glob: config.private.yml\n", encoding="utf-8")
        self.assertTrue(any("bypasses" in error for error in safety.check_sources(self.root)))

    def test_local_production_frontend_env_and_external_api_env_are_rejected(self):
        folder = self.root / "frontend/user"
        folder.mkdir(parents=True)
        (folder / ".env.production.local").write_text("VITE_API_BASE_URL=http://localhost:8080", encoding="utf-8")
        self.assertTrue(any("production frontend" in error for error in safety.check_sources(self.root)))
        with patch.dict(os.environ, {"VITE_API_BASE_URL": "http://localhost:8080"}):
            self.assertTrue(any("same origin" in error for error in safety.check_sources(self.root)))

    def tar(self, names):
        path = self.root / "release.tar.gz"
        with tarfile.open(path, "w:gz") as archive:
            for name, data in names:
                entry = tarfile.TarInfo(name)
                entry.size = len(data)
                archive.addfile(entry, io.BytesIO(data))
        return path

    def test_archive_accepts_public_files_and_rejects_private_extra_and_traversal(self):
        valid = [(name, b"example") for name in sorted(safety.RELEASE_FILES)] + [("dujiao-next", b"binary")]
        self.assertEqual(safety.check_archive(self.tar(valid)), [])
        for name in ("config.private.yml", "config.yml", "uploads/customer.png", "../config.private.yml", "/config.private.yml"):
            with self.subTest(name=name):
                self.assertTrue(safety.check_archive(self.tar(valid + [(name, b"private")])))

    def test_archive_rejects_duplicates_missing_binary_and_secret_content(self):
        self.assertTrue(safety.check_archive(self.tar([("README.md", b"example")])))
        self.assertTrue(safety.check_archive(self.tar([("dujiao-next", b"binary"), ("dujiao-next", b"binary")])))
        self.assertTrue(safety.check_archive(self.tar([("dujiao-next", b"binary"), ("./dujiao-next", b"binary")])))
        errors = safety.check_archive(self.tar([("dujiao-next", b"isolated-private-content")]), {b"isolated-private-content"})
        self.assertTrue(errors)
        self.assertNotIn("isolated-private-content", "\n".join(errors))

    def test_archive_directory_paths_are_checked(self):
        self.assertEqual(safety.archive_entry("scripts/", False, directory=True), [])
        self.assertTrue(safety.archive_entry("../outside/", False, directory=True))
        self.assertTrue(safety.archive_entry("secrets/", False, directory=True))

    def test_archive_rejects_tar_and_zip_symlinks(self):
        path = self.root / "symlink.tar.gz"
        with tarfile.open(path, "w:gz") as archive:
            entry = tarfile.TarInfo("dujiao-next")
            entry.type = tarfile.SYMTYPE
            entry.linkname = "/etc/passwd"
            archive.addfile(entry)
        self.assertTrue(safety.check_archive(path))
        path = self.root / "symlink.zip"
        with zipfile.ZipFile(path, "w") as archive:
            entry = zipfile.ZipInfo("dujiao-next.exe")
            entry.create_system = 3
            entry.external_attr = 0o120777 << 16
            archive.writestr(entry, "/etc/passwd")
        self.assertTrue(safety.check_archive(path))

    def test_windows_zip_public_files_are_allowed(self):
        path = self.root / "release.zip"
        with zipfile.ZipFile(path, "w") as archive:
            archive.writestr("dujiao-next.exe", "binary")
            archive.writestr("config.private.yml.example", 'jwt:\n  secret: ""')
        self.assertEqual(safety.check_archive(path), [])


if __name__ == "__main__":
    unittest.main()
