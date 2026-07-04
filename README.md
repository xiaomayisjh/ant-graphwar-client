# Graphwar Desktop Open

Open-source Wails desktop client for Graphwar. This version keeps networking,
room hosting, chat translation, Graphwar I/II connection support, and manual
function input.

This package intentionally excludes automatic solver features, auto-play,
target picking, and trajectory fitting UI/code.

## Local Build

Requirements:

- Go 1.23 or newer
- Wails v2.12.0
- Python 3.10 or newer

Build for the current platform:

```sh
python scripts/build.py --clean
```

Build Windows amd64 explicitly:

```sh
python scripts/build.py --platform windows/amd64 --clean
```

Artifacts are written to `build/bin/`.

## GitHub Actions

The workflow at `.github/workflows/manual-build.yml` is manual only. Open the
Actions tab, choose `Manual Build`, click `Run workflow`, and it builds:

- `windows/amd64`
- `linux/amd64`
- `darwin/amd64`
