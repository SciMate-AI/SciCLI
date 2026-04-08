package dialog

import (
	"fmt"
	"slices"
	"strings"

	"github.com/SciMate-AI/scicli/internal/config"
	"github.com/SciMate-AI/scicli/internal/llm/models"
	"github.com/SciMate-AI/scicli/internal/tui/layout"
	"github.com/SciMate-AI/scicli/internal/tui/styles"
	"github.com/SciMate-AI/scicli/internal/tui/theme"
	"github.com/SciMate-AI/scicli/internal/tui/util"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type StartCompactSessionMsg struct{}

const (
	numVisibleModels = 10
	maxDialogWidth   = 56
)

type ModelSelectedMsg struct {
	Model models.Model
}

type CloseModelDialogMsg struct{}

type ModelDialog interface {
	tea.Model
	layout.Bindings
}

type modelDialogCmp struct {
	models             []models.Model
	provider           models.ModelProvider
	availableProviders []models.ModelProvider

	selectedIdx     int
	width           int
	height          int
	scrollOffset    int
	hScrollOffset   int
	hScrollPossible bool
}

type modelKeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Left   key.Binding
	Right  key.Binding
	Enter  key.Binding
	Escape key.Binding
	J      key.Binding
	K      key.Binding
	H      key.Binding
	L      key.Binding
}

var modelKeys = modelKeyMap{
	Up: key.NewBinding(
		key.WithKeys("up"),
		key.WithHelp("up", "previous model"),
	),
	Down: key.NewBinding(
		key.WithKeys("down"),
		key.WithHelp("down", "next model"),
	),
	Left: key.NewBinding(
		key.WithKeys("left"),
		key.WithHelp("left", "previous provider"),
	),
	Right: key.NewBinding(
		key.WithKeys("right"),
		key.WithHelp("right", "next provider"),
	),
	Enter: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "select model"),
	),
	Escape: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "close"),
	),
	J: key.NewBinding(
		key.WithKeys("j"),
		key.WithHelp("j", "next model"),
	),
	K: key.NewBinding(
		key.WithKeys("k"),
		key.WithHelp("k", "previous model"),
	),
	H: key.NewBinding(
		key.WithKeys("h"),
		key.WithHelp("h", "previous provider"),
	),
	L: key.NewBinding(
		key.WithKeys("l"),
		key.WithHelp("l", "next provider"),
	),
}

func (m *modelDialogCmp) Init() tea.Cmd {
	m.setupModels()
	return nil
}

func (m *modelDialogCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, modelKeys.Up) || key.Matches(msg, modelKeys.K):
			m.moveSelectionUp()
		case key.Matches(msg, modelKeys.Down) || key.Matches(msg, modelKeys.J):
			m.moveSelectionDown()
		case key.Matches(msg, modelKeys.Left) || key.Matches(msg, modelKeys.H):
			if m.hScrollPossible {
				m.switchProvider(-1)
			}
		case key.Matches(msg, modelKeys.Right) || key.Matches(msg, modelKeys.L):
			if m.hScrollPossible {
				m.switchProvider(1)
			}
		case key.Matches(msg, modelKeys.Enter):
			if len(m.models) == 0 {
				return m, nil
			}
			if !config.ProviderReady(m.provider) {
				return m, util.CmdHandler(ShowProviderSetupDialogMsg{
					Provider: m.provider,
					ModelID:  m.models[m.selectedIdx].ID,
				})
			}
			util.ReportInfo(fmt.Sprintf("selected model: %s", m.models[m.selectedIdx].Name))
			return m, util.CmdHandler(ModelSelectedMsg{Model: m.models[m.selectedIdx]})
		case key.Matches(msg, modelKeys.Escape):
			return m, util.CmdHandler(CloseModelDialogMsg{})
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	}

	return m, nil
}

