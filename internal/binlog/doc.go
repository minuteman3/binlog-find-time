// Package binlog provides utilities for working with MySQL binary logs.
//
// It includes functionality to search through binlog files to find which file
// contains events for a specific timestamp. The package uses binary search
// for efficient searching through multiple binlog files.
package binlog
