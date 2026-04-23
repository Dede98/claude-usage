package main

import (
	"fmt"
	"os"
)

const version = "1.0.0"

func main() {
	args := os.Args[1:]

	if len(args) == 0 {
		runDisplay()
		return
	}

	cmd := args[0]
	rest := args[1:]

	switch cmd {
	case "--json":
		runJSON()
	case "--read":
		runRead()
	case "--watch":
		runWatch()
	case "--daemon":
		runDaemonMode()
	case "--status":
		runStatusMode()
	case "--version":
		fmt.Printf("claude-usage %s\n", version)
	case "--help", "-h":
		showHelp()
	case "install":
		runInstall(rest)
	case "uninstall":
		runUninstall()
	case "guard":
		runGuard(rest)
	case "statusline":
		runStatusline(rest)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", cmd)
		showHelp()
		os.Exit(1)
	}
}

func showHelp() {
	fmt.Print(`claude-usage — Monitor your Claude and Codex usage limits

USAGE
  claude-usage              Refresh providers, display formatted output
  claude-usage --json       Refresh providers, output JSON
  claude-usage --read       Display from cached file (no API call)
  claude-usage --watch      Refresh every 60s, display live
  claude-usage --daemon     Background: refresh + write to file
  claude-usage --status     One-line summary for scripting
  claude-usage --version    Show version

  claude-usage install          Install Claude Code statusline integration
  claude-usage install --daemon Also install background daemon
  claude-usage uninstall        Remove everything

  claude-usage guard --pid PID         Watch process, pause at threshold
  claude-usage guard --pid-file PATH   Watch PID from lock file
  claude-usage guard status            Show current guard state

  claude-usage statusline              Output status bar segment
  claude-usage statusline --wrap CMD   Wrap existing statusline command

HOW IT WORKS
  Claude: reads your OAuth token from macOS Keychain and makes a
  1-token API call. Codex: reads live subscription limits from the
  local Codex app-server. Claude Code statusline can be configured
  automatically; Codex uses its own native /statusline setup.

100% vibecoded. Not affiliated with Anthropic. Use at your own risk.
`)
}
