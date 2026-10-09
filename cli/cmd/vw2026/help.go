package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/format"
	"io"
	"strings"
)

//go:generate go test -run ^TestDoc$ -update

// command is one subcommand of vw2026, or a help topic when Run is nil.
//
// Its fields are the only place the usage of a command is written. The usage
// summary, "vw2026 help <command>", "vw2026 <command> -h", and the package
// documentation in doc.go are all built from them, so they cannot disagree.
type command struct {
	// UsageLine is the one-line usage, starting with "vw2026 <name>".
	// For a help topic it is the topic name alone.
	UsageLine string
	// Short is the description shown in the usage summary, in lower case
	// without a final period.
	Short string
	// Long is the full description, written as godoc text: paragraphs,
	// tab-indented code blocks, and "  - " lists. It is printed as is by
	// "vw2026 help" and copied into doc.go.
	Long string
	// Run runs the command. It is nil for a help topic.
	Run func(cmd *command, args []string, e env) int
}

// Name is the command name: the word after "vw2026" in UsageLine.
func (c *command) Name() string {
	fields := strings.Fields(c.UsageLine)
	if len(fields) > 1 && fields[0] == "vw2026" {
		return fields[1]
	}
	return fields[0]
}

func (c *command) long() string { return strings.Trim(c.Long, "\n") }

// commands lists the commands in the order the usage summary shows them.
var commands = []*command{
	statusCommand,
	callCommand,
	waitCommand,
	launchCommand,
	versionCommand,
}

// topics lists the help topics that are not commands.
var topics = []*command{
	spoolTopic,
	exitStatusTopic,
	buildTopic,
}

// overview opens "vw2026 help" and doc.go. Its first sentence follows the
// godoc convention for commands.
const overview = `
Vw2026 sends tool calls, one at a time, to the bridge that the cli plug-in
runs inside Vectorworks 2026.

It is a primitive: each run does exactly what it is told once. It does not
tell sessions apart, take exclusive use of Vectorworks, lock across
sessions, retry, or interpret the tools; the caller does those
(docs/design.md). The bridge and vw2026 exchange files in a spool by the
protocol in docs/protocol.md.

Each command prints one line of JSON to the standard output, writes the
reason for a failure to the standard error, and reports the outcome in its
exit status (see "vw2026 help exit-status"). Flags may come before or after
the positional arguments.

To update the plug-in, the caller combines the commands (install is not
implemented yet; see docs/plugin/install-and-update.md):

	vw2026 call quit && vw2026 wait --down && vw2026 install && vw2026 launch && vw2026 wait
`

var spoolTopic = &command{
	UsageLine: "spool",
	Short:     "where the spool is",
	Long: `
The spool is the directory through which vw2026 and the bridge exchange
requests and responses. Its default is <CLI>/spool, where <CLI> is the
directory of the CLI (docs/protocol.md). The plug-in uses the same rule, so
both find the same place without searching.

The --spool flag, or the VW2026_SPOOL environment variable, points vw2026 at
another directory. That is for tests: the plug-in never reads it, so with a
production Vectorworks the bridge is then reported as not running.
`,
}

var exitStatusTopic = &command{
	UsageLine: "exit-status",
	Short:     "exit status of the commands",
	Long: `
The commands exit with

	0  success
	1  the tool reported a failure (reason on the standard error; with --raw also on the standard output)
	2  wrong usage
	3  the bridge is not running
	4  timed out (call withdrew its request; Vectorworks is running)
	5  unused (meant "protocol mismatch" up to protocol 2; the numbers are not reused)
	6  any other failure (cannot write, cannot start, cannot locate the spool)
	7  Vectorworks is running, so install or uninstall did nothing (planned)

Vectorworks can be running, holding the lock, while the plug-in defers the
requests, for example during a modal dialog or while undo is being recorded
(docs/protocol.md). Status does not tell this state apart. Call and "wait
--down" wait up to --timeout and then exit with 4.

Recover from 3 and 4 differently: 3 means Vectorworks is not running; 4 means
it is running but did not answer in time, so launching it again or
reinstalling the plug-in does not help.
`,
}

