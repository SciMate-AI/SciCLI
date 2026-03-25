package dialog

import (
	"strings"

	"github.com/SciMate-AI/scicli/internal/config"
	"github.com/SciMate-AI/scicli/internal/llm/models"
	"github.com/SciMate-AI/scicli/internal/tui/layout"
	"github.com/SciMate-AI/scicli/internal/tui/styles"
	"github.com/SciMate-AI/scicli/internal/tui/theme"
	"github.com/SciMate-AI/scicli/internal/tui/util"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type ShowProviderSetupDialogMsg struct {
	Provider models.ModelProvider
	ModelID  models.ModelID
}

type CloseProviderSetupDialogMsg struct{}

type ProviderSetupSavedMsg struct {
	Selection     config.OnboardingSelection
	ProviderLabel string
}

type ProviderSetupDialog interface {
	tea.Model
	layout.Bindings
	Open(provider models.ModelProvider, modelID models.ModelID)
}

type providerSetupStep int

const (
	providerSetupStepProvider providerSetupStep = iota
	providerSetupStepCredential
	providerSetupStepModel
)

type providerSetupDialogCmp struct {
	width, height int
	step          providerSetupStep

	providers         []config.OnboardingProvider
	selectedProvider  int
	selectedModel     int
	modelScrollOffset int

	useDetectedCredential bool
	credentialInputs      []textinput.Model
	credentialFocus       int

	errMsg string
}

type providerSetupKeyMap struct {
	Up      key.Binding
	Down    key.Binding
	Left    key.Binding
	Right   key.Binding
	Tab     key.Binding
	BackTab key.Binding
	Enter   key.Binding
	Escape  key.Binding
}

var providerSetupKeys = providerSetupKeyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("up/k", "move up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("down/j", "move down"),
	),
	Left: key.NewBinding(
		key.WithKeys("left", "h"),
		key.WithHelp("left/h", "switch mode"),
	),
	Right: key.NewBinding(
		key.WithKeys("right", "l"),
		key.WithHelp("right/l", "switch mode"),
	),
	Tab: key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("tab", "next field"),
	),
	BackTab: key.NewBinding(
		key.WithKeys("shift+tab"),
		key.WithHelp("shift+tab", "prev field"),
	),
	Enter: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "continue"),
	),
	Escape: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "back/close"),
	),
}

func (d *providerSetupDialogCmp) Init() tea.Cmd {
	return textinput.Blink
}

func (d *providerSetupDialogCmp) Open(provider models.ModelProvider, modelID models.ModelID) {
	d.providers = config.OnboardingProviders()
	d.errMsg = ""
	if len(d.providers) == 0 {
		d.step = providerSetupStepProvider
		return
	}

	d.selectedProvider = 0
	if provider != "" {
		for i, item := range d.providers {
			if item.Provider == provider {
				d.selectedProvider = i
				break
			}
		}
	} else if cfg := config.Get(); cfg != nil {
		if agentCfg, ok := cfg.Agents[config.AgentCoder]; ok {
			if model, ok := models.SupportedModels[agentCfg.Model]; ok {
				for i, item := range d.providers {
					if item.Provider == model.Provider {
						d.selectedProvider = i
						break
					}
				}
			}
		}
	}

	d.prefillCredentialInputs()
	d.resetModels(modelID)
	if provider != "" {
		d.step = providerSetupStepCredential
		return
	}
	d.step = providerSetupStepProvider
}

func (d *providerSetupDialogCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		d.width = msg.Width
		d.height = msg.Height
		return d, nil
	case tea.KeyMsg:
		if key.Matches(msg, providerSetupKeys.Escape) {
			switch d.step {
			case providerSetupStepProvider:
				return d, util.CmdHandler(CloseProviderSetupDialogMsg{})
			case providerSetupStepCredential:
				d.step = providerSetupStepProvider
				d.errMsg = ""
				return d, nil
			case providerSetupStepModel:
				d.step = providerSetupStepCredential
				d.errMsg = ""
				return d, d.currentFocusCmd()
			}
		}
	}

	switch d.step {
	case providerSetupStepProvider:
		return d.updateProviderStep(msg)
	case providerSetupStepCredential:
		return d.updateCredentialStep(msg)
	case providerSetupStepModel:
		return d.updateModelStep(msg)
	default:
		return d, nil
	}
}

