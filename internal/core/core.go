package core

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ValidateAlienxEndpoint validates and normalizes a personal ALIEN X server URL.
func ValidateAlienxEndpoint(endpoint string, allowDevelopmentLan bool) (string, error) {
	trimmed := strings.TrimSpace(endpoint)
	if trimmed == "" {
		return "", errors.New("Enter a valid server URL.")
	}
	u, err := url.Parse(trimmed)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", errors.New("Enter a valid server URL.")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("Use a server URL without credentials or query parameters.")
	}
	host := u.Hostname()
	local := host == "localhost" || host == "127.0.0.1"
	privateLan := isPrivateLanIPv4(host)

	if u.Scheme != "https" && !(u.Scheme == "http" && (local || (allowDevelopmentLan && privateLan))) {
		return "", errors.New("Use HTTPS for your personal server. Development builds also allow HTTP on localhost or a private Wi-Fi IP.")
	}
	return strings.TrimRight(u.String(), "/"), nil
}

func isPrivateLanIPv4(host string) bool {
	parts := strings.Split(host, ".")
	if len(parts) != 4 {
		return false
	}
	var octets [4]int
	for i, p := range parts {
		if p == "" {
			return false
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || n > 255 {
			return false
		}
		octets[i] = n
	}
	return octets[0] == 10 ||
		(octets[0] == 172 && octets[1] >= 16 && octets[1] <= 31) ||
		(octets[0] == 192 && octets[1] == 168)
}

var (
	trailingPunctRe = regexp.MustCompile(`[.!?।]+$`)
	tamilAliases    = map[string]string{
		"சிஇஓவிடம் பேசு":              "talk to ax-ceo",
		"சிஇஓ உடன் பேசு":              "talk to ax-ceo",
		"ஹேக்கரிடம் பேசு":             "talk to ax-hacker",
		"ஹேக்கர் உடன் பேசு":           "talk to ax-hacker",
		"ஏலியன்எக்ஸ் உதவியாளர்":        "switch to alienx",
		"இன்றைய திட்டம்":              "plan my day",
		"இன்றைக்கு என்ன செய்ய வேண்டும்": "plan my day",
		"எனது பணிகள்":                "tasks",
		"என் பணிகள்":                 "tasks",
		"பணிகளை காட்டு":              "tasks",
		"எனது பழக்கங்கள்":             "habits",
		"என் பழக்கங்கள்":              "habits",
		"எனது வழக்கம்":               "routine",
		"எனது செலவுகள்":              "finances",
		"என் செலவுகள்":               "finances",
		"எனது நிதி":                  "finances",
		"பாதுகாப்பு நிலை":             "security checks",
		"இன்றைய நிலை":                "status report",
		"என்னைப் பற்றி என்ன தெரியும்":   "what do you remember about me",
		"என்னை பற்றி என்ன தெரியும்":    "what do you remember about me",
		"செயலியை பூட்டு":              "lock app",
		"செயலியைப் பூட்டு":             "lock app",
		"முகப்புக்கு செல்":             "exit to os",
		"indraya thittam":            "plan my day",
		"en panigal":                 "tasks",
		"en selavugal":               "finances",
	}
)

// NormalizeTamilCommand maps exact Tamil/Tanglish voice phrases to canonical directives.
func NormalizeTamilCommand(input string) string {
	clean := strings.TrimSpace(trailingPunctRe.ReplaceAllString(strings.TrimSpace(input), ""))
	if mapped, ok := tamilAliases[strings.ToLower(clean)]; ok {
		return mapped
	}
	return clean
}

// VoiceAgent identifies the active ALIEN X agent workspace.
type VoiceAgent string

const (
	AgentPersonal VoiceAgent = "personal"
	AgentCEO      VoiceAgent = "ax-ceo"
	AgentHacker   VoiceAgent = "ax-hacker"
)

// AgentDirectiveResult holds the resolved agent and cleaned directive.
type AgentDirectiveResult struct {
	Agent      VoiceAgent
	Directive  string
	SwitchOnly bool
}

var (
	alienxPrefixRe   = regexp.MustCompile(`(?i)^(?:alienx|alien\s*x|ஏலியன்எக்ஸ்|எலியன்எக்ஸ்)[\s,:]+`)
	switchPersonalRe = regexp.MustCompile(`(?i)^(?:switch to alien\s*x|return to alien\s*x|personal assistant|personal mode|back to personal|ஏலியன்எக்ஸ் உதவியாளர்)[.!?]?$`)
	agentMatchRe     = regexp.MustCompile(`(?i)^(?:(?:ask|talk to|switch to|connect to|open|use)\s+)?(?:ax[\s-]*)?(ceo|hacker)(?:\s+agent)?(?:[\s,:-]+(.*))?[.!?]?$`)
)

// ResolveAgentDirective parses agent switch commands and follow-up directives.
func ResolveAgentDirective(input string, current VoiceAgent) AgentDirectiveResult {
	if current == "" {
		current = AgentPersonal
	}
	text := strings.TrimSpace(alienxPrefixRe.ReplaceAllString(strings.TrimSpace(input), ""))
	if switchPersonalRe.MatchString(text) {
		return AgentDirectiveResult{Agent: AgentPersonal, Directive: "", SwitchOnly: true}
	}
	if m := agentMatchRe.FindStringSubmatch(text); m != nil {
		ag := AgentHacker
		if strings.EqualFold(m[1], "ceo") {
			ag = AgentCEO
		}
		dir := strings.TrimSpace(m[2])
		return AgentDirectiveResult{
			Agent:      ag,
			Directive:  dir,
			SwitchOnly: dir == "",
		}
	}
	return AgentDirectiveResult{Agent: current, Directive: text, SwitchOnly: false}
}

// SpeechActivity tracks live voice turn VAD metering state.
type SpeechActivity struct {
	HeardSpeech  bool
	LastSpeechAt int
	PeakMetering *float64
	Finished     bool
}

// UpdateSpeechActivity updates VAD state for a live voice recording turn.
func UpdateSpeechActivity(previous SpeechActivity, durationMs int, metering *float64) SpeechActivity {
	if metering == nil {
		finished := previous.HeardSpeech && durationMs > 1500 && durationMs-previous.LastSpeechAt > 1300
		return SpeechActivity{
			HeardSpeech:  previous.HeardSpeech,
			LastSpeechAt: previous.LastSpeechAt,
			PeakMetering: previous.PeakMetering,
			Finished:     finished,
		}
	}
	peak := *metering
	if previous.PeakMetering != nil && *previous.PeakMetering > peak {
		peak = *previous.PeakMetering
	}
	adaptiveThreshold := -38.0
	if peak > -26.0 {
		adaptiveThreshold = math.Max(-38.0, peak-12.0)
	}
	isSpeech := *metering > adaptiveThreshold
	next := SpeechActivity{
		HeardSpeech:  previous.HeardSpeech,
		LastSpeechAt: previous.LastSpeechAt,
		PeakMetering: &peak,
	}
	if isSpeech {
		next.HeardSpeech = true
		next.LastSpeechAt = durationMs
	}
	next.Finished = next.HeardSpeech && durationMs > 1500 && durationMs-next.LastSpeechAt > 1300
	return next
}

var (
	thinkBlockRe = regexp.MustCompile(`(?is)<think>.*?</think>`)
	clauseSepRe  = regexp.MustCompile(`[,;:—–]\s+`)
)

const (
	maxSpokenChars   = 2500
	firstChunkTarget = 140
	mergeMinChars    = 35
	chunkMaxChars    = 480
)

// SplitSpeechChunks splits assistant replies into natural clause/sentence chunks for pipelined TTS.
func SplitSpeechChunks(input string) []string {
	cleaned := strings.TrimSpace(thinkBlockRe.ReplaceAllString(input, ""))
	runes := []rune(cleaned)
	if len(runes) > maxSpokenChars {
		cleaned = string(runes[:maxSpokenChars])
	}
	if cleaned == "" {
		return nil
	}

	rawSentences := splitSentences(cleaned)
	if len(rawSentences) == 0 {
		return nil
	}

	var expanded []string
	for i, sentence := range rawSentences {
		sRunes := []rune(sentence)
		if i == 0 && len(sRunes) > firstChunkTarget {
			window := string(sRunes[24:firstChunkTarget])
			if loc := clauseSepRe.FindStringIndex(window); loc != nil {
				firstSepRuneLen := utf8.RuneCountInString(window[:loc[0]]) + 1
				splitAt := 24 + firstSepRuneLen
				firstClause := strings.TrimSpace(string(sRunes[:splitAt]))
				remainder := strings.TrimSpace(string(sRunes[splitAt:]))
				if firstClause != "" && remainder != "" {
					expanded = append(expanded, firstClause, remainder)
					continue
				}
			}
		}
		if len(sRunes) <= chunkMaxChars {
			expanded = append(expanded, sentence)
		} else {
			expanded = append(expanded, splitLongSentence(sentence, chunkMaxChars)...)
		}
	}

	var merged []string
	for _, part := range expanded {
		if len(merged) == 0 {
			merged = append(merged, part)
			continue
		}
		prev := merged[len(merged)-1]
		prevLen := utf8.RuneCountInString(prev)
		partLen := utf8.RuneCountInString(part)
		limit := chunkMaxChars
		if len(merged) == 1 {
			limit = firstChunkTarget
		}
		if (prevLen < mergeMinChars || partLen < 20) && prevLen+1+partLen <= limit {
			merged[len(merged)-1] = prev + " " + part
		} else {
			merged = append(merged, part)
		}
	}
	return merged
}

func isSentenceTerminator(r rune) bool {
	return r == '.' || r == '!' || r == '?' || r == '।' || r == '…'
}

func splitSentences(text string) []string {
	var out []string
	var buf strings.Builder
	runes := []rune(text)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if r == '\n' {
			if s := strings.TrimSpace(buf.String()); s != "" {
				out = append(out, s)
			}
			buf.Reset()
			continue
		}
		buf.WriteRune(r)
		if isSentenceTerminator(r) && i+1 < len(runes) && (runes[i+1] == ' ' || runes[i+1] == '\t' || runes[i+1] == '\n') {
			if s := strings.TrimSpace(buf.String()); s != "" {
				out = append(out, s)
			}
			buf.Reset()
			for i+1 < len(runes) && (runes[i+1] == ' ' || runes[i+1] == '\t') {
				i++
			}
		}
	}
	if s := strings.TrimSpace(buf.String()); s != "" {
		out = append(out, s)
	}
	return out
}

