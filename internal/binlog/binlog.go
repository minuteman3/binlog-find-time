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
	// Create context with timeout to prevent hanging
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Get the first event timestamp
	streamer, err := syncer.StartSync(mysql.Position{Name: binlogFile, Pos: 4})
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("failed to start sync from %s: %v", binlogFile, err)
	}
	// Make sure we close the sync after we're done
	defer syncer.Close()

	// Get first event with timestamp
	var firstTimestamp uint32
	var foundTimestamp bool

	// Try to get the first timestamp
	for i := 0; i < 10; i++ { // Limit attempts to prevent infinite loop
		select {
		case <-ctx.Done():
			return time.Time{}, time.Time{}, fmt.Errorf("timeout getting first event timestamp for %s", binlogFile)
		default:
			ev, err := streamer.GetEvent(ctx)
			if err != nil {
				return time.Time{}, time.Time{}, fmt.Errorf("failed to get event: %v", err)
			}

			// Check for rotation event which might indicate we're reading the wrong file
			if ev.Header.EventType == replication.ROTATE_EVENT {
				rotateEvent := ev.Event.(*replication.RotateEvent)
				nextFile := string(rotateEvent.NextLogName)
				if nextFile != binlogFile {
					log.Printf("Detected rotation from %s to %s", binlogFile, nextFile)
					// If this is just the start event pointing to itself, continue
					if i == 0 && nextFile == binlogFile {
						continue
					}
				}
			}

			// Skip events with no timestamp (like FORMAT_DESCRIPTION)
			if ev.Header.Timestamp > 0 {
				firstTimestamp = ev.Header.Timestamp
				foundTimestamp = true
				goto found // Use goto instead of break to clearly exit the outer loop
			}
		}
	}

found:

	if !foundTimestamp {
		return time.Time{}, time.Time{}, fmt.Errorf("no events with timestamp found in %s", binlogFile)
	}

	// For the last event, we need to seek to the end
	// This requires reading all events, which could be optimized with more knowledge of the binlog format
	var lastTimestamp uint32 = firstTimestamp

	// Create a channel to signal completion
	done := make(chan struct{})

	// Use a goroutine to read events with timeout
	go func() {
		defer close(done)

		// Limit the number of events to read
		for i := 0; i < 1000; i++ {
			select {
			case <-ctx.Done():
				return
			default:
				ev, err := streamer.GetEvent(ctx)
				if err != nil {
					// End of file or other error
					return
				}

				if ev.Header.Timestamp > 0 {
					lastTimestamp = ev.Header.Timestamp
				}
			}
		}
	}()

	// Wait for completion or timeout
	select {
	case <-done:
		// Reading completed normally
	case <-ctx.Done():
		log.Printf("Timeout reading events from %s, using available timestamps", binlogFile)
	}

	// Convert Unix timestamps to time.Time
	startTime := time.Unix(int64(firstTimestamp), 0)
	endTime := time.Unix(int64(lastTimestamp), 0)

	return startTime, endTime, nil
}

// BinarySearchBinlogs performs a binary search on binlog files to find which contains the target timestamp
func BinarySearchBinlogs(syncerConfig replication.BinlogSyncerConfig, binlogFiles []string, targetTime time.Time) (string, bool) {
	if len(binlogFiles) == 0 {
		log.Printf("Warning: No binlog files provided")
		return "", false
	}

	log.Printf("Searching through %d binlog files for timestamp %s", len(binlogFiles), targetTime.Format("2006-01-02 15:04:05"))

	// Track files that we've checked successfully
	validFiles := make(map[string]struct{})
	timeRanges := make(map[string]struct{ start, end time.Time })

	// If only one file, check if it contains the target time
	if len(binlogFiles) == 1 {
		syncer := replication.NewBinlogSyncer(syncerConfig)
		start, end, err := GetTimeRangeForBinlog(syncer, binlogFiles[0])
		if err != nil {
			log.Printf("Warning: Could not get time range for %s: %v", binlogFiles[0], err)
			return binlogFiles[0], false
		}

		log.Printf("Binlog %s has time range: %s to %s",
			binlogFiles[0],
			start.Format("2006-01-02 15:04:05"),
			end.Format("2006-01-02 15:04:05"))

		validFiles[binlogFiles[0]] = struct{}{}
		timeRanges[binlogFiles[0]] = struct{ start, end time.Time }{start, end}

		if !targetTime.Before(start) && !targetTime.After(end) {
			return binlogFiles[0], true
		}

		return binlogFiles[0], false
	}

	// Binary search
	left, right := 0, len(binlogFiles)-1
	var errorCount int

	for left <= right {
		mid := left + (right-left)/2

		// Check if we already processed this file
		if _, exists := timeRanges[binlogFiles[mid]]; !exists {
			// Create new syncer for each file to avoid "Sync is running" errors
			syncer := replication.NewBinlogSyncer(syncerConfig)
			start, end, err := GetTimeRangeForBinlog(syncer, binlogFiles[mid])
			if err != nil {
				log.Printf("Warning: Could not get time range for %s: %v", binlogFiles[mid], err)
				errorCount++
				// If we've had too many errors, return what we have
				if errorCount > 3 {
					log.Printf("Too many errors encountered. Stopping search.")
					break
				}

				// Try to continue with the search
				if mid > 0 {
					right = mid - 1
				} else {
					left = mid + 1
				}
				continue
			}

			log.Printf("Binlog %s has time range: %s to %s",
				binlogFiles[mid],
				start.Format("2006-01-02 15:04:05"),
				end.Format("2006-01-02 15:04:05"))

			validFiles[binlogFiles[mid]] = struct{}{}
			timeRanges[binlogFiles[mid]] = struct{ start, end time.Time }{start, end}
		}

		timeRange := timeRanges[binlogFiles[mid]]
		start, end := timeRange.start, timeRange.end

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
	if len(validFiles) > 0 {
		// Find the closest valid file that's before the target time
		var closestFile string
		var closestEnd time.Time

		for file := range validFiles {
			timeRange := timeRanges[file]
			if !targetTime.Before(timeRange.end) {
				if closestFile == "" || timeRange.end.After(closestEnd) {
					closestFile = file
					closestEnd = timeRange.end
				}
			}
		}

		if closestFile != "" {
			return closestFile, false
		}
	}

	// If no match found and we have files, return the first file
	if len(binlogFiles) > 0 {
		return binlogFiles[0], false
	}

	return "", false
}
