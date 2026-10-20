package core

import (
	"strings"
	"testing"
)

func TestValidateAlienxEndpoint(t *testing.T) {
	got, err := ValidateAlienxEndpoint("http://127.0.0.1:8787", false)
	if err != nil || got != "http://127.0.0.1:8787" {
		t.Fatalf("expected http://127.0.0.1:8787, got %q err=%v", got, err)
	}
	if _, err := ValidateAlienxEndpoint("http://remote.example", false); err == nil {
		t.Fatalf("expected error for non-HTTPS remote URL")
	}
	if _, err := ValidateAlienxEndpoint("https://user:secret@remote.example", false); err == nil {
		t.Fatalf("expected error for embedded credentials")
	}

	privateHosts := []string{"10.207.34.71", "172.16.0.2", "172.31.255.254", "192.168.1.10"}
	for _, host := range privateHosts {
		u := "http://" + host + ":8787"
		res, err := ValidateAlienxEndpoint(u, true)
		if err != nil || res != u {
			t.Fatalf("expected private LAN %s allowed in dev, got %q err=%v", u, res, err)
		}
		if _, err := ValidateAlienxEndpoint(u, false); err == nil {
			t.Fatalf("expected private LAN %s rejected when dev=false", u)
		}
	}

	publicHosts := []string{"remote.example", "8.8.8.8", "172.32.0.1", "192.169.1.1", "10.1.2.3.evil.example"}
	for _, host := range publicHosts {
		u := "http://" + host + ":8787"
		if _, err := ValidateAlienxEndpoint(u, true); err == nil {
			t.Fatalf("expected public host %s rejected over HTTP even in dev", u)
		}
	}
}

func TestNormalizeTamilCommandAndSessionActions(t *testing.T) {
	if got := NormalizeTamilCommand("செயலியை பூட்டு"); got != "lock app" {
		t.Fatalf("expected 'lock app', got %q", got)
	}
	if got := NormalizeTamilCommand("செயலியை பூட்டாதே"); got != "செயலியை பூட்டாதே" {
		t.Fatalf("expected negation preserved, got %q", got)
	}

	for _, cmd := range []string{"lock", "LOCKDOWN!", "please lock app", "lock my session now"} {
		if got := AlienxSessionAction(cmd); got != "lock" {
			t.Fatalf("expected lock for %q, got %q", cmd, got)
		}
	}
	for _, cmd := range []string{"unlock", "clock", "do not lock app", "don't lock", "how does lockdown work?", "emergency contacts", "purge", "lock status"} {
		if got := AlienxSessionAction(cmd); got != "" {
			t.Fatalf("expected no session action for %q, got %q", cmd, got)
		}
	}
	if got := AlienxSessionAction("exit to os"); got != "ui" {
		t.Fatalf("expected ui for 'exit to os', got %q", got)
	}
}

func TestResolveAgentDirective(t *testing.T) {
	r1 := ResolveAgentDirective("ALIEN X, talk to ax-ceo", AgentPersonal)
	if r1.Agent != AgentCEO || !r1.SwitchOnly {
		t.Fatalf("unexpected r1: %+v", r1)
	}
	r2 := ResolveAgentDirective(NormalizeTamilCommand("ஹேக்கரிடம் பேசு"), AgentPersonal)
	if r2.Agent != AgentHacker || !r2.SwitchOnly {
		t.Fatalf("unexpected r2: %+v", r2)
	}
	r3 := ResolveAgentDirective("ALIEN X, switch to ALIEN X", AgentHacker)
	if r3.Agent != AgentPersonal || !r3.SwitchOnly {
		t.Fatalf("unexpected r3: %+v", r3)
	}
}

func TestUpdateSpeechActivity(t *testing.T) {
	silent := SpeechActivity{HeardSpeech: false, LastSpeechAt: 0}
	m70 := -70.0
	m20 := -20.0

	if UpdateSpeechActivity(silent, 5000, &m70).Finished {
		t.Fatalf("expected silent turn not finished")
	}
	speaking := UpdateSpeechActivity(silent, 1000, &m20)
	if speaking.Finished || !speaking.HeardSpeech {
		t.Fatalf("expected speaking turn active")
	}
	if UpdateSpeechActivity(speaking, 2000, &m70).Finished {
		t.Fatalf("expected pause <1300ms not finished")
	}
	if !UpdateSpeechActivity(speaking, 2400, &m70).Finished {
		t.Fatalf("expected pause >1300ms after >1500ms duration to finish")
	}
	if UpdateSpeechActivity(silent, 5000, nil).Finished {
		t.Fatalf("expected no-metering silent turn not finished")
	}
}

func TestSplitSpeechChunks(t *testing.T) {
	chunks := SplitSpeechChunks("<think>reasoning block</think> வணக்கம் அருண், இன்று உங்களுக்கு இரண்டு முக்கிய பணிகள் உள்ளன. முதலில் ஆப் பரிசோதனையை முடிக்கவும்! அதன் பிறகு உடற்பயிற்சி செய்யலாம்.")
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d: %v", len(chunks), chunks)
	}
	joined := strings.Join(chunks, " ")
	if strings.Contains(joined, "reasoning block") {
		t.Fatalf("expected <think> stripped, got %q", joined)
	}
	if !strings.HasPrefix(chunks[0], "வணக்கம் அருண்") {
		t.Fatalf("unexpected first chunk: %q", chunks[0])
	}

	longOpening := "Good morning Arun, here is your focused schedule for the next two hours of deep engineering work, followed by your afternoon routine review and habit check-in."
	longChunks := SplitSpeechChunks(longOpening)
	if len(longChunks) != 2 {
		t.Fatalf("expected 2 chunks for long opening, got %d: %v", len(longChunks), longChunks)
	}
	expectedFirst := "Good morning Arun, here is your focused schedule for the next two hours of deep engineering work,"
	if longChunks[0] != expectedFirst {
		t.Fatalf("expected %q, got %q", expectedFirst, longChunks[0])
	}
}