func splitLongSentence(sentence string, maxLen int) []string {
	words := strings.Fields(sentence)
	var chunks []string
	current := ""
	for _, w := range words {
		wRunes := []rune(w)
		if len(wRunes) > maxLen {
			w = string(wRunes[:maxLen])
		}
		if current == "" {
			current = w
		} else if utf8.RuneCountInString(current)+1+utf8.RuneCountInString(w) <= maxLen {
			current = current + " " + w
		} else {
			chunks = append(chunks, current)
			current = w
		}
	}
	if current != "" {
		chunks = append(chunks, current)
	}
	return chunks
}

var (
	lockCmdRe = regexp.MustCompile(`^(?:please )?(?:lock|lockdown|lock (?:the )?app|lock (?:my )?session|lock (?:the )?vault|lock (?:it|me) down)(?: please| now)?$`)
	uiCmdRe   = regexp.MustCompile(`^(?:please )?(?:exit|ui|exit ai|switch to ui|return to ui|exit to os|go home)(?: please)?$`)
	spaceRe   = regexp.MustCompile(`\s+`)
)

func normalizeQuery(input string) string {
	q := strings.ToLower(strings.TrimSpace(input))
	q = strings.ReplaceAll(q, "’", "'")
	q = strings.TrimRight(q, ".!?")
	q = spaceRe.ReplaceAllString(q, " ")
	return strings.TrimSpace(q)
}

