#!/usr/bin/env python3
"""
bump_version.py — bump FuseItAll version (x.y.z) and build number across all mirrors.

Single source of truth: packages/core/version.go (CurrentAppVersion, CurrentBuild,
BuildToVersion, CurrentProtocolV, CurrentMinPeerBuild).

Mirrors updated atomically:
  packages/core/version.go
  apps/android/lib/version.dart
  apps/android/pubspec.yaml          (version: x.y.z+N)
  apps/mac/frontend/package.json
  apps/mac/build/darwin/Info.plist            (CFBundleVersion + ShortVersionString)
  apps/mac/build/darwin/Info.dev.plist        (same)
  apps/mac/build/config.yml                   (info.version)
  apps/mac/build/windows/info.json            (fixed.file_version + ProductVersion)

Stability decisions (see plan):
  - Every version change => build+1 unless --build overrides (>current required).
  - No --keep-build; version display is cheap, build gate is safety.
  - major -> (X+1).0.0, minor -> X.(Y+1).0, patch -> X.Y.(Z+1).
  - --min-peer-build / --protocol-v optional, default unchanged, require -y.
  - BuildToVersion append-only, sorted.

Usage:
  python3 scripts/bump_version.py --bump patch
  python3 scripts/bump_version.py --bump minor --dry-run
  python3 scripts/bump_version.py --set 0.6.0
  python3 scripts/bump_version.py --set 0.6.0 --build 7 -y
  python3 scripts/bump_version.py --check        # verify alignment, no writes
  python3 scripts/bump_version.py --protocol-v 2 --min-peer-build 6 -y
"""
from __future__ import annotations
import argparse
import json
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
CORE_GO = ROOT / "packages/core/version.go"
DART = ROOT / "apps/android/lib/version.dart"
PUBSPEC = ROOT / "apps/android/pubspec.yaml"
PKG_JSON = ROOT / "apps/mac/frontend/package.json"
INFO_PLIST = ROOT / "apps/mac/build/darwin/Info.plist"
INFO_DEV_PLIST = ROOT / "apps/mac/build/darwin/Info.dev.plist"
CONFIG_YML = ROOT / "apps/mac/build/config.yml"
WIN_INFO = ROOT / "apps/mac/build/windows/info.json"

SEMVER_RE = re.compile(r"^(\d+)\.(\d+)\.(\d+)$")

def parse_semver(s: str) -> tuple[int,int,int]:
    m = SEMVER_RE.match(s.strip())
    if not m:
        raise ValueError(f"expected x.y.z, got {s!r}")
    return int(m.group(1)), int(m.group(2)), int(m.group(3))

def semver_str(t: tuple[int,int,int]) -> str:
    return f"{t[0]}.{t[1]}.{t[2]}"

def bump_semver(cur: tuple[int,int,int], kind: str) -> tuple[int,int,int]:
    x,y,z = cur
    if kind == "major":
        return (x+1, 0, 0)
    if kind == "minor":
        return (x, y+1, 0)
    if kind == "patch":
        return (x, y, z+1)
    raise ValueError(kind)

def read_core() -> dict:
    txt = CORE_GO.read_text()
    m_ver = re.search(r'CurrentAppVersion\s*=\s*"([^"]+)"', txt)
    m_build = re.search(r'CurrentBuild\s*=\s*(\d+)', txt)
    m_min = re.search(r'CurrentMinPeerBuild\s*=\s*(\d+)', txt)
    m_proto = re.search(r'CurrentProtocolV\s*=\s*(\d+)', txt)
    m_map = re.search(r'BuildToVersion\s*=\s*map\[int\]string\{([^}]+)\}', txt)
    if not (m_ver and m_build and m_min and m_proto and m_map):
        raise RuntimeError("failed to parse packages/core/version.go")
    # parse map entries like 1: "0.1.0", 2: "0.2.0"
    entries = {}
    for em in re.finditer(r'(\d+)\s*:\s*"([^"]+)"', m_map.group(1)):
        entries[int(em.group(1))] = em.group(2)
    return {
        "version": m_ver.group(1),
        "build": int(m_build.group(1)),
        "min_peer_build": int(m_min.group(1)),
        "protocol_v": int(m_proto.group(1)),
        "map": entries,
        "raw": txt,
    }

