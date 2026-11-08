# Editor setup: templ + gopls

The UI is `.templ` sources compiled to Go by `templ generate`. That means
**two** language servers are involved, and you need both:

- **gopls** — standard Go language server. Reads the generated `_templ.go`
  files and type-checks them against the packages they import.
- **templ lsp** — starts its own gopls process and proxies requests to it,
  adding templ-aware handling (component names, `templ`/`css` blocks,
  formatting, diagnostics inside the template markup).

`templ lsp` proxies to gopls; both required, gopls alone does not parse
`.templ`. With only gopls configured, a `.templ` file is a plain unknown
filetype and you get no completion, no diagnostics, and no go-to-definition
of templ components.

## Versions

Pin the tools to what the repo builds against, not to whatever `@latest`
resolves to on the day you set up:

| Tool | Version | Source of truth |
| --- | --- | --- |
| Go | 1.27 | `go.mod` (`go 1.27`) |
| templ | v0.3.1100 | `go.mod` (`github.com/a-h/templ v0.3.1100`) |
| gopls | v0.24.0 | current stable for the Go 1.27 toolchain |

## Install

```sh
# Check what the repo expects first
grep -E '^go |a-h/templ' go.mod

# Install the pinned versions into $(go env GOPATH)/bin
go install github.com/a-h/templ/cmd/templ@v0.3.1100
go install golang.org/x/tools/gopls@v0.24.0

# Both must resolve on PATH — the LSP launches `gopls` by name
export PATH="$(go env GOPATH)/bin:$PATH"
which go gopls templ
templ version    # v0.3.1100
gopls version    # golang.org/x/tools/gopls v0.24.0
```

If `templ version` disagrees with `go.mod`, the binary is stale: reinstall
the pinned version. A mismatch shows up as confusing "unknown component"
diagnostics because generated code and LSP disagree about the template
language.

`templ lsp --help` should print its flags; if it does not, the binary is not
the one on `PATH`.

## VS Code

1. Install the **a-h.templ** extension (`a-h.templ` on the Marketplace,
   `a-h.templ` on Open VSX for VSCodium).
2. Make sure `templ` is on the `PATH` of the process that launched VS Code —
   in a devcontainer or WSL that is the container/VM `PATH`, not the host's.
3. Add to `.vscode/settings.json` (user settings work too, but per-repo keeps
   the formatter contract with the repo):

```json
{
  "editor.formatOnSave": true,
  "[templ]": {
    "editor.defaultFormatter": "a-h.templ"
  },
  "emmet.includeLanguages": {
    "templ": "html"
  }
}
```

The extension starts `templ lsp` itself; do not add a separate `gopls`
language-server entry for `.templ` files. It spawns gopls internally and
forwards requests, so Go semantics and templ semantics both come from the one
process. Go files keep using the standard Go extension's gopls.

### When it misbehaves

Logs are the fastest signal — point them somewhere writable and reload the
window:

```json
{
  "templ.log": "/tmp/templ.log",
  "templ.goplsLog": "/tmp/gopls.log",
  "templ.goplsRPCTrace": true
}
```

`templ info` prints the resolved binary paths and versions; include its output
in any bug report.

## Neovim

`templ` is in `nvim-lspconfig`. The server command is `templ lsp`, and the
filetype must be registered or nothing attaches:

```lua
vim.filetype.add({ extension = { templ = "templ" } })

require("lspconfig").templ.setup({})
```

Keep `lspconfig.gopls` configured separately for `.go` files — gopls still
serves the generated code and the rest of the module. Optional companions
(`html`, `htmx`, `tailwindcss`) accept `filetypes = { "html", "templ" }`;
htmx attributes only activate after you type `hx-`.

Formatting on save without LSP formatting:

```lua
vim.api.nvim_create_autocmd("BufWritePre", {
  pattern = "*.templ",
  callback = function()
    vim.lsp.buf.format()
  end,
})
```

Treesitter highlighting: `:TSInstall templ`.

## Verification

From a clean shell:

```sh
templ generate          # must succeed and touch no .templ file
gopls check internal/ui/layout.templ   # not supported — expect an error
```

That second line is the point: **gopls cannot parse `.templ`**. Do not "fix"
it by pointing gopls at the templates. Instead confirm the templ server is
alive:

```sh
templ lsp -log=/tmp/templ.log   # starts, prints nothing, waits on stdin
```

or, in the editor, open `internal/ui/layout.templ`, place the cursor on
`tickerdata.Attempt` and check that go-to-definition jumps into
`internal/tickerdata/data.go`. If it does, the proxy chain is working.