// AlienxSessionAction returns "lock", "ui", or "" for explicit session control commands.
func AlienxSessionAction(input string) string {
	q := normalizeQuery(input)
	if lockCmdRe.MatchString(q) {
		return "lock"
	}
	if uiCmdRe.MatchString(q) {
		return "ui"
	}
	return ""
}

// Protocol defines a quick command chip in ALIEN X.
type Protocol struct {
	Label string
	Cmd   string
}

// AlienxDestinations maps destination action keys to their Expo Router hrefs and labels.
var AlienxDestinations = map[string]struct {
	Href  string
	Label string
}{
	"tasks":         {Href: "/tasks", Label: "Open tasks"},
	"security":      {Href: "/security/overview", Label: "Review local security checks"},
	"finance":       {Href: "/finance", Label: "Open finance"},
	"habits":        {Href: "/habits", Label: "Open habits"},
	"routine":       {Href: "/routine", Label: "Open routine"},
	"agents-hacker": {Href: "/agents?role=hacker", Label: "Open AX-Hacker"},
	"agents-ceo":    {Href: "/agents?role=ceo", Label: "Open AX-CEO"},
	"agents-gates":  {Href: "/agents?tab=approvals", Label: "Open approval queue"},
}

// AlienxProtocols lists the standard ALIEN X protocol commands.
var AlienxProtocols = []Protocol{
	{Label: "Plan my day", Cmd: "plan my day"},
	{Label: "My memory", Cmd: "what do you remember about me"},
	{Label: "Status", Cmd: "status report"},
	{Label: "Tasks", Cmd: "tasks"},
	{Label: "Habits", Cmd: "habits"},
	{Label: "Finance", Cmd: "finances"},
	{Label: "Security", Cmd: "security checks"},
	{Label: "AX-Hacker", Cmd: "open ax-hacker"},
	{Label: "AX-CEO", Cmd: "open ax-ceo"},
	{Label: "Approvals", Cmd: "approval gates"},
	{Label: "Device", Cmd: "device specs"},
	{Label: "Help", Cmd: "help"},
}