def read_pubspec_version() -> tuple[str,int]:
    txt = PUBSPEC.read_text()
    m = re.search(r'^version:\s*(\d+\.\d+\.\d+)\+(\d+)\s*$', txt, re.M)
    if not m:
        raise RuntimeError(f"pubspec version not found: {PUBSPEC}")
    return m.group(1), int(m.group(2))

def read_package_json_version() -> str:
    return json.loads(PKG_JSON.read_text())["version"]

def read_dart() -> dict:
    txt = DART.read_text()
    mv = re.search(r"kAppVersion\s*=\s*'([^']+)'", txt)
    mb = re.search(r"kAppBuild\s*=\s*(\d+)", txt)
    mm = re.search(r"kMinPeerBuild\s*=\s*(\d+)", txt)
    mp = re.search(r"kProtocolV\s*=\s*(\d+)", txt)
    if not (mv and mb):
        raise RuntimeError("failed to parse version.dart")
    # map literal <int, String>{1: '0.1.0', ...}
    entries = {}
    for em in re.finditer(r"(\d+)\s*:\s*'([^']+)'", txt):
        entries[int(em.group(1))] = em.group(2)
    return {
        "version": mv.group(1) if mv else "",
        "build": int(mb.group(1)) if mb else 0,
        "min_peer_build": int(mm.group(1)) if mm else 0,
        "protocol_v": int(mp.group(1)) if mp else 0,
        "map": entries,
        "raw": txt,
    }

def check_alignment(core=None, dart=None) -> list[str]:
    errs = []
    if core is None:
        core = read_core()
    if dart is None:
        dart = read_dart()
    v = core["version"]
    b = core["build"]
    # pubspec
    try:
        pv, pb = read_pubspec_version()
        if pv != v:
            errs.append(f"pubspec version {pv!r} != core {v!r}")
        if pb != b:
            errs.append(f"pubspec build +{pb} != core build {b}")
    except Exception as e:
        errs.append(str(e))
    # package.json
    try:
        pj = read_package_json_version()
        if pj != v:
            errs.append(f"package.json version {pj!r} != core {v!r}")
    except Exception as e:
        errs.append(str(e))
    # dart
    try:
        if dart["version"] != v:
            errs.append(f"version.dart kAppVersion {dart['version']!r} != core {v!r}")
        if dart["build"] != b:
            errs.append(f"version.dart kAppBuild {dart['build']} != core {b}")
        if dart["min_peer_build"] != core["min_peer_build"]:
            errs.append(f"version.dart kMinPeerBuild {dart['min_peer_build']} != core {core['min_peer_build']}")
        if dart["protocol_v"] != core["protocol_v"]:
            errs.append(f"version.dart kProtocolV {dart['protocol_v']} != core {core['protocol_v']}")
        if dart["map"].get(b) != v:
            errs.append(f"version.dart map[{b}]={dart['map'].get(b)!r} != {v!r}")
        if core["map"].get(b) != v:
            errs.append(f"core BuildToVersion[{b}]={core['map'].get(b)!r} != {v!r}")
    except Exception as e:
        errs.append(str(e))
    # plists
    for name, path in [("Info.plist", INFO_PLIST), ("Info.dev.plist", INFO_DEV_PLIST)]:
        if path.exists():
            txt = path.read_text()
            # CFBundleShortVersionString and CFBundleVersion
            vals = re.findall(r"<key>CFBundle(?:Short)?VersionString</key>\s*<string>([^<]+)</string>", txt)
            # also CFBundleVersion key is "CFBundleVersion"
            vals += re.findall(r"<key>CFBundleVersion</key>\s*<string>([^<]+)</string>", txt)
            # deduplicate: actually first regex already captures Short, second captures Version; do union above double counts, just check all strings equal v
            all_vals = re.findall(r"<key>CFBundle(?:Short)?VersionString</key>\s*<string>([^<]+)</string>|<key>CFBundleVersion</key>\s*<string>([^<]+)</string>", txt)
            flat = [a or b for a,b in all_vals]
            for fv in flat:
                if fv != v:
                    errs.append(f"{name} CFBundleVersion/Short {fv!r} != core {v!r}")
            # also catch direct CFBundleVersion exclusive regex
            if not flat:
                errs.append(f"{name}: no CFBundleVersion found")
    if CONFIG_YML.exists():
        txt = CONFIG_YML.read_text()
        m = re.search(r'^\s*version:\s*"(.*?)"', txt, re.M)
        if m and m.group(1) != v:
            errs.append(f"config.yml version {m.group(1)!r} != core {v!r}")
    if WIN_INFO.exists():
        try:
            data = json.loads(WIN_INFO.read_text())
            fv = data.get("fixed",{}).get("file_version","")
            pv = data.get("info",{}).get("0000",{}).get("ProductVersion","")
            if fv and fv != v:
                errs.append(f"windows/info.json file_version {fv!r} != core {v!r}")
            if pv and pv != v:
                errs.append(f"windows/info.json ProductVersion {pv!r} != core {v!r}")
        except Exception:
            pass
    return errs

