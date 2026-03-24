package onboarding

import (
	"fmt"
	"strings"

	"github.com/SciMate-AI/scicli/internal/auth"
	"github.com/SciMate-AI/scicli/internal/config"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type step int
type authMode int

const (
	stepAuth step = iota
	stepAuthForm
	stepProvider
	stepCredential
	stepModel
)

const (
	authModeLogin authMode = iota
	authModeRegister
)

type wizardModel struct {
	width  int
	height int

	step step

	authService *auth.Service
	session     *auth.Session
	authMode    authMode
	authInputs  []textinput.Model
	authFocus   int
	authNotice  string

	providers         []config.OnboardingProvider
	selectedProvider  int
	selectedModel     int
	modelScrollOffset int

	useDetectedCredential bool
	credentialInputs      []textinput.Model
	credentialFocus       int

	err       error
	cancelled bool
}

func Run() error {
	model, err := newWizardModel()
	if err != nil {
		return err
	}

	program := tea.NewProgram(model, tea.WithAltScreen())
	result, err := program.Run()
	if err != nil {
		return err
	}

	finished, ok := result.(*wizardModel)
	if !ok {
		return fmt.Errorf("unexpected onboarding result type %T", result)
	}
	if finished.cancelled {
		return fmt.Errorf("first-run setup cancelled")
	}
	return finished.err
}

func newWizardModel() (*wizardModel, error) {
	authService, err := auth.NewService()
	if err != nil {
		return nil, err
	}
	session, err := authService.Status()
	if err != nil {
		return nil, err
	}

	model := &wizardModel{
		authService: authService,
		session:     session,
		authMode:    authModeLogin,
		providers:   config.OnboardingProviders(),
	}
	model.initAuthInputs()
	model.initCredentialInputs()
	model.resetCredentialState()
	model.resetModels()
	if session != nil {
		model.step = stepProvider
	} else {
		model.step = stepAuth
	}
	return model, nil
}

func (m *wizardModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m *wizardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			if m.step == stepAuth || m.step == stepProvider {
				m.cancelled = true
				return m, tea.Quit
			}
			m.prevStep()
			return m, m.currentFocusCmd()
		}
	}

	switch m.step {
	case stepAuth:
		return m.updateAuthStep(msg)
	case stepAuthForm:
		return m.updateAuthFormStep(msg)
	case stepProvider:
		return m.updateProviderStep(msg)
	case stepCredential:
		return m.updateCredentialStep(msg)
	case stepModel:
		return m.updateModelStep(msg)
	default:
		return m, nil
	}
}

func (m *wizardModel) updateAuthStep(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.session != nil {
		if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.String() == "enter" {
			m.step = stepProvider
		}
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k", "down", "j", "tab":
			if m.authMode == authModeLogin {
				m.authMode = authModeRegister
			} else {
				m.authMode = authModeLogin
			}
		case "enter":
			m.step = stepAuthForm
			return m, m.focusAuthInput(0)
		}
	}

	return m, nil
}

func (m *wizardModel) updateAuthFormStep(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "shift+tab":
			return m, m.focusAuthInput((m.authFocus + len(m.authInputs) - 1) % len(m.authInputs))
		case "down", "tab":
			return m, m.focusAuthInput((m.authFocus + 1) % len(m.authInputs))
		case "enter":
			if m.authFocus < len(m.authInputs)-1 {
				return m, m.focusAuthInput(m.authFocus + 1)
			}
			email := strings.TrimSpace(m.authInputs[0].Value())
			password := m.authInputs[1].Value()
			if email == "" || password == "" {
				return m, nil
			}
			if m.authMode == authModeRegister {
				session, needsConfirmation, err := m.authService.Register(email, password)
				if err != nil {
					m.authNotice = err.Error()
					return m, nil
				}
				if needsConfirmation {
					m.authNotice = "Registration succeeded. Confirm your email, then log in."
					m.authMode = authModeLogin
					m.authInputs[1].SetValue("")
					m.step = stepAuth
					return m, nil
				}
				m.session = session
			} else {
				session, err := m.authService.Login(email, password)
				if err != nil {
					m.authNotice = err.Error()
					return m, nil
				}
				m.session = session
			}
			m.authNotice = ""
			m.step = stepProvider
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.authInputs[m.authFocus], cmd = m.authInputs[m.authFocus].Update(msg)
	return m, cmd
}

func (m *wizardModel) updateProviderStep(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.selectedProvider > 0 {
				m.selectedProvider--
			} else {
				m.selectedProvider = len(m.providers) - 1
			}
			m.resetCredentialState()
			m.resetModels()
		case "down", "j":
			if m.selectedProvider < len(m.providers)-1 {
				m.selectedProvider++
			} else {
				m.selectedProvider = 0
			}
			m.resetCredentialState()
			m.resetModels()
		case "enter":
			m.step = stepCredential
			return m, m.currentFocusCmd()
		}
	}

	return m, nil
}

