package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/alexnthnz/distri-kv/internal/api"
	"github.com/alexnthnz/distri-kv/internal/node"
	"github.com/alexnthnz/distri-kv/internal/replication"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

const (
	defaultConfigFile = "config.yaml"
	defaultPort       = 8080
)

// Config represents the application configuration
type Config struct {
	Node node.Config `yaml:"node"`
	API  APIConfig   `yaml:"api"`
	Log  LogConfig   `yaml:"log"`
}

// APIConfig represents the API server configuration
type APIConfig struct {
	Port int `yaml:"port"`
}

// LogConfig represents the logging configuration
type LogConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

func main() {
	// Parse command line flags
	var (
		configFile = flag.String("config", defaultConfigFile, "Path to configuration file")
		nodeID     = flag.String("node-id", "", "Node ID (overrides config)")
		address    = flag.String("address", "", "Node address (overrides config)")
		port       = flag.Int("port", 0, "API port (overrides config)")
		logLevel   = flag.String("log-level", "", "Log level (overrides config)")
		help       = flag.Bool("help", false, "Show help")
	)
	flag.Parse()

	if *help {
		printUsage()
		os.Exit(0)
	}

	// Initialize logger
	logger := logrus.New()

	// Load configuration
	config, err := loadConfig(*configFile)
	if err != nil {
		logger.WithError(err).Fatal("Failed to load configuration")
	}

	// Apply command line overrides
	if *nodeID != "" {
		config.Node.NodeID = *nodeID
	}
	if *address != "" {
		config.Node.Address = *address
	}
	if *port != 0 {
		config.API.Port = *port
	}
	if *logLevel != "" {
		config.Log.Level = *logLevel
	}

	// Configure logger
	if err := configureLogger(logger, &config.Log); err != nil {
		logger.WithError(err).Fatal("Failed to configure logger")
	}

	// Validate configuration
	if err := validateConfig(config); err != nil {
		logger.WithError(err).Fatal("Invalid configuration")
	}

	logger.Infof("Starting distri-kv node %s", config.Node.NodeID)

	// Create and start the node
	kvNode, err := node.NewNode(&config.Node, logger)
	if err != nil {
		logger.WithError(err).Fatal("Failed to create node")
	}

	if err := kvNode.Start(); err != nil {
		logger.WithError(err).Fatal("Failed to start node")
	}

	// Create and start the API server
	apiServer := api.NewServer(kvNode, config.API.Port, logger)
	if err := apiServer.Start(); err != nil {
		logger.WithError(err).Fatal("Failed to start API server")
	}

	logger.Infof("distri-kv is running - Node: %s, API: :%d",
		config.Node.NodeID, config.API.Port)

	// Wait for shutdown signal
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)
	<-shutdown

	logger.Info("Shutting down distri-kv...")

	// Graceful shutdown
	if err := apiServer.Stop(); err != nil {
		logger.WithError(err).Error("Error stopping API server")
	}

	if err := kvNode.Stop(); err != nil {
		logger.WithError(err).Error("Error stopping node")
	}

	logger.Info("distri-kv stopped")
}