def patch_core(next_v: str, next_build: int, next_min: int|None, next_proto: int|None) -> str:
    txt = CORE_GO.read_text()
    cur = read_core()
    # build
    txt = re.sub(r'CurrentBuild\s*=\s*\d+', f'CurrentBuild = {next_build}', txt, count=1)
    # version
    txt = re.sub(r'CurrentAppVersion\s*=\s*"[^"]+"', f'CurrentAppVersion = "{next_v}"', txt, count=1)
    if next_min is not None:
        txt = re.sub(r'CurrentMinPeerBuild\s*=\s*\d+', f'CurrentMinPeerBuild = {next_min}', txt, count=1)
    if next_proto is not None:
        txt = re.sub(r'CurrentProtocolV\s*=\s*\d+', f'CurrentProtocolV = {next_proto}', txt, count=1)
        # also update MinPeerBuildByProtocol map: add/overwrite entry for next_proto
        # Keep existing map; if proto already present update its value to next_min or current min
        min_for_proto = next_min if next_min is not None else cur["min_peer_build"]
        # naive: if entry not present, append inside braces
        def repl_proto_map(m):
            inner = m.group(1)
            if re.search(rf'\b{next_proto}\s*:', inner):
                inner = re.sub(rf'\b{next_proto}\s*:\s*\d+', f'{next_proto}: {min_for_proto}', inner)
            else:
                # add before closing }
                inner = inner.rstrip()
                sep = ", " if inner.strip() else ""
                inner = inner + f"{sep}{next_proto}: {min_for_proto}"
            return f"MinPeerBuildByProtocol = map[int]int{{{inner}}}"
        txt = re.sub(r'MinPeerBuildByProtocol\s*=\s*map\[int\]int\{([^}]*)\}', repl_proto_map, txt, count=1)
    # BuildToVersion map: insert sorted
    entries = cur["map"].copy()
    entries[next_build] = next_v
    # if same build already existed with different version, overwrite to next_v
    sorted_items = sorted(entries.items())
    map_str = ", ".join(f'{k}: "{v}"' for k,v in sorted_items)
    txt = re.sub(r'BuildToVersion\s*=\s*map\[int\]string\{[^}]+\}', f'BuildToVersion = map[int]string{{{map_str}}}', txt, count=1)
    return txt