func (m *wizardModel) updateCredentialStep(msg tea.Msg) (tea.Model, tea.Cmd) {
	current := m.currentProvider()

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "left", "right":
			if current.SupportsManualAPIKey && current.DetectedCredential {
				m.useDetectedCredential = !m.useDetectedCredential
				return m, m.currentFocusCmd()
			}
		case "up", "shift+tab":
			if m.shouldFocusCredentialInputs() {
				return m, m.focusCredentialInput((m.credentialFocus + len(m.activeCredentialInputs()) - 1) % len(m.activeCredentialInputs()))
			}
		case "down", "tab":
			if m.shouldFocusCredentialInputs() {
				return m, m.focusCredentialInput((m.credentialFocus + 1) % len(m.activeCredentialInputs()))
			}
		case "enter":
			if !m.validateCredentialStep() {
				return m, nil
			}
			m.step = stepModel
			return m, nil
		}
	}

	if m.shouldFocusCredentialInputs() {
		active := m.activeCredentialInputs()
		var cmd tea.Cmd
		active[m.credentialFocus], cmd = active[m.credentialFocus].Update(msg)
		m.syncCredentialInputs(active)
		return m, cmd
	}

	return m, nil
}

func (m *wizardModel) updateModelStep(msg tea.Msg) (tea.Model, tea.Cmd) {
	modelsForProvider := config.OnboardingModels(m.currentProvider().Provider)
	if len(modelsForProvider) == 0 {
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.selectedModel > 0 {
				m.selectedModel--
			} else {
				m.selectedModel = len(modelsForProvider) - 1
			}
			m.keepModelSelectionVisible(len(modelsForProvider))
		case "down", "j":
			if m.selectedModel < len(modelsForProvider)-1 {
				m.selectedModel++
			} else {
				m.selectedModel = 0
			}
			m.keepModelSelectionVisible(len(modelsForProvider))
		case "enter":
			apiKey := strings.TrimSpace(m.credentialInputValue("apiKey"))
			selection := config.OnboardingSelection{
				Provider:      m.currentProvider().Provider,
				APIKey:        apiKey,
				PersistAPIKey: apiKey != "" && (!m.currentProvider().DetectedCredential || !m.useDetectedCredential),
				ModelID:       modelsForProvider[m.selectedModel].ID,
				BaseURL:       strings.TrimSpace(m.credentialInputValue("baseURL")),
				CustomModel:   strings.TrimSpace(m.credentialInputValue("model")),
			}
			m.err = config.SaveOnboardingSelection(selection)
			return m, tea.Quit
		}
	}

	return m, nil
}

func (m *wizardModel) View() string {
	if m.width == 0 || m.height == 0 {
		return "Loading setup..."
	}

	container := lipgloss.NewStyle().
		Width(minInt(92, m.width-4)).
		Padding(1, 2).
		Border(lipgloss.RoundedBorder())

	parts := []string{
		lipgloss.NewStyle().Bold(true).Render("SciCLI onboarding"),
		"Complete this once to connect an account, choose a provider, and set your default model.",
		"",
		m.stepIndicator(),
		"",
	}

	switch m.step {
	case stepAuth:
		parts = append(parts, m.renderAuthStep()...)
	case stepAuthForm:
		parts = append(parts, m.renderAuthFormStep()...)
	case stepProvider:
		parts = append(parts, m.renderProviderStep()...)
	case stepCredential:
		parts = append(parts, m.renderCredentialStep()...)
	case stepModel:
		parts = append(parts, m.renderModelStep()...)
	}

	if strings.TrimSpace(m.authNotice) != "" {
		parts = append(parts, "", lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render(m.authNotice))
	}

	parts = append(
		parts,
		"",
		lipgloss.NewStyle().Faint(true).Render("Project memory is optional and can be generated later from the command palette."),
		lipgloss.NewStyle().Faint(true).Render("Config file: "+config.ConfigFilePath()),
	)

	return lipgloss.Place(
		m.width,
		m.height,
		lipgloss.Center,
		lipgloss.Center,
		container.Render(strings.Join(parts, "\n")),
	)
}

func (m *wizardModel) renderAuthStep() []string {
	if m.session != nil {
		return []string{
			"Step 1/4: SciMate account",
			"Current session: " + strings.TrimSpace(m.session.Email),
			"Press Enter to continue to provider setup.",
		}
	}

	current := "Login"
	other := "Register"
	if m.authMode == authModeRegister {
		current = "Register"
		other = "Login"
	}

	return []string{
		"Step 1/4: SciMate account",
		"Up/Down to switch mode, Enter to continue.",
		"",
		"> " + current,
		"  " + other,
	}
}

