package domain

import "testing"

func TestParseIntakeAcceptsAnHTTPSRepository(t *testing.T) {
	t.Parallel()
	intake, err := ParseIntake("https://github.com/org/app", "main")
	if err != nil {
		t.Fatal(err)
	}
	if intake.RepoURL != "https://github.com/org/app" || intake.Ref != "main" {
		t.Fatalf("intake = %+v", intake)
	}
}

func TestParseIntakeRejectsCredentialsAndMissingFields(t *testing.T) {
	t.Parallel()
	cases := []struct {
		url, ref string
	}{
		{"", "main"},
		{"https://github.com/org/app", ""},
		{"file:///tmp/app", "main"},
		{"https://user:pass@github.com/org/app", "main"},
		{"https://github.com", "main"},
		{"https://github.com/org/app", "main branch"},
		{"https://github.com/org/app", "../HEAD"},
	}
	for _, test := range cases {
		if _, err := ParseIntake(test.url, test.ref); err == nil {
			t.Fatalf("accepted %q %q", test.url, test.ref)
		}
	}
}

func TestPendingRequiredIgnoresOptionalQuestions(t *testing.T) {
	t.Parallel()
	questions := []Question{
		{ID: "db", Label: "Needs a database?", Type: QuestionBoolean, Required: true},
		{ID: "note", Label: "Notes", Type: QuestionString, Required: false},
	}
	if !PendingRequired(questions, nil) {
		t.Fatal("expected the required question to be pending")
	}
	if PendingRequired(questions, []Answer{{ID: "db", Value: "no"}}) {
		t.Fatal("required question was answered")
	}
}