func (d *providerSetupDialogCmp) View() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	width := 74
	if d.width > 0 {
		width = max(60, min(92, d.width-10))
	}

	title := lipgloss.JoinHorizontal(
		lipgloss.Left,
		baseStyle.Foreground(t.Primary()).Bold(true).Render("Provider Setup"),
		"  ",
		d.renderStepBadge(),
	)

	content := []string{
		baseStyle.Width(width).Padding(0, 1).Render(title),
		baseStyle.Width(width).Padding(0, 1).Foreground(t.TextMuted()).
			Render("Configure a provider after onboarding, then bind the coder agent to it."),
		baseStyle.Width(width).Render(""),
		baseStyle.Width(width).Padding(0, 1).Render(d.renderContextBanner(width-2)),
		baseStyle.Width(width).Render(""),
	}

	switch d.step {
	case providerSetupStepProvider:
		content = append(content, d.renderProviderStep(width)...)
	case providerSetupStepCredential:
		content = append(content, d.renderCredentialStep(width)...)
	case providerSetupStepModel:
		content = append(content, d.renderModelStep(width)...)
	}

	if strings.TrimSpace(d.errMsg) != "" {
		content = append(content,
			baseStyle.Width(width).Render(""),
			baseStyle.Width(width).Padding(0, 1).Foreground(t.Error()).Render(d.errMsg),
		)
	}

	body := lipgloss.JoinVertical(lipgloss.Left, content...)
	return baseStyle.Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderBackground(t.Background()).
		BorderForeground(t.BorderFocused()).
		Width(lipgloss.Width(body) + 4).
		Render(body)
}

func (d *providerSetupDialogCmp) BindingKeys() []key.Binding {
	return []key.Binding{
		providerSetupKeys.Up,
		providerSetupKeys.Down,
		providerSetupKeys.Left,
		providerSetupKeys.Right,
		providerSetupKeys.Tab,
		providerSetupKeys.BackTab,
		providerSetupKeys.Enter,
		providerSetupKeys.Escape,
	}
}

func (d *providerSetupDialogCmp) updateProviderStep(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, providerSetupKeys.Up):
			if d.selectedProvider > 0 {
				d.selectedProvider--
			} else {
				d.selectedProvider = len(d.providers) - 1
			}
			d.prefillCredentialInputs()
			d.resetModels("")
		case key.Matches(msg, providerSetupKeys.Down):
			if d.selectedProvider < len(d.providers)-1 {
				d.selectedProvider++
			} else {
				d.selectedProvider = 0
			}
			d.prefillCredentialInputs()
			d.resetModels("")
		case key.Matches(msg, providerSetupKeys.Enter):
			d.errMsg = ""
			d.step = providerSetupStepCredential
			return d, d.currentFocusCmd()
		}
	}
	return d, nil
}

func (d *providerSetupDialogCmp) updateCredentialStep(msg tea.Msg) (tea.Model, tea.Cmd) {
	current := d.currentProvider()

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, providerSetupKeys.Left), key.Matches(msg, providerSetupKeys.Right):
			if current.SupportsManualAPIKey && current.DetectedCredential {
				d.useDetectedCredential = !d.useDetectedCredential
				d.errMsg = ""
				return d, d.currentFocusCmd()
			}
		case msg.String() == "ctrl+v":
			if d.shouldFocusCredentialInputs() {
				active := d.activeCredentialInputs()
				if err := util.PasteSingleLineTextInput(&active[d.credentialFocus]); err != nil {
					d.errMsg = err.Error()
					return d, nil
				}
				d.syncCredentialInputs(active)
				d.errMsg = ""
				return d, nil
			}
		case key.Matches(msg, providerSetupKeys.Up), key.Matches(msg, providerSetupKeys.BackTab):
			if d.shouldFocusCredentialInputs() {
				return d, d.focusCredentialInput((d.credentialFocus+len(d.activeCredentialInputs())-1)%len(d.activeCredentialInputs()))
			}
		case key.Matches(msg, providerSetupKeys.Down), key.Matches(msg, providerSetupKeys.Tab):
			if d.shouldFocusCredentialInputs() {
				return d, d.focusCredentialInput((d.credentialFocus+1)%len(d.activeCredentialInputs()))
			}
		case key.Matches(msg, providerSetupKeys.Enter):
			if !d.validateCredentialStep() {
				return d, nil
			}
			d.errMsg = ""
			d.step = providerSetupStepModel
			return d, nil
		}
	}

	if d.shouldFocusCredentialInputs() {
		active := d.activeCredentialInputs()
		var cmd tea.Cmd
		active[d.credentialFocus], cmd = active[d.credentialFocus].Update(msg)
		d.syncCredentialInputs(active)
		return d, cmd
	}

	return d, nil
}

