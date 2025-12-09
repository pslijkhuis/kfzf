package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/pslijkhuis/kfzf/internal/client"
	"github.com/pslijkhuis/kfzf/internal/config"
	"github.com/pslijkhuis/kfzf/internal/server"
	"github.com/spf13/cobra"
)

var (
	version = "dev"
	cfgFile string
	cfg     *config.Config

	//go:embed completion.zsh
	zshCompletionScript string
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "kfzf",
		Short: "Kubernetes completion with fzf",
		Long: `kfzf is a daemon that provides fast kubectl completions with fzf.

It watches Kubernetes resources and maintains an in-memory cache,
providing instant completions without repeatedly hitting the API server.`,
		Version: version,
	}

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: ~/.config/kfzf/config.yaml)")

	// Add commands
	rootCmd.AddCommand(serverCmd())
	rootCmd.AddCommand(completeCmd())
	rootCmd.AddCommand(containersCmd())
	rootCmd.AddCommand(portsCmd())
	rootCmd.AddCommand(labelsCmd())
	rootCmd.AddCommand(fieldValuesCmd())
	rootCmd.AddCommand(statusCmd())
	rootCmd.AddCommand(refreshCmd())
	rootCmd.AddCommand(watchCmd())
	rootCmd.AddCommand(configCmd())
	rootCmd.AddCommand(zshCompletionCmd())
	rootCmd.AddCommand(systemdCmd())
	rootCmd.AddCommand(recentCmd())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func loadConfig() *config.Config {
	if cfg != nil {
		return cfg
	}

	var err error
	if cfgFile != "" {
		cfg, err = config.LoadFrom(cfgFile)
	} else {
		cfg, err = config.Load()
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to load config: %v\n", err)
		cfg = config.DefaultConfig()
	}

	return cfg
}

func serverCmd() *cobra.Command {
	var foreground bool
	var logLevel string

	cmd := &cobra.Command{
		Use:   "server",
		Short: "Start the kfzf server",
		Long:  "Start the background server that watches Kubernetes resources",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadConfig()

			// Setup logger
			var level slog.Level
			switch logLevel {
			case "debug":
				level = slog.LevelDebug
			case "info":
				level = slog.LevelInfo
			case "warn":
				level = slog.LevelWarn
			case "error":
				level = slog.LevelError
			default:
				level = slog.LevelInfo
			}

			logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
				Level: level,
			}))

			// Check if server is already running
			c := client.NewClient(cfg)
			if c.IsServerRunning() {
				return fmt.Errorf("server is already running")
			}

			// Create and start server
			srv, err := server.NewServer(cfg, logger)
			if err != nil {
				return fmt.Errorf("failed to create server: %w", err)
			}

			// Setup signal handling
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

			go func() {
				<-sigCh
				logger.Info("received shutdown signal")
				cancel()
			}()

			if !foreground {
				fmt.Printf("Server starting on %s\n", cfg.Server.SocketPath)
			}

			return srv.Start(ctx)
		},
	}

	cmd.Flags().BoolVarP(&foreground, "foreground", "f", false, "Run in foreground")
	cmd.Flags().StringVar(&logLevel, "log-level", "info", "Log level (debug, info, warn, error)")

	return cmd
}

func completeCmd() *cobra.Command {
	var ctx string
	var namespace string
	var useFzf bool

	cmd := &cobra.Command{
		Use:   "complete <resource-type>",
		Short: "Get completions for a resource type",
		Long: `Get completions for a Kubernetes resource type.

Examples:
  kfzf complete pods
  kfzf complete pods -n kube-system
  kfzf complete deployments --fzf`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadConfig()
			c := client.NewClient(cfg)

			if !c.IsServerRunning() {
				return fmt.Errorf("server is not running. Start it with: kfzf server")
			}

			resourceType := args[0]

			if useFzf {
				result, err := c.CompleteWithFzf(ctx, namespace, resourceType, nil)
				if err != nil {
					return err
				}
				if result != "" {
					fmt.Println(result)
				}
				return nil
			}

			output, err := c.Complete(ctx, namespace, resourceType)
			if err != nil {
				return err
			}

			fmt.Print(output)
			return nil
		},
	}

	cmd.Flags().StringVarP(&ctx, "context", "c", "", "Kubernetes context (default: current)")
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "Kubernetes namespace (default: from context)")
	cmd.Flags().BoolVar(&useFzf, "fzf", false, "Pipe output through fzf")

	return cmd
}

