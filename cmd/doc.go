// Command binlog-find-time is a tool to find MySQL binary log files containing a specific timestamp.
//
// It connects to a MySQL server, retrieves the list of binary log files, and uses
// binary search to efficiently find which file contains events for the specified time.
//
// Usage:
//
//	binlog-find-time --host=localhost --port=3306 --user=root
//	                --password=secret --timestamp="2023-04-01 12:30:45"
package main
