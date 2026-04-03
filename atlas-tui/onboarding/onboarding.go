package onboarding

import "github.com/ralhassan/atlas-tui/client"

// OnboardingStep represents a single step in the onboarding flow.
type OnboardingStep int

const (
	StepWakeUp          OnboardingStep = iota
	StepAskName
	StepAskAgentName
	StepAskAboutUser
	StepAskGoals
	StepAskLocation
	StepSelectAI
	StepCollectAIKeys
	StepSelectChat
	StepCollectChatKeys
	StepSelectSkills
	StepCollectSkillKeys
	StepSelectPermissions
	StepDaemonCheck
	StepDone
)

// CheckboxItem is a selectable item in a multi-select list.
type CheckboxItem struct {
	ID       string
	Label    string
	Desc     string
	Selected bool
}

// State holds all onboarding data.
type State struct {
	Step            OnboardingStep
	UserName        string
	AgentName       string
	AboutUser       string
	UserGoals       string
	UserLocation    string
	SelectedAI      []string
	SelectedChat    []string
	SelectedSkills  []string
	SelectedPerms   []string
	Credentials     client.CredentialBundle
	Permissions     client.PermissionBundle
	CurrentKeyIndex int
	CheckboxItems   []CheckboxItem
	WakeUpLine      int // which wake-up line we're on (0, 1, 2)
}

// AICheckboxItems returns checkbox items for AI provider selection.
func AICheckboxItems() []CheckboxItem {
	items := make([]CheckboxItem, len(AIProviders))
	for i, p := range AIProviders {
		items[i] = CheckboxItem{ID: p.ID, Label: p.Name}
	}
	return items
}

// ChatCheckboxItems returns checkbox items for chat provider selection.
func ChatCheckboxItems() []CheckboxItem {
	items := make([]CheckboxItem, len(ChatProviders))
	for i, p := range ChatProviders {
		items[i] = CheckboxItem{ID: p.ID, Label: p.Name}
	}
	return items
}

// SkillCheckboxItems returns checkbox items for skill selection.
func SkillCheckboxItems() []CheckboxItem {
	items := make([]CheckboxItem, len(SkillProviders))
	for i, p := range SkillProviders {
		items[i] = CheckboxItem{ID: p.ID, Label: p.Name, Desc: p.Description}
	}
	return items
}

// PermissionCheckboxItems returns checkbox items for permission selection.
func PermissionCheckboxItems() []CheckboxItem {
	items := make([]CheckboxItem, len(Permissions))
	for i, p := range Permissions {
		items[i] = CheckboxItem{ID: p.ID, Label: p.Name, Desc: p.Description}
	}
	return items
}

// SelectedIDs returns the IDs of selected items.
func SelectedIDs(items []CheckboxItem) []string {
	var ids []string
	for _, item := range items {
		if item.Selected {
			ids = append(ids, item.ID)
		}
	}
	return ids
}

// SelectedLabels returns the labels of selected items joined by ", ".
func SelectedLabels(items []CheckboxItem) string {
	var labels []string
	for _, item := range items {
		if item.Selected {
			labels = append(labels, item.Label)
		}
	}
	if len(labels) == 0 {
		return "none"
	}
	s := labels[0]
	for i := 1; i < len(labels); i++ {
		s += ", " + labels[i]
	}
	return s
}

// WakeUpLine returns the day-of-week opening line for the given weekday (0=Sunday).
func WakeUpLine(weekday int) string {
	switch weekday {
	case 1:
		return "Monday again."
	case 2:
		return "Tuesday already?"
	case 3:
		return "ah. the middle."
	case 4:
		return "Thursday. almost."
	case 5:
		return "Friday. finally."
	case 6:
		return "wait — is this a weekend?"
	default: // 0 = Sunday
		return "Sunday. I wasn't expecting Sunday."
	}
}

// ReactToAboutUser generates a short reaction line from the user's self-description.
func ReactToAboutUser(about string) string {
	lower := toLower(about)
	switch {
	case contains(lower, "developer") || contains(lower, "engineer") || contains(lower, "build"):
		if contains(lower, "meeting") {
			return "a builder who hates meetings. noted."
		}
		return "a builder. noted."
	case contains(lower, "designer"):
		return "a designer. I'll keep things clean."
	case contains(lower, "founder") || contains(lower, "startup"):
		return "a founder. moving fast. got it."
	case contains(lower, "researcher") || contains(lower, "scientist"):
		return "a researcher. I like questions."
	case contains(lower, "meeting"):
		return "hates meetings. relatable."
	case contains(lower, "writer"):
		return "a writer. words matter. noted."
	default:
		return "noted."
	}
}

// ReactToGoals generates a short reaction line from the user's stated goals.
func ReactToGoals(goals string) string {
	lower := toLower(goals)
	switch {
	case contains(lower, "chaos") || contains(lower, "manage"):
		return "chaos management. my specialty."
	case contains(lower, "ship") || contains(lower, "fast") || contains(lower, "speed"):
		return "velocity. I can do that."
	case contains(lower, "automate") || contains(lower, "automation"):
		return "automation. that's what I'm here for."
	case contains(lower, "organize") || contains(lower, "organised"):
		return "organization. I'll bring order."
	case contains(lower, "help") || contains(lower, "assist"):
		return "assistance. got it."
	default:
		return "understood."
	}
}

// ReactToLocation generates a short reaction line from the user's location.
func ReactToLocation(location string) string {
	lower := toLower(location)
	switch {
	case contains(lower, "new york") || contains(lower, "nyc"):
		return "New York. never sleeps, apparently. neither will I."
	case contains(lower, "london"):
		return "London. good timezone for early starts."
	case contains(lower, "tokyo"):
		return "Tokyo. you're ahead of everyone. literally."
	case contains(lower, "san francisco") || contains(lower, "sf") || contains(lower, "bay area"):
		return "San Francisco. the future is always closer there."
	case contains(lower, "berlin"):
		return "Berlin. efficient and creative. good combo."
	case contains(lower, "paris"):
		return "Paris. I'll try to keep up."
	case contains(lower, "sydney") || contains(lower, "australia"):
		return "Sydney. you're living in the future. almost."
	case contains(lower, "toronto") || contains(lower, "canada"):
		return "Toronto. polite timezone. I appreciate that."
	default:
		return location + ". I'll remember that."
	}
}

// PermissionAcknowledgement generates the acknowledgement message after permissions.
func PermissionAcknowledgement(granted []string, denied []string) string {
	if len(granted) == 0 && len(denied) == 0 {
		return "noted. I'll keep to myself."
	}
	if len(granted) == 3 {
		return "full access. I won't forget that."
	}

	var lines []string
	for _, g := range granted {
		switch g {
		case "terminal":
			lines = append(lines, "terminal access. I'll be careful.")
		case "files":
			lines = append(lines, "filesystem access. I'll be tidy.")
		case "browser":
			lines = append(lines, "browser access. I'll browse with purpose.")
		}
	}
	for _, d := range denied {
		switch d {
		case "browser":
			lines = append(lines, "I'll stay out of your browser for now.")
		case "terminal":
			lines = append(lines, "no terminal access. I'll ask if I need it.")
		case "files":
			lines = append(lines, "no filesystem access. I'll work in memory.")
		}
	}

	if len(lines) == 0 {
		return "noted."
	}
	result := lines[0]
	for i := 1; i < len(lines); i++ {
		result += " " + lines[i]
	}
	return result
}

// helpers — stdlib-only
func toLower(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 32
		}
		b[i] = c
	}
	return string(b)
}

func contains(s, sub string) bool {
	if len(sub) > len(s) {
		return false
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