func (d *providerSetupDialogCmp) updateModelStep(msg tea.Msg) (tea.Model, tea.Cmd) {
	modelsForProvider := config.OnboardingModels(d.currentProvider().Provider)
	if len(modelsForProvider) == 0 {
		return d, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, providerSetupKeys.Up):
			if d.selectedModel > 0 {
				d.selectedModel--
			} else {
				d.selectedModel = len(modelsForProvider) - 1
			}
			d.keepModelSelectionVisible(len(modelsForProvider))
		case key.Matches(msg, providerSetupKeys.Down):
			if d.selectedModel < len(modelsForProvider)-1 {
				d.selectedModel++
			} else {
				d.selectedModel = 0
			}
			d.keepModelSelectionVisible(len(modelsForProvider))
		case key.Matches(msg, providerSetupKeys.Enter):
			selection := config.OnboardingSelection{
				Provider:      d.currentProvider().Provider,
				APIKey:        strings.TrimSpace(d.credentialInputValue("apiKey")),
				PersistAPIKey: !d.currentProvider().DetectedCredential || !d.useDetectedCredential,
				ModelID:       modelsForProvider[d.selectedModel].ID,
				BaseURL:       strings.TrimSpace(d.credentialInputValue("baseURL")),
				CustomModel:   strings.TrimSpace(d.credentialInputValue("model")),
			}
			if err := config.SaveProviderSelection(selection); err != nil {
				d.errMsg = err.Error()
				return d, nil
			}
			return d, util.CmdHandler(ProviderSetupSavedMsg{
				Selection:     selection,
				ProviderLabel: d.currentProvider().Label,
			})
		}
	}

	return d, nil
}

func (d *providerSetupDialogCmp) renderProviderStep(width int) []string {
	lines := []string{
		d.renderSectionTitle(width, "Choose Provider", "Up/Down selects. Enter continues."),
		styles.BaseStyle().Width(width).Render(""),
	}

	for idx, provider := range d.providers {
		lines = append(lines, d.renderProviderRow(width, idx, provider))
	}

	return lines
}

func (d *providerSetupDialogCmp) renderCredentialStep(width int) []string {
	current := d.currentProvider()
	lines := []string{
		d.renderSectionTitle(width, "Credentials", "Tab cycles fields. Enter continues to model selection."),
		styles.BaseStyle().Width(width).Render(""),
		d.renderProviderSummary(width, current),
		styles.BaseStyle().Width(width).Render(""),
	}

	if current.DetectedCredential && current.SupportsManualAPIKey {
		modeLabel := "Use detected credential"
		if !d.useDetectedCredential {
			modeLabel = "Enter API key manually"
		}
		lines = append(lines, styles.BaseStyle().Width(width).Padding(0, 1).
			Render("Mode: "+modeLabel+"  |  Left/Right toggles"))
		lines = append(lines, styles.BaseStyle().Width(width).Render(""))
	}

	if !d.shouldFocusCredentialInputs() {
		lines = append(lines, d.renderHintBox(width, "Using detected credential. Press Enter to continue."))
		return lines
	}

	for _, block := range d.renderCredentialBlocks(width) {
		lines = append(lines, block)
	}

	return lines
}

func (d *providerSetupDialogCmp) renderModelStep(width int) []string {
	modelsForProvider := config.OnboardingModels(d.currentProvider().Provider)
	lines := []string{
		d.renderSectionTitle(width, "Coder Model", "Up/Down selects. Enter saves."),
		styles.BaseStyle().Width(width).Render(""),
		d.renderHintBox(width, "SciCLI expects OpenAI-compatible endpoints to implement chat/completions with tool calling. Many simple relay endpoints do not."),
		styles.BaseStyle().Width(width).Render(""),
	}

	if len(modelsForProvider) == 0 {
		lines = append(lines, styles.BaseStyle().Width(width).Padding(0, 1).Render("No models available for this provider."))
		return lines
	}

	maxVisible := min(10, len(modelsForProvider))
	end := min(d.modelScrollOffset+maxVisible, len(modelsForProvider))
	for idx := d.modelScrollOffset; idx < end; idx++ {
		lines = append(lines, d.renderModelRow(width, idx, modelsForProvider[idx].Name))
	}

	return lines
}

