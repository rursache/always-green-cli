package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rursache/always-green-cli/internal/autostart"
	"github.com/rursache/always-green-cli/internal/bootstrap"
	"github.com/rursache/always-green-cli/internal/daemon"
	"github.com/rursache/always-green-cli/internal/importws"
	"github.com/rursache/always-green-cli/internal/paths"
	"github.com/rursache/always-green-cli/internal/store"
	"github.com/rursache/always-green-cli/internal/tui"
)

var version = "1.0.0"

// progName follows however the user invoked us, so help and hints read back
// correctly whether they typed always-green-cli or the always-green alias
func progName() string {
	if len(os.Args) > 0 {
		if base := filepath.Base(os.Args[0]); base != "" && base != "." && base != "/" {
			return base
		}
	}
	return "always-green-cli"
}

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
		exit(startCmd())
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
	case "reauth", "relogin":
		exit(reauth())
	case "autostart":
		exit(autostartCmd(os.Args[2:]))
	case "tui", "ui":
		exit(tui.Run())
	case "uninstall":
		exit(uninstall())
	default:
		fmt.Fprintf(os.Stderr, "%s: unknown command %q\n", progName(), os.Args[1])
		fmt.Fprintln(os.Stderr, "Run "+progName()+" --help")
		os.Exit(1)
	}
}

func printHelp() {
	name := progName()
	fmt.Print(name + ` ` + version + ` - keep your Slack presence active locally

Usage: ` + name + ` [command]

Commands:
  (none)       Get tokens, stay green (always on)
  start        Start the background daemon
  stop         Stop being green
  status       Show daemon and workspace state
  tui          Open the dashboard (schedules, pause, add)
  snippet      Print the Chrome console snippet (xoxc only)
  reauth       Refresh tokens Slack has expired
  autostart    Launch at login: autostart [on|off|status]
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
	if err := bootstrap.EnsureValid(st, os.Stdout); err != nil {
		return err
	}
	if err := start(); err != nil {
		return err
	}
	cfg, _ := st.Config()
	if cfg.Autostart == nil || *cfg.Autostart {
		enableAutostart()
	}
	fmt.Println()
	fmt.Println("You're green while this machine is on")
	name := progName()
	fmt.Println("  " + name + " status")
	fmt.Println("  " + name + " tui      schedules / pause")
	fmt.Println("  " + name + " stop     turn it off")
	return nil
}

// enableAutostart registers the login item on first run. It is best effort:
// staying green right now matters more than surviving a reboot, so a failure
// is reported but never blocks startup.
func enableAutostart() {
	if !autostart.Supported() || autostart.Enabled() {
		return
	}
	if err := autostart.Enable(); err != nil {
		fmt.Fprintf(os.Stderr, "note: could not set up launch at login (%v)\n", err)
		fmt.Fprintln(os.Stderr, "      run: "+progName()+" autostart on")
		return
	}
	fmt.Println("Launch at login: enabled")
}

func autostartCmd(args []string) error {
	if !autostart.Supported() {
		return autostart.ErrUnsupported
	}
	action := "status"
	if len(args) > 0 {
		action = strings.ToLower(strings.TrimSpace(args[0]))
	}
	switch action {
	case "on", "enable":
		if err := autostart.Enable(); err != nil {
			return err
		}
		if err := setAutostartPref(true); err != nil {
			return err
		}
		fmt.Println("Launch at login: enabled")
		return nil
	case "off", "disable":
		if err := autostart.Disable(); err != nil {
			return err
		}
		if err := setAutostartPref(false); err != nil {
			return err
		}
		fmt.Println("Launch at login: disabled")
		return nil
	case "status":
		if autostart.Enabled() {
			fmt.Println("Launch at login: enabled")
		} else {
			fmt.Println("Launch at login: disabled")
		}
		return nil
	default:
		return fmt.Errorf("unknown autostart action %q, use on, off or status", action)
	}
}

// setAutostartPref persists the user's explicit choice so a later bare run
// never silently re-enables a login item the user turned off
func setAutostartPref(enabled bool) error {
	st, err := store.Open()
	if err != nil {
		return err
	}
	cfg, err := st.Config()
	if err != nil {
		return err
	}
	cfg.Autostart = &enabled
	return st.SaveConfig(cfg)
}

func startCmd() error {
	st, err := store.Open()
	if err != nil {
		return err
	}
	if err := bootstrap.EnsureValid(st, os.Stdout); err != nil {
		return err
	}
	return start()
}

func reauth() error {
	st, err := store.Open()
	if err != nil {
		return err
	}
	list, err := st.Workspaces()
	if err != nil {
		return err
	}
	flagged := false
	for _, ws := range list {
		if ws.TokenInvalid {
			flagged = true
			break
		}
	}
	if !flagged {
		fmt.Println("No workspace is flagged as expired, refreshing anyway")
	}
	// nothing flagged means the user is refreshing pre-emptively, so do them all
	if err := bootstrap.Reauth(st, os.Stdout, !flagged); err != nil {
		return err
	}
	if daemon.Running() {
		if err := daemon.Reload(); err != nil {
			return fmt.Errorf("tokens saved, but the daemon did not pick them up: %w\nrun: always-green stop && always-green", err)
		}
	} else if err := start(); err != nil {
		return err
	}
	fmt.Println("Back to green")
	return nil
}

func start() error {
	if daemon.Running() {
		fmt.Println("Daemon is already running")
		if err := daemon.Reload(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not reload the daemon: %v\n", err)
		}
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
	if autostart.Supported() {
		state := "no"
		if autostart.Enabled() {
			state = "yes"
		}
		fmt.Printf("Login:   starts at login: %s\n", state)
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
	needsReauth := false
	for _, ws := range list {
		state := "idle"
		dws, live := byTeam[ws.TeamID]
		switch {
		case ws.TokenInvalid || (live && !dws.TokenValid):
			state = "tokens expired, run: always-green reauth"
			needsReauth = true
		case ws.Paused:
			state = "paused"
		case live && dws.Connected:
			state = "connected"
		case live:
			state = dws.Status
		}
		fmt.Printf("  %s: %s\n", ws.Name, state)
	}
	if needsReauth {
		fmt.Println()
		fmt.Println("Run: always-green reauth")
		os.Exit(1)
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
		res, err := importws.Save(st, f, store.SourceDesktop)
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
		if err := daemon.Reload(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not reload the daemon: %v\n", err)
		}
	}
	fmt.Printf("Done (%d added, %d refreshed)\n", added, refreshed)
	return nil
}

func uninstall() error {
	_ = daemon.Stop()
	fmt.Println("Daemon stopped")
	if autostart.Supported() {
		if err := autostart.Disable(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not remove the login item: %v\n", err)
		} else {
			fmt.Println("Launch at login removed")
		}
	}
	fmt.Println()
	fmt.Println(progName() + " is a single binary: delete it from wherever you installed it")
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
