package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/go-ini/ini"
	"github.com/go-mysql-org/go-mysql/replication"

	"github.com/minuteman3/binlog-find-time/internal/binlog"
)

const defaultConfigFile = ".binlog-find-time.ini"

type config struct {
	Host      string
	Port      int
	User      string
	Password  string
	Timestamp string
}

func printHelp() {
	helpText := `
Binlog Find Time - Find MySQL binlog file containing a specific timestamp

Usage:
  binlog-find-time [flags]

Flags:
  --host=HOST           MySQL host (default: localhost)
  --port=PORT           MySQL port (default: 3306)
  --user=USER           MySQL user (default: root)
  --password=PASSWORD   MySQL password
  --timestamp=TIME      Timestamp to search for (format: YYYY-MM-DD HH:MM:SS)
  --config=FILE         Path to configuration file (default: .binlog-find-time.ini)
  --help                Display this help message

Configuration file format (.ini):
  [mysql]
  host = localhost
  port = 3306
  user = root
  password = secret

  [search]
  timestamp = 2023-04-01 12:30:45

Example:
  binlog-find-time --timestamp="2023-04-01 12:30:45"
  binlog-find-time --config=my-config.ini
  binlog-find-time --host=db.example.com --port=3306 --user=binlog --password=secret --timestamp="2023-04-01 12:30:45"
`
	fmt.Println(helpText)
}

func loadConfig(filepath string) (*config, error) {
	cfg := &config{
		Host: "localhost",
		Port: 3306,
		User: "root",
	}

	// Check if config file exists
	if _, err := os.Stat(filepath); err == nil {
		iniFile, err := ini.Load(filepath)
		if err != nil {
			return nil, fmt.Errorf("failed to load config file: %v", err)
		}

		// MySQL section
		mysqlSection := iniFile.Section("mysql")
		if mysqlSection != nil {
			cfg.Host = mysqlSection.Key("host").MustString(cfg.Host)
			cfg.Port = mysqlSection.Key("port").MustInt(cfg.Port)
			cfg.User = mysqlSection.Key("user").MustString(cfg.User)
			cfg.Password = mysqlSection.Key("password").MustString(cfg.Password)
		}

		// Search section
		searchSection := iniFile.Section("search")
		if searchSection != nil {
			cfg.Timestamp = searchSection.Key("timestamp").String()
		}
	}

	return cfg, nil
}

func main() {
	// Define command line flags
	configFile := flag.String("config", getDefaultConfigPath(), "Path to configuration file")
	help := flag.Bool("help", false, "Display help message")
	mysqlHost := flag.String("host", "", "MySQL host")
	mysqlPort := flag.Int("port", 0, "MySQL port")
	mysqlUser := flag.String("user", "", "MySQL user")
	mysqlPass := flag.String("password", "", "MySQL password")
	timestamp := flag.String("timestamp", "", "Timestamp to search for (format: YYYY-MM-DD HH:MM:SS)")
	flag.Parse()

	// Check if help flag is set or no arguments provided
	if *help || len(os.Args) == 1 {
		printHelp()
		os.Exit(0)
	}

	// Load config from file
	cfg, err := loadConfig(*configFile)
	if err != nil {
		log.Fatalf("Error loading config: %v", err)
	}

	// Override config with command line flags if provided
	if *mysqlHost != "" {
		cfg.Host = *mysqlHost
	}
	if *mysqlPort != 0 {
		cfg.Port = *mysqlPort
	}
	if *mysqlUser != "" {
		cfg.User = *mysqlUser
	}
	if *mysqlPass != "" {
		cfg.Password = *mysqlPass
	}
	if *timestamp != "" {
		cfg.Timestamp = *timestamp
	}

	// Validate timestamp
	if cfg.Timestamp == "" {
		log.Fatal("Timestamp is required. Use --timestamp flag or set in config file.")
	}

	// Parse the timestamp
	targetTime, err := time.Parse("2006-01-02 15:04:05", cfg.Timestamp)
	if err != nil {
		log.Fatalf("Invalid timestamp format: %v", err)
	}

	// Configure MySQL connection
	syncerCfg := replication.BinlogSyncerConfig{
		ServerID: 100,
		Flavor:   "mysql",
		Host:     cfg.Host,
		Port:     uint16(cfg.Port),
		User:     cfg.User,
		Password: cfg.Password,
	}

	syncer := replication.NewBinlogSyncer(syncerCfg)
	defer syncer.Close()

	// Get list of binlog files
	binlogFiles, err := binlog.GetBinlogFiles(syncerCfg)
	if err != nil {
		log.Fatalf("Failed to get binlog files: %v", err)
	}

	if len(binlogFiles) == 0 {
		log.Fatal("No binlog files found")
	}

	// Binary search for the binlog file
	binlogFile, exactMatch := binlog.BinarySearchBinlogs(syncer, binlogFiles, targetTime)

	fmt.Printf("Target time: %s\n", targetTime.Format("2006-01-02 15:04:05"))

	if exactMatch {
		fmt.Printf("Found exact match in binlog file: %s\n", binlogFile)
		os.Exit(0)
	} else if binlogFile != "" {
		fmt.Printf("Closest binlog file containing or preceding the timestamp: %s\n", binlogFile)
		os.Exit(0)
	} else {
		fmt.Println("No binlog containing the target timestamp was found")
		os.Exit(1)
	}
}

// getDefaultConfigPath returns the path to the default config file in the user's home directory
func getDefaultConfigPath() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return defaultConfigFile
	}
	return filepath.Join(homeDir, defaultConfigFile)
}