func containersCmd() *cobra.Command {
	var ctx string
	var namespace string

	cmd := &cobra.Command{
		Use:   "containers <pod-name>",
		Short: "Get container names for a pod",
		Long: `Get container names for a pod from cache.

Examples:
  kfzf containers my-pod
  kfzf containers my-pod -n kube-system`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadConfig()
			c := client.NewClient(cfg)

			if !c.IsServerRunning() {
				return fmt.Errorf("server is not running. Start it with: kfzf server")
			}

			podName := args[0]

			output, err := c.Containers(ctx, namespace, podName)
			if err != nil {
				return err
			}

			fmt.Print(output)
			return nil
		},
	}

	cmd.Flags().StringVarP(&ctx, "context", "c", "", "Kubernetes context (default: current)")
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "Kubernetes namespace")

	return cmd
}

func portsCmd() *cobra.Command {
	var ctx string
	var namespace string
	var resourceType string

	cmd := &cobra.Command{
		Use:   "ports <resource-name>",
		Short: "Get ports for a pod or service",
		Long: `Get ports for a pod or service from cache.

Examples:
  kfzf ports my-pod
  kfzf ports my-pod -n kube-system
  kfzf ports my-service -t services -n kube-system`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadConfig()
			c := client.NewClient(cfg)

			if !c.IsServerRunning() {
				return fmt.Errorf("server is not running. Start it with: kfzf server")
			}

			resourceName := args[0]

			output, err := c.Ports(ctx, namespace, resourceType, resourceName)
			if err != nil {
				return err
			}

			fmt.Print(output)
			return nil
		},
	}

	cmd.Flags().StringVarP(&ctx, "context", "c", "", "Kubernetes context (default: current)")
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "Kubernetes namespace")
	cmd.Flags().StringVarP(&resourceType, "type", "t", "pods", "Resource type (pods or services)")

	return cmd
}

func labelsCmd() *cobra.Command {
	var ctx string
	var namespace string

	cmd := &cobra.Command{
		Use:   "labels <resource-type>",
		Short: "Get labels for a resource type",
		Long: `Get unique label key=value pairs for a resource type from cache.

Examples:
  kfzf labels pods
  kfzf labels pods -n kube-system
  kfzf labels nodes`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadConfig()
			c := client.NewClient(cfg)

			if !c.IsServerRunning() {
				return fmt.Errorf("server is not running. Start it with: kfzf server")
			}

			resourceType := args[0]

			output, err := c.Labels(ctx, namespace, resourceType)
			if err != nil {
				return err
			}

			fmt.Print(output)
			return nil
		},
	}

	cmd.Flags().StringVarP(&ctx, "context", "c", "", "Kubernetes context (default: current)")
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "Kubernetes namespace")

	return cmd
}

func fieldValuesCmd() *cobra.Command {
	var ctx string
	var namespace string

	cmd := &cobra.Command{
		Use:   "field-values <resource-type> <field-name>",
		Short: "Get field values for field selector completion",
		Long: `Get unique values for a field from cached resources.

Supported fields: metadata.name, metadata.namespace, spec.nodeName,
spec.restartPolicy, spec.schedulerName, spec.serviceAccountName,
status.phase, status.podIP, status.nominatedNodeName

Examples:
  kfzf field-values pods status.phase
  kfzf field-values pods spec.nodeName -n kube-system`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadConfig()
			c := client.NewClient(cfg)

			if !c.IsServerRunning() {
				return fmt.Errorf("server is not running. Start it with: kfzf server")
			}

			resourceType := args[0]
			fieldName := args[1]

			output, err := c.FieldValues(ctx, namespace, resourceType, fieldName)
			if err != nil {
				return err
			}

			fmt.Print(output)
			return nil
		},
	}

	cmd.Flags().StringVarP(&ctx, "context", "c", "", "Kubernetes context (default: current)")
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "Kubernetes namespace")

	return cmd
}

func statusCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show server status",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadConfig()
			c := client.NewClient(cfg)

			if !c.IsServerRunning() {
				if jsonOutput {
					fmt.Println(`{"running": false}`)
				} else {
					fmt.Println("Server is not running")
				}
				return nil
			}

			status, err := c.Status()
			if err != nil {
				return err
			}

			if jsonOutput {
				data, _ := json.MarshalIndent(status, "", "  ")
				fmt.Println(string(data))
			} else {
			fmt.Printf("Server running\n")
			fmt.Printf("  Uptime: %s\n", status.Uptime)
			fmt.Printf("  Cached resources: %d\n", status.ResourceCount)
			fmt.Printf("  Contexts:\n")
			for ctx, stats := range status.ResourceStats {
				fmt.Printf("    %s:\n", ctx)
				for resource, count := range stats {
					fmt.Printf("      %s: %d\n", resource, count)
				}
			}
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	return cmd
}

func refreshCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "refresh",
		Short: "Refresh kubeconfig",
		Long:  "Tell the server to reload the kubeconfig file",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadConfig()
			c := client.NewClient(cfg)

			if !c.IsServerRunning() {
				return fmt.Errorf("server is not running")
			}

			if err := c.Refresh(); err != nil {
				return err
			}

			fmt.Println("Kubeconfig refreshed")
			return nil
		},
	}
}

func watchCmd() *cobra.Command {
	var ctx string
	var stop bool

	cmd := &cobra.Command{
		Use:   "watch <resource-types...>",
		Short: "Start or stop watching resource types",
		Long: `Start or stop watching specific resource types.

Examples:
  kfzf watch pods deployments
  kfzf watch --stop pods`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadConfig()
			c := client.NewClient(cfg)

			if !c.IsServerRunning() {
				return fmt.Errorf("server is not running")
			}

			if stop {
				if err := c.StopWatch(ctx, args); err != nil {
					return err
				}
				fmt.Printf("Stopped watching: %v\n", args)
			} else {
				if err := c.Watch(ctx, args); err != nil {
					return err
				}
				fmt.Printf("Started watching: %v\n", args)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&ctx, "context", "c", "", "Kubernetes context")
	cmd.Flags().BoolVar(&stop, "stop", false, "Stop watching instead of starting")

	return cmd
}

func recentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "recent",
		Short: "Manage recently accessed resources",
	}

	// Get subcommand
	getCmd := &cobra.Command{
		Use:   "get <resource-type>",
		Short: "Get recently accessed resources",
		Long: `Get recently accessed resources of a given type.

Examples:
  kfzf recent get pods
  kfzf recent get pods -n kube-system
  kfzf recent get deployments`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadConfig()
			c := client.NewClient(cfg)

			if !c.IsServerRunning() {
				return fmt.Errorf("server is not running. Start it with: kfzf server")
			}

			ctx, _ := cmd.Flags().GetString("context")
			namespace, _ := cmd.Flags().GetString("namespace")
			resourceType := args[0]

			output, err := c.GetRecent(ctx, namespace, resourceType)
			if err != nil {
				return err
			}

			if output != "" {
				fmt.Print(output)
			}
			return nil
		},
	}
	getCmd.Flags().StringP("context", "c", "", "Kubernetes context")
	getCmd.Flags().StringP("namespace", "n", "", "Kubernetes namespace")

	// Record subcommand
	recordCmd := &cobra.Command{
		Use:   "record <resource-type> <resource-name>",
		Short: "Record a recently accessed resource",
		Long: `Record a resource as recently accessed.

Examples:
  kfzf recent record pods my-pod
  kfzf recent record pods my-pod -n kube-system`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadConfig()
			c := client.NewClient(cfg)

			if !c.IsServerRunning() {
				return fmt.Errorf("server is not running. Start it with: kfzf server")
			}

			ctx, _ := cmd.Flags().GetString("context")
			namespace, _ := cmd.Flags().GetString("namespace")
			resourceType := args[0]
			resourceName := args[1]

			if err := c.RecordRecent(ctx, namespace, resourceType, resourceName); err != nil {
				return err
			}

			return nil
		},
	}
	recordCmd.Flags().StringP("context", "c", "", "Kubernetes context")
	recordCmd.Flags().StringP("namespace", "n", "", "Kubernetes namespace")

	cmd.AddCommand(getCmd)
	cmd.AddCommand(recordCmd)

	return cmd
}

func configCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Configuration commands",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "path",
		Short: "Show config file path",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(config.ConfigPath())
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "init",
		Short: "Create default config file",
		RunE: func(cmd *cobra.Command, args []string) error {
			path := config.ConfigPath()
			if _, err := os.Stat(path); err == nil {
				return fmt.Errorf("config file already exists: %s", path)
			}

			cfg := config.DefaultConfig()
			if err := cfg.SaveTo(path); err != nil {
				return err
			}

			fmt.Printf("Created config file: %s\n", path)
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Show current configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadConfig()
			data, err := json.MarshalIndent(cfg, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(data))
			return nil
		},
	})

	return cmd
}

func zshCompletionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "zsh-completion",
		Short: "Generate ZSH completion script for kubectl integration",
		Long: `Generate a ZSH completion script that integrates kfzf with kubectl.

Add this to your .zshrc:
  source <(kfzf zsh-completion)

Or save to a file:
  kfzf zsh-completion > ~/.zsh/completions/_kfzf_kubectl`,
		Run: func(cmd *cobra.Command, args []string) {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s", zshCompletionScript)
		},
	}
}

func systemdCmd() *cobra.Command {
	var install bool
	var uninstall bool

	cmd := &cobra.Command{
		Use:   "systemd",
		Short: "Manage systemd user service",
		Long: `Manage kfzf as a systemd user service.

Examples:
  kfzf systemd              # Print the service file
  kfzf systemd --install    # Install and enable the service
  kfzf systemd --uninstall  # Stop and remove the service`,
		RunE: func(cmd *cobra.Command, args []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("failed to get home directory: %w", err)
			}

			serviceDir := home + "/.config/systemd/user"
			servicePath := serviceDir + "/kfzf.service"

			if uninstall {
				// Stop and disable service
				exec := func(args ...string) {
					c := execCommand("systemctl", args...)
					_ = c.Run()
				}
				exec("--user", "stop", "kfzf.service")
				exec("--user", "disable", "kfzf.service")

				if err := os.Remove(servicePath); err != nil && !os.IsNotExist(err) {
					return fmt.Errorf("failed to remove service file: %w", err)
				}

				exec("--user", "daemon-reload")
				fmt.Println("kfzf service uninstalled")
				return nil
			}

			// Get the path to kfzf binary
			kfzfPath, err := os.Executable()
			if err != nil {
				return fmt.Errorf("failed to get executable path: %w", err)
			}

			// Get KUBECONFIG from environment, default to ~/.kube/config
			kubeconfig := os.Getenv("KUBECONFIG")
			if kubeconfig == "" {
				kubeconfig = home + "/.kube/config"
			}

			// Generate service content
			serviceContent := fmt.Sprintf(systemdServiceTemplate, kfzfPath, home, kubeconfig)

			if install {
				// Create directory if needed
				if err := os.MkdirAll(serviceDir, 0755); err != nil {
					return fmt.Errorf("failed to create service directory: %w", err)
				}

				// Write service file
				if err := os.WriteFile(servicePath, []byte(serviceContent), 0644); err != nil {
					return fmt.Errorf("failed to write service file: %w", err)
				}

				// Reload and enable
				c := execCommand("systemctl", "--user", "daemon-reload")
				if err := c.Run(); err != nil {
					return fmt.Errorf("failed to reload systemd: %w", err)
				}

				c = execCommand("systemctl", "--user", "enable", "--now", "kfzf.service")
				if err := c.Run(); err != nil {
					return fmt.Errorf("failed to enable service: %w", err)
				}

				fmt.Println("kfzf service installed and started")
				fmt.Println("Check status with: systemctl --user status kfzf")
				return nil
			}

			// Just print the service file
			fmt.Print(serviceContent)
			return nil
		},
	}

	cmd.Flags().BoolVar(&install, "install", false, "Install and enable the systemd service")
	cmd.Flags().BoolVar(&uninstall, "uninstall", false, "Stop and remove the systemd service")

	return cmd
}

// execCommand is a wrapper to make exec.Command available
var execCommand = exec.Command

const systemdServiceTemplate = `[Unit]
Description=kfzf - Kubernetes completion with fzf
After=network.target

[Service]
Type=simple
ExecStart=%s server -f
Restart=on-failure
RestartSec=5
Environment="HOME=%s"
Environment="KUBECONFIG=%s"

[Install]
WantedBy=default.target
`