func TestQuickCommandsAndDirectiveEvidence(t *testing.T) {
	ctx := AlienxContext{
		Priorities:   []TaskItem{{Title: "Ship mobile fixes", DueOn: "2026-10-16"}},
		OpenTasks:    1,
		DoneToday:    2,
		Routine:      RoutineSummary{Done: 2, Total: 3},
		Habits:       []HabitItem{{Name: "Run", Frequency: "weekdays", Done: false, DueToday: false, Streak: 8}},
		Money:        MoneySummary{Income: 100, Expenses: 40, Net: 60, TopCategory: &CategoryTotal{Name: "Food", Amount: 40}},
		Checks:       []Check{{Label: "Encryption at rest", State: "warn", Detail: "Database is not encrypted."}},
		Posture:      0.5,
		TotalRecords: 12,
		DeviceName:   "Test phone",
		OSName:       "Test OS",
		RamGB:        "Unknown",
		IsRealDevice: false,
	}

	expectedActions := []string{"", "tasks", "habits", "finance", "security", "agents-hacker", "agents-ceo", "agents-gates", "", ""}
	for i, protocol := range AlienxProtocols[2:] {
		got := ProcessAlienxDirective(protocol.Cmd, ctx).Action
		if got != expectedActions[i] {
			t.Fatalf("protocol %q: expected action %q, got %q", protocol.Cmd, expectedActions[i], got)
		}
	}
	if AlienxDestinations["agents-gates"].Href != "/agents?tab=approvals" {
		t.Fatalf("unexpected agents-gates href")
	}

	greeting := InitialAlienxDialogue(ctx)
	if !strings.HasPrefix(greeting, "Good morning.") {
		t.Fatalf("unexpected greeting: %q", greeting)
	}
	sec := ProcessAlienxDirective("security", ctx)
	if !strings.Contains(sec.Text, "Database is not encrypted") || sec.Tone != "alert" {
		t.Fatalf("unexpected security reply: %+v", sec)
	}
	hacker := ProcessAlienxDirective("run ax-hacker audit", ctx)
	if !strings.Contains(hacker.Text, "does not run") {
		t.Fatalf("unexpected hacker reply: %+v", hacker)
	}
	dev := ProcessAlienxDirective("device specs", ctx)
	if !strings.Contains(dev.Text, "memory: Unavailable") {
		t.Fatalf("unexpected device reply: %+v", dev)
	}
}

func TestBuildPersonalContextAndHomeSummaries(t *testing.T) {
	ctx := AlienxContext{
		OpenTasks: 1,
		Money:     MoneySummary{Income: 100, Expenses: 30, Net: 70},
	}
	profile := PersonalProfile{
		Language:     "ta-IN",
		Name:         "Arun",
		ShareFinance: false,
		Endpoint:     "https://secret.example",
	}
	safe := BuildPersonalContext(ctx, profile)
	if _, hasFinance := safe["finance"]; hasFinance {
		t.Fatalf("expected finance omitted when ShareFinance=false")
	}
	profMap := safe["profile"].(map[string]any)
	if _, hasEndpoint := profMap["endpoint"]; hasEndpoint {
		t.Fatalf("expected endpoint omitted from cloud profile context")
	}

	profile.ShareFinance = true
	withFin := BuildPersonalContext(ctx, profile)
	if fin, ok := withFin["finance"].(MoneySummary); !ok || fin.Net != 70 {
		t.Fatalf("expected finance net=70 when ShareFinance=true, got %+v", withFin["finance"])
	}

	today := "2026-10-20"
	var txs []Transaction
	for i := 0; i < 205; i++ {
		txs = append(txs, Transaction{Kind: "expense", Amount: 1, Category: "Food", OccurredOn: today})
	}
	txs = append(txs,
		Transaction{Kind: "income", Amount: 1000, Category: "Salary", OccurredOn: today},
		Transaction{Kind: "expense", Amount: 9000, Category: "Future", OccurredOn: "2026-10-21"},
		Transaction{Kind: "transfer", Amount: 300, Category: "Transfer", OccurredOn: today},
	)
	summary := SummarizeTransactions(txs, today)
	if summary.Income != 1000 || summary.Expenses != 205 || summary.Net != 795 || summary.TopCategory == nil || summary.TopCategory.Name != "Food" {
		t.Fatalf("unexpected transaction summary: %+v", summary)
	}

	ranked := RankTasks([]TaskItem{
		{ID: "undated", Title: "Undated", DueOn: ""},
		{ID: "urgent", Title: "Urgent", DueOn: "2026-10-20"},
		{ID: "overdue", Title: "Overdue", DueOn: "2026-10-19"},
	})
	if ranked[0].ID != "overdue" || ranked[1].ID != "urgent" || ranked[2].ID != "undated" {
		t.Fatalf("unexpected task ranking: %+v", ranked)
	}
}