// TaskItem represents a priority task in the ALIEN X brief.
type TaskItem struct {
	ID    string
	Title string
	DueOn string
}

// HabitItem represents a habit status in the ALIEN X brief.
type HabitItem struct {
	Name      string
	Frequency string
	Done      bool
	DueToday  bool
	Streak    int
}

// MoneySummary represents monthly cashflow totals.
type MoneySummary struct {
	Income      float64        `json:"income"`
	Expenses    float64        `json:"expenses"`
	Net         float64        `json:"net"`
	TopCategory *CategoryTotal `json:"topCategory,omitempty"`
}

// CategoryTotal holds the top expense category name and amount.
type CategoryTotal struct {
	Name   string  `json:"name"`
	Amount float64 `json:"amount"`
}

// RoutineSummary holds routine completion counts.
type RoutineSummary struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}

// Check represents a local security posture check.
type Check struct {
	Label  string
	State  string
	Detail string
}

// AlienxContext holds the local context evaluated by ALIEN X directives.
type AlienxContext struct {
	Priorities   []TaskItem
	OpenTasks    int
	DoneToday    int
	Routine      RoutineSummary
	Habits       []HabitItem
	Money        MoneySummary
	Checks       []Check
	Posture      float64
	AlertsCount  int
	TotalRecords int
	DeviceName   string
	OSName       string
	RamGB        string
	IsRealDevice bool
}

// AlienxResponse represents the local response from ProcessAlienxDirective.
type AlienxResponse struct {
	Text     string
	Tone     string
	Action   string
	Agent    VoiceAgent
	Proposal *TaskItem
}

func mentions(q string, terms ...string) bool {
	for _, term := range terms {
		escaped := regexp.QuoteMeta(term)
		re := regexp.MustCompile(`(^|[^a-z0-9])` + escaped + `s?($|[^a-z0-9])`)
		if re.MatchString(q) {
			return true
		}
	}
	return false
}

// InitialAlienxDialogue returns the session greeting for ALIEN X.
func InitialAlienxDialogue(ctx AlienxContext) string {
	alertText := "No recent failed authentication or lockout events in the local log."
	if ctx.AlertsCount > 0 {
		alertText = fmt.Sprintf("%d failed authentication or lockout event(s) to review.", ctx.AlertsCount)
	}
	return fmt.Sprintf("Good morning. ALIEN X is ready.\n\n%d open task(s). %s\n\nAsk for your status, tasks, habits, finances, or local security checks. Responses use data on this device; this chat lasts for this session.", ctx.OpenTasks, alertText)
}