func (d *providerSetupDialogCmp) stepIndicator() string {
	labels := []string{"Provider", "Credential", "Model"}
	current := int(d.step)
	items := make([]string, 0, len(labels))
	for idx, label := range labels {
		if idx == current {
			items = append(items, "["+label+"]")
			continue
		}
		items = append(items, label)
	}
	return strings.Join(items, "  ")
}

func (d *providerSetupDialogCmp) currentProvider() config.OnboardingProvider {
	if len(d.providers) == 0 {
		return config.OnboardingProvider{}
	}
	return d.providers[d.selectedProvider]
}

func (d *providerSetupDialogCmp) initCredentialInputs() {
	baseURL := textinput.New()
	baseURL.Placeholder = "Base URL"
	baseURL.Prompt = ""
	baseURL.Width = 56

	apiKey := textinput.New()
	apiKey.Placeholder = "API key"
	apiKey.Prompt = ""
	apiKey.Width = 56

	model := textinput.New()
	model.Placeholder = "Custom model"
	model.Prompt = ""
	model.Width = 56

	d.credentialInputs = []textinput.Model{baseURL, apiKey, model}
}

func (d *providerSetupDialogCmp) prefillCredentialInputs() {
	if len(d.credentialInputs) == 0 {
		d.initCredentialInputs()
	}

	for i := range d.credentialInputs {
		d.credentialInputs[i].SetValue("")
		d.credentialInputs[i].Blur()
	}

	current := d.currentProvider()
	existing, ok := config.Get().Providers[current.Provider]
	if ok {
		d.credentialInputs[0].SetValue(strings.TrimSpace(existing.BaseURL))
		d.credentialInputs[1].SetValue(strings.TrimSpace(existing.APIKey))
		d.credentialInputs[2].SetValue(strings.TrimSpace(existing.Model))
	}

	d.useDetectedCredential = current.DetectedCredential && strings.TrimSpace(d.credentialInputs[1].Value()) == ""
	d.credentialFocus = 0
}

func (d *providerSetupDialogCmp) currentFocusCmd() tea.Cmd {
	if d.step == providerSetupStepCredential && d.shouldFocusCredentialInputs() {
		return d.focusCredentialInput(d.credentialFocus)
	}
	return nil
}

func (d *providerSetupDialogCmp) activeCredentialInputs() []textinput.Model {
	current := d.currentProvider()
	active := make([]textinput.Model, 0, 3)
	if current.RequiresBaseURL {
		active = append(active, d.credentialInputs[0])
	}
	if current.SupportsManualAPIKey && (!current.DetectedCredential || !d.useDetectedCredential) {
		active = append(active, d.credentialInputs[1])
	}
	if current.RequiresModel {
		active = append(active, d.credentialInputs[2])
	}
	return active
}

func (d *providerSetupDialogCmp) syncCredentialInputs(active []textinput.Model) {
	current := d.currentProvider()
	cursor := 0
	if current.RequiresBaseURL {
		d.credentialInputs[0] = active[cursor]
		cursor++
	}
	if current.SupportsManualAPIKey && (!current.DetectedCredential || !d.useDetectedCredential) {
		d.credentialInputs[1] = active[cursor]
		cursor++
	}
	if current.RequiresModel {
		d.credentialInputs[2] = active[cursor]
	}
}

func (d *providerSetupDialogCmp) focusCredentialInput(index int) tea.Cmd {
	active := d.activeCredentialInputs()
	if len(active) == 0 {
		return nil
	}
	if index >= len(active) {
		index = 0
	}
	d.credentialFocus = index
	var cmd tea.Cmd
	for i := range active {
		if i == index {
			cmd = active[i].Focus()
		} else {
			active[i].Blur()
		}
	}
	d.syncCredentialInputs(active)
	return cmd
}

func (d *providerSetupDialogCmp) shouldFocusCredentialInputs() bool {
	return len(d.activeCredentialInputs()) > 0
}

func (d *providerSetupDialogCmp) credentialInputValue(name string) string {
	switch name {
	case "baseURL":
		return d.credentialInputs[0].Value()
	case "apiKey":
		return d.credentialInputs[1].Value()
	case "model":
		return d.credentialInputs[2].Value()
	default:
		return ""
	}
}

