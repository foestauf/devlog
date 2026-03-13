package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/foestauf/devlog/internal/broker"
	"github.com/foestauf/devlog/internal/client"
	"github.com/foestauf/devlog/internal/config"
	"github.com/foestauf/devlog/internal/ipc"
	"github.com/foestauf/devlog/internal/mcp"
	"github.com/foestauf/devlog/internal/protocol"
	"github.com/foestauf/devlog/internal/storage"
	"github.com/spf13/cobra"
)

var version = "dev"

var rootCmd = &cobra.Command{
	Use:   "devlog",
	Short: "Local development log broker",
	Long:  "DevLog captures stdout/stderr from dev processes, persists to SQLite, and exposes structured queries via CLI and MCP tools.",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		var err error
		cfg, err = config.Load(flagDataDir)
		return err
	},
}

var runCmd = &cobra.Command{
	Use:                "run",
	Short:              "Run a command and capture its logs",
	Long:               "Wraps a command, captures stdout/stderr, and streams logs to the broker. The broker is started automatically if not already running.",
	Example:            "  devlog run --service api -- go run ./cmd/server\n  devlog run --service frontend -- npm start",
	DisableFlagParsing: false,
	Args:               cobra.MinimumNArgs(1),
	RunE:               runCommand,
}

var brokerCmd = &cobra.Command{
	Use:    "broker",
	Short:  "Run the broker (internal, used for auto-start)",
	Hidden: true,
	RunE:   brokerCommand,
}

var recentCmd = &cobra.Command{
	Use:     "recent [service]",
	Short:   "Show recent logs for a service",
	Example: "  devlog recent api\n  devlog recent api -n 50",
	Args:    cobra.ExactArgs(1),
	RunE:    recentCommand,
}

var searchCmd = &cobra.Command{
	Use:     "search [service] [query]",
	Short:   "Search logs for a service",
	Example: "  devlog search api \"connection refused\"\n  devlog search api timeout --since 10m",
	Args:    cobra.ExactArgs(2),
	RunE:    searchCommand,
}

var errorsCmd = &cobra.Command{
	Use:     "errors [service]",
	Short:   "Show error logs for a service",
	Example: "  devlog errors api\n  devlog errors api --since 30m",
	Args:    cobra.ExactArgs(1),
	RunE:    errorsCommand,
}

var sessionsCmd = &cobra.Command{
	Use:     "sessions [service]",
	Short:   "List sessions",
	Example: "  devlog sessions\n  devlog sessions api",
	Args:    cobra.MaximumNArgs(1),
	RunE:    sessionsCommand,
}

var servicesCmd = &cobra.Command{
	Use:     "services",
	Short:   "List all known services",
	Example: "  devlog services",
	RunE:    servicesCommand,
}

var mcpCmd = &cobra.Command{
	Use:   "mcp-server",
	Short: "Run the MCP server (stdio transport)",
	Long:  "Starts the MCP server which communicates via stdin/stdout JSON-RPC for AI agent integration.",
	RunE:  mcpServerCommand,
}

var statusCmd = &cobra.Command{
	Use:     "status",
	Short:   "Show broker status",
	Example: "  devlog status",
	RunE:    statusCommand,
}

var initCmd = &cobra.Command{
	Use:     "init",
	Short:   "Create a .devlog.yaml config file in the current directory",
	Example: "  devlog init",
	RunE:    initCommand,
}

var pruneCmd = &cobra.Command{
	Use:     "prune",
	Short:   "Remove old log events and sessions",
	Example: "  devlog prune\n  devlog prune --days 7",
	RunE:    pruneCommand,
}

var (
	serviceName string
	limit       int
	since       string
	flagDataDir string
	pruneDays   int
	cfg         *config.Config
)