// loadConfig loads configuration from file
func loadConfig(configFile string) (*Config, error) {
	// Default configuration
	config := &Config{
		Node: node.Config{
			NodeID:      generateNodeID(),
			Address:     "localhost:9000",
			DataDir:     "./data",
			MaxMemItems: 1000,
			QuorumConfig: &replication.QuorumConfig{
				N: 3, // Replication factor
				R: 2, // Read quorum
				W: 2, // Write quorum
			},
			ClusterNodes: []string{},
		},
		API: APIConfig{
			Port: defaultPort,
		},
		Log: LogConfig{
			Level:  "info",
			Format: "text",
		},
	}

	// If config file doesn't exist, return default config
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		return config, nil
	}

	// Read config file
	data, err := os.ReadFile(configFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Parse YAML
	if err := yaml.Unmarshal(data, config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return config, nil
}

// configureLogger configures the logger based on configuration
func configureLogger(logger *logrus.Logger, config *LogConfig) error {
	// Set log level
	level, err := logrus.ParseLevel(config.Level)
	if err != nil {
		return fmt.Errorf("invalid log level: %w", err)
	}
	logger.SetLevel(level)

	// Set log format
	switch config.Format {
	case "json":
		logger.SetFormatter(&logrus.JSONFormatter{})
	case "text":
		logger.SetFormatter(&logrus.TextFormatter{
			FullTimestamp: true,
		})
	default:
		return fmt.Errorf("invalid log format: %s", config.Format)
	}

	return nil
}

// validateConfig validates the configuration
func validateConfig(config *Config) error {
	if config.Node.NodeID == "" {
		return fmt.Errorf("node ID is required")
	}
	if config.Node.Address == "" {
		return fmt.Errorf("node address is required")
	}
	if config.API.Port <= 0 || config.API.Port > 65535 {
		return fmt.Errorf("invalid API port: %d", config.API.Port)
	}

	// Validate quorum configuration
	if err := config.Node.QuorumConfig.ValidateQuorum(); err != nil {
		return fmt.Errorf("invalid quorum configuration: %w", err)
	}

	return nil
}

// generateNodeID generates a unique node ID
func generateNodeID() string {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}

	pid := os.Getpid()
	return fmt.Sprintf("%s-%d", hostname, pid)
}

// printUsage prints usage information
func printUsage() {
	fmt.Printf(`distri-kv - Distributed Key-Value Store

USAGE:
    %s [OPTIONS]

OPTIONS:
    -config string
        Path to configuration file (default: %s)
    -node-id string
        Node ID (overrides config)
    -address string
        Node address (overrides config)
    -port int
        API port (overrides config)
    -log-level string
        Log level: debug, info, warn, error (overrides config)
    -help
        Show this help message

EXAMPLES:
    # Start with default configuration
    %s

    # Start with custom config file
    %s -config /path/to/config.yaml

    # Start with custom node ID and port
    %s -node-id node1 -port 8081

    # Start with debug logging
    %s -log-level debug

CONFIGURATION:
    The configuration file is in YAML format. Example:

    node:
      node_id: "node1"
      address: "localhost:9000"
      data_dir: "./data/node1"
      max_mem_items: 1000
      quorum:
        n: 3  # Replication factor
        r: 2  # Read quorum
        w: 2  # Write quorum
      cluster_nodes:
        - "localhost:9001"
        - "localhost:9002"

    api:
      port: 8080

    log:
      level: "info"
      format: "text"

API ENDPOINTS:
    GET    /api/v1/kv/{key}       - Get value by key
    PUT    /api/v1/kv/{key}       - Store key-value pair
    DELETE /api/v1/kv/{key}       - Delete key
    GET    /api/v1/cluster/stats  - Get cluster statistics
    GET    /api/v1/cluster/health - Get cluster health
    GET    /api/v1/admin/stats    - Get detailed statistics

`, os.Args[0], defaultConfigFile, os.Args[0], os.Args[0], os.Args[0], os.Args[0])
}

// createExampleConfig creates an example configuration file
func createExampleConfig(filename string) error {
	config := &Config{
		Node: node.Config{
			NodeID:      "node1",
			Address:     "localhost:9000",
			DataDir:     "./data/node1",
			MaxMemItems: 1000,
			QuorumConfig: &replication.QuorumConfig{
				N: 3,
				R: 2,
				W: 2,
			},
			ClusterNodes: []string{
				"localhost:9001",
				"localhost:9002",
			},
		},
		API: APIConfig{
			Port: 8080,
		},
		Log: LogConfig{
			Level:  "info",
			Format: "text",
		},
	}

	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Create directory if it doesn't exist
	dir := filepath.Dir(filename)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Write config file
	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}
