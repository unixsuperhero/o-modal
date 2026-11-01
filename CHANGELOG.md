# Changelog

## v0.2.0 (2026-11-01)

- changed: renamed `o-modal` to `o-modes`. The module path is now `ofam.dev/o-modes` (`go get ofam.dev/o-modes/modal@latest`) and the repository is `github.com/ofamtools/o-modes`. The engine package keeps its directory and name, `modal`, so code changes only its import path (`ofam.dev/o-modes/modal`). `ofam.dev/o-modal` stays resolvable, so code pinned to the v0.1.x tags keeps building.
