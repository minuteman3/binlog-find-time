# Binlog Find Time

A Go application that uses binary search to find the MySQL binlog file containing a specific timestamp.

## Installation

```
go install github.com/minuteman3/binlog-find-time/cmd@latest
```

Or clone the repository and build it:

```
git clone https://github.com/minuteman3/binlog-find-time.git
cd binlog-find-time
go build -o binlog-finder ./cmd
```

## Usage

```
./binlog-finder --host=localhost --port=3306 --user=root --password=mysecret --timestamp="2023-04-01 12:30:45"
```

Or use a configuration file:

```
./binlog-finder --config=my-config.ini
```

Running the command without arguments will display help information.

### Command Line Parameters

- `--host`: MySQL host (default: localhost)
- `--port`: MySQL port (default: 3306)
- `--user`: MySQL user (default: root)
- `--password`: MySQL password
- `--timestamp`: Timestamp to search for, in format "YYYY-MM-DD HH:MM:SS"
- `--config`: Path to configuration file (default: ~/.binlog-find-time.ini)
- `--help`: Display help message

### Configuration File

You can use an INI configuration file like this:

```ini
[mysql]
host = localhost
port = 3306
user = root
password = secret

[search]
timestamp = 2023-04-01 12:30:45
```

By default, the tool looks for a configuration file named `.binlog-find-time.ini` in your home directory, but you can specify a different file with the `--config` flag.

## How It Works

1. Connects to the MySQL server
2. Retrieves a list of all binlog files
3. Uses binary search to efficiently find which binlog file contains the target timestamp
4. Returns the binlog file name that contains the timestamp or is closest to it

## Development

### Building

```
make build
```

### Testing

```
make test
```

### Linting

```
make lint
```

or

```
make golangci-lint
```

This tool can be useful for:
- Point-in-time recovery operations
- Debugging database issues at a specific time
- Analyzing binlog events around a particular timestamp