func init() {
	runCmd.Flags().StringVar(&serviceName, "service", "", "Service name for log tagging (required)")
	runCmd.MarkFlagRequired("service")

	recentCmd.Flags().IntVarP(&limit, "limit", "n", 100, "Number of recent lines to show")

	errorsCmd.Flags().StringVar(&since, "since", "1h", "Show errors since duration (e.g. 10m, 1h)")

	searchCmd.Flags().StringVar(&since, "since", "", "Search since duration (optional)")

	rootCmd.Version = version
	rootCmd.PersistentFlags().StringVar(&flagDataDir, "data-dir", "", "Override data directory path")

	pruneCmd.Flags().IntVar(&pruneDays, "days", 0, "Max age in days (default: from config)")

	rootCmd.AddCommand(runCmd, brokerCmd, mcpCmd, recentCmd, searchCmd, errorsCmd, sessionsCmd, servicesCmd, statusCmd, initCmd, pruneCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runCommand(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	exitCode, err := client.Run(ctx, serviceName, cfg.DataDir, args)
	if err != nil {
		return err
	}
	os.Exit(exitCode)
	return nil
}

func brokerCommand(cmd *cobra.Command, args []string) error {
	b, err := broker.New(cfg.DataDir,
		broker.WithRetention(cfg.Retention.MaxAgeDays, cfg.Retention.IntervalH),
	)
	if err != nil {
		return err
	}
	if err := b.Start(); err != nil {
		return err
	}
	// Block until the signal handler calls Stop.
	b.Wait()
	return nil
}

func recentCommand(cmd *cobra.Command, args []string) error {
	resp, err := queryBroker(&protocol.QueryMessage{
		Type:    protocol.TypeQuery,
		Command: "recent",
		Service: args[0],
		Limit:   limit,
	})
	if err != nil {
		return err
	}
	if resp.Error != "" {
		return fmt.Errorf("broker: %s", resp.Error)
	}
	for _, e := range resp.Events {
		printLogEvent(e)
	}
	return nil
}

func searchCommand(cmd *cobra.Command, args []string) error {
	sinceMs := parseSince(since)
	resp, err := queryBroker(&protocol.QueryMessage{
		Type:    protocol.TypeQuery,
		Command: "search",
		Service: args[0],
		Query:   args[1],
		Since:   sinceMs,
	})
	if err != nil {
		return err
	}
	if resp.Error != "" {
		return fmt.Errorf("broker: %s", resp.Error)
	}
	for _, e := range resp.Events {
		printLogEvent(e)
	}
	return nil
}

func errorsCommand(cmd *cobra.Command, args []string) error {
	sinceMs := parseSince(since)
	resp, err := queryBroker(&protocol.QueryMessage{
		Type:    protocol.TypeQuery,
		Command: "errors",
		Service: args[0],
		Since:   sinceMs,
	})
	if err != nil {
		return err
	}
	if resp.Error != "" {
		return fmt.Errorf("broker: %s", resp.Error)
	}
	for _, e := range resp.Events {
		printLogEvent(e)
	}
	return nil
}

func sessionsCommand(cmd *cobra.Command, args []string) error {
	service := ""
	if len(args) > 0 {
		service = args[0]
	}
	resp, err := queryBroker(&protocol.QueryMessage{
		Type:    protocol.TypeQuery,
		Command: "sessions",
		Service: service,
	})
	if err != nil {
		return err
	}
	if resp.Error != "" {
		return fmt.Errorf("broker: %s", resp.Error)
	}
	for _, s := range resp.Sessions {
		endStr := "active"
		if s.EndedAt != nil {
			endStr = time.UnixMilli(*s.EndedAt).Format("15:04:05")
		}
		fmt.Printf("[%s] session=%s pid=%d cmd=%q started=%s ended=%s\n",
			s.Service, s.ID, s.PID, s.Command,
			time.UnixMilli(s.StartedAt).Format("15:04:05"), endStr)
	}
	return nil
}

func servicesCommand(cmd *cobra.Command, args []string) error {
	resp, err := queryBroker(&protocol.QueryMessage{
		Type:    protocol.TypeQuery,
		Command: "services",
	})
	if err != nil {
		return err
	}
	if resp.Error != "" {
		return fmt.Errorf("broker: %s", resp.Error)
	}
	for _, s := range resp.Services {
		fmt.Println(s)
	}
	return nil
}

func mcpServerCommand(cmd *cobra.Command, args []string) error {
	srv := mcp.NewServer(cfg.DataDir, version)
	return srv.Run()
}

func statusCommand(cmd *cobra.Command, args []string) error {
	socketPath := filepath.Join(cfg.DataDir, "devlog.sock")
	if !client.IsSocketAvailable(socketPath) {
		fmt.Println("broker: not running")
		return nil
	}

	// Query rich status from the broker.
	conn, err := ipc.Dial(socketPath, 5*time.Second)
	if err != nil {
		fmt.Println("broker: not running")
		return nil
	}
	defer conn.Close()

	enc := protocol.NewEncoder(conn)
	dec := protocol.NewDecoder(conn)

	if err := enc.Encode(&protocol.QueryMessage{
		Type:    protocol.TypeQuery,
		Command: "status",
	}); err != nil {
		return fmt.Errorf("send status query: %w", err)
	}

	var resp protocol.StatusResponse
	if err := dec.Decode(&resp); err != nil {
		return fmt.Errorf("read status response: %w", err)
	}

	fmt.Println("broker: running")
	fmt.Printf("  pid:             %d\n", resp.PID)
	fmt.Printf("  uptime:          %s\n", formatDuration(resp.UptimeSeconds))
	fmt.Printf("  active sessions: %d\n", resp.ActiveSessions)
	fmt.Printf("  event queue:     %d\n", resp.QueueDepth)

	if len(resp.Services) > 0 {
		fmt.Printf("  services:        %s\n", strings.Join(resp.Services, ", "))
		for svc, count := range resp.CacheEntries {
			fmt.Printf("    %s: %d cached entries\n", svc, count)
		}
	} else {
		fmt.Println("  services:        (none)")
	}

	return nil
}

func initCommand(cmd *cobra.Command, args []string) error {
	const template = `# DevLog project configuration.
# Place this file in your project root as .devlog.yaml

# data_dir: ~/.devlog            # Override data directory
# detect_levels: true            # Auto-detect log levels from output

# retention:
#   max_age_days: 30             # Delete events older than this
#   interval_hours: 1            # How often to run automatic pruning
`
	path := ".devlog.yaml"
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf(".devlog.yaml already exists")
	}
	if err := os.WriteFile(path, []byte(template), 0644); err != nil {
		return err
	}
	fmt.Println("Created .devlog.yaml")
	return nil
}

func pruneCommand(cmd *cobra.Command, args []string) error {
	days := cfg.Retention.MaxAgeDays
	if pruneDays > 0 {
		days = pruneDays
	}

	dbPath := filepath.Join(cfg.DataDir, "devlog.db")
	store, err := storage.New(dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	deleted, err := store.PruneByAge(days)
	if err != nil {
		return err
	}
	fmt.Printf("Pruned %d events older than %d days\n", deleted, days)

	if err := store.Checkpoint(); err != nil {
		return fmt.Errorf("WAL checkpoint: %w", err)
	}
	return nil
}

func formatDuration(seconds float64) string {
	d := time.Duration(seconds * float64(time.Second))
	if d < time.Minute {
		return fmt.Sprintf("%.0fs", d.Seconds())
	}
	if d < time.Hour {
		return fmt.Sprintf("%.0fm%.0fs", d.Minutes(), d.Seconds()-d.Truncate(time.Minute).Seconds())
	}
	hours := int(d.Hours())
	mins := int(d.Minutes()) % 60
	return fmt.Sprintf("%dh%dm", hours, mins)
}

// queryBroker connects to the broker, sends a query, and returns the response.
func queryBroker(msg *protocol.QueryMessage) (*protocol.QueryResponse, error) {
	socketPath := filepath.Join(cfg.DataDir, "devlog.sock")
	conn, err := ipc.Dial(socketPath, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("cannot connect to broker (is it running?): %w", err)
	}
	defer conn.Close()

	enc := protocol.NewEncoder(conn)
	dec := protocol.NewDecoder(conn)

	if err := enc.Encode(msg); err != nil {
		return nil, fmt.Errorf("send query: %w", err)
	}

	var resp protocol.QueryResponse
	if err := dec.Decode(&resp); err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	return &resp, nil
}

func printLogEvent(e protocol.LogEvent) {
	ts := time.UnixMilli(e.Timestamp).Format("15:04:05.000")
	stream := ""
	if e.Stream == "stderr" {
		stream = " [ERR]"
	}
	fmt.Printf("%s %s%s: %s\n", ts, e.Service, stream, e.Message)
}

func parseSince(s string) string {
	if s == "" {
		return ""
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return ""
	}
	ms := time.Now().Add(-d).UnixMilli()
	return strconv.FormatInt(ms, 10)
}