func (m *modelDialogCmp) moveSelectionUp() {
	if m.selectedIdx > 0 {
		m.selectedIdx--
	} else {
		m.selectedIdx = len(m.models) - 1
		m.scrollOffset = max(0, len(m.models)-numVisibleModels)
	}

	if m.selectedIdx < m.scrollOffset {
		m.scrollOffset = m.selectedIdx
	}
}

func (m *modelDialogCmp) moveSelectionDown() {
	if m.selectedIdx < len(m.models)-1 {
		m.selectedIdx++
	} else {
		m.selectedIdx = 0
		m.scrollOffset = 0
	}

	if m.selectedIdx >= m.scrollOffset+numVisibleModels {
		m.scrollOffset = m.selectedIdx - (numVisibleModels - 1)
	}
}

func (m *modelDialogCmp) switchProvider(offset int) {
	newOffset := m.hScrollOffset + offset
	if newOffset < 0 {
		newOffset = len(m.availableProviders) - 1
	}
	if newOffset >= len(m.availableProviders) {
		newOffset = 0
	}

	m.hScrollOffset = newOffset
	m.provider = m.availableProviders[m.hScrollOffset]
	m.setupModelsForProvider(m.provider)
}

func (m *modelDialogCmp) View() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()
	width := maxDialogWidth
	if m.width > 0 {
		width = max(44, min(72, m.width-10))
	}

	providerName := strings.ToUpper(string(m.provider)[:1]) + string(m.provider[1:])
	if !config.ProviderReady(m.provider) {
		providerName += " [setup required]"
	}
	title := baseStyle.Foreground(t.Primary()).Bold(true).Width(width).Render("Provider / model")
	context := lipgloss.NewStyle().
		Width(width).
		BorderLeft(true).
		BorderForeground(t.BorderDim()).
		PaddingLeft(1).
		Foreground(t.TextMuted()).
		Render(fmt.Sprintf("provider  %s", providerName))

	endIdx := min(m.scrollOffset+numVisibleModels, len(m.models))
	modelItems := make([]string, 0, endIdx-m.scrollOffset)
	for i := m.scrollOffset; i < endIdx; i++ {
		prefix := "  "
		itemStyle := baseStyle.Width(width).Foreground(t.TextMuted())
		if i == m.selectedIdx {
			prefix = "> "
			itemStyle = itemStyle.Foreground(t.Text()).Bold(true)
		}
		modelItems = append(modelItems, itemStyle.Render(prefix+m.models[i].Name))
	}

	footer := baseStyle.
		Foreground(t.TextMuted()).
		Width(width).
		Render(m.getScrollIndicators())

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		baseStyle.Width(width).Foreground(t.TextMuted()).Render(m.headerText()),
		"",
		context,
		"",
		baseStyle.Width(width).Render(m.renderProviderStrip(width)),
		"",
		baseStyle.Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, modelItems...)),
		footer,
	)

	return lipgloss.PlaceHorizontal(max(width, m.width), lipgloss.Center, baseStyle.Width(width).Render(content))
}

func (m *modelDialogCmp) getScrollIndicators() string {
	parts := []string{}
	if m.hScrollPossible {
		parts = append(parts, fmt.Sprintf("provider %d/%d", m.hScrollOffset+1, len(m.availableProviders)))
	}
	if len(m.models) > 0 {
		parts = append(parts, fmt.Sprintf("model %d/%d", m.selectedIdx+1, len(m.models)))
	}

	scroll := []string{}
	if m.scrollOffset > 0 {
		scroll = append(scroll, "^")
	}
	if m.scrollOffset+numVisibleModels < len(m.models) {
		scroll = append(scroll, "v")
	}
	if len(scroll) > 0 {
		parts = append(parts, strings.Join(scroll, ""))
	}

	return strings.Join(parts, "  ")
}

func (m *modelDialogCmp) BindingKeys() []key.Binding {
	return layout.KeyMapToSlice(modelKeys)
}

