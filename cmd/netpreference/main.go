package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	np "github.com/LE-saber/NetPreference/internal/netpref"
)

func main() {
	if e := run(os.Args[1:]); e != nil {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"ok": false, "error": e.Error()})
		os.Exit(1)
	}
}
func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: netpreference serve|guard|status|devices|traffic|validate|apply|reload|restore|deactivate|nft-preview|version")
	}
	if args[0] == "version" || args[0] == "--version" {
		fmt.Println(np.Version)
		return nil
	}
	socket := os.Getenv("NETPREFERENCE_SOCKET")
	if socket == "" {
		socket = np.DefaultSocket
	}
	stateDir := os.Getenv("NETPREFERENCE_STATE_DIR")
	if stateDir == "" {
		stateDir = np.DefaultStateDir
	}
	if !filepath.IsAbs(socket) || !filepath.IsAbs(stateDir) {
		return fmt.Errorf("runtime paths must be absolute")
	}
	r := np.SystemRunner{}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	switch args[0] {
	case "rpc":
		return rpc(ctx, args[1:])
	case "serve":
		c, e := np.LoadConfig(ctx, r)
		if e != nil {
			return e
		}
		wanted := c.Enabled
		c.Enabled = false
		engine := np.NewEngine(c)
		m := np.NewManager(r, engine)
		m.StateDir = stateDir
		dns, e := np.ListenDNS(":1053", engine, m.Inventory.Identify)
		if e != nil {
			return e
		}
		defer dns.Close()
		control, e := np.ListenControl(socket, m)
		if e != nil {
			return e
		}
		defer control.Close()
		_ = m.Inventory.Refresh(ctx, &c)
		if _, e := os.Stat(filepath.Join(stateDir, "restored")); e == nil {
			wanted = false
		}
		if wanted {
			if e = m.Apply(ctx, false); e != nil {
				log.Printf("startup remains fail-open: %v", e)
				pending := c
				pending.Enabled = true
				m.QueueStartup(pending, e)
			}
		} else {
			if e = m.Restore(ctx, false); e != nil {
				log.Printf("startup cleanup: %v", e)
			}
		}
		m.Run(ctx)
		stop, done := context.WithTimeout(context.Background(), 12*time.Second)
		defer done()
		return m.Restore(stop, false)
	case "guard":
		np.Guard(ctx, r, socket)
		return nil
	case "nft-preview":
		c, e := np.LoadConfig(ctx, r)
		if e != nil {
			return e
		}
		c.Enabled = true
		inv := np.NewInventory(r)
		_ = inv.Refresh(ctx, &c)
		fmt.Print(np.BuildNFT(&c, inv.Hosts(), false))
		return nil
	case "status", "devices", "traffic", "validate", "apply", "reload", "restore", "deactivate":
		part, done := context.WithTimeout(ctx, 30*time.Second)
		defer done()
		b, e := np.Control(part, socket, args[0])
		if e == nil {
			fmt.Print(string(b))
			return nil
		}
		// Offline restore is essential for uninstall after daemon failure.
		if args[0] == "restore" || args[0] == "deactivate" {
			m := np.NewManager(r, np.NewEngine(np.DefaultConfig()))
			m.StateDir = stateDir
			if er := m.Restore(part, args[0] == "restore"); er != nil {
				return er
			}
			fmt.Println(`{"ok":true,"offline_recovery":true}`)
			return nil
		}
		return e
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}
func rpc(ctx context.Context, args []string) error {
	methods := map[string]any{"status": map[string]any{}, "devices": map[string]any{}, "traffic": map[string]any{}, "validate": map[string]any{}, "apply": map[string]any{}, "restore": map[string]any{}, "check_config": map[string]any{"config": ""}}
	if len(args) == 1 && args[0] == "list" {
		return json.NewEncoder(os.Stdout).Encode(methods)
	}
	if len(args) != 2 || args[0] != "call" {
		return fmt.Errorf("invalid rpc invocation")
	}
	if _, ok := methods[args[1]]; !ok {
		return fmt.Errorf("unknown RPC method")
	}
	text, e := readRPCInput(os.Stdin, args[1])
	if e != nil {
		return e
	}
	if args[1] == "check_config" {
		// Pure parse/validation. This works even with a stopped or broken daemon,
		// and never writes UCI or performs DNS/firewall commands.
		c, e := np.ParseUCI(text)
		if e != nil {
			return e
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"ok": true, "valid": true, "policies": len(c.Policies), "domain_sets": len(c.DomainSets)})
	}
	return run([]string{args[1]})
}

func readRPCInput(input io.Reader, method string) (string, error) {
	limit := int64(8192)
	if method == "check_config" {
		limit = 2 * 1024 * 1024
	}
	raw, e := io.ReadAll(io.LimitReader(input, limit+1))
	if e != nil {
		return "", e
	}
	if int64(len(raw)) > limit {
		return "", fmt.Errorf("RPC input too large")
	}
	var v map[string]any
	if len(raw) > 0 {
		if e = json.Unmarshal(raw, &v); e != nil {
			return "", e
		}
	}
	for k := range v {
		if k != "ubus_rpc_session" && !(method == "check_config" && k == "config") {
			return "", fmt.Errorf("unexpected RPC parameter %q", k)
		}
	}
	if method == "check_config" {
		text, ok := v["config"].(string)
		if !ok || text == "" {
			return "", fmt.Errorf("config must be nonempty UCI text")
		}
		return text, nil
	}
	return "", nil
}
