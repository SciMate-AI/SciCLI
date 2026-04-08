package permission

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCommandAllowedByPrefixRequiresAllSegmentsToMatch(t *testing.T) {
	assert.True(t, commandAllowedByPrefix("git status && go test ./...", []string{"git status", "go test"}))
	assert.False(t, commandAllowedByPrefix("git status && rm -rf tmp", []string{"git status", "go test"}))
	assert.False(t, commandAllowedByPrefix("", []string{"git status"}))
}

func TestAutoApproveSessionTracksSessionIDs(t *testing.T) {
	svc := NewPermissionService()

	assert.False(t, svc.IsAutoApproved("parent"))
	svc.AutoApproveSession("parent")

	assert.True(t, svc.IsAutoApproved("parent"))
	assert.False(t, svc.IsAutoApproved("child"))
}

func TestResolvePermissionPathUsesParentDirectory(t *testing.T) {
	path := filepath.Join("D:\\", "workspace", "logs", "runtime.log")
	assert.Equal(t, filepath.Join("D:\\", "workspace", "logs"), resolvePermissionPath(path))
}

func TestResolvePermissionPathFallsBackForDot(t *testing.T) {
	assert.NotEmpty(t, resolvePermissionPath("."))
}