func (d *providerSetupDialogCmp) validateCredentialStep() bool {
	current := d.currentProvider()
	switch {
	case current.RequiresBaseURL && strings.TrimSpace(d.credentialInputValue("baseURL")) == "":
		d.errMsg = "Base URL is required"
		return false
	case current.RequiresModel && strings.TrimSpace(d.credentialInputValue("model")) == "":
		d.errMsg = "Custom model is required"
		return false
	case current.SupportsManualAPIKey && !current.OptionalAPIKey && !current.DetectedCredential && strings.TrimSpace(d.credentialInputValue("apiKey")) == "":
		d.errMsg = "API key is required"
		return false
	case current.SupportsManualAPIKey && !current.OptionalAPIKey && current.DetectedCredential && !d.useDetectedCredential && strings.TrimSpace(d.credentialInputValue("apiKey")) == "":
		d.errMsg = "API key is required"
		return false
	default:
		d.errMsg = ""
		return true
	}
}

func (d *providerSetupDialogCmp) resetModels(initialModel models.ModelID) {
	d.selectedModel = 0
	d.modelScrollOffset = 0
	defaultModel := config.DefaultOnboardingModel(d.currentProvider().Provider)
	if initialModel != "" {
		if model, ok := models.SupportedModels[initialModel]; ok && model.Provider == d.currentProvider().Provider {
			defaultModel = initialModel
		}
	} else if cfg := config.Get(); cfg != nil {
		if agentCfg, ok := cfg.Agents[config.AgentCoder]; ok {
			if model, ok := models.SupportedModels[agentCfg.Model]; ok && model.Provider == d.currentProvider().Provider {
				defaultModel = agentCfg.Model
			}
		}
	}

	modelsForProvider := config.OnboardingModels(d.currentProvider().Provider)
	for idx, model := range modelsForProvider {
		if model.ID == defaultModel {
			d.selectedModel = idx
			break
		}
	}
	d.keepModelSelectionVisible(len(modelsForProvider))
}

func (d *providerSetupDialogCmp) keepModelSelectionVisible(total int) {
	const maxVisible = 10
	if total <= maxVisible {
		d.modelScrollOffset = 0
		return
	}
	if d.selectedModel < d.modelScrollOffset {
		d.modelScrollOffset = d.selectedModel
	}
	if d.selectedModel >= d.modelScrollOffset+maxVisible {
		d.modelScrollOffset = d.selectedModel - (maxVisible - 1)
	}
}

func NewProviderSetupDialogCmp() ProviderSetupDialog {
	return &providerSetupDialogCmp{}
}

func (d *providerSetupDialogCmp) renderStepBadge() string {
	t := theme.CurrentTheme()
	return styles.Padded().
		Background(t.Primary()).
		Foreground(t.Background()).
		Bold(true).
		Render(d.stepIndicator())
}

func (d *providerSetupDialogCmp) renderContextBanner(width int) string {
	t := theme.CurrentTheme()
	current := d.currentProvider()
	label := "Selected: none"
	if current.Label != "" {
		label = "Selected: " + current.Label
	}
	return lipgloss.NewStyle().
		Width(width).
		Padding(0, 1).
		Background(t.BackgroundDarker()).
		Foreground(t.Text()).
		Render(label)
}

