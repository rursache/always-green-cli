package cli

import (
	"fmt"
	"os"
	"strings"

	"always-green/internal/bootstrap"
	"always-green/internal/daemon"
	"always-green/internal/importws"
	"always-green/internal/paths"
	"always-green/internal/store"
	"always-green/internal/tui"
)

const version = "0.1.0"

func Main() {
	if len(os.Args) < 2 {
		exit(runDefault())
		return
	}
	switch os.Args[1] {
	case "--version", "-V", "version":
		fmt.Println(version)
	case "--help", "-h", "help":
		printHelp()
	case "snippet":
		fmt.Println(bootstrap.ChromeSnippet)
	case "start":
		exit(start())
	case "stop":
		exit(stop())
	case "status":
		exit(status())
	case "daemon":
		if err := daemon.RunForeground(); err != nil {
			fmt.Fprintf(os.Stderr, "daemon: %v\n", err)
			os.Exit(1)
		}
	case "import":
		exit(importDesktop())
	case "tui", "ui":
		exit(tui.Run())
	case "uninstall":
		exit(uninstall())
	default:
		fmt.Fprintf(os.Stderr, "always-green: unknown command %q\n", os.Args[1])
		fmt.Fprintln(os.Stderr, "Run always-green --help")
		os.Exit(1)
	}
}

func printHelp() {
	fmt.Print(`always-green ` + version + ` - keep your Slack presence active locally

Usage: always-green [command]

Commands:
  (none)       Get tokens, stay green (always on)
  start        Start the background daemon
  stop         Stop being green
  status       Show daemon and workspace state
  tui          Open the dashboard (schedules, pause, add)
  snippet      Print the Chrome console snippet (xoxc only)
  import       Re-read workspaces from the Slack desktop app (Keychain)
  uninstall    Stop the daemon and print cleanup hints
  daemon       Run the daemon in the foreground (used internally)

Flags:
  --version    Print version
  --help       Show this help
`)
}

func runDefault() error {
	st, err := store.Open()
	if err != nil {
		return err
	}
	if err := bootstrap.Ensure(st, os.Stdout); err != nil {
		return err
	}
	if err := start(); err != nil {
		return err
	}
	fmt.Println()
	fmt.Println("You're green while this machine is on")
	fmt.Println("  always-green status")
	fmt.Println("  always-green tui      schedules / pause")
	fmt.Println("  always-green stop     turn it off")
	return nil
}

func start() error {
	if daemon.Running() {
		fmt.Println("Daemon is already running")
		return nil
	}
	fmt.Println("Starting daemon...")
	if err := daemon.Start(); err != nil {
		return err
	}
	fmt.Println("Daemon started")
	return nil
}

func stop() error {
	if !daemon.Running() {
		fmt.Println("Daemon is not running")
		return nil
	}
	fmt.Println("Stopping daemon...")
	if err := daemon.Stop(); err != nil {
		return err
	}
	fmt.Println("Daemon stopped")
	return nil
}

func status() error {
	if daemon.Running() {
		fmt.Printf("Daemon:  running (PID %d)\n", daemon.PID())
	} else {
		fmt.Println("Daemon:  not running")
	}
	st, err := store.Open()
	if err != nil {
		return err
	}
	list, err := st.Workspaces()
	if err != nil {
		return err
	}
	snap, _ := daemon.ReadStatus()
	byTeam := map[string]daemon.WorkspaceStatus{}
	for _, ws := range snap.Workspaces {
		byTeam[ws.TeamID] = ws
	}
	if len(list) == 0 {
		fmt.Println("Workspaces: none")
		return nil
	}
	fmt.Printf("\nWorkspaces (%d):\n", len(list))
	for _, ws := range list {
		state := "idle"
		if dws, ok := byTeam[ws.TeamID]; ok {
			if !dws.TokenValid {
				state = "invalid tokens (re-add workspace)"
			} else if dws.Connected {
				state = "connected"
			} else {
				state = dws.Status
			}
		} else if ws.Paused {
			state = "paused"
		}
		fmt.Printf("  %s: %s\n", ws.Name, state)
	}
	return nil
}

func importDesktop() error {
	fmt.Println("Reading the Slack desktop app...")
	fmt.Println("(macOS may ask for Keychain access to Slack Safe Storage)")
	found, err := importws.Discover()
	if err != nil {
		return err
	}
	if len(found) == 0 {
		return fmt.Errorf("no workspaces found in the Slack app")
	}
	st, err := store.Open()
	if err != nil {
		return err
	}
	var added, refreshed int
	for _, f := range found {
		res, err := importws.Save(st, f)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  skip: %v\n", err)
			continue
		}
		if res.Added {
			fmt.Printf("  added %s\n", res.Name)
			added++
		} else {
			fmt.Printf("  refreshed %s\n", res.Name)
			refreshed++
		}
	}
	if added+refreshed == 0 {
		return fmt.Errorf("none of the desktop tokens were accepted by Slack")
	}
	if daemon.Running() {
		_ = daemon.Reload()
	}
	fmt.Printf("Done (%d added, %d refreshed)\n", added, refreshed)
	return nil
}

func uninstall() error {
	_ = daemon.Stop()
	fmt.Println("Daemon stopped")
	fmt.Println()
	fmt.Println("always-green is a single binary: delete it from wherever you installed it")
	fmt.Println("Config is in " + paths.Dir())
	fmt.Println("To wipe tokens and the daemon files:")
	fmt.Println("  rm -rf " + paths.Dir())
	return nil
}

func exit(err error) {
	if err != nil {
		msg := err.Error()
		if !strings.HasSuffix(msg, "\n") {
			msg += "\n"
		}
		fmt.Fprint(os.Stderr, "error: "+msg)
		os.Exit(1)
	}
}
