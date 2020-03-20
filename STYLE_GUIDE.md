# Go Coding Style Guide

In order to keep our code looking good with lots of programmers working on it, it helps to have a "style guide", so all
the code generally looks quite similar. This doesn't mean there is only one "right way" to write code, or even that this
standard is better than your style.  But if we agree to a number of stylistic practices, it makes it much easier to read
and modify new code. Please feel free to make suggestions if there's something you would like to add or modify.

Please, before going through the rest of the document, have a read over the [syntactical style](https://github.com/uber-go/guide/blob/master/style.md)

If you wish to gain some further understanding, we recommend having a read on how to code [effective go](https://golang.org/doc/effective_go.html)


## Code Structure

Perhaps more key for code readability than good commenting is having the right structure. As a rule of thumb, try to write
in a logical order of importance, taking a little time to think how to order and divide the code such that someone could
scroll down and understand the functionality of it just as well as you do. A loose example of such order would be:
* Constants and global variables
* Main Struct
* Options (only if they are seen as critical to the struct else they should be placed in another file)
* Initialisation / Start and stop of the service
* Msgs/Events
* Public Functions (In order of most important)
* Private/helper functions
* Auxiliary structs and function (can also be above private functions or in a separate file)

## General

 * Use `gofmt` (or `goimport`) to format all code upon saving it.  (If you use VIM, check out vim-go).
 * Use a linter (see below) and generally try to keep the linter happy (where it makes sense).
 * Think about documentation, and try to leave godoc comments, when it will help new developers.
 * Every package should have a high level doc.go file to describe the purpose of that package, its main functions, and any other relevant information.
 * `TODO` should not be used. If important enough should be recorded as an issue.
 * `BUG` / `FIXME` should be used sparingly to guide future developers on some of the vulnerabilities of the code.
 * `XXX` can be used in work-in-progress (prefixed with "WIP:" on github) branches but they must be removed before approving a PR.
 * Libraries *should* panic on developer usage error.
 * Applications (e.g. clis/servers) *should* panic on unexpected unrecoverable errors and print a stack trace.

## Comments

 * Use a space after comment deliminter (ex. `// your comment`).
 * Many comments are not sentences, these should begin with a lower case letter and end without a period.
 * The first letter of sentences in comments are capitalized and ends with a period.

## Linters

These must be applied to all (go) repos.

 * [shellcheck](https://github.com/koalaman/shellcheck)
 * [golangci (covers all important linters)](https://github.com/golangci/golangci-lint)
   - See the `.golangcli.yml` file in each repo for linter configuration.

## Various

 * Reserve "Save" and "Load" for long-running persistence operations. When parsing bytes, use "Encode" or "Decode".
 * Maintain consistency across the codebase.
 * Functions that return functions should have the suffix `Fn`
 * A struct generally shouldn’t have a field named after itself, aka. this shouldn't occur:
``` golang
type middleware struct {
	middleware Middleware
}
```
 * In comments, use "iff" to mean, "if and only if".
 * Product names are capitalized, like "Tendermint", "Basecoin", "Protobuf", etc except in command lines: `tendermint --help`
 * Acronyms are all capitalized, like "RPC", "gRPC", "API".  "MyID", rather than "MyId".
 * Prefer errors.New() instead of fmt.Errorf() unless you're actually using the format feature with arguments. (otherwise it's needlessly inefficient, & it'll crap out on %*)

## Importing Libraries

Sometimes it's necessary to rename libraries to avoid naming collisions or ambiguity.

 * Use [goimports](https://godoc.org/golang.org/x/tools/cmd/goimports)
 * Separate imports into blocks - one for the standard lib, one for external libs and one for tendermint libs.
 * Here are some common library labels for consistency:
   - dbm "github.com/tendermint/tmlibs/db"
   - cmn "github.com/tendermint/tmlibs/common"
   - tmcmd "github.com/tendermint/tendermint/cmd/tendermint/commands"
   - tmcfg "github.com/tendermint/tendermint/config/tendermint"
   - tmtypes "github.com/tendermint/tendermint/types"
 * Never use anonymous imports (the `.`), for example, `tmlibs/common` or anything else.
 * tip: Use the `_` library import to import a library for initialization effects (side effects)

## Dependencies

 * Dependencies should be pinned by a release tag, or specific commit, to avoid breaking `go get` when external dependencies are updated.

## Testing

 * The first rule of testing is: we add tests to our code
 * The second rule of testing is: we add tests to our code
 * For Golang testing:
   * Make use of table driven testing where possible and not-cumbersome
     - [Inspiration](https://dave.cheney.net/2013/06/09/writing-table-driven-tests-in-go)
   * Make use of [assert](https://godoc.org/github.com/stretchr/testify/assert) and [require](https://godoc.org/github.com/stretchr/testify/require)
 * When using mocks, it is recommended to use either [testify mock](https://pkg.go.dev/github.com/stretchr/testify/mock
 ) or [Mockery](https://github.com/vektra/mockery)

## Errors

 * Ensure that errors are concise, clear and traceable.
 * Use stdlib errors package.
 * For wrapping errors, use `fmt.Errorf()` with `%w`.

## Config

 * Currently the TOML filetype is being used for config files
 * A good practice is to store the default Config file under `~/.[yourAppName]/config.toml`

## CLI

 * When implementing a CLI use [Cobra](https://github.com/spf13/cobra) and [Viper](https://github.com/spf13/viper). urfave/cli can be replace with cobra in those repos where applicable.
 * Helper messages for commands and flags must be all lowercase.
 * Instead of using pointer flags (eg. `FlagSet().StringVar`) use viper to retrieve flag values (eg. `viper.GetString`)
   - The flag key used when setting and getting the flag should always be stored in a
   variable taking the form `FlagXxx` or `flagXxx`.
   - Flag short variable descriptions should always start with a lower case charater as to remain consistent with
   the description provided in the default `--help` flag.

## Version

 * Every repo should have a version/version.go file that mimicks the tendermint core repo
 * We read the value of the constant version in our build scripts and hence it has to be a string

## Non-Golang Code

 * All non-go code (`*.proto`, `Makefile`, `*.sh`), where there is no common
   agreement on style, should be formatted according to
   [EditorConfig](http://editorconfig.org/) config:

   ```
   # top-most EditorConfig file
   root = true

   # Unix-style newlines with a newline ending every file
   [*]
   charset = utf-8
   end_of_line = lf
   insert_final_newline = true
   trim_trailing_whitespace = true

   [Makefile]
   indent_style = tab

   [*.sh]
   indent_style = tab

   [*.proto]
   indent_style = space
   indent_size = 2
   ```

   Make sure the file above (`.editorconfig`) are in the root directory of your
   repo and you have a [plugin for your
   editor](http://editorconfig.org/#download) installed.
