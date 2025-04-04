package binlog

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sort"
	"time"

	"github.com/go-mysql-org/go-mysql/mysql"
	"github.com/go-mysql-org/go-mysql/replication"
	_ "github.com/go-sql-driver/mysql" // Import MySQL driver
)

// GetBinlogFiles fetches a list of all available binlog files from MySQL
func GetBinlogFiles(cfg replication.BinlogSyncerConfig) ([]string, error) {
	// Create a connection string
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/", cfg.User, cfg.Password, cfg.Host, cfg.Port)

	// Open a connection to the database
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MySQL: %v", err)
	}
	defer func() {
		if cerr := db.Close(); cerr != nil {
			log.Printf("Error closing database connection: %v", cerr)
		}
	}()

	// Execute SHOW BINARY LOGS command
	rows, err := db.Query("SHOW BINARY LOGS")
	if err != nil {
		return nil, fmt.Errorf("failed to execute SHOW BINARY LOGS: %v", err)
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil {
			log.Printf("Error closing rows: %v", cerr)
		}
	}()

	var binlogFiles []string
	for rows.Next() {
		var filename string
		var size int64
		if err := rows.Scan(&filename, &size); err != nil {
			return nil, fmt.Errorf("failed to scan row: %v", err)
		}
		binlogFiles = append(binlogFiles, filename)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %v", err)
	}

	// Sort binlog files (they should already be sorted by the server, but just to be safe)
	sort.Strings(binlogFiles)
	return binlogFiles, nil
}

// GetTimeRangeForBinlog returns the start and end timestamps for a binlog file
func GetTimeRangeForBinlog(syncer *replication.BinlogSyncer, binlogFile string) (start, end time.Time, err error) {
	// Get the first event timestamp
	streamer, err := syncer.StartSync(mysql.Position{Name: binlogFile, Pos: 4})
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("failed to start sync from %s: %v", binlogFile, err)
	}

	// Get first event with timestamp
	var firstTimestamp uint32
	ctx := context.Background()
	for {
		ev, err := streamer.GetEvent(ctx)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("failed to get event: %v", err)
		}

		// Skip events with no timestamp (like FORMAT_DESCRIPTION)
		if ev.Header.Timestamp > 0 {
			firstTimestamp = ev.Header.Timestamp
			break
		}
	}

	// For the last event, we need to seek to the end
	// This requires reading all events, which could be optimized with more knowledge of the binlog format
	var lastTimestamp uint32 = firstTimestamp

	for {
		ev, err := streamer.GetEvent(ctx)
		if err != nil {
			// End of file or other error
			break
		}

		if ev.Header.Timestamp > 0 {
			lastTimestamp = ev.Header.Timestamp
		}
	}

	// Convert Unix timestamps to time.Time
	startTime := time.Unix(int64(firstTimestamp), 0)
	endTime := time.Unix(int64(lastTimestamp), 0)

	return startTime, endTime, nil
}

// BinarySearchBinlogs performs a binary search on binlog files to find which contains the target timestamp
func BinarySearchBinlogs(syncer *replication.BinlogSyncer, binlogFiles []string, targetTime time.Time) (string, bool) {
	if len(binlogFiles) == 0 {
		return "", false
	}

	// If only one file, check if it contains the target time
	if len(binlogFiles) == 1 {
		start, end, err := GetTimeRangeForBinlog(syncer, binlogFiles[0])
		if err != nil {
			log.Printf("Warning: Could not get time range for %s: %v", binlogFiles[0], err)
			return binlogFiles[0], false
		}

		if !targetTime.Before(start) && !targetTime.After(end) {
			return binlogFiles[0], true
		}

		return binlogFiles[0], false
	}

	// Binary search
	left, right := 0, len(binlogFiles)-1

	for left <= right {
		mid := left + (right-left)/2

		start, end, err := GetTimeRangeForBinlog(syncer, binlogFiles[mid])
		if err != nil {
			log.Printf("Warning: Could not get time range for %s: %v", binlogFiles[mid], err)
			// Try to continue with the search
			if mid > 0 {
				right = mid - 1
			} else {
				left = mid + 1
			}
			continue
		}

		// Target time is within this binlog's range
		if !targetTime.Before(start) && !targetTime.After(end) {
			return binlogFiles[mid], true
		}

		// Target time is before this binlog
		if targetTime.Before(start) {
			right = mid - 1
		} else {
			// Target time is after this binlog
			left = mid + 1
		}
	}

	// If we didn't find an exact match, return the closest binlog that's before the target time
	if left > 0 {
		return binlogFiles[left-1], false
	}

	return binlogFiles[0], false
}
