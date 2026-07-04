#!/usr/bin/env python3
from __future__ import annotations

import argparse
import os
import platform
import shutil
import subprocess
import sys
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
WAILS_VERSION = "v2.12.0"


def run(cmd: list[str], *, env: dict[str, str] | None = None) -> None:
    print("+ " + " ".join(cmd), flush=True)
    subprocess.run(cmd, cwd=ROOT, env=env, check=True)


def detect_webkit_tag() -> str | None:
    """Detect which webkit2gtk version is available on the system, return the
    matching Go build tag (``webkit2_41`` for 4.1, ``None`` for the default
    4.0).  Exits with a helpful message if neither is found."""
    versions = [
        ("webkit2gtk-4.1", "webkit2_41"),
        ("webkit2gtk-4.0", None),
    ]
    for pkg, tag in versions:
        try:
            subprocess.run(
                ["pkg-config", "--exists", pkg],
                check=True,
                capture_output=True,
            )
            return tag
        except subprocess.CalledProcessError:
            continue

    print(
        "Error: no webkit2gtk development package found.\n"
        "Install one of the following:\n"
        "  sudo apt install libwebkit2gtk-4.1-dev   (recommended)\n"
        "  sudo apt install libwebkit2gtk-4.0-dev",
        file=sys.stderr,
    )
    raise SystemExit(1)


def host_platform() -> str:
    system = platform.system().lower()
    machine = platform.machine().lower()
    if system.startswith("windows"):
        os_name = "windows"
    elif system == "darwin":
        os_name = "darwin"
    elif system == "linux":
        os_name = "linux"
    else:
        raise SystemExit(f"unsupported host OS: {platform.system()}")

    if machine in {"amd64", "x86_64"}:
        arch = "amd64"
    elif machine in {"arm64", "aarch64"}:
        arch = "arm64"
    else:
        raise SystemExit(f"unsupported host architecture: {platform.machine()}")
    return f"{os_name}/{arch}"


def wails_command() -> list[str]:
    wails = shutil.which("wails")
    if wails:
        return [wails]
    return ["go", "run", f"github.com/wailsapp/wails/v2/cmd/wails@{WAILS_VERSION}"]


def main() -> int:
    parser = argparse.ArgumentParser(description="Build Graphwar Desktop Open with Wails.")
    parser.add_argument(
        "--platform",
        default=os.environ.get("WAILS_PLATFORM") or host_platform(),
        help="Wails target platform, for example windows/amd64, linux/amd64, darwin/amd64.",
    )
    parser.add_argument("--clean", action="store_true", help="Pass -clean to Wails.")
    parser.add_argument("--test", action="store_true", help="Run go test ./... before building.")
    parser.add_argument(
        "--webview2",
        default=os.environ.get("WAILS_WEBVIEW2", ""),
        help="Windows WebView2 mode passed to -webview2, for example download, embed, browser, or error.",
    )
    parser.add_argument("--tags", default=os.environ.get("GO_TAGS", ""), help="Optional Go build tags.")
    args = parser.parse_args()

    run(["go", "mod", "download"])
    if args.test:
        run(["go", "test", "./..."])

    cmd = wails_command() + ["build", "-platform", args.platform]
    if args.clean:
        cmd.append("-clean")

    if args.platform.startswith("linux/"):
        webkit_tag = detect_webkit_tag()
        if webkit_tag:
            linux_tags = [webkit_tag]
            if args.tags:
                linux_tags.append(args.tags)
            args.tags = ",".join(linux_tags)

    if args.tags:
        cmd += ["-tags", args.tags]

    if args.webview2 and args.platform.startswith("windows/"):
        cmd += ["-webview2", args.webview2]

    env = os.environ.copy()
    env.setdefault("CGO_ENABLED", "1")
    run(cmd, env=env)
    print(f"Build output: {ROOT / 'build' / 'bin'}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