var buildTopic = &command{
	UsageLine: "build",
	Short:     "building and testing vw2026",
	Long: `
In the cli module:

	go vet ./...
	go test ./...
	go build -ldflags "-X main.version=$(git describe --always)" -o vw2026 ./cmd/vw2026

The tests exchange requests with a fake plug-in (cli/internal/fakeplugin), a
goroutine that holds the lock and writes responses to the spool.

The command definitions in cmd/vw2026 are the source of this documentation.
After changing them, regenerate doc.go:

	go generate ./cmd/vw2026
`,
}

func lookup(name string) *command {
	for _, list := range [][]*command{commands, topics} {
		for _, c := range list {
			if c.Name() == name {
				return c
			}
		}
	}
	return nil
}

// summary is the list part of the usage, shared by printUsage and doc.go.
func summary() string {
	var b strings.Builder
	b.WriteString("Usage:\n\n\tvw2026 <command> [arguments]\n\nThe commands are:\n\n")
	writeList(&b, commands)
	b.WriteString("\nUse \"vw2026 help <command>\" for more information about a command.\n\nAdditional help topics:\n\n")
	writeList(&b, topics)
	b.WriteString("\nUse \"vw2026 help <topic>\" for more information about that topic.\n")
	return b.String()
}

func writeList(w io.Writer, list []*command) {
	for _, c := range list {
		fmt.Fprintf(w, "\t%-12s %s\n", c.Name(), c.Short)
	}
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, summary())
}

// printFlagUsage is the -h output of a command.
func printFlagUsage(cmd *command, fs *flag.FlagSet) {
	w := fs.Output()
	fmt.Fprintf(w, "usage: %s\nRun \"vw2026 help %s\" for details.\n", cmd.UsageLine, cmd.Name())
	if hasFlags(fs) {
		fmt.Fprint(w, "\nflags:\n")
		fs.PrintDefaults()
	}
}

func hasFlags(fs *flag.FlagSet) bool {
	found := false
	fs.VisitAll(func(*flag.Flag) { found = true })
	return found
}

// cmdHelp prints the overview, or the description of one command or topic.
func cmdHelp(args []string, e env) int {
	switch len(args) {
	case 0:
		fmt.Fprintf(e.stdout, "%s\n\n%s", strings.Trim(overview, "\n"), summary())
		return exitOK
	case 1:
		cmd := lookup(args[0])
		if cmd == nil {
			fmt.Fprintf(e.stderr, "vw2026: unknown help topic %q (run \"vw2026 help\")\n", args[0])
			return exitUsage
		}
		if cmd.Run != nil {
			fmt.Fprintf(e.stdout, "usage: %s\n\n", cmd.UsageLine)
		}
		fmt.Fprintf(e.stdout, "%s\n", cmd.long())
		return exitOK
	default:
		fmt.Fprint(e.stderr, "usage: vw2026 help [command | topic]\n")
		return exitUsage
	}
}

// renderDoc returns the contents of doc.go: the package documentation built
// from overview, commands, and topics. Headings follow cmd/go's alldocs.go.
func renderDoc() ([]byte, error) {
	var text strings.Builder
	text.WriteString(strings.Trim(overview, "\n") + "\n\n" + summary())
	for _, list := range [][]*command{commands, topics} {
		for _, c := range list {
			fmt.Fprintf(&text, "\n# %s\n\n", heading(c.Short))
			if c.Run != nil {
				fmt.Fprintf(&text, "Usage:\n\n\t%s\n\n", c.UsageLine)
			}
			text.WriteString(c.long() + "\n")
		}
	}

	var src bytes.Buffer
	src.WriteString("// Code generated by \"go test -run ^TestDoc$ -update\"; DO NOT EDIT.\n")
	src.WriteString("// Edit the command definitions in cmd/vw2026 and run \"go generate\".\n\n")
	for _, line := range strings.Split(strings.TrimRight(text.String(), "\n"), "\n") {
		switch {
		case line == "":
			src.WriteString("//\n")
		case strings.HasPrefix(line, "\t"):
			src.WriteString("//" + line + "\n")
		default:
			src.WriteString("// " + line + "\n")
		}
	}
	src.WriteString("package main\n")
	// gofmt reformats doc comments; formatting here keeps doc.go stable under it.
	return format.Source(src.Bytes())
}

func heading(short string) string {
	return strings.ToUpper(short[:1]) + short[1:]
}
