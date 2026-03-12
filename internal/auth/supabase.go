package auth

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/SciMate-AI/scicli/internal/config"
)

type Service struct {
	supabase config.SupabaseConfig
	store    *Store
	client   *http.Client
}

type User struct {
	Email string `json:"email"`
}

type authResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	User         User   `json:"user"`
}

type authErrorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
	Msg              string `json:"msg"`
	Message          string `json:"message"`
}

func NewService() (*Service, error) {
	supabase, err := config.RequireSupabase()
	if err != nil {
		return nil, err
	}
	store, err := NewStore()
	if err != nil {
		return nil, err
	}
	return &Service{
		supabase: supabase,
		store:    store,
		client:   &http.Client{Timeout: 30 * time.Second},
	}, nil
}

func (s *Service) Register(email, password string) (*Session, bool, error) {
	var response authResponse
	err := s.request("POST", "/auth/v1/signup", map[string]any{
		"email":    strings.TrimSpace(email),
		"password": password,
	}, &response)
	if err != nil {
		return nil, false, err
	}
	session := response.toSession()
	if session != nil {
		if err := s.store.Save(session); err != nil {
			return nil, false, err
		}
		return session, false, nil
	}
	return nil, true, nil
}

func (s *Service) Login(email, password string) (*Session, error) {
	var response authResponse
	err := s.request(
		"POST",
		"/auth/v1/token?grant_type=password",
		map[string]any{
			"email":    strings.TrimSpace(email),
			"password": password,
		},
		&response,
	)
	if err != nil {
		return nil, err
	}
	session := response.toSession()
	if session == nil {
		return nil, fmt.Errorf("supabase login returned no access token")
	}
	if err := s.store.Save(session); err != nil {
		return nil, err
	}
	return session, nil
}

func (s *Service) Logout() error {
	return s.store.Clear()
}

func (s *Service) Status() (*Session, error) {
	return s.currentSession(true)
}

func (s *Service) GetAccessToken() (string, error) {
	session, err := s.currentSession(true)
	if err != nil {
		return "", err
	}
	if session == nil {
		return "", nil
	}
	return session.AccessToken, nil
}

func (s *Service) RequireAccessToken() (string, error) {
	token, err := s.GetAccessToken()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(token) == "" {
		return "", fmt.Errorf("not logged in: run `scicli auth login` first")
	}
	return token, nil
}

func (s *Service) Refresh() (*Session, error) {
	session, err := s.currentSession(false)
	if err != nil {
		return nil, err
	}
	if session == nil || strings.TrimSpace(session.RefreshToken) == "" {
		return nil, fmt.Errorf("no refresh token available")
	}

	var response authResponse
	err = s.request(
		"POST",
		"/auth/v1/token?grant_type=refresh_token",
		map[string]any{
			"refresh_token": session.RefreshToken,
		},
		&response,
	)
	if err != nil {
		return nil, err
	}
	refreshed := response.toSession()
	if refreshed == nil {
		return nil, fmt.Errorf("supabase refresh returned no access token")
	}
	if refreshed.Email == "" {
		refreshed.Email = session.Email
	}
	if refreshed.RefreshToken == "" {
		refreshed.RefreshToken = session.RefreshToken
	}
	if err := s.store.Save(refreshed); err != nil {
		return nil, err
	}
	return refreshed, nil
}

func (s *Service) currentSession(allowRefresh bool) (*Session, error) {
	session, err := s.store.Load()
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, nil
	}
	if allowRefresh && session.RefreshToken != "" && isLikelyExpired(session) {
		refreshed, refreshErr := s.Refresh()
		if refreshErr == nil {
			return refreshed, nil
		}
	}
	return session, nil
}

func (s *Service) request(method, path string, payload any, out any) error {
	baseURL := strings.TrimRight(strings.TrimSpace(s.supabase.URL), "/")
	target, err := url.Parse(baseURL + path)
	if err != nil {
		return fmt.Errorf("invalid supabase url: %w", err)
	}

	var body []byte
	if payload != nil {
		body, err = json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("failed to encode supabase request: %w", err)
		}
	}

	req, err := http.NewRequest(method, target.String(), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create supabase request: %w", err)
	}
	req.Header.Set("apikey", s.supabase.AnonKey)
	req.Header.Set("authorization", "Bearer "+s.supabase.AnonKey)
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("supabase request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var authErr authErrorResponse
		_ = json.NewDecoder(resp.Body).Decode(&authErr)
		msg := strings.TrimSpace(authErr.Message)
		if msg == "" {
			msg = strings.TrimSpace(authErr.ErrorDescription)
		}
		if msg == "" {
			msg = strings.TrimSpace(authErr.Msg)
		}
		if msg == "" {
			msg = strings.TrimSpace(authErr.Error)
		}
		if msg == "" {
			msg = resp.Status
		}
		return fmt.Errorf("supabase request failed: %s", msg)
	}

	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("failed to decode supabase response: %w", err)
	}
	return nil
}

func (r authResponse) toSession() *Session {
	if strings.TrimSpace(r.AccessToken) == "" {
		return nil
	}
	expiresAt := time.Time{}
	if r.ExpiresIn > 0 {
		expiresAt = time.Now().Add(time.Duration(r.ExpiresIn) * time.Second)
	}
	return &Session{
		AccessToken:  r.AccessToken,
		RefreshToken: r.RefreshToken,
		TokenType:    r.TokenType,
		ExpiresAt:    expiresAt,
		Email:        strings.TrimSpace(r.User.Email),
	}
}

func isLikelyExpired(session *Session) bool {
	if session == nil {
		return true
	}
	if !session.ExpiresAt.IsZero() {
		return time.Until(session.ExpiresAt) <= time.Minute
	}
	parts := strings.Split(session.AccessToken, ".")
	if len(parts) < 2 {
		return false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return false
	}
	if claims.Exp == 0 {
		return false
	}
	return time.Until(time.Unix(claims.Exp, 0)) <= time.Minute
}
