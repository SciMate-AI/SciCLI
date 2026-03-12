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

const (
	numVisibleModels = 10
	maxDialogWidth   = 40
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

	providerName := strings.ToUpper(string(m.provider)[:1]) + string(m.provider[1:])
	title := baseStyle.
		Foreground(t.Primary()).
		Bold(true).
		Width(maxDialogWidth).
		Padding(0, 0, 1).
		Render(fmt.Sprintf("Provider: %s", providerName))

	endIdx := min(m.scrollOffset+numVisibleModels, len(m.models))
	modelItems := make([]string, 0, endIdx-m.scrollOffset)
	for i := m.scrollOffset; i < endIdx; i++ {
		itemStyle := baseStyle.Width(maxDialogWidth)
		if i == m.selectedIdx {
			itemStyle = itemStyle.Background(t.Primary()).
				Foreground(t.Background()).
				Bold(true)
		}
		modelItems = append(modelItems, itemStyle.Render(m.models[i].Name))
	}

	footer := baseStyle.
		Foreground(t.TextMuted()).
		Width(maxDialogWidth).
		Render(m.getScrollIndicators())

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		baseStyle.Width(maxDialogWidth).Render("Select Provider / Model"),
		baseStyle.Width(maxDialogWidth).Render(""),
		baseStyle.Width(maxDialogWidth).Render(lipgloss.JoinVertical(lipgloss.Left, modelItems...)),
		footer,
	)

	return baseStyle.Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderBackground(t.Background()).
		BorderForeground(t.TextMuted()).
		Width(lipgloss.Width(content) + 4).
		Render(content)
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
	m.availableProviders = getEnabledProviders(cfg)
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

func getEnabledProviders(cfg *config.Config) []models.ModelProvider {
	var providers []models.ModelProvider
	for providerID, provider := range cfg.Providers {
		if !provider.Disabled {
			providers = append(providers, providerID)
		}
	}

	slices.SortFunc(providers, func(a, b models.ModelProvider) int {
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
	return providers
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

func NewModelDialogCmp() ModelDialog {
	return &modelDialogCmp{}
}