def patch_dart(next_v: str, next_build: int, next_min: int|None, next_proto: int|None) -> str:
    txt = DART.read_text()
    cur = read_dart()
    txt = re.sub(r"kAppVersion\s*=\s*'[^']+'", f"kAppVersion = '{next_v}'", txt, count=1)
    txt = re.sub(r"kAppBuild\s*=\s*\d+", f"kAppBuild = {next_build}", txt, count=1)
    if next_min is not None:
        txt = re.sub(r"kMinPeerBuild\s*=\s*\d+", f"kMinPeerBuild = {next_min}", txt, count=1)
    if next_proto is not None:
        txt = re.sub(r"kProtocolV\s*=\s*\d+", f"kProtocolV = {next_proto}", txt, count=1)
    entries = cur["map"].copy()
    entries[next_build] = next_v
    sorted_items = sorted(entries.items())
    map_inner = ", ".join(f"{k}: '{v}'" for k,v in sorted_items)
    # replace the const versions map line
    txt = re.sub(r"const versions\s*=\s*<int,\s*String>\{[^}]+\}", f"const versions = <int, String>{{{map_inner}}}", txt, count=1)
    return txt

def patch_pubspec(next_v: str, next_build: int) -> str:
    txt = PUBSPEC.read_text()
    txt, n = re.subn(r'^version:\s*\d+\.\d+\.\d+\+\d+\s*$', f'version: {next_v}+{next_build}', txt, flags=re.M)
    if n == 0:
        raise RuntimeError("pubspec version line not found")
    return txt

def patch_package_json(next_v: str) -> str:
    data = json.loads(PKG_JSON.read_text())
    data["version"] = next_v
    return json.dumps(data, indent=2) + "\n"

def patch_plist(path: Path, next_v: str) -> str | None:
    if not path.exists():
        return None
    txt = path.read_text()
    # CFBundleVersion and CFBundleShortVersionString both -> next_v
    txt = re.sub(r"(<key>CFBundleVersion</key>\s*<string>)[^<]+(</string>)", rf"\g<1>{next_v}\2", txt)
    txt = re.sub(r"(<key>CFBundleShortVersionString</key>\s*<string>)[^<]+(</string>)", rf"\g<1>{next_v}\2", txt)
    return txt

def patch_config_yml(next_v: str) -> str | None:
    if not CONFIG_YML.exists():
        return None
    txt = CONFIG_YML.read_text()
    # info.version: "x.y.z"
    txt = re.sub(r'^(\s*version:\s*)"[^"]+"', rf'\g<1>"{next_v}"', txt, flags=re.M)
    return txt

def patch_win_info(next_v: str) -> str | None:
    if not WIN_INFO.exists():
        return None
    data = json.loads(WIN_INFO.read_text())
    data.setdefault("fixed", {})["file_version"] = next_v
    data.setdefault("info", {}).setdefault("0000", {})["ProductVersion"] = next_v
    return json.dumps(data, indent=2) + "\n"

