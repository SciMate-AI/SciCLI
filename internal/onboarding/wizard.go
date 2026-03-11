package onboarding

import (
	"fmt"
	"strings"

	"github.com/SciMate-AI/scicli/internal/config"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type step int

const (
	stepProvider step = iota
	stepCredential
	stepModel
)

type wizardModel struct {
	width  int
	height int

	step step

	providers         []config.OnboardingProvider
	selectedProvider  int
	selectedModel     int
	modelScrollOffset int

	useDetectedCredential bool
	apiKeyInput           textinput.Model

	err       error
	cancelled bool
}

func Run() error {
	model := newWizardModel()
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

func newWizardModel() *wizardModel {
	apiKeyInput := textinput.New()
	apiKeyInput.Placeholder = "Paste API key and press Enter"
	apiKeyInput.Prompt = "> "
	apiKeyInput.CharLimit = 512
	apiKeyInput.Width = 56
	apiKeyInput.EchoMode = textinput.EchoPassword
	apiKeyInput.EchoCharacter = '*'

	model := &wizardModel{
		step:        stepProvider,
		providers:   config.OnboardingProviders(),
		apiKeyInput: apiKeyInput,
	}
	model.resetCredentialState()
	model.resetModels()
	return model
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
			if m.step == stepProvider {
				m.cancelled = true
				return m, tea.Quit
			}
			m.prevStep()
			return m, nil
		}
	}

	switch m.step {
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
			if m.shouldFocusAPIInput() {
				return m, m.apiKeyInput.Focus()
			}
			m.apiKeyInput.Blur()
		}
	}

	return m, nil
}

func (m *wizardModel) updateCredentialStep(msg tea.Msg) (tea.Model, tea.Cmd) {
	current := m.currentProvider()

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "left", "right", "tab":
			if current.SupportsManualAPIKey && current.DetectedCredential {
				m.useDetectedCredential = !m.useDetectedCredential
				if m.useDetectedCredential {
					m.apiKeyInput.Blur()
				} else {
					return m, m.apiKeyInput.Focus()
				}
			}
			return m, nil
		case "enter":
			if current.SupportsManualAPIKey {
				if m.useDetectedCredential {
					m.step = stepModel
					m.apiKeyInput.Blur()
					return m, nil
				}
				if strings.TrimSpace(m.apiKeyInput.Value()) == "" {
					return m, nil
				}
			}

			m.step = stepModel
			m.apiKeyInput.Blur()
			return m, nil
		}
	}

	if m.shouldFocusAPIInput() {
		var cmd tea.Cmd
		m.apiKeyInput, cmd = m.apiKeyInput.Update(msg)
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
			apiKey := strings.TrimSpace(m.apiKeyInput.Value())
			persistAPIKey := m.currentProvider().SupportsManualAPIKey && !m.useDetectedCredential && apiKey != ""
			m.err = config.SaveOnboardingSelection(
				m.currentProvider().Provider,
				apiKey,
				persistAPIKey,
				modelsForProvider[m.selectedModel].ID,
			)
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
		Width(minInt(88, m.width-4)).
		Padding(1, 2).
		Border(lipgloss.RoundedBorder())

	parts := []string{
		lipgloss.NewStyle().Bold(true).Render("SciCLI first-run setup"),
		"Configure an AI provider and default model before opening the main UI.",
		"",
		m.stepIndicator(),
		"",
	}

	switch m.step {
	case stepProvider:
		parts = append(parts, m.renderProviderStep()...)
	case stepCredential:
		parts = append(parts, m.renderCredentialStep()...)
	case stepModel:
		parts = append(parts, m.renderModelStep()...)
	}

	parts = append(parts,
		"",
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

func (m *wizardModel) renderProviderStep() []string {
	lines := []string{
		"Step 1/3: Choose a provider",
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
		"Step 2/3: Provide credentials for " + current.Label,
		"",
	}

	if current.SupportsManualAPIKey {
		if current.DetectedCredential {
			modeLabel := "Use detected credential"
			if !m.useDetectedCredential {
				modeLabel = "Enter API key manually"
			}
			lines = append(lines,
				"Tab to switch mode. Current mode: "+modeLabel,
				"Detected source: "+current.CredentialHint,
				"",
			)
		} else {
			lines = append(lines,
				"Enter your API key. It will be saved to the global config file.",
				"Expected source: "+current.CredentialHint,
				"",
			)
		}

		if m.shouldFocusAPIInput() {
			lines = append(lines, m.apiKeyInput.View())
		} else {
			lines = append(lines, "Using detected credential. Press Enter to continue.")
		}
	} else {
		lines = append(lines,
			"Detected credential source: "+current.CredentialHint,
			"Press Enter to continue.",
		)
	}

	return lines
}

func (m *wizardModel) renderModelStep() []string {
	modelsForProvider := config.OnboardingModels(m.currentProvider().Provider)
	lines := []string{
		"Step 3/3: Choose the default coder model",
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
	labels := []string{"Provider", "Credential", "Model"}
	out := make([]string, 0, len(labels))
	for idx, label := range labels {
		if idx == int(m.step) {
			out = append(out, "["+label+"]")
			continue
		}
		out = append(out, label)
	}
	return strings.Join(out, "  ")
}

func (m *wizardModel) prevStep() {
	switch m.step {
	case stepCredential:
		m.step = stepProvider
	case stepModel:
		m.step = stepCredential
		if m.shouldFocusAPIInput() {
			m.apiKeyInput.Focus()
		}
	}
}

func (m *wizardModel) currentProvider() config.OnboardingProvider {
	return m.providers[m.selectedProvider]
}

func (m *wizardModel) shouldFocusAPIInput() bool {
	current := m.currentProvider()
	if !current.SupportsManualAPIKey {
		return false
	}
	if current.DetectedCredential {
		return !m.useDetectedCredential
	}
	return true
}

func (m *wizardModel) resetCredentialState() {
	current := m.currentProvider()
	m.apiKeyInput.SetValue("")
	m.useDetectedCredential = current.DetectedCredential
	if m.shouldFocusAPIInput() {
		m.apiKeyInput.Focus()
	} else {
		m.apiKeyInput.Blur()
	}
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