func (m *wizardModel) renderAuthFormStep() []string {
	title := "Step 1/4: Login"
	if m.authMode == authModeRegister {
		title = "Step 1/4: Register"
	}

	return []string{
		title,
		"Tab to move between fields, Enter on password to submit.",
		"",
		m.authInputs[0].View(),
		m.authInputs[1].View(),
	}
}

func (m *wizardModel) renderProviderStep() []string {
	lines := []string{
		"Step 2/4: Choose a provider",
		"Up/Down to move, Enter to continue.",
		"",
	}

	for idx, provider := range m.providers {
		prefix := "  "
		if idx == m.selectedProvider {
			prefix = "> "
		}

		suffix := ""
		if provider.DetectedCredential {
			suffix = "  [detected " + provider.DetectedCredentialID + "]"
		}

		lines = append(lines, prefix+provider.Label+suffix)
	}

	return lines
}

func (m *wizardModel) renderCredentialStep() []string {
	current := m.currentProvider()
	lines := []string{
		"Step 3/4: Configure " + current.Label,
		"",
	}

	if current.DetectedCredential && current.SupportsManualAPIKey {
		modeLabel := "Use detected credential"
		if !m.useDetectedCredential {
			modeLabel = "Enter API key manually"
		}
		lines = append(lines, "Left/Right to switch mode. Current mode: "+modeLabel, "")
	}

	if current.RequiresBaseURL {
		lines = append(lines, "Base URL: "+current.BaseURLHint)
	}
	if current.RequiresModel {
		lines = append(lines, "Model: "+current.ModelHint)
	}
	if current.CredentialHint != "" {
		lines = append(lines, "Credential: "+current.CredentialHint)
	}
	lines = append(lines, "")

	if !m.shouldFocusCredentialInputs() {
		lines = append(lines, "Using detected credential. Press Enter to continue.")
		return lines
	}

	for _, input := range m.activeCredentialInputs() {
		lines = append(lines, input.View())
	}

	return lines
}

func (m *wizardModel) renderModelStep() []string {
	modelsForProvider := config.OnboardingModels(m.currentProvider().Provider)
	lines := []string{
		"Step 4/4: Choose the default coder model",
		"Up/Down to move, Enter to save and continue.",
		"",
	}

	if len(modelsForProvider) == 0 {
		lines = append(lines, "No models available for this provider.")
		return lines
	}

	maxVisible := minInt(10, len(modelsForProvider))
	end := minInt(m.modelScrollOffset+maxVisible, len(modelsForProvider))
	for idx := m.modelScrollOffset; idx < end; idx++ {
		prefix := "  "
		if idx == m.selectedModel {
			prefix = "> "
		}
		lines = append(lines, prefix+modelsForProvider[idx].Name)
	}

	return lines
}

func (m *wizardModel) stepIndicator() string {
	labels := []string{"Auth", "Provider", "Credential", "Model"}
	current := 0
	switch m.step {
	case stepProvider:
		current = 1
	case stepCredential:
		current = 2
	case stepModel:
		current = 3
	default:
		current = 0
	}

	out := make([]string, 0, len(labels))
	for idx, label := range labels {
		if idx == current {
			out = append(out, "["+label+"]")
			continue
		}
		out = append(out, label)
	}
	return strings.Join(out, "  ")
}

func (m *wizardModel) prevStep() {
	switch m.step {
	case stepAuthForm:
		m.step = stepAuth
	case stepProvider:
		if m.session == nil {
			m.step = stepAuth
		}
	case stepCredential:
		m.step = stepProvider
	case stepModel:
		m.step = stepCredential
	}
}

func (m *wizardModel) currentProvider() config.OnboardingProvider {
	return m.providers[m.selectedProvider]
}

func (m *wizardModel) initAuthInputs() {
	email := textinput.New()
	email.Placeholder = "Email"
	email.Prompt = "> "
	email.Width = 56

	password := textinput.New()
	password.Placeholder = "Password"
	password.Prompt = "> "
	password.Width = 56
	password.EchoMode = textinput.EchoPassword
	password.EchoCharacter = '*'

	m.authInputs = []textinput.Model{email, password}
}

func (m *wizardModel) initCredentialInputs() {
	baseURL := textinput.New()
	baseURL.Placeholder = "Base URL"
	baseURL.Prompt = "> "
	baseURL.Width = 56

	apiKey := textinput.New()
	apiKey.Placeholder = "API key"
	apiKey.Prompt = "> "
	apiKey.Width = 56
	apiKey.EchoMode = textinput.EchoPassword
	apiKey.EchoCharacter = '*'

	model := textinput.New()
	model.Placeholder = "Model"
	model.Prompt = "> "
	model.Width = 56

	m.credentialInputs = []textinput.Model{baseURL, apiKey, model}
}

