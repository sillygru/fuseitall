# SPDX-License-Identifier: AGPL-3.0-only
"""Fail when a hand-written source file lacks the SPDX header. Excludes generated/config."""
import re
import subprocess
import sys
from pathlib import Path

TAG = "SPDX-License-Identifier: AGPL-3.0-only"
# Extensions that must carry the header when hand-written.
WANT_EXTS = {
    ".go", ".dart", ".kt", ".ts", ".svelte", ".css", ".sh", ".py",
    ".html", ".xml", ".svg", ".md", ".h", ".m",
}
# Paths that are generated, vendored, docs, or config-exempt.
SKIP_PREFIXES = (
    "apps/mac/frontend/bindings/",
    "apps/mac/frontend/dist/",
    "apps/mac/frontend/node_modules/",
    "apps/android/build/",
    "apps/android/.dart_tool/",
    "apps/android/android/app/src/main/jniLibs/",
    "packages/proto/",
    "docs/",
    # Vendored third-party skills: only the two first-party skills are checked.
    ".agents/skills/design-taste-frontend/",
    ".agents/skills/flutter/",
    ".agents/skills/golang-error-handling/",
    ".agents/skills/golang-security/",
    ".agents/skills/svelte/",
    ".agents/skills/sveltekit-structure/",
    ".agents/skills/tailwind-v4-shadcn/",
    ".agents/skills/vercel-react-best-practices/",
)
SKIP_SUFFIXES = (".json", ".yaml", ".yml", ".toml", ".lock")
SKIP_NAMES = {
    "Taskfile.yml",
    "pubspec.yaml",
    "package.json",
    "README.md",
    "AGENTS.md",
    # Tooling/config files that never carried headers.
    "vite.config.ts",
    "vite-env.d.ts",
    "AndroidManifest.xml",
    "file_provider_paths.xml",
    "launch_background.xml",
    "styles.xml",
}


def tracked_files() -> list[str]:
    out = subprocess.check_output(
        ["git", "ls-files"], text=True, cwd=Path(__file__).resolve().parent.parent
    )
    return out.splitlines()


def main() -> int:
    root = Path(__file__).resolve().parent.parent
    bad: list[str] = []
    for f in tracked_files():
        p = root / f
        if not p.is_file():
            continue
        if f.startswith(".git/"):
            continue
        if f.startswith(SKIP_PREFIXES):
            continue
        if p.suffix.lower() not in WANT_EXTS:
            continue
        if f.endswith(SKIP_SUFFIXES) or p.name in SKIP_NAMES:
            continue
        try:
            head = p.read_text(encoding="utf-8", errors="strict")[:2000]
        except (UnicodeDecodeError, OSError):
            continue
        if TAG not in head:
            bad.append(f)
    if bad:
        print(f"Missing {TAG} in {len(bad)} file(s):")
        for f in sorted(bad):
            print(f"  {f}")
        return 1
    print("license headers ok")
    return 0


if __name__ == "__main__":
    sys.exit(main())