// ProcessAlienxDirective evaluates local ALIEN X directives deterministically.
func ProcessAlienxDirective(directive string, ctx AlienxContext) AlienxResponse {
	q := normalizeQuery(directive)
	if sessionAction := AlienxSessionAction(q); sessionAction != "" {
		if sessionAction == "lock" {
			return AlienxResponse{Text: "Locking your session.", Tone: "alert", Action: "lock"}
		}
		return AlienxResponse{Text: "Returning to your dashboard.", Tone: "nominal", Action: "ui"}
	}

	if mentions(q, "approval", "gate", "human in the loop") {
		return AlienxResponse{
			Text:   "Open the agent approval queue to review saved requests. This command does not approve or execute any request.",
			Tone:   "directive",
			Action: "agents-gates",
		}
	}
	if mentions(q, "ax-hacker", "hacker", "audit repo", "vulnerability", "vulnerabilities", "semgrep", "trivy", "gitleaks") {
		return AlienxResponse{
			Text:   "The AX-Hacker workspace contains local findings and approval requests. Opening it does not run a repository or network scan; scanner results require an actual connected execution service.",
			Tone:   "directive",
			Action: "agents-hacker",
		}
	}
	if mentions(q, "ax-ceo", "ceo", "executive", "runway", "briefing", "semiconductor") {
		return AlienxResponse{
			Text:   "Open the AX-CEO workspace for its stored briefings and requests. ALIEN X has no live email, calendar, market, or payroll connection. Sample briefing figures are not verified business data.",
			Tone:   "directive",
			Action: "agents-ceo",
		}
	}
	if mentions(q, "security", "threat", "posture", "sweep", "audit") {
		var issues []string
		for _, c := range ctx.Checks {
			if c.State != "pass" {
				issues = append(issues, fmt.Sprintf("• %s: %s", c.Label, c.Detail))
			}
		}
		issueBlock := fmt.Sprintf("%d local checks passing.", len(ctx.Checks))
		if len(issues) > 0 {
			issueBlock = strings.Join(issues, "\n")
		}
		tone := "nominal"
		if len(issues) > 0 || ctx.AlertsCount > 0 {
			tone = "alert"
		}
		return AlienxResponse{
			Text:   fmt.Sprintf("Local security checks: %d%%.\n\n%s\n\n%d recent failed authentication or lockout event(s). These checks do not detect intrusions or scan the device for malware.", int(math.Round(ctx.Posture*100)), issueBlock, ctx.AlertsCount),
			Tone:   tone,
			Action: "security",
		}
	}
	if mentions(q, "task", "todo", "priority", "priorities") {
		if ctx.OpenTasks == 0 {
			return AlienxResponse{Text: "No open tasks. You can add one in Tasks.", Tone: "nominal", Action: "tasks"}
		}
		var lines []string
		for i, t := range ctx.Priorities {
			suffix := ""
			if t.DueOn != "" {
				suffix = " · Today"
			}
			lines = append(lines, fmt.Sprintf("%d. %s%s", i+1, t.Title, suffix))
		}
		return AlienxResponse{
			Text:   fmt.Sprintf("%d open task(s). Due dates first:\n\n%s", ctx.OpenTasks, strings.Join(lines, "\n")),
			Tone:   "nominal",
			Action: "tasks",
		}
	}
	if mentions(q, "routine") {
		return AlienxResponse{
			Text:   fmt.Sprintf("Today’s routine: %d/%d steps completed.", ctx.Routine.Done, ctx.Routine.Total),
			Tone:   "nominal",
			Action: "routine",
		}
	}
	if mentions(q, "habit", "streak") {
		if len(ctx.Habits) == 0 {
			return AlienxResponse{Text: "No habits yet. Add one in Habits.", Tone: "nominal", Action: "habits"}
		}
		var lines []string
		for _, h := range ctx.Habits {
			state := "Not scheduled today"
			if h.Done {
				state = "Done"
			} else if h.DueToday {
				state = "Pending"
			}
			lines = append(lines, fmt.Sprintf("• %s: %s · %d completion streak", h.Name, state, h.Streak))
		}
		return AlienxResponse{
			Text:   "Today’s habits:\n\n" + strings.Join(lines, "\n"),
			Tone:   "nominal",
			Action: "habits",
		}
	}
	if mentions(q, "money", "finance", "financial", "cash", "cashflow", "budget", "expense", "income", "balance") {
		m := ctx.Money
		top := ""
		if m.TopCategory != nil {
			top = fmt.Sprintf("\n• Largest expense category: %s ($%.0f)", m.TopCategory.Name, m.TopCategory.Amount)
		}
		tone := "nominal"
		if m.Net < 0 {
			tone = "alert"
		}
		return AlienxResponse{
			Text:   fmt.Sprintf("This month through today:\n\n• Income: $%.0f\n• Expenses: $%.0f\n• Net cashflow: $%.0f%s\n\nInternal transfers are excluded.", m.Income, m.Expenses, m.Net, top),
			Tone:   tone,
			Action: "finance",
		}
	}
	if mentions(q, "device", "hardware", "spec", "system", "ram") {
		envDesc := "Simulator, emulator, or browser"
		if ctx.IsRealDevice {
			envDesc = "Physical device"
		}
		memDesc := "Unavailable"
		if ctx.RamGB != "" && ctx.RamGB != "Unknown" {
			memDesc = ctx.RamGB + " GB"
		}
		return AlienxResponse{
			Text: fmt.Sprintf("Device information:\n\n• Model: %s\n• OS: %s\n• Environment: %s\n• Reported memory: %s\n• Local records: %d", ctx.DeviceName, ctx.OSName, envDesc, memDesc, ctx.TotalRecords),
			Tone: "nominal",
		}
	}
	if mentions(q, "status", "report", "overview", "brief") || q == "1" {
		return AlienxResponse{
			Text: fmt.Sprintf("Your local status:\n\n• Tasks: %d open · %d completed today\n• Routine: %d/%d steps", ctx.OpenTasks, ctx.DoneToday, ctx.Routine.Done, ctx.Routine.Total),
			Tone: "nominal",
		}
	}
	if mentions(q, "help", "commands", "what can you do", "who are you", "hello", "hi", "hey", "alienx") {
		return AlienxResponse{
			Text: "I’m ALIEN X, your local command assistant.",
			Tone: "nominal",
		}
	}
	return AlienxResponse{
		Text: "I couldn’t match that command. Try “status”, “tasks”, “habits”, “routine”, “finances”, “security”, or “help”.",
		Tone: "nominal",
	}
}