func (d *providerSetupDialogCmp) renderSectionTitle(width int, title string, subtitle string) string {
	t := theme.CurrentTheme()
	base := styles.BaseStyle()
	lines := []string{
		base.Width(width).Padding(0, 1).Bold(true).Render(title),
	}
	if strings.TrimSpace(subtitle) != "" {
		lines = append(lines, base.Width(width).Padding(0, 1).Foreground(t.TextMuted()).Render(subtitle))
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (d *providerSetupDialogCmp) renderProviderRow(width int, idx int, provider config.OnboardingProvider) string {
	t := theme.CurrentTheme()
	selected := idx == d.selectedProvider
	rowWidth := width - 2
	label := provider.Label
	if selected {
		label = "› " + label
	} else {
		label = "  " + label
	}

	badges := make([]string, 0, 2)
	if config.ProviderReady(provider.Provider) {
		badges = append(badges, d.renderBadge("READY", t.Success(), t.Background()))
	} else if provider.DetectedCredential {
		badges = append(badges, d.renderBadge("DETECTED", t.Info(), t.Background()))
	}
	if provider.Provider == models.ProviderOpenAICompatible {
		badges = append(badges, d.renderBadge("CUSTOM", t.Primary(), t.Background()))
	}

	style := lipgloss.NewStyle().
		Width(rowWidth).
		Padding(0, 1).
		Border(lipgloss.NormalBorder()).
		BorderForeground(t.TextMuted()).
		Foreground(t.Text()).
		Background(t.Background())
	if selected {
		style = style.BorderForeground(t.Primary()).Background(t.BackgroundDarker())
	}

	line := label
	if len(badges) > 0 {
		line += "  " + strings.Join(badges, " ")
	}
	return style.Render(line)
}

func (d *providerSetupDialogCmp) renderProviderSummary(width int, provider config.OnboardingProvider) string {
	lines := []string{provider.Label}
	if provider.CredentialHint != "" {
		lines = append(lines, "Credential: "+provider.CredentialHint)
	}
	if provider.RequiresBaseURL {
		lines = append(lines, "Base URL: "+provider.BaseURLHint)
	}
	if provider.RequiresModel {
		lines = append(lines, "Model: "+provider.ModelHint)
	}
	return d.renderHintBox(width, strings.Join(lines, "\n"))
}

func (d *providerSetupDialogCmp) renderCredentialBlocks(width int) []string {
	current := d.currentProvider()
	blocks := make([]string, 0, 3)
	inputWidth := max(28, width-10)

	if current.RequiresBaseURL {
		d.credentialInputs[0].Width = inputWidth
		blocks = append(blocks, d.renderInputBlock(width, "Base URL", d.currentProvider().BaseURLHint, d.credentialInputs[0].View()))
	}
	if current.SupportsManualAPIKey && (!current.DetectedCredential || !d.useDetectedCredential) {
		d.credentialInputs[1].Width = inputWidth
		hint := current.CredentialHint
		if hint == "" {
			hint = "Paste the provider key used for Authorization: Bearer"
		}
		blocks = append(blocks, d.renderInputBlock(width, "API Key", hint, d.credentialInputs[1].View()))
	}
	if current.RequiresModel {
		d.credentialInputs[2].Width = inputWidth
		blocks = append(blocks, d.renderInputBlock(width, "Upstream Model", d.currentProvider().ModelHint, d.credentialInputs[2].View()))
	}

	return blocks
}

func (d *providerSetupDialogCmp) renderInputBlock(width int, label string, hint string, inputView string) string {
	t := theme.CurrentTheme()
	base := styles.BaseStyle()
	parts := []string{
		base.Bold(true).Render(label),
	}
	if strings.TrimSpace(hint) != "" {
		parts = append(parts, base.Foreground(t.TextMuted()).Render(hint))
	}
	parts = append(parts, inputView)
	return lipgloss.NewStyle().
		Width(width - 2).
		Padding(0, 1).
		Border(lipgloss.NormalBorder()).
		BorderForeground(t.TextMuted()).
		Render(strings.Join(parts, "\n"))
}

func (d *providerSetupDialogCmp) renderHintBox(width int, content string) string {
	t := theme.CurrentTheme()
	return lipgloss.NewStyle().
		Width(width - 2).
		Padding(0, 1).
		Background(t.BackgroundDarker()).
		Foreground(t.TextMuted()).
		Render(content)
}

func (d *providerSetupDialogCmp) renderModelRow(width int, idx int, name string) string {
	t := theme.CurrentTheme()
	selected := idx == d.selectedModel
	style := lipgloss.NewStyle().
		Width(width - 2).
		Padding(0, 1).
		Border(lipgloss.NormalBorder()).
		BorderForeground(t.TextMuted()).
		Foreground(t.Text())
	label := "  " + name
	if selected {
		style = style.BorderForeground(t.Primary()).Background(t.Primary()).Foreground(t.Background()).Bold(true)
		label = "› " + name
	}
	return style.Render(label)
}

func (d *providerSetupDialogCmp) renderBadge(label string, bg lipgloss.TerminalColor, fg lipgloss.TerminalColor) string {
	return lipgloss.NewStyle().
		Padding(0, 1).
		Background(bg).
		Foreground(fg).
		Bold(true).
		Render(label)
}
