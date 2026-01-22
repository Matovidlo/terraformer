package providerwrapper //nolint

import (
	"testing"
)

// TestIgnoredAttributes tests readObjBlocks functionality which was removed
// during Phase 1 migration to gRPC provider protocol.
// TODO: Reimplement this test for the new tfplugin6-based schema system
func TestIgnoredAttributes(t *testing.T) {
	t.Skip("Test disabled - readObjBlocks method removed during provider wrapper migration")
}
