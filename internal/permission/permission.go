package permission

import (
	"errors"
	"path/filepath"
	"strings"
	"sync"

	"github.com/SciMate-AI/scicli/internal/config"
	"github.com/SciMate-AI/scicli/internal/pubsub"
	"github.com/google/uuid"
)

var ErrorPermissionDenied = errors.New("permission denied")

type CreatePermissionRequest struct {
	SessionID   string `json:"session_id"`
	ToolName    string `json:"tool_name"`
	Description string `json:"description"`
	Action      string `json:"action"`
	Params      any    `json:"params"`
	Path        string `json:"path"`
	Command     string `json:"command,omitempty"`
}

type PermissionRequest struct {
	ID          string `json:"id"`
	SessionID   string `json:"session_id"`
	ToolName    string `json:"tool_name"`
	Description string `json:"description"`
	Action      string `json:"action"`
	Params      any    `json:"params"`
	Path        string `json:"path"`
}

type Service interface {
	pubsub.Suscriber[PermissionRequest]
	GrantPersistant(permission PermissionRequest)
	Grant(permission PermissionRequest)
	Deny(permission PermissionRequest)
	Request(opts CreatePermissionRequest) bool
	AutoApproveSession(sessionID string)
	IsAutoApproved(sessionID string) bool
	PendingCount() int
}

type permissionService struct {
	*pubsub.Broker[PermissionRequest]

	sessionPermissions  []PermissionRequest
	pendingRequests     sync.Map
	autoApproveMu       sync.RWMutex
	autoApproveSessions map[string]struct{}
}

func (s *permissionService) GrantPersistant(permission PermissionRequest) {
	respCh, ok := s.pendingRequests.Load(permission.ID)
	if ok {
		respCh.(chan bool) <- true
	}
	s.sessionPermissions = append(s.sessionPermissions, permission)
}

func (s *permissionService) Grant(permission PermissionRequest) {
	respCh, ok := s.pendingRequests.Load(permission.ID)
	if ok {
		respCh.(chan bool) <- true
	}
}

func (s *permissionService) Deny(permission PermissionRequest) {
	respCh, ok := s.pendingRequests.Load(permission.ID)
	if ok {
		respCh.(chan bool) <- false
	}
}

func (s *permissionService) Request(opts CreatePermissionRequest) bool {
	if s.IsAutoApproved(opts.SessionID) {
		return true
	}
	cfg := config.Get()
	if cfg != nil {
		if cfg.Permissions.AutoApprove {
			return true
		}
		if opts.ToolName == "bash" && opts.Action == "execute" && commandAllowedByPrefix(opts.Command, cfg.Permissions.AllowCommandPrefixes) {
			return true
		}
	}
	dir := filepath.Dir(opts.Path)
	if dir == "." {
		dir = config.WorkingDirectory()
	}
	permission := PermissionRequest{
		ID:          uuid.New().String(),
		Path:        dir,
		SessionID:   opts.SessionID,
		ToolName:    opts.ToolName,
		Description: opts.Description,
		Action:      opts.Action,
		Params:      opts.Params,
	}

	for _, p := range s.sessionPermissions {
		if p.ToolName == permission.ToolName && p.Action == permission.Action && p.SessionID == permission.SessionID && p.Path == permission.Path {
			return true
		}
	}

	respCh := make(chan bool, 1)

	s.pendingRequests.Store(permission.ID, respCh)
	defer s.pendingRequests.Delete(permission.ID)

	s.Publish(pubsub.CreatedEvent, permission)

	// Wait for the response with a timeout
	resp := <-respCh
	return resp
}

func (s *permissionService) AutoApproveSession(sessionID string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	s.autoApproveMu.Lock()
	defer s.autoApproveMu.Unlock()
	s.autoApproveSessions[sessionID] = struct{}{}
}

func (s *permissionService) IsAutoApproved(sessionID string) bool {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return false
	}
	s.autoApproveMu.RLock()
	defer s.autoApproveMu.RUnlock()
	_, ok := s.autoApproveSessions[sessionID]
	return ok
}

func (s *permissionService) PendingCount() int {
	count := 0
	s.pendingRequests.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}

func commandAllowedByPrefix(command string, prefixes []string) bool {
	command = strings.TrimSpace(command)
	if command == "" || len(prefixes) == 0 {
		return false
	}

	segments := splitCommandSegments(command)
	if len(segments) == 0 {
		return false
	}

	for _, segment := range segments {
		matched := false
		lowerSegment := strings.ToLower(segment)
		for _, prefix := range prefixes {
			prefix = strings.ToLower(strings.TrimSpace(prefix))
			if prefix == "" {
				continue
			}
			if strings.HasPrefix(lowerSegment, prefix) {
				if len(lowerSegment) == len(prefix) || lowerSegment[len(prefix)] == ' ' {
					matched = true
					break
				}
			}
		}
		if !matched {
			return false
		}
	}

	return true
}

func splitCommandSegments(command string) []string {
	replacer := strings.NewReplacer("&&", "\n", "||", "\n", ";", "\n", "|", "\n")
	parts := strings.Split(replacer.Replace(command), "\n")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

func NewPermissionService() Service {
	return &permissionService{
		Broker:              pubsub.NewBroker[PermissionRequest](),
		sessionPermissions:  make([]PermissionRequest, 0),
		autoApproveSessions: make(map[string]struct{}),
	}
}