func (m *wizardModel) currentFocusCmd() tea.Cmd {
	switch m.step {
	case stepAuthForm:
		return m.focusAuthInput(m.authFocus)
	case stepCredential:
		if m.shouldFocusCredentialInputs() {
			return m.focusCredentialInput(m.credentialFocus)
		}
	}
	return nil
}

func (m *wizardModel) focusAuthInput(index int) tea.Cmd {
	m.authFocus = index
	for i := range m.authInputs {
		m.authInputs[i].Blur()
	}
	return m.authInputs[index].Focus()
}

func (m *wizardModel) activeCredentialInputs() []textinput.Model {
	current := m.currentProvider()
	active := make([]textinput.Model, 0, 3)
	if current.RequiresBaseURL {
		active = append(active, m.credentialInputs[0])
	}
	if current.SupportsManualAPIKey && (!current.DetectedCredential || !m.useDetectedCredential) {
		active = append(active, m.credentialInputs[1])
	}
	if current.RequiresModel {
		active = append(active, m.credentialInputs[2])
	}
	return active
}

func (m *wizardModel) syncCredentialInputs(active []textinput.Model) {
	current := m.currentProvider()
	cursor := 0
	if current.RequiresBaseURL {
		m.credentialInputs[0] = active[cursor]
		cursor++
	}
	if current.SupportsManualAPIKey && (!current.DetectedCredential || !m.useDetectedCredential) {
		m.credentialInputs[1] = active[cursor]
		cursor++
	}
	if current.RequiresModel {
		m.credentialInputs[2] = active[cursor]
	}
}

func (m *wizardModel) focusCredentialInput(index int) tea.Cmd {
	active := m.activeCredentialInputs()
	if len(active) == 0 {
		return nil
	}
	if index >= len(active) {
		index = 0
	}
	m.credentialFocus = index
	for i := range active {
		if i == index {
			active[i].Focus()
		} else {
			active[i].Blur()
		}
	}
	m.syncCredentialInputs(active)
	return nil
}

func (m *wizardModel) shouldFocusCredentialInputs() bool {
	return len(m.activeCredentialInputs()) > 0
}

func (m *wizardModel) credentialInputValue(name string) string {
	switch name {
	case "baseURL":
		return m.credentialInputs[0].Value()
	case "apiKey":
		return m.credentialInputs[1].Value()
	case "model":
		return m.credentialInputs[2].Value()
	default:
		return ""
	}
}

func (m *wizardModel) validateCredentialStep() bool {
	current := m.currentProvider()
	if current.RequiresBaseURL && strings.TrimSpace(m.credentialInputValue("baseURL")) == "" {
		return false
	}
	if current.RequiresModel && strings.TrimSpace(m.credentialInputValue("model")) == "" {
		return false
	}
	if current.SupportsManualAPIKey && !current.OptionalAPIKey && !current.DetectedCredential && strings.TrimSpace(m.credentialInputValue("apiKey")) == "" {
		return false
	}
	if current.SupportsManualAPIKey && !current.OptionalAPIKey && current.DetectedCredential && !m.useDetectedCredential && strings.TrimSpace(m.credentialInputValue("apiKey")) == "" {
		return false
	}
	return true
}

func (m *wizardModel) resetCredentialState() {
	for i := range m.credentialInputs {
		m.credentialInputs[i].SetValue("")
		m.credentialInputs[i].Blur()
	}
	current := m.currentProvider()
	m.useDetectedCredential = current.DetectedCredential
	m.credentialFocus = 0
}

func (m *wizardModel) resetModels() {
	m.selectedModel = 0
	m.modelScrollOffset = 0
	defaultModel := config.DefaultOnboardingModel(m.currentProvider().Provider)
	modelsForProvider := config.OnboardingModels(m.currentProvider().Provider)
	for idx, model := range modelsForProvider {
		if model.ID == defaultModel {
			m.selectedModel = idx
			break
		}
	}
	m.keepModelSelectionVisible(len(modelsForProvider))
}

func (m *wizardModel) keepModelSelectionVisible(total int) {
	const maxVisible = 10
	if total <= maxVisible {
		m.modelScrollOffset = 0
		return
	}

	if m.selectedModel < m.modelScrollOffset {
		m.modelScrollOffset = m.selectedModel
	}
	if m.selectedModel >= m.modelScrollOffset+maxVisible {
		m.modelScrollOffset = m.selectedModel - (maxVisible - 1)
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
