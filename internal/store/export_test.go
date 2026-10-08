package store

// CopyAuditRecords is migration 13, for its test.
var CopyAuditRecords = copyAuditRecords

// SetCopyPage sets how many records migration 13 copies at a time until the
// returned function restores it.
func SetCopyPage(n int) func() {
	before := copyPage
	copyPage = n
	return func() { copyPage = before }
}