func (m *modelDialogCmp) setupModels() {
	cfg := config.Get()
	modelInfo := GetSelectedModel(cfg)
	m.availableProviders = getSelectableProviders(cfg)
	m.hScrollPossible = len(m.availableProviders) > 1

	m.provider = modelInfo.Provider
	m.hScrollOffset = findProviderIndex(m.availableProviders, m.provider)
	if m.hScrollOffset < 0 {
		m.hScrollOffset = 0
	}
	m.setupModelsForProvider(m.provider)
}

func GetSelectedModel(cfg *config.Config) models.Model {
	agentCfg := cfg.Agents[config.AgentCoder]
	selectedModelID := agentCfg.Model
	return models.SupportedModels[selectedModelID]
}

func getSelectableProviders(cfg *config.Config) []models.ModelProvider {
	onboardingProviders := config.OnboardingProviders()
	providers := make([]models.ModelProvider, 0, len(onboardingProviders))
	for _, provider := range onboardingProviders {
		providers = append(providers, provider.Provider)
	}
	if len(providers) > 0 {
		return providers
	}

	var configured []models.ModelProvider
	for providerID, provider := range cfg.Providers {
		if !provider.Disabled {
			configured = append(configured, providerID)
		}
	}

	slices.SortFunc(configured, func(a, b models.ModelProvider) int {
		rA := models.ProviderPopularity[a]
		rB := models.ProviderPopularity[b]
		if rA == 0 {
			rA = 999
		}
		if rB == 0 {
			rB = 999
		}
		return rA - rB
	})
	return configured
}

func findProviderIndex(providers []models.ModelProvider, provider models.ModelProvider) int {
	for i, p := range providers {
		if p == provider {
			return i
		}
	}
	return -1
}

func (m *modelDialogCmp) setupModelsForProvider(provider models.ModelProvider) {
	cfg := config.Get()
	agentCfg := cfg.Agents[config.AgentCoder]
	selectedModelID := agentCfg.Model

	m.provider = provider
	m.models = getModelsForProvider(provider)
	m.selectedIdx = 0
	m.scrollOffset = 0

	if provider == models.SupportedModels[selectedModelID].Provider {
		for i, model := range m.models {
			if model.ID == selectedModelID {
				m.selectedIdx = i
				if m.selectedIdx >= numVisibleModels {
					m.scrollOffset = m.selectedIdx - (numVisibleModels - 1)
				}
				break
			}
		}
	}
}

func getModelsForProvider(provider models.ModelProvider) []models.Model {
	var providerModels []models.Model
	for _, model := range models.SupportedModels {
		if model.Provider == provider {
			providerModels = append(providerModels, model)
		}
	}

	slices.SortFunc(providerModels, func(a, b models.Model) int {
		if a.Name > b.Name {
			return -1
		}
		if a.Name < b.Name {
			return 1
		}
		return 0
	})
	return providerModels
}

func (m *modelDialogCmp) headerText() string {
	if config.ProviderReady(m.provider) {
		return "Left/Right switches provider. Up/Down selects model. Enter confirms."
	}
	return "Enter opens provider setup for this provider."
}

func (m *modelDialogCmp) renderProviderStrip(width int) string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()
	if len(m.availableProviders) == 0 {
		return ""
	}

	lines := make([]string, 0, len(m.availableProviders))
	for idx, provider := range m.availableProviders {
		label := "  " + string(provider)
		style := baseStyle.Width(width).Foreground(t.TextMuted())
		if idx == m.hScrollOffset {
			label = "> " + string(provider)
			style = baseStyle.Width(width).Foreground(t.Text()).Bold(true)
		}
		lines = append(lines, style.Render(label))
	}

	return lipgloss.NewStyle().
		BorderLeft(true).
		BorderForeground(t.BorderDim()).
		PaddingLeft(1).
		Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func NewModelDialogCmp() ModelDialog {
	return &modelDialogCmp{}
}