// PersonalProfile holds user preferences for ALIEN X.
type PersonalProfile struct {
	ActiveAgent  VoiceAgent
	Language     string
	Name         string
	Focus        string
	Style        string
	Memories     []string
	ShareFinance bool
	CloudEnabled bool
	Endpoint     string
}

// BuildPersonalContext constructs the sanitized context map sent to the AI server.
func BuildPersonalContext(ctx AlienxContext, profile PersonalProfile) map[string]any {
	out := map[string]any{
		"profile": map[string]any{
			"activeAgent": profile.ActiveAgent,
			"language":    profile.Language,
			"name":        profile.Name,
			"focus":       profile.Focus,
			"style":       profile.Style,
			"memories":    profile.Memories,
		},
		"tasks": map[string]any{
			"open":           ctx.OpenTasks,
			"completedToday": ctx.DoneToday,
			"priorities":     ctx.Priorities,
		},
		"habits":  ctx.Habits,
		"routine": ctx.Routine,
	}
	if profile.ShareFinance {
		out["finance"] = ctx.Money
		out["currency"] = "USD"
	}
	return out
}

// RankTasks orders open tasks with overdue/due tasks first.
func RankTasks(tasks []TaskItem) []TaskItem {
	sorted := make([]TaskItem, len(tasks))
	copy(sorted, tasks)
	sort.SliceStable(sorted, func(i, j int) bool {
		di, dj := sorted[i].DueOn, sorted[j].DueOn
		if di == "" && dj == "" {
			return false
		}
		if di == "" {
			return false
		}
		if dj == "" {
			return true
		}
		return di < dj
	})
	return sorted
}

// Transaction represents a ledger entry for financial summary calculations.
type Transaction struct {
	Kind       string
	Amount     float64
	Category   string
	OccurredOn string
}

// SummarizeTransactions calculates monthly income, expenses, net, and top expense category up to today, excluding transfers and future entries.
func SummarizeTransactions(txs []Transaction, today string) MoneySummary {
	var income, expenses float64
	byCat := map[string]float64{}
	for _, tx := range txs {
		if tx.Kind == "transfer" {
			continue
		}
		if tx.OccurredOn != "" && tx.OccurredOn > today {
			continue
		}
		if tx.Kind == "income" {
			income += tx.Amount
		} else if tx.Kind == "expense" {
			expenses += tx.Amount
			byCat[tx.Category] += tx.Amount
		}
	}
	var top *CategoryTotal
	for cat, amt := range byCat {
		if top == nil || amt > top.Amount {
			top = &CategoryTotal{Name: cat, Amount: amt}
		}
	}
	return MoneySummary{
		Income:      income,
		Expenses:    expenses,
		Net:         income - expenses,
		TopCategory: top,
	}
}