def main():
    ap = argparse.ArgumentParser(description="Bump FuseItAll version + build across all mirrors")
    g = ap.add_mutually_exclusive_group()
    g.add_argument("--bump", choices=["major","minor","patch"], help="increment x/y/z (build auto +1)")
    g.add_argument("--set", dest="set_version", metavar="X.Y.Z", help="set explicit version (build auto +1 unless --build)")
    ap.add_argument("--build", type=int, help="override build number (must be > current)")
    ap.add_argument("--min-peer-build", type=int, dest="min_peer_build", help="set CurrentMinPeerBuild (breaking)")
    ap.add_argument("--protocol-v", type=int, dest="protocol_v", help="set CurrentProtocolV (breaking)")
    ap.add_argument("--check", action="store_true", help="verify alignment only, no writes")
    ap.add_argument("--dry-run", action="store_true", help="print diff, no writes")
    ap.add_argument("-y","--yes", action="store_true", help="skip confirmation prompt")
    args = ap.parse_args()

    core = read_core()
    cur_v_str = core["version"]
    cur_v = parse_semver(cur_v_str)
    cur_build = core["build"]

    if args.check:
        errs = check_alignment(core)
        if errs:
            for e in errs:
                print(f"FAIL: {e}", file=sys.stderr)
            print(f"versions misaligned ({len(errs)} issues)", file=sys.stderr)
            sys.exit(1)
        print(f"versions aligned at {cur_v_str}+{cur_build}")
        sys.exit(0)

    # determine next version
    if args.set_version:
        try:
            nxt_v_t = parse_semver(args.set_version)
        except ValueError as e:
            ap.error(str(e))
        next_v = semver_str(nxt_v_t)
        if nxt_v_t <= cur_v and semver_str(nxt_v_t) != cur_v_str:
            # allow equal if only build bump? But we require version > current when --set moves version
            # If user sets same version but bumps build via --build, allow
            if not args.build or args.build <= cur_build:
                ap.error(f"--set {next_v} must be > current {cur_v_str} (or use --build >{cur_build} to only bump build)")
        if semver_str(nxt_v_t) == cur_v_str and not args.build:
            ap.error(f"--set {next_v} equals current; nothing to do (use --build to bump build only or --bump)")
    elif args.bump:
        nxt_v_t = bump_semver(cur_v, args.bump)
        next_v = semver_str(nxt_v_t)
    else:
        ap.error("need one of --bump or --set or --check")

    # determine next build
    if args.build is not None:
        next_build = args.build
        if next_build <= cur_build:
            ap.error(f"--build {next_build} must be > current {cur_build}")
    else:
        next_build = cur_build + 1

    next_min = args.min_peer_build
    next_proto = args.protocol_v

    # also handle bare --build without version change: allow version unchanged with build bump
    # already covered; if next_v == cur_v_str and --build set, we still update maps
    version_changed = next_v != cur_v_str

    print(f"Current: {cur_v_str}+{cur_build}  ->  Next: {next_v}+{next_build}", file=sys.stderr)
    if next_min is not None:
        print(f"  MinPeerBuild {core['min_peer_build']} -> {next_min} (BREAKING)", file=sys.stderr)
    if next_proto is not None:
        print(f"  ProtocolV {core['protocol_v']} -> {next_proto} (BREAKING)", file=sys.stderr)

    # prepare patches
    patches: list[tuple[Path, str]] = []
    patches.append((CORE_GO, patch_core(next_v, next_build, next_min, next_proto)))
    patches.append((DART, patch_dart(next_v, next_build, next_min, next_proto)))
    patches.append((PUBSPEC, patch_pubspec(next_v, next_build)))
    patches.append((PKG_JSON, patch_package_json(next_v)))
    for plist in [INFO_PLIST, INFO_DEV_PLIST]:
        np = patch_plist(plist, next_v)
        if np is not None:
            patches.append((plist, np))
    cfg = patch_config_yml(next_v)
    if cfg is not None:
        patches.append((CONFIG_YML, cfg))
    wi = patch_win_info(next_v)
    if wi is not None:
        patches.append((WIN_INFO, wi))

    if args.dry_run:
        for path, new_txt in patches:
            old = path.read_text() if path.exists() else ""
            if old != new_txt:
                print(f"\n--- {path.relative_to(ROOT)} ---", file=sys.stderr)
                # simple line diff
                import difflib
                for line in difflib.unified_diff(old.splitlines(), new_txt.splitlines(), fromfile="old", tofile="new", lineterm=""):
                    print(line)
        print("\n(dry-run, no files written)", file=sys.stderr)
        return

    if not args.yes:
        # require confirmation unless -y
        if not version_changed and next_build == cur_build + 1:
            pass  # still prompt? we prompt for any mutation
        resp = input(f"Apply bump {cur_v_str}+{cur_build} -> {next_v}+{next_build}? [y/N] ")
        if resp.strip().lower() not in ("y","yes"):
            print("aborted", file=sys.stderr)
            sys.exit(1)

    for path, new_txt in patches:
        old = path.read_text() if path.exists() else ""
        if old != new_txt:
            path.write_text(new_txt)
            print(f"wrote {path.relative_to(ROOT)}", file=sys.stderr)
        else:
            print(f"unchanged {path.relative_to(ROOT)}", file=sys.stderr)

    # post-check
    errs = check_alignment()
    if errs:
        for e in errs:
            print(f"WARN after write: {e}", file=sys.stderr)
        sys.exit(1)
    print(f"done: {next_v}+{next_build} aligned", file=sys.stderr)

if __name__ == "__main__":
    main()
