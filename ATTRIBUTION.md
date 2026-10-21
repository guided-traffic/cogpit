# Attribution

Third-party material in this repository, one row per item, added in the same change as the
material ([conventions](docs/developer/conventions.md#clean-room)).

| Path | Title | Author | Source | Licence |
|---|---|---|---|---|
| `third_party/fyne-terminal/` | fyne-io/terminal at commit `46cd12724ff3`, with Cogpit's patches listed in its `PATCHES.md` | Fyne.io developers | https://github.com/fyne-io/terminal | BSD-3-Clause (`third_party/fyne-terminal/LICENSE`) |
| `AGENTS.md`, `ROADMAP.md`, `docs/developer/*`, `docs/parity/*`, `docs/security/*` | Structure, method and some rule text adapted from PhotoCraft's documentation (`AGENTS.md`, `ROADMAP.md`, `docs/`) at commit `67e3d9be9f2a` | ArtCraft Team and the PhotoCraft contributors | https://github.com/storytold/photocraft | MIT (PhotoCraft is MIT OR Apache-2.0; used under MIT, notice below) |
| `internal/model/k8seol/releases.json`, and the copy each release's pipeline fetches and builds in | The Kubernetes release schedule: every minor's end of life, the maintained minors' release day and maintenance mode, the ended minors' final patch, from `data/releases/schedule.yaml` and `data/releases/eol.yaml` as of the day the file names (`fetched`); changed: merged into one table, reduced to these fields, written as JSON by `hack/k8seol` ([D-91](docs/developer/decisions.md#d-91-the-kubernetes-version-and-upstream-end-of-life)) | The Kubernetes Authors | https://github.com/kubernetes/website (`data/releases/`), shown on https://kubernetes.io/releases/ | CC BY 4.0 (https://creativecommons.org/licenses/by/4.0/) |
| `internal/ui/fyneui/termview/termview.go` (and `internal/ui/fyneui/theme.go` until 2026-10-21, [D-109](docs/developer/decisions.md#d-109-dark-cockpit-cogpits-design-language)) | The colours of the Solarized Dark and Solarized Light themes (`theme-solarized-dark`, `theme-solarized-light`, VS Code 1.140.0, extension version 10.0.0), copied once into Cogpit's theme tokens and the terminal's sixteen colours, a few adjusted for contrast ([D-22](docs/developer/decisions.md#d-22-colour-theme)); the values are Solarized's palette | VS Code: Microsoft Corporation and contributors. Solarized: Ethan Schoonover | https://github.com/microsoft/vscode (`extensions/theme-solarized-dark`, `extensions/theme-solarized-light`), https://github.com/altercation/solarized | MIT (both, notices below) |

Facts about Lens Desktop in `docs/parity/` are descriptions of its behaviour with links to its
documentation. No code, text or asset of Lens Desktop, OpenLens or Freelens is used
([D-12](docs/developer/decisions.md#d-12-parity-sources)).

## PhotoCraft licence notice

```text
MIT License

Copyright (c) 2026 ArtCraft Team and the PhotoCraft contributors

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

## Solarized and VS Code licence notices

The colours of Solarized Dark and Solarized Light are Ethan Schoonover's palette, as the Solarized
themes of VS Code colour VS Code's interface. Both are MIT licensed; the MIT text is the one
above, with these copyright lines in place of PhotoCraft's:

```text
Copyright (c) 2011 Ethan Schoonover
Copyright (c) Microsoft Corporation. All rights reserved.
```
